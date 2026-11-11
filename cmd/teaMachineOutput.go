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
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	tea "github.com/oolong-project/tea-client-go"
	"github.com/oolong-project/tea-client-go/teaapi"
	"github.com/oolong-project/tea-client-go/tei"
)

// The JSON contract of the TEA machine mode, format version 1 (task TPF-178, design D2),
// its reasons and exit codes (D5), and the error object. Every key of a shape is always
// present: an absent optional value is null, an empty list []. Within format version 1 a
// release may add keys and values of the enumerations marked open; it changes or removes
// nothing.

// teaFormatVersion is the format version every document and error object carries.
const teaFormatVersion = 1

// teaDocKind is the document type, the second key of every document.
type teaDocKind string

const (
	teaDocResolution teaDocKind = "TEA_RESOLUTION"
	teaDocProduct    teaDocKind = "TEA_PRODUCT"
	teaDocComponent  teaDocKind = "TEA_COMPONENT"
	teaDocReleases   teaDocKind = "TEA_RELEASE_LIST"
	teaDocRelease    teaDocKind = "TEA_RELEASE"
	teaDocManifest   teaDocKind = "TEA_DOWNLOAD_MANIFEST"
	teaDocError      teaDocKind = "TEA_ERROR"
)

// teaPreRelease is the pre-release mark of a release; UNSTATED when the server omitted it.
type teaPreRelease string

const (
	teaPreReleaseYes      teaPreRelease = "YES"
	teaPreReleaseNo       teaPreRelease = "NO"
	teaPreReleaseUnstated teaPreRelease = "UNSTATED"
)

func teaPreReleaseOf(p *bool) teaPreRelease {
	switch {
	case p == nil:
		return teaPreReleaseUnstated
	case *p:
		return teaPreReleaseYes
	default:
		return teaPreReleaseNo
	}
}

// teaParentKind is the kind of the parent of a release list.
type teaParentKind string

const (
	teaParentProduct   teaParentKind = "PRODUCT"
	teaParentComponent teaParentKind = "COMPONENT"
)

// teaReleaseKind is the kind of a release.
type teaReleaseKind string

const (
	teaReleaseKindProduct   teaReleaseKind = "PRODUCT_RELEASE"
	teaReleaseKindComponent teaReleaseKind = "COMPONENT_RELEASE"
)

// teaListStop says why a release list stopped (closed).
type teaListStop string

const (
	teaListEnd         teaListStop = "END"
	teaListMaxPages    teaListStop = "MAX_PAGES"
	teaListOutputBytes teaListStop = "OUTPUT_BYTES"
)

// teaCLEStatus says whether the lifecycle was read (closed).
type teaCLEStatus string

const (
	teaCLERead         teaCLEStatus = "READ"
	teaCLENotAvailable teaCLEStatus = "NOT_AVAILABLE"
)

// teaReason is the open enumeration of the error object and of manifest rows.
type teaReason string

const (
	teaReasonUsage            teaReason = "USAGE"
	teaReasonFailed           teaReason = "FAILED"
	teaReasonServerError      teaReason = "SERVER_ERROR"
	teaReasonInvalidResponse  teaReason = "INVALID_RESPONSE"
	teaReasonTLSCertificate   teaReason = "TLS_CERTIFICATE"
	teaReasonLocalIO          teaReason = "LOCAL_IO"
	teaReasonInterrupted      teaReason = "INTERRUPTED"
	teaReasonNotFound         teaReason = "NOT_FOUND"
	teaReasonURLRefused       teaReason = "URL_REFUSED"
	teaReasonRedirectLimit    teaReason = "REDIRECT_LIMIT"
	teaReasonAuth             teaReason = "AUTH"
	teaReasonAddressRefused   teaReason = "ADDRESS_REFUSED"
	teaReasonDeadline         teaReason = "DEADLINE"
	teaReasonRequestTimeout   teaReason = "REQUEST_TIMEOUT"
	teaReasonDocumentBytes    teaReason = "DOCUMENT_BYTES"
	teaReasonStringBytes      teaReason = "STRING_BYTES"
	teaReasonOutputBytes      teaReason = "OUTPUT_BYTES"
	teaReasonFileCount        teaReason = "FILE_COUNT"
	teaReasonDiscoveryResults teaReason = "DISCOVERY_RESULTS"
	teaReasonConnectFailed    teaReason = "CONNECT_FAILED"
	// Only a manifest row carries these.
	teaReasonNoUsableChecksum   teaReason = "NO_USABLE_CHECKSUM"
	teaReasonChecksumMismatch   teaReason = "CHECKSUM_MISMATCH"
	teaReasonNoAcceptableFormat teaReason = "NO_ACCEPTABLE_FORMAT"
	teaReasonArtifactBytes      teaReason = "ARTIFACT_BYTES"
	teaReasonTotalBytes         teaReason = "TOTAL_BYTES"
)

