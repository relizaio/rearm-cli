package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// Mutation round for the download verb: the string bound at every field of a manifest row,
// the selection and its bounds, the part file and what becomes of it, the summary, and the
// size the manifest is refused at before anything is fetched.

const (
	teaArt4 = "66666666-6666-4666-8666-666666666664"
	teaArt5 = "66666666-6666-4666-8666-666666666665"
)

// collection routes the collection a download reads, with the given artifacts.
func (d *teaDL) collection(belongsTo, updateReason string) {
	d.f.route(d.f.api("/componentRelease/"+teaCR1+"/collection/2"), teaJSON(`{"uuid":"`+teaCR1+`","version":2,"createdDate":"2026-01-16T10:00:00Z",`+
		`"belongsTo":"`+belongsTo+`","updateReason":`+updateReason+`,"artifacts":[`+strings.Join(d.arts, ",")+`]}`))
}

// exec runs the download without routing the collection (the test did).
func (d *teaDL) exec(args ...string) teaExit {
	base := []string{"--api-root", d.f.root, "--component-release", teaCR1, "--collection-version", "2", "--dir", d.dir}
	return d.r.exec(teaVerbDownload, append(base, args...)...)
}

const teaShortFormat = `{"mediaType":"text/plain","checksums":[{"algType":"SHA-256","algValue":"aa"}]}`

func TestTeaMachineDownloadStringBoundNamesTheField(t *testing.T) {
	long := teaA(257)
	url := func(s string) string { return "https://cdn.example.com/" + s }
	for _, c := range []struct {
		name, want string
		third      func(d *teaDL)
	}{
		{"artifact name", "files[2].artifactName", func(d *teaDL) { d.art(teaArtComp1, 1, "BOM", `"`+long+`"`, teaShortFormat) }},
		{"artifact type", "files[2].artifactType", func(d *teaDL) { d.art(teaArtComp1, 1, long, "", teaShortFormat) }},
		{"media type", "files[2].mediaType", func(d *teaDL) {
			d.art(teaArtComp1, 1, "BOM", "", `{"mediaType":"text/`+long+`","checksums":[]}`)
		}},
		{"description", "files[2].description", func(d *teaDL) {
			d.art(teaArtComp1, 1, "BOM", "", `{"mediaType":"text/plain","description":"`+long+`","checksums":[]}`)
		}},
		{"source url", "files[2].sourceUrl", func(d *teaDL) {
			d.art(teaArtComp1, 1, "BOM", "", `{"mediaType":"text/plain","url":"`+url(long)+`","checksums":[]}`)
		}},
		{"signature url", "files[2].signatureUrl", func(d *teaDL) {
			d.art(teaArtComp1, 1, "BOM", "", `{"mediaType":"text/plain","signatureUrl":"`+url(long)+`","checksums":[]}`)
		}},
		{"checksum type", "files[2].checksums[1].algType", func(d *teaDL) {
			d.art(teaArtComp1, 1, "BOM", "", `{"mediaType":"text/plain","checksums":[{"algType":"SHA-1","algValue":"aa"},{"algType":"`+long+`","algValue":"bb"}]}`)
		}},
		{"checksum value", "files[2].checksums[1].algValue", func(d *teaDL) {
			d.art(teaArtComp1, 1, "BOM", "", `{"mediaType":"text/plain","checksums":[{"algType":"SHA-1","algValue":"aa"},{"algType":"SHA-256","algValue":"`+long+`"}]}`)
		}},
	} {
		d := newTeaDL(t)
		d.art(teaArtProd, 1, "BOM", "", teaShortFormat, `{"mediaType":"text/xml","checksums":[]}`)
		c.third(d)
		d.collection("COMPONENT_RELEASE", `{"type":"ARTIFACT_ADDED"}`)
		d.r.wantStringBound(d.exec("--max-string-bytes", "256"), c.want, 256)
		if _, err := os.Lstat(d.dir); !os.IsNotExist(err) {
			t.Errorf("%s: the directory was created", c.name)
		}
	}
	for _, c := range []struct{ name, belongsTo, reason, want string }{
		{"belongs to", long, `{"type":"ARTIFACT_ADDED"}`, "collection.belongsTo"},
		{"update reason type", "COMPONENT_RELEASE", `{"type":"` + long + `"}`, "collection.updateReason.type"},
		{"update reason comment", "COMPONENT_RELEASE", `{"type":"ARTIFACT_ADDED","comment":"` + long + `"}`, "collection.updateReason.comment"},
	} {
		d := newTeaDL(t)
		d.art(teaArtProd, 1, "BOM", "", teaShortFormat)
		d.collection(c.belongsTo, c.reason)
		d.r.wantStringBound(d.exec("--max-string-bytes", "256"), c.want, 256)
	}
	// a string error is reported before the number of files is looked at
	d := newTeaDL(t)
	d.art(teaArtProd, 1, "BOM", `"`+long+`"`, teaShortFormat)
	d.art(teaArtComp1, 1, "BOM", "", teaShortFormat)
	d.collection("COMPONENT_RELEASE", `{"type":"ARTIFACT_ADDED"}`)
	d.r.wantStringBound(d.exec("--max-string-bytes", "256", "--max-files", "1"), "files[0].artifactName", 256)
}

