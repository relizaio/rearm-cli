/*
The MIT License (MIT)

Copyright (c) 2020 - 2026 Reliza Incorporated (Reliza (tm), https://reliza.io)

Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the "Software"),
to deal in the Software without restriction, including without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense,
and/or sell copies of the Software, and to permit persons to whom the Software is furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY,
WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.

*/

package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"

	tea "github.com/oolong-project/tea-client-go"
	"github.com/oolong-project/tea-client-go/teaapi"
)

// The download verb of the TEA machine mode (task TPF-178, design D3): the artifacts of one
// collection version into a directory the CLI creates, each verified against its checksums
// by the library while streaming, with a manifest. File names are the row numbers, never a
// server string; a final name exists only for a verified file.

// teaParentDir is the parent directory of a --dir value.
func teaParentDir(dir string) string {
	return filepath.Dir(filepath.Clean(dir))
}

// teaFileName is the final name of row n; the part file adds ".part".
func teaFileName(n int) string {
	return fmt.Sprintf("%06d.bin", n)
}

// teaCountingWriter counts the bytes the library hands over, kept or not, so that
// --max-total-bytes bounds what a server can make a run receive.
type teaCountingWriter struct {
	w io.Writer
	n int64
}

func (c *teaCountingWriter) Write(p []byte) (int, error) {
	k, err := c.w.Write(p)
	c.n += int64(k)
	return k, err
}

// teaDownloadRow is one row of the file list: a format of an artifact.
type teaDownloadRow struct {
	file     teaManifestFile
	artifact teaapi.Artifact
	format   teaapi.ArtifactFormat
	selected bool
}

func (r *teaDownloadRow) notAttempted(reason teaReason) {
	r.file.Status = teaFileNotAttempted
	r.file.Reason = &reason
}

// teaSelector is the selection of item 11: artifact types (or all but some) and media types.
type teaSelector struct {
	include, exclude, media []string
}

func (s teaSelector) selects(a teaapi.Artifact, f teaapi.ArtifactFormat) bool {
	t := string(a.Type)
	switch {
	case len(s.include) > 0 && !slices.Contains(s.include, t):
		return false
	case len(s.exclude) > 0 && slices.Contains(s.exclude, t):
		return false
	}
	if len(s.media) == 0 {
		return true
	}
	key := teaMediaTypeKey(f.MediaType)
	for _, m := range s.media {
		if key != "" && teaMediaTypeKey(m) == key {
			return true
		}
	}
	return false
}