// exit is the exit code of a run that ends with this reason (D5).
func (r teaReason) exit() teaExit {
	switch r {
	case teaReasonUsage:
		return teaExitUsage
	case teaReasonNotFound:
		return teaExitNotResolved
	case teaReasonURLRefused, teaReasonRedirectLimit:
		return teaExitRefusedURL
	case teaReasonAuth:
		return teaExitAuth
	case teaReasonAddressRefused:
		return teaExitAddressRefused
	case teaReasonDeadline, teaReasonRequestTimeout, teaReasonDocumentBytes, teaReasonStringBytes,
		teaReasonOutputBytes, teaReasonFileCount, teaReasonDiscoveryResults:
		return teaExitLimit
	case teaReasonConnectFailed:
		return teaExitConnect
	}
	return teaExitFailed
}

// errTeaDeadline is the cause of the run context when --deadline ends it.
var errTeaDeadline = errors.New("the run deadline (--deadline) was reached")

// errTeaNoResult: discovery answered an empty list.
var errTeaNoResult = errors.New("discovery returned no result")

// teaLimitError is a bound the CLI itself enforces (STRING_BYTES, OUTPUT_BYTES, FILE_COUNT).
type teaLimitError struct {
	reason teaReason
	msg    string
}

func (e *teaLimitError) Error() string { return e.msg }

// teaFileBound says which bound a download was called with, to name an oversized body.
type teaFileBound int

const (
	teaFileBoundNone     teaFileBound = iota // not a download: DOCUMENT_BYTES
	teaFileBoundArtifact                     // MaxBytes was --max-artifact-bytes (ties included)
	teaFileBoundTotal                        // MaxBytes was what was left of --max-total-bytes
)

// teaMachineReason is the one classification of the machine mode (D5): the first match
// wins. runCtx is the run's context (deadline and interrupt).
func teaMachineReason(runCtx context.Context, err error, bound teaFileBound) teaReason {
	var usage *teaUsageError
	var refused *teaAddressRefusedError
	var connect *teaConnectError
	var tokErr *tea.TokenError
	var authErr *teaAuthError
	var notResolved *tea.NotResolvedError
	var limit *teaLimitError
	var unexpected *tea.UnexpectedResponseError
	var certErr *tls.CertificateVerificationError
	var pathErr *fs.PathError
	var linkErr *os.LinkError
	switch {
	case errors.As(err, &usage), errors.Is(err, tei.ErrSyntax), errors.Is(err, tei.ErrLegacyForm),
		errors.Is(err, tea.ErrInvalidOption):
		return teaReasonUsage
	case runCtx.Err() != nil:
		if errors.Is(context.Cause(runCtx), errTeaDeadline) {
			return teaReasonDeadline
		}
		return teaReasonInterrupted
	case errors.As(err, &refused):
		return teaReasonAddressRefused
	case errors.As(err, &connect):
		return teaReasonConnectFailed
	case errors.Is(err, tea.ErrRedirectLimit):
		return teaReasonRedirectLimit
	case errors.Is(err, tea.ErrInsecureURL):
		return teaReasonURLRefused
	case errors.Is(err, tea.ErrUnauthorized), errors.Is(err, tea.ErrForbidden),
		errors.As(err, &tokErr), errors.As(err, &authErr):
		return teaReasonAuth
	case errors.As(err, &notResolved), errors.Is(err, tea.ErrObjectUnknown), errors.Is(err, errTeaNoResult):
		return teaReasonNotFound
	case errors.Is(err, tea.ErrResponseTooLarge):
		switch bound {
		case teaFileBoundArtifact:
			return teaReasonArtifactBytes
		case teaFileBoundTotal:
			return teaReasonTotalBytes
		}
		return teaReasonDocumentBytes
	case errors.Is(err, tea.ErrTooManyResults):
		return teaReasonDiscoveryResults
	case errors.Is(err, tea.ErrTimeout):
		return teaReasonRequestTimeout
	case errors.As(err, &limit):
		return limit.reason
	case errors.Is(err, tea.ErrNoUsableChecksum):
		return teaReasonNoUsableChecksum
	case errors.Is(err, tea.ErrChecksumMismatch):
		return teaReasonChecksumMismatch
	case errors.Is(err, tea.ErrNoAcceptableFormat):
		return teaReasonNoAcceptableFormat
	case errors.Is(err, tea.ErrServer):
		return teaReasonServerError
	case errors.As(err, &unexpected), errors.Is(err, tea.ErrUnsupportedContentEncoding),
		errors.Is(err, tea.ErrInvalidArgument):
		return teaReasonInvalidResponse
	case errors.As(err, &certErr):
		return teaReasonTLSCertificate
	case errors.As(err, &pathErr), errors.As(err, &linkErr):
		return teaReasonLocalIO
	}
	return teaReasonFailed
}