func TestTeaMachineDownloadEmptyCollection(t *testing.T) {
	d := newTeaDL(t)
	d.collection("COMPONENT_RELEASE", `{"type":"INITIAL_RELEASE"}`)
	d.r.want(d.exec(), teaExitOK)
	doc := d.r.doc()
	if l, ok := doc["files"].([]any); !ok || len(l) != 0 || doc["stoppedBy"] != "END" {
		t.Errorf("files %v stoppedBy %v", doc["files"], doc["stoppedBy"])
	}
	if teaPath(doc, "summary.verified") != 0.0 || teaPath(doc, "summary.bytesReceived") != 0.0 {
		t.Errorf("summary %v", doc["summary"])
	}
	if got := strings.Join(d.files(), ","); got != "" {
		t.Errorf("directory %q", got)
	}
}

func TestTeaMachineDownloadFileCountEdges(t *testing.T) {
	three := func() *teaDL {
		d := newTeaDL(t)
		d.art(teaArtProd, 1, "BOM", "", d.serverBody(teaArtProd, 1, "a/one", "one"), d.serverBody(teaArtProd, 1, "a/two", "two"))
		d.art(teaArtComp1, 1, "OTHER", "", d.serverBody(teaArtComp1, 1, "a/three", "three"))
		return d
	}
	// as many files as the bound is allowed; unselected rows do not count
	d := three()
	d.r.want(d.run("--max-files", "3"), teaExitOK)
	d = three()
	d.r.want(d.run("--max-files", "2", "--artifact-type", "BOM"), teaExitOK)
	d = three()
	d.r.want(d.run("--max-files", "1", "--artifact-type", "OTHER"), teaExitOK)
	if got := teaStatuses(d.r.doc()); got != "NNV" {
		t.Errorf("statuses %s", got)
	}
	// one more than the bound
	d = three()
	e := d.r.wantFail(d.run("--max-files", "2"), teaExitLimit, teaReasonFileCount)
	if e["message"] != "3 files are selected, more than --max-files (2)" {
		t.Errorf("message %v", e["message"])
	}
	if _, err := os.Lstat(d.dir); !os.IsNotExist(err) || len(d.f.requests()) != 1 {
		t.Errorf("the directory exists or %d requests were made", len(d.f.requests()))
	}
	d = three()
	e = d.r.wantFail(d.run("--max-files", "1", "--artifact-type", "BOM"), teaExitLimit, teaReasonFileCount)
	if e["message"] != "2 files are selected, more than --max-files (1)" {
		t.Errorf("message %v", e["message"])
	}
}