// download runs D3 steps 2 to 6; the flags (step 1) were validated before.
func (m *teaMachineRun) download() teaExit {
	api, err := m.api()
	if err != nil {
		return m.fail(err)
	}
	if err := m.ctx.Err(); err != nil {
		return m.fail(err)
	}
	// 2. The collection version itself: immutable, exactly the one asked for.
	var col *teaapi.Collection
	if m.target.kind == teaReleaseKindProduct {
		col, err = api.GetProductReleaseCollection(m.ctx, m.target.id, m.target.version)
	} else {
		col, err = api.GetComponentReleaseCollection(m.ctx, m.target.id, m.target.version)
	}
	if err != nil {
		return m.fail(err)
	}

	// 3. The file list, the selection, and the bounds checked before anything is fetched.
	sc := m.stringsChecker()
	doc := teaManifestDoc{FormatVersion: teaFormatVersion, Kind: teaDocManifest, Server: m.server(),
		Release: teaManifestRelease{Kind: m.target.kind, UUID: m.target.id.String()},
		Collection: teaManifestCollection{UUID: col.Uuid.String(), Version: col.Version, CreatedDate: teaInstant(col.CreatedDate),
			BelongsTo: sc.s("collection.belongsTo", string(col.BelongsTo)), UpdateReason: sc.updateReason("collection.updateReason", col.UpdateReason)},
		Files: []teaManifestFile{}, StoppedBy: teaDownloadEnd}
	sel := teaSelector{include: m.in.artifactTypes, exclude: m.in.excludeArtifactTypes, media: m.in.mediaTypes}
	var rows []teaDownloadRow
	selected := 0
	for _, a := range col.Artifacts {
		for _, f := range a.Formats {
			n := len(rows) + 1
			p := "files[" + strconv.Itoa(n-1) + "]"
			source, sourceURL := teaFormatSourceURL(api, a, f)
			row := teaDownloadRow{artifact: a, format: f, selected: sel.selects(a, f)}
			row.file = teaManifestFile{Number: n, ArtifactUUID: a.Uuid.String(), ArtifactVersion: a.Version,
				ArtifactName: sc.p(p+".artifactName", a.Name), ArtifactType: sc.s(p+".artifactType", string(a.Type)),
				ArtifactCreatedDate: teaInstant(a.CreatedDate), MediaType: sc.s(p+".mediaType", f.MediaType),
				Description: sc.p(p+".description", f.Description), Source: source, SourceURL: sc.s(p+".sourceUrl", sourceURL),
				SignatureURL: sc.p(p+".signatureUrl", f.SignatureUrl), Checksums: sc.checksums(p+".checksums", f.Checksums),
				Status: teaFileNotSelected, VerifiedAlgorithms: []string{}}
			if row.selected {
				selected++
			}
			rows = append(rows, row)
		}
	}
	if sc.err != nil {
		return m.fail(sc.err)
	}
	if selected > m.in.maxFiles {
		return m.fail(&teaLimitError{reason: teaReasonFileCount,
			msg: fmt.Sprintf("%d files are selected, more than --max-files (%d)", selected, m.in.maxFiles)})
	}
	if err := m.sizeManifest(doc, rows); err != nil {
		return m.fail(err)
	}

	// 4. The directory: created here, never trusted when it exists.
	if err := os.Mkdir(m.in.dir, 0o700); err != nil {
		if errors.Is(err, os.ErrExist) {
			return m.fail(teaUsage("--dir must not exist: the command creates it"))
		}
		return m.fail(err)
	}

	// 5. One selected row at a time.
	var received int64
	stop := teaDownloadEnd
	var stopReason teaReason
	for i := range rows {
		r := &rows[i]
		if !r.selected {
			continue
		}
		if stop != teaDownloadEnd {
			r.notAttempted(stopReason)
			continue
		}
		if err := m.ctx.Err(); err != nil {
			if teaMachineReason(m.ctx, err, teaFileBoundNone) != teaReasonDeadline {
				return m.fail(err)
			}
			stop, stopReason = teaDownloadDeadline, teaReasonDeadline
			r.notAttempted(stopReason)
			continue
		}
		left := m.in.maxTotalBytes - received
		if left <= 0 {
			// Nothing left of the total: no request. The library would read a bound of 0 as
			// its 512 MiB default.
			stop, stopReason = teaDownloadTotalBytes, teaReasonTotalBytes
			r.notAttempted(stopReason)
			continue
		}
		bound, kind := m.in.maxArtifactBytes, teaFileBoundArtifact
		if left < m.in.maxArtifactBytes {
			bound, kind = left, teaFileBoundTotal
		}
		res, n, err := m.fetchFile(api, r, bound)
		received += n
		if err == nil {
			name, size := teaFileName(r.file.Number), res.Bytes
			r.file.Status, r.file.FileName, r.file.SizeBytes = teaFileVerified, &name, &size
			r.file.ResponseMediaType = teaResponseMediaType(res.MediaType)
			for _, a := range res.Verified {
				r.file.VerifiedAlgorithms = append(r.file.VerifiedAlgorithms, string(a))
			}
			continue
		}
		reason := teaMachineReason(m.ctx, err, kind)
		switch reason {
		case teaReasonLocalIO, teaReasonInterrupted:
			return m.fail(err)
		case teaReasonDeadline:
			stop, stopReason = teaDownloadDeadline, reason
		case teaReasonTotalBytes:
			stop, stopReason = teaDownloadTotalBytes, reason
		}
		m.failRow(r, err, reason)
	}

	// 6. The manifest.
	code := teaExitOK
	for _, r := range rows {
		f := r.file
		switch f.Status {
		case teaFileVerified:
			doc.Summary.Verified++
		case teaFileFailed:
			doc.Summary.Failed++
		case teaFileNotAttempted:
			doc.Summary.NotAttempted++
		case teaFileNotSelected:
			doc.Summary.NotSelected++
		}
		if r.selected && f.Status != teaFileVerified {
			code = teaExitPartial
		}
		doc.Files = append(doc.Files, f)
	}
	doc.Summary.BytesReceived = received
	doc.StoppedBy = stop
	return m.emit(doc, code)
}