// Fixed hints of the error object.
const (
	teaHintThisHost  = "this host is refused as a target by default; --allow-this-host lifts it for local testing"
	teaHintNever     = "this address class is never a TEA target"
	teaHintConnect   = "the connection could not be made; the network policy of the deployment may block this address"
	teaRowMessageMax = 1024 // bytes of a manifest row's message, ASCII
)

// teaErrorDoc is the error object: one line on stderr.
type teaErrorDoc struct {
	FormatVersion         int        `json:"formatVersion"`
	Kind                  teaDocKind `json:"kind"`
	ExitCode              int        `json:"exitCode"`
	Reason                teaReason  `json:"reason"`
	Message               string     `json:"message"`
	URL                   *string    `json:"url"`
	HTTPStatus            *int       `json:"httpStatus"`
	Address               *string    `json:"address"`
	AddressClass          *string    `json:"addressClass"`
	Phase                 *string    `json:"phase"`
	Hint                  *string    `json:"hint"`
	IgnoredProxyVariables []string   `json:"ignoredProxyVariables"`
}

// teaRedactURL keeps scheme, host, port and path of a URL: never userinfo, query or fragment.
func teaRedactURL(s string) *string {
	if s == "" {
		return nil
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return nil
	}
	r := u.Scheme + "://" + u.Host + u.EscapedPath()
	return &r
}

// teaErrorURL is the redacted URL of the failing request when the error carries one, else
// the last request the run sent for an error that comes from a request.
func teaErrorURL(err error, reason teaReason, lastRequest string) *string {
	var pe *tea.URLPolicyError
	if errors.As(err, &pe) {
		return teaRedactURL(pe.URL)
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		return teaRedactURL(ue.URL)
	}
	switch reason {
	case teaReasonUsage, teaReasonStringBytes, teaReasonOutputBytes, teaReasonFileCount,
		teaReasonLocalIO, teaReasonInterrupted, teaReasonDeadline:
		return nil
	}
	return teaRedactURL(lastRequest)
}

// teaErrorText is the message of an error: the CLI's text around the library's.
func teaErrorText(err error, reason teaReason, u *string) string {
	var usage *teaUsageError
	var refused *teaAddressRefusedError
	switch {
	case errors.As(err, &usage):
		return usage.msg
	case errors.As(err, &refused):
		s := refused.Error()
		if u != nil {
			s += " for " + *u
		}
		return s
	case reason == teaReasonDeadline:
		return errTeaDeadline.Error() + ": " + err.Error()
	case reason == teaReasonInterrupted:
		return "interrupted: " + err.Error()
	}
	return err.Error()
}