func TestTeaMachineDownloadMediaTypeSelection(t *testing.T) {
	d := newTeaDL(t)
	d.art(teaArtProd, 1, "BOM", "",
		d.serverBody(teaArtProd, 1, "application/vnd.cyclonedx+json", "json"),
		d.serverBody(teaArtProd, 1, "application/vnd.cyclonedx+xml", "xml"),
		d.serverBody(teaArtProd, 1, "TEXT/Plain; charset=utf-8", "text"))
	d.r.want(d.run("--media-type", "Application/VND.CycloneDX+JSON; charset=utf-8", "--media-type", " text/plain "), teaExitOK)
	if got := teaStatuses(d.r.doc()); got != "VNV" {
		t.Errorf("statuses %s", got)
	}
	// a media type that matches nothing selects nothing
	d = newTeaDL(t)
	d.art(teaArtProd, 1, "BOM", "", d.serverBody(teaArtProd, 1, "application/json", "json"))
	d.r.want(d.run("--media-type", "application/xml"), teaExitOK)
	if got := teaStatuses(d.r.doc()); got != "N" {
		t.Errorf("statuses %s", got)
	}
}

func TestTeaMachineDownloadOfAProductRelease(t *testing.T) {
	d := newTeaDL(t)
	d.art(teaArtProd, 1, "BOM", "", d.serverBody(teaArtProd, 1, "a/one", "one"))
	d.f.route(d.f.api("/productRelease/"+teaPR1+"/collection/3"), teaJSON(`{"uuid":"`+teaPR1+`","version":3,"createdDate":"2026-01-16T10:00:00Z",`+
		`"belongsTo":"PRODUCT_RELEASE","updateReason":{"type":"ARTIFACT_ADDED"},"artifacts":[`+strings.Join(d.arts, ",")+`]}`))
	d.r.want(d.r.exec(teaVerbDownload, "--api-root", d.f.root, "--product-release", teaPR1, "--collection-version", "3", "--dir", d.dir), teaExitOK)
	doc := d.r.doc()
	if teaPath(doc, "release.kind") != "PRODUCT_RELEASE" || teaPath(doc, "release.uuid") != teaPR1 || teaPath(doc, "collection.version") != 3.0 ||
		teaPath(doc, "collection.belongsTo") != "PRODUCT_RELEASE" || teaStatuses(doc) != "V" {
		t.Errorf("manifest %s", d.r.out.String())
	}
	if d.f.count(d.f.api("/productRelease/"+teaPR1+"/collection/3")) != 1 {
		t.Error("the product release's collection was not read at /collection/3")
	}
}

func TestTeaMachineDownloadDirectoryNeedsNoCleanPath(t *testing.T) {
	d := newTeaDL(t)
	d.art(teaArtProd, 1, "BOM", "", d.serverBody(teaArtProd, 1, "a/one", "one"))
	d.dir += "/"
	d.r.want(d.run(), teaExitOK)
	if got := strings.Join(d.files(), ","); got != "000001.bin" {
		t.Errorf("directory %s", got)
	}
}

// TestTeaMachineDownloadDirectoryCreatedMeanwhile: the directory appears between the check
// of the flags and its creation (a race a caller could lose); it is not used.
func TestTeaMachineDownloadDirectoryCreatedMeanwhile(t *testing.T) {
	d := newTeaDL(t)
	d.art(teaArtProd, 1, "BOM", "", d.serverBody(teaArtProd, 1, "a/one", "one"))
	d.f.routeFunc(d.f.api("/componentRelease/"+teaCR1+"/collection/2"), func(q *http.Request) teaAnswer {
		_ = os.Mkdir(d.dir, 0o755)
		return teaJSON(`{"uuid":"` + teaCR1 + `","version":2,"createdDate":"2026-01-16T10:00:00Z","belongsTo":"COMPONENT_RELEASE","updateReason":{"type":"ARTIFACT_ADDED"},"artifacts":[` +
			strings.Join(d.arts, ",") + `]}`)
	})
	e := d.r.wantFail(d.exec(), teaExitUsage, teaReasonUsage)
	if e["message"] != "--dir must not exist: the command creates it" {
		t.Errorf("message %v", e["message"])
	}
	if got := strings.Join(d.files(), ","); got != "" {
		t.Errorf("files were written into the directory that was not ours: %s", got)
	}
	if d.count(d.downloadPath(teaArtProd, 1)) != 0 {
		t.Error("an artifact was requested")
	}
}