// failRow marks a row FAILED with the reason of err.
func (m *teaMachineRun) failRow(r *teaDownloadRow, err error, reason teaReason) {
	u := teaErrorURL(err, reason, m.run.lastRequest)
	msg := teaRowMessage(teaErrorText(err, reason, u))
	r.file.Status, r.file.Reason, r.file.Message = teaFileFailed, &reason, &msg
	var refused *teaAddressRefusedError
	if reason == teaReasonAddressRefused && errors.As(err, &refused) {
		addr, class := refused.Address.String(), string(refused.Class)
		r.file.Address, r.file.AddressClass = &addr, &class
	}
}

// fetchFile downloads one row into <number>.bin.part (exclusive create, mode 0600) and,
// verified, renames it to <number>.bin; on any failure the part file is removed. It
// returns the bytes the library handed over, kept or not.
func (m *teaMachineRun) fetchFile(api *tea.API, r *teaDownloadRow, bound int64) (tea.DownloadResult, int64, error) {
	final := filepath.Join(m.in.dir, teaFileName(r.file.Number))
	part := final + ".part"
	f, err := os.OpenFile(part, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return tea.DownloadResult{}, 0, err
	}
	cw := &teaCountingWriter{w: f}
	ref := tea.ArtifactRef{UUID: r.artifact.Uuid, Version: r.artifact.Version}
	res, err := api.DownloadArtifact(m.ctx, ref, r.format, cw, tea.DownloadOptions{MaxBytes: bound})
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(part, final)
	}
	if err != nil {
		if rerr := os.Remove(part); rerr != nil && !errors.Is(rerr, os.ErrNotExist) {
			return tea.DownloadResult{}, cw.n, rerr
		}
		return tea.DownloadResult{}, cw.n, err
	}
	return res, cw.n, nil
}

// sizeManifest refuses a run whose manifest could not fit --max-output-bytes, before
// anything is fetched: every selected row with the longest status fields it can get.
func (m *teaMachineRun) sizeManifest(doc teaManifestDoc, rows []teaDownloadRow) error {
	worst := doc
	worst.Files = make([]teaManifestFile, 0, len(rows))
	for _, r := range rows {
		if r.selected {
			worst.Files = append(worst.Files, teaWorstRow(r.file))
		} else {
			worst.Files = append(worst.Files, r.file)
		}
	}
	const most = 1<<31 - 1
	worst.Summary = teaManifestSummary{Verified: most, Failed: most, NotAttempted: most, NotSelected: most, BytesReceived: 1<<63 - 1}
	worst.StoppedBy = teaDownloadTotalBytes
	b, err := teaEncode(worst)
	if err != nil {
		return err
	}
	if int64(len(b)+1) > m.in.maxOutputBytes {
		return &teaLimitError{reason: teaReasonOutputBytes,
			msg: fmt.Sprintf("the manifest of %d files could exceed --max-output-bytes (%d bytes)", len(rows), m.in.maxOutputBytes)}
	}
	return nil
}