// newTeaErrorDoc builds the error object of a run that ended with err.
func newTeaErrorDoc(runCtx context.Context, err error, lastRequest string, proxies []string) teaErrorDoc {
	reason := teaMachineReason(runCtx, err, teaFileBoundNone)
	d := teaErrorDoc{
		FormatVersion: teaFormatVersion, Kind: teaDocError, ExitCode: int(reason.exit()), Reason: reason,
		IgnoredProxyVariables: []string{},
	}
	d.URL = teaErrorURL(err, reason, lastRequest)
	d.Message = teaText(teaErrorText(err, reason, d.URL))
	var apiErr *tea.APIError
	if errors.As(err, &apiErr) {
		status := apiErr.Status
		d.HTTPStatus = &status
	}
	var refused *teaAddressRefusedError
	var connect *teaConnectError
	switch {
	case reason == teaReasonAddressRefused && errors.As(err, &refused):
		addr, class := refused.Address.String(), string(refused.Class)
		d.Address, d.AddressClass = &addr, &class
		hint := teaHintNever
		if refused.Class.liftedByAllowThisHost() {
			hint = teaHintThisHost
		}
		d.Hint = &hint
	case reason == teaReasonConnectFailed && errors.As(err, &connect):
		phase, hint := string(connect.Phase), teaHintConnect
		d.Phase, d.Hint = &phase, &hint
		d.IgnoredProxyVariables = append(d.IgnoredProxyVariables, proxies...)
	}
	return d
}

// teaEncode writes v as one line of printable ASCII (D2): encoding/json (which escapes
// control characters, <, >, &, U+2028 and U+2029), then every rune above 0x7E, and 0x7F, as
// \uXXXX (a surrogate pair above the BMP). Non-ASCII bytes occur only inside strings, so
// the escapes keep the document valid and the decoded strings unchanged.
func teaEncode(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(raw))
	for i := 0; i < len(raw); {
		c := raw[i]
		if c < utf8.RuneSelf && c != 0x7f {
			out = append(out, c)
			i++
			continue
		}
		r, size := utf8.DecodeRune(raw[i:])
		i += size
		if r > 0xffff {
			hi, lo := utf16.EncodeRune(r)
			out = fmt.Appendf(out, `\u%04x\u%04x`, hi, lo)
			continue
		}
		out = fmt.Appendf(out, `\u%04x`, r)
	}
	return out, nil
}

// teaStrings checks every server string copied into a document against --max-string-bytes
// (D4, D9): the first one longer fails the run with a message naming its path and the
// bound, never the value. A string is never cut.
type teaStrings struct {
	max int
	err error
}

func (c *teaStrings) s(path, v string) string {
	if c.err == nil && len(v) > c.max {
		c.err = &teaLimitError{reason: teaReasonStringBytes,
			msg: fmt.Sprintf("the server string at %s is longer than --max-string-bytes (%d bytes)", path, c.max)}
	}
	return v
}

func (c *teaStrings) p(path string, v *string) *string {
	if v == nil {
		return nil
	}
	s := c.s(path, *v)
	return &s
}

// teaInstant is a timestamp as a UTC instant (RFC 3339 with Z).
func teaInstant(t time.Time) string { return teaTime(t) }

func teaInstantP(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := teaTime(*t)
	return &s
}

// Shapes shared by several documents.

type teaServerJSON struct {
	RootURL    string `json:"rootUrl"`
	APIVersion string `json:"apiVersion"`
}

type teaIdentifierJSON struct {
	IDType  string `json:"idType"`
	IDValue string `json:"idValue"`
}

func (c *teaStrings) identifiers(path string, ids []teaapi.Identifier) []teaIdentifierJSON {
	out := make([]teaIdentifierJSON, 0, len(ids))
	for i, id := range ids {
		p := path + "[" + strconv.Itoa(i) + "]"
		out = append(out, teaIdentifierJSON{IDType: c.s(p+".idType", string(id.IdType)), IDValue: c.s(p+".idValue", id.IdValue)})
	}
	return out
}

func (c *teaStrings) identifiersP(path string, ids *[]teaapi.Identifier) []teaIdentifierJSON {
	if ids == nil {
		return []teaIdentifierJSON{}
	}
	return c.identifiers(path, *ids)
}

// TEA_RESOLUTION