func TestTeaMachineDownloadPartFile(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	d := newTeaDL(t)
	d.art(teaArtProd, 1, "BOM", "", d.server(teaArtProd, 1, "a/one", func(w http.ResponseWriter, q *http.Request) {
		es, _ := os.ReadDir(d.dir)
		var names []string
		for _, e := range es {
			names = append(names, e.Name())
		}
		mu.Lock()
		seen = names
		mu.Unlock()
		teaBody("one")(w, q)
	}, teaSums("SHA-256", teaSumSHA256("one"))))
	d.r.want(d.run(), teaExitOK)
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(seen, ",") != "000001.bin.part" {
		t.Errorf("while the file was received the directory held %v, want the part file alone", seen)
	}
	if got := strings.Join(d.files(), ","); got != "000001.bin" {
		t.Errorf("afterwards %s", got)
	}
}

func TestTeaMachinePartFileIsNeverReused(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	d := newTeaDL(t)
	d.art(teaArtProd, 1, "BOM", "",
		d.server(teaArtProd, 1, "a/one", func(w http.ResponseWriter, q *http.Request) {
			// something puts a link where the next file is to be received
			_ = os.Symlink(outside, filepath.Join(d.dir, "000002.bin.part"))
			teaBody("one")(w, q)
		}, teaSums("SHA-256", teaSumSHA256("one"))),
		d.serverBody(teaArtProd, 1, "a/two", "two"))
	e := d.r.wantFail(d.run(), teaExitFailed, teaReasonLocalIO)
	if m, _ := e["message"].(string); !strings.Contains(m, "000002.bin.part") {
		t.Errorf("message %q", m)
	}
	if b, _ := os.ReadFile(outside); string(b) != "keep" {
		t.Errorf("the file behind the link was written: %q", b)
	}
	if d.count(d.downloadPath(teaArtProd, 1)) != 1 {
		t.Errorf("the second file was requested")
	}
}

func TestTeaMachineMissingPartFileOfAFailedFileIsNoError(t *testing.T) {
	d := newTeaDL(t)
	d.art(teaArtProd, 1, "BOM", "",
		d.server(teaArtProd, 1, "a/one", func(w http.ResponseWriter, q *http.Request) {
			_ = os.Remove(filepath.Join(d.dir, "000001.bin.part"))
			teaBody("tampered")(w, q)
		}, teaSums("SHA-256", teaSumSHA256("original"))))
	d.r.want(d.run(), teaExitPartial)
	d.wantRow(d.r.doc(), 0, "FAILED", "CHECKSUM_MISMATCH")
}

func TestTeaMachinePartFileThatCannotBeRemovedFailsTheRun(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permissions do not apply to root")
	}
	d := newTeaDL(t)
	d.art(teaArtProd, 1, "BOM", "",
		d.server(teaArtProd, 1, "a/one", func(w http.ResponseWriter, q *http.Request) {
			_ = os.Chmod(d.dir, 0o500)
			teaBody("tampered")(w, q)
		}, teaSums("SHA-256", teaSumSHA256("original"))))
	t.Cleanup(func() { _ = os.Chmod(d.dir, 0o700) })
	e := d.r.wantFail(d.run(), teaExitFailed, teaReasonLocalIO)
	if m, _ := e["message"].(string); !strings.Contains(m, "remove") || !strings.Contains(m, "000001.bin.part") {
		t.Errorf("message %q", m)
	}
}