type teaResolutionDoc struct {
	FormatVersion int                   `json:"formatVersion"`
	Kind          teaDocKind            `json:"kind"`
	TEI           string                `json:"tei"`
	Results       []teaResolutionResult `json:"results"`
}

type teaResolutionResult struct {
	ProductReleaseUUID string                `json:"productReleaseUuid"`
	DiscoveredAt       teaServerJSON         `json:"discoveredAt"`
	Servers            []teaResolutionServer `json:"servers"`
}

type teaResolutionServer struct {
	RootURL  string   `json:"rootUrl"`
	Versions []string `json:"versions"`
	Priority *float32 `json:"priority"`
}

// TEA_PRODUCT and TEA_COMPONENT

type teaNamedJSON struct {
	UUID        string              `json:"uuid"`
	Name        string              `json:"name"`
	Identifiers []teaIdentifierJSON `json:"identifiers"`
}

type teaProductDoc struct {
	FormatVersion int           `json:"formatVersion"`
	Kind          teaDocKind    `json:"kind"`
	Server        teaServerJSON `json:"server"`
	Product       teaNamedJSON  `json:"product"`
}

type teaComponentDoc struct {
	FormatVersion int           `json:"formatVersion"`
	Kind          teaDocKind    `json:"kind"`
	Server        teaServerJSON `json:"server"`
	Component     teaNamedJSON  `json:"component"`
}

// TEA_RELEASE_LIST

type teaReleaseListDoc struct {
	FormatVersion int                `json:"formatVersion"`
	Kind          teaDocKind         `json:"kind"`
	Server        teaServerJSON      `json:"server"`
	Parent        teaParentJSON      `json:"parent"`
	Releases      []teaReleaseItem   `json:"releases"`
	Page          teaReleaseListPage `json:"page"`
}

type teaParentJSON struct {
	Kind teaParentKind `json:"kind"`
	UUID string        `json:"uuid"`
}

type teaReleaseItem struct {
	UUID        string              `json:"uuid"`
	Version     string              `json:"version"`
	Name        *string             `json:"name"`
	CreatedDate string              `json:"createdDate"`
	ReleaseDate *string             `json:"releaseDate"`
	PreRelease  teaPreRelease       `json:"preRelease"`
	Identifiers []teaIdentifierJSON `json:"identifiers"`
}

type teaReleaseListPage struct {
	StoppedBy     teaListStop `json:"stoppedBy"`
	NextPageToken *string     `json:"nextPageToken"`
	PagesRead     int         `json:"pagesRead"`
}

// TEA_RELEASE

type teaReleaseDoc struct {
	FormatVersion int              `json:"formatVersion"`
	Kind          teaDocKind       `json:"kind"`
	Server        teaServerJSON    `json:"server"`
	Release       teaReleaseJSON   `json:"release"`
	Collection    teaCollectionOut `json:"collection"`
	CLE           teaCLEJSON       `json:"cle"`
}

type teaReleaseJSON struct {
	Kind        teaReleaseKind      `json:"kind"`
	UUID        string              `json:"uuid"`
	ParentUUID  string              `json:"parentUuid"`
	Name        *string             `json:"name"`
	Version     string              `json:"version"`
	CreatedDate string              `json:"createdDate"`
	ReleaseDate *string             `json:"releaseDate"`
	PreRelease  teaPreRelease       `json:"preRelease"`
	Identifiers []teaIdentifierJSON `json:"identifiers"`
	Components  []teaComponentRef   `json:"components"`
}

type teaComponentRef struct {
	ComponentUUID string  `json:"componentUuid"`
	ReleaseUUID   *string `json:"releaseUuid"`
}

type teaUpdateReasonJSON struct {
	Type    string  `json:"type"`
	Comment *string `json:"comment"`
}

type teaCollectionOut struct {
	UUID         string              `json:"uuid"`
	Version      int                 `json:"version"`
	CreatedDate  string              `json:"createdDate"`
	BelongsTo    string              `json:"belongsTo"`
	UpdateReason teaUpdateReasonJSON `json:"updateReason"`
	Artifacts    []teaArtifactJSON   `json:"artifacts"`
}

type teaArtifactJSON struct {
	UUID        string          `json:"uuid"`
	Version     int             `json:"version"`
	Name        *string         `json:"name"`
	Type        string          `json:"type"`
	CreatedDate string          `json:"createdDate"`
	Formats     []teaFormatJSON `json:"formats"`
}