func TestTeaMachineDownloadInterrupted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := newTeaDL(t)
	d.art(teaArtProd, 1, "BOM", "", d.server(teaArtProd, 1, "a/one", func(w http.ResponseWriter, q *http.Request) {
		cancel()
		<-q.Context().Done()
	}, teaSums("SHA-256", teaSumSHA256("x"))))
	d.collection("COMPONENT_RELEASE", `{"type":"ARTIFACT_ADDED"}`)
	d.r.out.Reset()
	got := d.r.execCtx(ctx, teaVerbDownload, "--api-root", d.f.root, "--component-release", teaCR1, "--collection-version", "2", "--dir", d.dir)
	// an interrupt ends the run: there is no manifest to read
	d.r.wantFail(got, teaExitFailed, teaReasonInterrupted)
}

func TestTeaMachineDownloadDeadlineDuringTheLastFile(t *testing.T) {
	d := newTeaDL(t)
	d.art(teaArtProd, 1, "BOM", "", d.server(teaArtProd, 1, "a/one", teaStall, teaSums("SHA-256", teaSumSHA256("x"))))
	d.r.want(d.run("--deadline", "1s"), teaExitPartial)
	doc := d.r.doc()
	d.wantRow(doc, 0, "FAILED", "DEADLINE")
	if doc["stoppedBy"] != "DEADLINE" || teaPath(doc, "summary.failed") != 1.0 {
		t.Errorf("stoppedBy %v summary %v", doc["stoppedBy"], doc["summary"])
	}
	if got := strings.Join(d.files(), ","); got != "" {
		t.Errorf("directory %s", got)
	}
}

func TestTeaMachineDownloadTotalBoundEdges(t *testing.T) {
	// the last byte of the total is usable
	d := newTeaDL(t)
	d.art(teaArtProd, 1, "BOM", "", d.serverBody(teaArtProd, 1, "a/one", "abcd"), d.serverBody(teaArtProd, 1, "a/two", "x"))
	d.r.want(d.run("--max-total-bytes", "5"), teaExitOK)
	doc := d.r.doc()
	if teaStatuses(doc) != "VV" || teaPath(doc, "summary.bytesReceived") != 5.0 || doc["stoppedBy"] != "END" {
		t.Errorf("manifest %s", d.r.out.String())
	}
	// and when none is left the next file is not asked for
	d = newTeaDL(t)
	d.art(teaArtProd, 1, "BOM", "", d.serverBody(teaArtProd, 1, "a/one", "abcd"), d.serverBody(teaArtProd, 1, "a/two", "x"))
	d.r.want(d.run("--max-total-bytes", "4"), teaExitPartial)
	doc = d.r.doc()
	d.wantRow(doc, 1, "NOT_ATTEMPTED", "TOTAL_BYTES")
	if doc["stoppedBy"] != "TOTAL_BYTES" {
		t.Errorf("stoppedBy %v", doc["stoppedBy"])
	}
	for _, q := range d.f.requests() {
		if mt, _ := urlQueryValue(q.rawQuery, "mediaType"); mt == "a/two" {
			t.Error("the second file was requested")
		}
	}
	// a file larger than both bounds, which are equal, is named for the file
	d = newTeaDL(t)
	d.art(teaArtProd, 1, "BOM", "", d.serverBody(teaArtProd, 1, "a/one", strings.Repeat("z", 20)))
	d.r.want(d.run("--max-total-bytes", "10", "--max-artifact-bytes", "10"), teaExitPartial)
	d.wantRow(d.r.doc(), 0, "FAILED", "ARTIFACT_BYTES")
	// a file larger than what is left of the total is named for the total
	d = newTeaDL(t)
	d.art(teaArtProd, 1, "BOM", "", d.serverBody(teaArtProd, 1, "a/one", strings.Repeat("z", 20)))
	d.r.want(d.run("--max-total-bytes", "10", "--max-artifact-bytes", "11"), teaExitPartial)
	d.wantRow(d.r.doc(), 0, "FAILED", "TOTAL_BYTES")
}