type teaFormatJSON struct {
	MediaType    string            `json:"mediaType"`
	Description  *string           `json:"description"`
	ExternalURL  *string           `json:"externalUrl"`
	SignatureURL *string           `json:"signatureUrl"`
	Checksums    []teaChecksumJSON `json:"checksums"`
}

type teaChecksumJSON struct {
	AlgType  string `json:"algType"`
	AlgValue string `json:"algValue"`
}

type teaCLEJSON struct {
	Status             teaCLEStatus           `json:"status"`
	Events             []teaCLEEventJSON      `json:"events"`
	SupportDefinitions []teaSupportDefinition `json:"supportDefinitions"`
}

type teaCLEEventJSON struct {
	ID                  int                 `json:"id"`
	Type                string              `json:"type"`
	Effective           string              `json:"effective"`
	Published           string              `json:"published"`
	Version             *string             `json:"version"`
	Versions            []teaCLEVersionJSON `json:"versions"`
	License             *string             `json:"license"`
	SupportID           *string             `json:"supportId"`
	SupersededByVersion *string             `json:"supersededByVersion"`
	Description         *string             `json:"description"`
	Reason              *string             `json:"reason"`
	References          []string            `json:"references"`
	Identifiers         []teaIdentifierJSON `json:"identifiers"`
	EventID             *int                `json:"eventId"`
}

type teaCLEVersionJSON struct {
	Version *string `json:"version"`
	Range   *string `json:"range"`
}

type teaSupportDefinition struct {
	ID          string  `json:"id"`
	Description string  `json:"description"`
	URL         *string `json:"url"`
}

func (c *teaStrings) checksums(path string, sums *[]teaapi.Checksum) []teaChecksumJSON {
	out := []teaChecksumJSON{}
	if sums == nil {
		return out
	}
	for i, s := range *sums {
		p := path + "[" + strconv.Itoa(i) + "]"
		out = append(out, teaChecksumJSON{AlgType: c.s(p+".algType", string(s.AlgType)), AlgValue: c.s(p+".algValue", s.AlgValue)})
	}
	return out
}

func (c *teaStrings) updateReason(path string, r teaapi.CollectionUpdateReason) teaUpdateReasonJSON {
	return teaUpdateReasonJSON{Type: c.s(path+".type", string(r.Type)), Comment: c.p(path+".comment", r.Comment)}
}

func (c *teaStrings) collection(path string, col teaapi.Collection) teaCollectionOut {
	out := teaCollectionOut{
		UUID: col.Uuid.String(), Version: col.Version, CreatedDate: teaInstant(col.CreatedDate),
		BelongsTo: c.s(path+".belongsTo", string(col.BelongsTo)), UpdateReason: c.updateReason(path+".updateReason", col.UpdateReason),
		Artifacts: []teaArtifactJSON{},
	}
	for i, a := range col.Artifacts {
		ap := path + ".artifacts[" + strconv.Itoa(i) + "]"
		aj := teaArtifactJSON{
			UUID: a.Uuid.String(), Version: a.Version, Name: c.p(ap+".name", a.Name), Type: c.s(ap+".type", string(a.Type)),
			CreatedDate: teaInstant(a.CreatedDate), Formats: []teaFormatJSON{},
		}
		for j, f := range a.Formats {
			fp := ap + ".formats[" + strconv.Itoa(j) + "]"
			aj.Formats = append(aj.Formats, teaFormatJSON{
				MediaType: c.s(fp+".mediaType", f.MediaType), Description: c.p(fp+".description", f.Description),
				ExternalURL: c.p(fp+".externalUrl", f.Url), SignatureURL: c.p(fp+".signatureUrl", f.SignatureUrl),
				Checksums: c.checksums(fp+".checksums", f.Checksums),
			})
		}
		out.Artifacts = append(out.Artifacts, aj)
	}
	return out
}

func (c *teaStrings) cle(path string, cle *teaapi.Cle) teaCLEJSON {
	out := teaCLEJSON{Status: teaCLERead, Events: []teaCLEEventJSON{}, SupportDefinitions: []teaSupportDefinition{}}
	if cle == nil {
		out.Status = teaCLENotAvailable
		return out
	}
	for i, ev := range cle.Events {
		ep := path + ".events[" + strconv.Itoa(i) + "]"
		ej := teaCLEEventJSON{
			ID: ev.Id, Type: c.s(ep+".type", string(ev.Type)), Effective: teaInstant(ev.Effective), Published: teaInstant(ev.Published),
			Version: c.p(ep+".version", ev.Version), Versions: []teaCLEVersionJSON{}, License: c.p(ep+".license", ev.License),
			SupportID: c.p(ep+".supportId", ev.SupportId), SupersededByVersion: c.p(ep+".supersededByVersion", ev.SupersededByVersion),
			Description: c.p(ep+".description", ev.Description), Reason: c.p(ep+".reason", ev.Reason),
			References: []string{}, Identifiers: c.identifiersP(ep+".identifiers", ev.Identifiers), EventID: ev.EventId,
		}
		if ev.Versions != nil {
			for j, v := range *ev.Versions {
				vp := ep + ".versions[" + strconv.Itoa(j) + "]"
				ej.Versions = append(ej.Versions, teaCLEVersionJSON{Version: c.p(vp+".version", v.Version), Range: c.p(vp+".range", v.Range)})
			}
		}
		if ev.References != nil {
			for j, r := range *ev.References {
				ej.References = append(ej.References, c.s(ep+".references["+strconv.Itoa(j)+"]", r))
			}
		}
		out.Events = append(out.Events, ej)
	}
	if cle.Definitions != nil && cle.Definitions.Support != nil {
		for i, s := range *cle.Definitions.Support {
			sp := path + ".supportDefinitions[" + strconv.Itoa(i) + "]"
			out.SupportDefinitions = append(out.SupportDefinitions, teaSupportDefinition{
				ID: c.s(sp+".id", s.Id), Description: c.s(sp+".description", s.Description), URL: c.p(sp+".url", s.Url),
			})
		}
	}
	return out
}

// TEA_DOWNLOAD_MANIFEST

// teaFileStatus is the status of one manifest row (closed).
type teaFileStatus string

const (
	teaFileVerified     teaFileStatus = "VERIFIED"
	teaFileFailed       teaFileStatus = "FAILED"
	teaFileNotAttempted teaFileStatus = "NOT_ATTEMPTED"
	teaFileNotSelected  teaFileStatus = "NOT_SELECTED"
)

// teaFileSource is where a row's bytes come from (closed).
type teaFileSource string

const (
	teaFileSourceServer   teaFileSource = "SERVER"
	teaFileSourceExternal teaFileSource = "EXTERNAL"
)

// teaDownloadStop says why a download stopped (closed).
type teaDownloadStop string

const (
	teaDownloadEnd        teaDownloadStop = "END"
	teaDownloadTotalBytes teaDownloadStop = "TOTAL_BYTES"
	teaDownloadDeadline   teaDownloadStop = "DEADLINE"
)

type teaManifestDoc struct {
	FormatVersion int                   `json:"formatVersion"`
	Kind          teaDocKind            `json:"kind"`
	Server        teaServerJSON         `json:"server"`
	Release       teaManifestRelease    `json:"release"`
	Collection    teaManifestCollection `json:"collection"`
	Files         []teaManifestFile     `json:"files"`
	Summary       teaManifestSummary    `json:"summary"`
	StoppedBy     teaDownloadStop       `json:"stoppedBy"`
}

type teaManifestRelease struct {
	Kind teaReleaseKind `json:"kind"`
	UUID string         `json:"uuid"`
}

type teaManifestCollection struct {
	UUID         string              `json:"uuid"`
	Version      int                 `json:"version"`
	CreatedDate  string              `json:"createdDate"`
	BelongsTo    string              `json:"belongsTo"`
	UpdateReason teaUpdateReasonJSON `json:"updateReason"`
}