func TestTeaMachineDownloadSummaryCountsEachStatus(t *testing.T) {
	d := newTeaDL(t)
	d.art(teaArtProd, 1, "BOM", "", d.serverBody(teaArtProd, 1, "a/one", "ab"),
		d.server(teaArtProd, 1, "a/two", teaChunked(strings.Repeat("c", 10)), teaSums("SHA-256", teaSumSHA256(strings.Repeat("c", 10)))), d.serverBody(teaArtProd, 1, "a/three", "ef"))
	d.art(teaArtComp1, 1, "OTHER", "", d.serverBody(teaArtComp1, 1, "a/four", "gh"))
	d.r.want(d.run("--artifact-type", "BOM", "--max-total-bytes", "6"), teaExitPartial)
	doc := d.r.doc()
	d.wantRow(doc, 0, "VERIFIED", nil)
	d.wantRow(doc, 1, "FAILED", "TOTAL_BYTES")
	d.wantRow(doc, 2, "NOT_ATTEMPTED", "TOTAL_BYTES")
	d.wantRow(doc, 3, "NOT_SELECTED", nil)
	got, _ := json.Marshal(doc["summary"])
	received := teaPath(doc, "summary.bytesReceived").(float64)
	if received < 6 || received > 7 {
		t.Errorf("bytes received %v", received)
	}
	wantPrefix := `{"bytesReceived":` + strconv.Itoa(int(received)) + `,"failed":1,"notAttempted":1,"notSelected":1,"verified":1}`
	if string(got) != wantPrefix {
		t.Errorf("summary %s, want %s", got, wantPrefix)
	}
	// every row carries the list of verified algorithms, empty when nothing was verified
	for i, f := range doc["files"].([]any) {
		l, ok := f.(map[string]any)["verifiedAlgorithms"].([]any)
		if !ok || (i == 0) != (len(l) == 1) {
			t.Errorf("row %d: verifiedAlgorithms %v", i+1, f.(map[string]any)["verifiedAlgorithms"])
		}
	}
}

func TestTeaMachineDownloadManifestIsRefusedBeforeAnythingIsFetched(t *testing.T) {
	d := newTeaDL(t)
	for i := 0; i < 12; i++ {
		id := fmt.Sprintf("66666666-6666-4666-8666-6666666666%02d", i+10)
		d.art(id, 1, "BOM", "", d.serverBody(id, 1, "a/f", "x"))
	}
	e := d.r.wantFail(d.run("--max-output-bytes", "65536"), teaExitLimit, teaReasonOutputBytes)
	if e["message"] != "the manifest of 12 files could exceed --max-output-bytes (65536 bytes)" {
		t.Errorf("message %v", e["message"])
	}
	if _, err := os.Lstat(d.dir); !os.IsNotExist(err) {
		t.Error("the directory was created")
	}
	if len(d.f.requests()) != 1 {
		t.Errorf("%d requests, want only the collection", len(d.f.requests()))
	}
	// the same list is fetched when most of it is not selected
	d2 := newTeaDL(t)
	for i := 0; i < 12; i++ {
		id := fmt.Sprintf("66666666-6666-4666-8666-6666666666%02d", i+10)
		typ := "OTHER"
		if i == 0 {
			typ = "BOM"
		}
		d2.art(id, 1, typ, "", d2.serverBody(id, 1, "a/f", "x"))
	}
	d2.r.want(d2.run("--max-output-bytes", "65536", "--artifact-type", "BOM"), teaExitOK)
}