type teaManifestFile struct {
	Number              int               `json:"number"`
	ArtifactUUID        string            `json:"artifactUuid"`
	ArtifactVersion     int               `json:"artifactVersion"`
	ArtifactName        *string           `json:"artifactName"`
	ArtifactType        string            `json:"artifactType"`
	ArtifactCreatedDate string            `json:"artifactCreatedDate"`
	MediaType           string            `json:"mediaType"`
	Description         *string           `json:"description"`
	Source              teaFileSource     `json:"source"`
	SourceURL           string            `json:"sourceUrl"`
	SignatureURL        *string           `json:"signatureUrl"`
	Checksums           []teaChecksumJSON `json:"checksums"`
	Status              teaFileStatus     `json:"status"`
	FileName            *string           `json:"fileName"`
	SizeBytes           *int64            `json:"sizeBytes"`
	ResponseMediaType   *string           `json:"responseMediaType"`
	VerifiedAlgorithms  []string          `json:"verifiedAlgorithms"`
	Reason              *teaReason        `json:"reason"`
	Message             *string           `json:"message"`
	Address             *string           `json:"address"`
	AddressClass        *string           `json:"addressClass"`
}

type teaManifestSummary struct {
	Verified      int   `json:"verified"`
	Failed        int   `json:"failed"`
	NotAttempted  int   `json:"notAttempted"`
	NotSelected   int   `json:"notSelected"`
	BytesReceived int64 `json:"bytesReceived"`
}

// teaRowMessage is a manifest row's message: the error text made printable by teaText,
// every non-ASCII rune then written as \uXXXX, cut at teaRowMessageMax bytes. It is ASCII
// so that the manifest can be sized before anything is fetched.
func teaRowMessage(s string) string {
	var b strings.Builder
	for _, r := range teaText(s) {
		if r < utf8.RuneSelf {
			b.WriteRune(r)
			continue
		}
		if r > 0xffff {
			hi, lo := utf16.EncodeRune(r)
			fmt.Fprintf(&b, `\u%04x\u%04x`, hi, lo)
			continue
		}
		fmt.Fprintf(&b, `\u%04x`, r)
	}
	q := b.String()
	if len(q) > teaRowMessageMax {
		q = q[:teaRowMessageMax-3] + "..."
	}
	return q
}

// teaMediaTypeMax bounds a response media type kept in a row: RFC 6838 allows 127
// characters for the type and for the subtype.
const teaMediaTypeMax = 255

// teaResponseMediaType keeps the response's media type when it is a plausible one.
func teaResponseMediaType(mt string) *string {
	if mt == "" || len(mt) > teaMediaTypeMax {
		return nil
	}
	for _, r := range mt {
		if r <= 0x20 || r >= 0x7f {
			return nil
		}
	}
	return &mt
}

// teaFormatSourceURL is the URL a format is first requested at: the external URL as the
// server wrote it, or the server's versioned download endpoint.
func teaFormatSourceURL(api *tea.API, a teaapi.Artifact, f teaapi.ArtifactFormat) (teaFileSource, string) {
	if f.Url != nil {
		return teaFileSourceExternal, *f.Url
	}
	return teaFileSourceServer, fmt.Sprintf("%s/artifact/%s/%d/download?mediaType=%s",
		api.Base().String(), a.Uuid.String(), a.Version, url.QueryEscape(f.MediaType))
}

// teaWorstRow fills a row with the longest values its status fields can take, to size a
// manifest before anything is fetched (D3 step 3).
func teaWorstRow(f teaManifestFile) teaManifestFile {
	name := "000000.bin"
	size := int64(1<<63 - 1)
	mt := strings.Repeat("&", teaMediaTypeMax)
	reason := teaReasonNoAcceptableFormat
	msg := strings.Repeat("<", teaRowMessageMax) // the longest escape: \u003c
	addr := "ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff"
	class := string(teaAddressCloudMetadata)
	f.Status = teaFileNotAttempted
	f.FileName, f.SizeBytes, f.ResponseMediaType = &name, &size, &mt
	f.VerifiedAlgorithms = []string{"SHA-1", "SHA-256", "SHA-384", "SHA-512", "SHA3-256", "SHA3-384", "SHA3-512", "BLAKE2b-256", "BLAKE2b-384", "BLAKE2b-512"}
	f.Reason, f.Message, f.Address, f.AddressClass = &reason, &msg, &addr, &class
	return f
}