// The largest manifest a run can write, built by hand from the specification of the
// document: every selected row at its longest, the counters at their largest.
func TestTeaMachineManifestSizeIsTheWorstCase(t *testing.T) {
	const rootURL = "https://tea.example.com/tea"
	selected := teaManifestFile{Number: 1, ArtifactUUID: teaArtProd, ArtifactVersion: 1, ArtifactType: "BOM", ArtifactCreatedDate: "2026-01-10T08:05:00Z",
		MediaType: "a/b", Source: teaFileSourceServer, SourceURL: "https://tea.example.com/x", Checksums: []teaChecksumJSON{}, Status: teaFileNotSelected,
		VerifiedAlgorithms: []string{}}
	other := selected
	other.Number = 2
	other.ArtifactVersion = 2
	doc := teaManifestDoc{FormatVersion: 1, Kind: teaDocManifest, Server: teaServerJSON{RootURL: rootURL, APIVersion: "1.0.0"},
		Release: teaManifestRelease{Kind: teaReleaseKindComponent, UUID: teaCR1},
		Collection: teaManifestCollection{UUID: teaCR1, Version: 2, CreatedDate: "2026-01-16T10:00:00Z", BelongsTo: "COMPONENT_RELEASE",
			UpdateReason: teaUpdateReasonJSON{Type: "ARTIFACT_ADDED"}},
		Files: []teaManifestFile{}, StoppedBy: teaDownloadEnd}
	rows := []teaDownloadRow{{file: selected, selected: true}, {file: other, selected: false}}

	row := func(n, version int, worst bool) string {
		head := `{"number":` + strconv.Itoa(n) + `,"artifactUuid":"` + teaArtProd + `","artifactVersion":` + strconv.Itoa(version) +
			`,"artifactName":null,"artifactType":"BOM","artifactCreatedDate":"2026-01-10T08:05:00Z","mediaType":"a/b","description":null,` +
			`"source":"SERVER","sourceUrl":"https://tea.example.com/x","signatureUrl":null,"checksums":[],`
		if !worst {
			return head + `"status":"NOT_SELECTED","fileName":null,"sizeBytes":null,"responseMediaType":null,"verifiedAlgorithms":[],"reason":null,"message":null,"address":null,"addressClass":null}`
		}
		return head + `"status":"NOT_ATTEMPTED","fileName":"000000.bin","sizeBytes":9223372036854775807,"responseMediaType":"` + strings.Repeat(`\u0026`, 255) +
			`","verifiedAlgorithms":["SHA-1","SHA-256","SHA-384","SHA-512","SHA3-256","SHA3-384","SHA3-512","BLAKE2b-256","BLAKE2b-384","BLAKE2b-512"],` +
			`"reason":"NO_ACCEPTABLE_FORMAT","message":"` + strings.Repeat(`\u003c`, 1024) + `","address":"ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff","addressClass":"CLOUD_METADATA"}`
	}
	want := `{"formatVersion":1,"kind":"TEA_DOWNLOAD_MANIFEST","server":{"rootUrl":"` + rootURL + `","apiVersion":"1.0.0"},` +
		`"release":{"kind":"COMPONENT_RELEASE","uuid":"` + teaCR1 + `"},` +
		`"collection":{"uuid":"` + teaCR1 + `","version":2,"createdDate":"2026-01-16T10:00:00Z","belongsTo":"COMPONENT_RELEASE","updateReason":{"type":"ARTIFACT_ADDED","comment":null}},` +
		`"files":[` + row(1, 1, true) + `,` + row(2, 2, false) + `],` +
		`"summary":{"verified":2147483647,"failed":2147483647,"notAttempted":2147483647,"notSelected":2147483647,"bytesReceived":9223372036854775807},` +
		`"stoppedBy":"TOTAL_BYTES"}`
	size := int64(len(want) + 1)
	for _, c := range []struct {
		max int64
		ok  bool
	}{{size - 1, false}, {size, true}, {size + 1, true}} {
		m := &teaMachineRun{in: teaMachineInputs{maxOutputBytes: c.max}}
		err := m.sizeManifest(doc, rows)
		if (err == nil) != c.ok {
			t.Errorf("a bound of %d (the worst case is %d bytes): %v", c.max, size, err)
		}
	}
	m := &teaMachineRun{in: teaMachineInputs{maxOutputBytes: size - 1}}
	err := m.sizeManifest(doc, rows)
	if err == nil || err.Error() != fmt.Sprintf("the manifest of 2 files could exceed --max-output-bytes (%d bytes)", size-1) {
		t.Errorf("message: %v", err)
	}
	// the worst row is at least as long as the longest row a run can write
	longest := teaWorstRow(selected)
	b, _ := teaEncode(longest)
	if len(b) != len(row(1, 1, true)) {
		t.Errorf("the worst row is %d bytes, the specified one %d", len(b), len(row(1, 1, true)))
	}
}

func TestTeaMachineDownloadRefusedRowNamesTheAddress(t *testing.T) {
	d := newTeaDL(t)
	d.r.m.resolveTo("evil.example.com", "169.254.169.254")
	d.art(teaArtProd, 1, "BOM", "",
		`{"mediaType":"a/one","url":"https://evil.example.com:`+d.cdn.port+`/x","checksums":`+teaSums("SHA-256", teaSumSHA256("x"))+`}`)
	d.r.want(d.run(), teaExitPartial)
	doc := d.r.doc()
	d.wantRow(doc, 0, "FAILED", "ADDRESS_REFUSED")
	msg, _ := d.row(doc, 0, "message").(string)
	if !strings.HasSuffix(msg, "refused to connect to 169.254.169.254 (cloud metadata) for https://evil.example.com:"+d.cdn.port+"/x") {
		t.Errorf("message %q", msg)
	}
	if d.row(doc, 0, "address") != "169.254.169.254" || d.row(doc, 0, "addressClass") != "CLOUD_METADATA" {
		t.Errorf("row %v", doc["files"])
	}
	// a row that did not fail for a refusal names no address
	d = newTeaDL(t)
	d.art(teaArtProd, 1, "BOM", "", d.server(teaArtProd, 1, "a/one", teaBody("tampered"), teaSums("SHA-256", teaSumSHA256("original"))))
	d.r.want(d.run(), teaExitPartial)
	if d.row(d.r.doc(), 0, "address") != nil || d.row(d.r.doc(), 0, "addressClass") != nil {
		t.Errorf("row %v", d.r.doc()["files"])
	}
}

func TestTeaMachineFailRowRecordsTheReason(t *testing.T) {
	refused := &teaAddressRefusedError{Address: netip.MustParseAddr("10.1.2.3"), Class: teaAddressOwn, Host: "h"}
	m := &teaMachineRun{run: &teaRun{lastRequest: "https://h.example.com/p?q=1"}}
	row := func() *teaDownloadRow { return &teaDownloadRow{file: teaManifestFile{Status: teaFileNotSelected}} }
	str := func(p *string) string {
		if p == nil {
			return "<nil>"
		}
		return *p
	}

	r := row()
	m.failRow(r, refused, teaReasonAddressRefused)
	if r.file.Status != teaFileFailed || r.file.Reason == nil || *r.file.Reason != teaReasonAddressRefused ||
		str(r.file.Message) != "refused to connect to 10.1.2.3 (an address of this host) for https://h.example.com/p" ||
		str(r.file.Address) != "10.1.2.3" || str(r.file.AddressClass) != "OWN_ADDRESS" {
		t.Errorf("a refusal: %+v", r.file)
	}

	// the message is ASCII and at most 1024 bytes
	r = row()
	m.failRow(r, errors.New("caf\u00e9 "+strings.Repeat("x", 2000)), teaReasonFailed)
	if got := str(r.file.Message); len(got) != 1024 || !strings.HasPrefix(got, "caf\\u00e9 xxx") || !strings.HasSuffix(got, "xxx...") {
		t.Errorf("a long message of %d bytes: %.40q", len(got), got)
	}
	if r.file.Address != nil || r.file.AddressClass != nil || *r.file.Reason != teaReasonFailed {
		t.Errorf("a failure that is not a refusal: %+v", r.file)
	}

	// a refusal that the run's deadline overtook names no address
	r = row()
	m.failRow(r, refused, teaReasonDeadline)
	if r.file.Address != nil || r.file.AddressClass != nil || *r.file.Reason != teaReasonDeadline {
		t.Errorf("a refusal under the deadline: %+v", r.file)
	}
	// a row that is not attempted keeps its reason and no message
	r = row()
	r.notAttempted(teaReasonTotalBytes)
	if r.file.Status != teaFileNotAttempted || r.file.Reason == nil || *r.file.Reason != teaReasonTotalBytes || r.file.Message != nil {
		t.Errorf("not attempted: %+v", r.file)
	}
}
