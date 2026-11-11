package cmd

import (
	"bytes"
	"compress/gzip"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// The download verb (task TPF-178, design section 4, rows T-15 to T-28).

const teaArt3 = "66666666-6666-4666-8666-666666666663"

// teaDL is a TEA server with one component release collection (version 2), an external
// host, and a directory to download into.
type teaDL struct {
	t    *testing.T
	r    *teaMRow
	f    *teaFixture
	cdn  *teaFixture
	dir  string
	arts []string

	mu     sync.Mutex
	served map[string]map[string]http.HandlerFunc // download path -> media type -> handler
}

func newTeaDL(t *testing.T) *teaDL {
	r := newTeaMRow(t)
	d := &teaDL{t: t, r: r, f: r.fixture("tea.example.com", true), cdn: r.fixture("cdn.example.com", true),
		dir: filepath.Join(t.TempDir(), "out"), served: map[string]map[string]http.HandlerFunc{}}
	return d
}

func teaSumSHA256(body string) string {
	s := sha256.Sum256([]byte(body))
	return hex.EncodeToString(s[:])
}
func teaSumSHA1(body string) string { s := sha1.Sum([]byte(body)); return hex.EncodeToString(s[:]) }
func teaSumSHA512(body string) string {
	s := sha512.Sum512([]byte(body))
	return hex.EncodeToString(s[:])
}

func teaSums(pairs ...string) string {
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, `{"algType":"`+pairs[i]+`","algValue":"`+pairs[i+1]+`"}`)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func teaBody(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, q *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = w.Write([]byte(body))
	}
}

// teaChunked writes body without a Content-Length, in two flushed parts.
func teaChunked(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, q *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		half := len(body) / 2
		_, _ = w.Write([]byte(body[:half]))
		w.(http.Flusher).Flush()
		_, _ = w.Write([]byte(body[half:]))
	}
}

// server adds a format served by the TEA server's versioned endpoint; sums "" means the
// right SHA-256 of body.
func (d *teaDL) server(id string, version int, media string, h http.HandlerFunc, sums string) string {
	path := d.f.api(fmt.Sprintf("/artifact/%s/%d/download", id, version))
	d.mu.Lock()
	m, ok := d.served[path]
	if !ok {
		m = map[string]http.HandlerFunc{}
		d.served[path] = m
	}
	m[media] = h
	d.mu.Unlock()
	if !ok {
		d.f.handle(path, func(w http.ResponseWriter, q *http.Request) {
			d.mu.Lock()
			h := d.served[path][q.URL.Query().Get("mediaType")]
			d.mu.Unlock()
			if h == nil {
				writeTeaAnswer(w, teaTEAError(http.StatusNotAcceptable, "NO_ACCEPTABLE_FORMAT"))
				return
			}
			h(w, q)
		})
	}
	return `{"mediaType":"` + media + `","checksums":` + sums + `}`
}

func (d *teaDL) serverBody(id string, version int, media, body string) string {
	return d.server(id, version, media, teaBody(body), teaSums("SHA-256", teaSumSHA256(body)))
}

// external adds a format at a URL on the external host.
func (d *teaDL) external(path, media string, h http.HandlerFunc, sums string) string {
	d.cdn.handle(path, h)
	return `{"mediaType":"` + media + `","url":"` + d.cdn.origin + path + `","checksums":` + sums + `}`
}

func (d *teaDL) externalBody(path, media, body string) string {
	return d.external(path, media, teaBody(body), teaSums("SHA-256", teaSumSHA256(body)))
}

func (d *teaDL) art(id string, version int, typ, nameJSON string, formats ...string) {
	name := ""
	if nameJSON != "" {
		name = `"name":` + nameJSON + `,`
	}
	d.arts = append(d.arts, `{"uuid":"`+id+`","version":`+strconv.Itoa(version)+`,`+name+`"type":"`+typ+
		`","createdDate":"2026-01-10T08:05:00Z","formats":[`+strings.Join(formats, ",")+`]}`)
}

func (d *teaDL) run(args ...string) teaExit {
	d.f.route(d.f.api("/componentRelease/"+teaCR1+"/collection/2"), teaJSON(`{"uuid":"`+teaCR1+`","version":2,"createdDate":"2026-01-16T10:00:00Z",`+
		`"belongsTo":"COMPONENT_RELEASE","updateReason":{"type":"ARTIFACT_ADDED"},"artifacts":[`+strings.Join(d.arts, ",")+`]}`))
	base := []string{"--api-root", d.f.root, "--component-release", teaCR1, "--collection-version", "2", "--dir", d.dir}
	return d.r.exec(teaVerbDownload, append(base, args...)...)
}

// count is the number of requests the TEA server and the external host received on path.
func (d *teaDL) count(path string) int {
	return d.f.count(path) + d.cdn.count(path)
}

func (d *teaDL) downloadPath(id string, version int) string {
	return d.f.api(fmt.Sprintf("/artifact/%s/%d/download", id, version))
}

// files lists the directory.
func (d *teaDL) files() []string {
	d.t.Helper()
	es, err := os.ReadDir(d.dir)
	if err != nil {
		d.t.Fatalf("reading the directory: %v", err)
	}
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out
}

func (d *teaDL) row(doc map[string]any, i int, key string) any {
	return teaPath(doc, fmt.Sprintf("files.%d.%s", i, key))
}

func (d *teaDL) wantRow(doc map[string]any, i int, status string, reason any) {
	d.t.Helper()
	if d.row(doc, i, "status") != status || d.row(doc, i, "reason") != reason {
		d.t.Errorf("row %d: status %v reason %v, want %s %v (message %v)", i+1, d.row(doc, i, "status"), d.row(doc, i, "reason"), status, reason, d.row(doc, i, "message"))
	}
	if status != "VERIFIED" && d.row(doc, i, "fileName") != nil {
		d.t.Errorf("row %d: fileName %v on a %s row", i+1, d.row(doc, i, "fileName"), status)
	}
}

func TestTeaMachineDownloadGolden(t *testing.T) { // T-15
	d := newTeaDL(t)
	d.art(teaArtProd, 1, "BOM", `"Build SBOM"`,
		d.serverBody(teaArtProd, 1, "application/vnd.cyclonedx+json", "one"),
		d.externalBody("/a.xml", "application/vnd.cyclonedx+xml", "two"))
	d.art(teaArtComp1, 2, "VULNERABILITIES", "",
		d.server(teaArtComp1, 2, "application/vnd.cyclonedx+json", teaBody("three"), teaSums("SHA-256", teaSumSHA256("three"), "SHA-512", teaSumSHA512("three"))),
		d.external("/b.json", "application/json", teaBody("four"), teaSums("SHA-256", teaSumSHA256("four"))))
	d.art(teaArt3, 1, "OTHER", `"notes"`, d.serverBody(teaArt3, 1, "text/plain", "five"))
	d.r.want(d.run(), teaExitOK)

	server := func(id string, v int, media string) string {
		return d.f.root + "/v1.0.0/artifact/" + id + "/" + strconv.Itoa(v) + "/download?mediaType=" + strings.NewReplacer("/", "%2F", "+", "%2B").Replace(media)
	}
	row := func(n int, id string, v int, name, typ, media, source, url, sums string, size int, algs string) string {
		return `{"number":` + strconv.Itoa(n) + `,"artifactUuid":"` + id + `","artifactVersion":` + strconv.Itoa(v) + `,"artifactName":` + name +
			`,"artifactType":"` + typ + `","artifactCreatedDate":"2026-01-10T08:05:00Z","mediaType":"` + media + `","description":null,"source":"` + source +
			`","sourceUrl":"` + url + `","signatureUrl":null,"checksums":` + sums + `,"status":"VERIFIED","fileName":"` + fmt.Sprintf("%06d.bin", n) +
			`","sizeBytes":` + strconv.Itoa(size) + `,"responseMediaType":"application/octet-stream","verifiedAlgorithms":` + algs +
			`,"reason":null,"message":null,"address":null,"addressClass":null}`
	}
	want := `{"formatVersion":1,"kind":"TEA_DOWNLOAD_MANIFEST","server":{"rootUrl":"` + d.f.root + `","apiVersion":"1.0.0"},` +
		`"release":{"kind":"COMPONENT_RELEASE","uuid":"` + teaCR1 + `"},` +
		`"collection":{"uuid":"` + teaCR1 + `","version":2,"createdDate":"2026-01-16T10:00:00Z","belongsTo":"COMPONENT_RELEASE","updateReason":{"type":"ARTIFACT_ADDED","comment":null}},` +
		`"files":[` +
		row(1, teaArtProd, 1, `"Build SBOM"`, "BOM", "application/vnd.cyclonedx+json", "SERVER", server(teaArtProd, 1, "application/vnd.cyclonedx+json"), teaSums("SHA-256", teaSumSHA256("one")), 3, `["SHA-256"]`) + "," +
		row(2, teaArtProd, 1, `"Build SBOM"`, "BOM", "application/vnd.cyclonedx+xml", "EXTERNAL", d.cdn.origin+"/a.xml", teaSums("SHA-256", teaSumSHA256("two")), 3, `["SHA-256"]`) + "," +
		row(3, teaArtComp1, 2, "null", "VULNERABILITIES", "application/vnd.cyclonedx+json", "SERVER", server(teaArtComp1, 2, "application/vnd.cyclonedx+json"), teaSums("SHA-256", teaSumSHA256("three"), "SHA-512", teaSumSHA512("three")), 5, `["SHA-256","SHA-512"]`) + "," +
		row(4, teaArtComp1, 2, "null", "VULNERABILITIES", "application/json", "EXTERNAL", d.cdn.origin+"/b.json", teaSums("SHA-256", teaSumSHA256("four")), 4, `["SHA-256"]`) + "," +
		row(5, teaArt3, 1, `"notes"`, "OTHER", "text/plain", "SERVER", server(teaArt3, 1, "text/plain"), teaSums("SHA-256", teaSumSHA256("five")), 4, `["SHA-256"]`) +
		`],"summary":{"verified":5,"failed":0,"notAttempted":0,"notSelected":0,"bytesReceived":19},"stoppedBy":"END"}`
	d.r.golden(want)

	if got := strings.Join(d.files(), ","); got != "000001.bin,000002.bin,000003.bin,000004.bin,000005.bin" {
		t.Errorf("directory %s", got)
	}
	for i, body := range []string{"one", "two", "three", "four", "five"} {
		p := filepath.Join(d.dir, fmt.Sprintf("%06d.bin", i+1))
		b, err := os.ReadFile(p)
		st, _ := os.Stat(p)
		if err != nil || string(b) != body || st.Mode().Perm() != 0o600 {
			t.Errorf("%s: %q %v mode %v", p, b, err, st.Mode().Perm())
		}
	}
	if st, _ := os.Stat(d.dir); st.Mode().Perm() != 0o700 {
		t.Errorf("directory mode %v", st.Mode().Perm())
	}
	if d.f.count(d.f.api("/componentRelease/"+teaCR1+"/collection/2")) != 1 {
		t.Errorf("the collection was not read at /collection/2")
	}
}

// teaSelectionDL has a BOM (two formats), a VULNERABILITIES, an OTHER and an artifact of a
// type the specification does not know.
func teaSelectionDL(t *testing.T) *teaDL {
	d := newTeaDL(t)
	d.art(teaArtProd, 1, "BOM", "",
		d.serverBody(teaArtProd, 1, "Application/VND.CycloneDX+JSON; version=1.6", "bom-json"),
		d.serverBody(teaArtProd, 1, "application/vnd.cyclonedx+xml", "bom-xml"))
	d.art(teaArtComp1, 1, "VULNERABILITIES", "", d.serverBody(teaArtComp1, 1, "application/vnd.cyclonedx+json", "vex"))
	d.art(teaArt3, 1, "OTHER", "", d.serverBody(teaArt3, 1, "text/plain", "other"))
	d.art("66666666-6666-4666-8666-666666666664", 1, "FUTURE_TYPE", "", d.serverBody("66666666-6666-4666-8666-666666666664", 1, "text/plain", "future"))
	return d
}

func teaStatuses(doc map[string]any) string {
	var out []string
	files, _ := doc["files"].([]any)
	for _, f := range files {
		out = append(out, f.(map[string]any)["status"].(string)[:1])
	}
	return strings.Join(out, "")
}

func TestTeaMachineDownloadSelection(t *testing.T) { // T-16
	// (a) only BOM
	d := teaSelectionDL(t)
	d.r.want(d.run("--artifact-type", "BOM"), teaExitOK)
	if s := teaStatuses(d.r.doc()); s != "VVNNN" {
		t.Errorf("(a) %s", s)
	}
	if d.count(d.downloadPath(teaArtComp1, 1))+d.count(d.downloadPath(teaArt3, 1)) != 0 {
		t.Errorf("(a) an unselected artifact was requested")
	}
	// (b) everything but BOM and VULNERABILITIES, the unknown type included
	d = teaSelectionDL(t)
	d.r.want(d.run("--exclude-artifact-type", "BOM", "--exclude-artifact-type", "VULNERABILITIES"), teaExitOK)
	if s := teaStatuses(d.r.doc()); s != "NNNVV" {
		t.Errorf("(b) %s", s)
	}
	// (c) the media type, case-insensitive, parameters ignored
	d = teaSelectionDL(t)
	d.r.want(d.run("--media-type", "application/vnd.cyclonedx+json"), teaExitOK)
	if s := teaStatuses(d.r.doc()); s != "VNVNN" {
		t.Errorf("(c) %s", s)
	}
	// (d) both type flags; (e) a type TEA does not have
	for _, args := range [][]string{{"--artifact-type", "BOM", "--exclude-artifact-type", "OTHER"}, {"--artifact-type", "SBOM"}} {
		d = teaSelectionDL(t)
		d.r.wantFail(d.run(args...), teaExitUsage, teaReasonUsage)
		teaNoRequests(t, d.f, d.cdn)
	}
}

func TestTeaMachineDownloadChecksumMismatch(t *testing.T) { // T-17
	d := newTeaDL(t)
	d.art(teaArtProd, 1, "BOM", "",
		d.serverBody(teaArtProd, 1, "a/json", "good-1"),
		d.server(teaArtProd, 1, "a/xml", teaBody("tampered"), teaSums("SHA-256", teaSumSHA256("original"))),
		d.serverBody(teaArtProd, 1, "a/txt", "good-3"))
	d.r.want(d.run(), teaExitPartial)
	doc := d.r.doc()
	d.wantRow(doc, 0, "VERIFIED", nil)
	d.wantRow(doc, 1, "FAILED", "CHECKSUM_MISMATCH")
	d.wantRow(doc, 2, "VERIFIED", nil)
	if got := strings.Join(d.files(), ","); got != "000001.bin,000003.bin" {
		t.Errorf("directory %s", got)
	}
}

func TestTeaMachineDownloadWhatVerifiedMeans(t *testing.T) { // T-18
	d := newTeaDL(t)
	d.art(teaArtProd, 1, "BOM", "",
		d.server(teaArtProd, 1, "a/none", teaBody("x"), "[]"),
		d.server(teaArtProd, 1, "a/sha1", teaBody("x"), teaSums("SHA-1", teaSumSHA1("x"))),
		d.server(teaArtProd, 1, "a/blake3", teaBody("x"), teaSums("BLAKE3", teaSumSHA256("x"))),
		d.server(teaArtProd, 1, "a/both", teaBody("x"), teaSums("SHA-1", teaSumSHA1("x"), "SHA-256", teaSumSHA256("x"))),
		d.server(teaArtProd, 1, "a/weakwrong", teaBody("x"), teaSums("SHA-256", teaSumSHA256("x"), "SHA-1", teaSumSHA1("y"))))
	d.r.want(d.run(), teaExitPartial)
	doc := d.r.doc()
	for i := 0; i < 3; i++ {
		d.wantRow(doc, i, "FAILED", "NO_USABLE_CHECKSUM")
	}
	d.wantRow(doc, 3, "VERIFIED", nil)
	if fmt.Sprint(d.row(doc, 3, "verifiedAlgorithms")) != "[SHA-1 SHA-256]" {
		t.Errorf("verifiedAlgorithms %v", d.row(doc, 3, "verifiedAlgorithms"))
	}
	d.wantRow(doc, 4, "FAILED", "CHECKSUM_MISMATCH")
	n := 0
	for _, q := range d.f.requests() {
		if q.path == d.downloadPath(teaArtProd, 1) {
			n++
			if mt, _ := urlQueryValue(q.rawQuery, "mediaType"); mt == "a/none" || mt == "a/sha1" || mt == "a/blake3" {
				t.Errorf("%s was requested", mt)
			}
		}
	}
	if n != 2 {
		t.Errorf("%d download requests, want 2", n)
	}
}

func TestTeaMachineDownloadMalformedChecksum(t *testing.T) { // T-19
	d := newTeaDL(t)
	d.art(teaArtProd, 1, "BOM", "",
		d.server(teaArtProd, 1, "a/upper", teaBody("x"), teaSums("SHA-256", strings.ToUpper(teaSumSHA256("x")))),
		d.server(teaArtProd, 1, "a/short", teaBody("x"), teaSums("SHA-256", teaSumSHA256("x")[:10])))
	d.r.want(d.run(), teaExitPartial)
	doc := d.r.doc()
	d.wantRow(doc, 0, "FAILED", "INVALID_RESPONSE")
	d.wantRow(doc, 1, "FAILED", "INVALID_RESPONSE")
	if d.count(d.downloadPath(teaArtProd, 1)) != 0 {
		t.Errorf("a download was requested")
	}
}

func TestTeaMachineDownloadDirectoryRules(t *testing.T) { // T-20
	for _, c := range []string{"existing", "link", "dangling", "noparent", "file"} {
		d := newTeaDL(t)
		d.art(teaArtProd, 1, "BOM", "", d.serverBody(teaArtProd, 1, "a/json", "x"))
		target := filepath.Join(t.TempDir(), "target")
		switch c {
		case "existing":
			_ = os.Mkdir(d.dir, 0o700)
		case "link":
			_ = os.Mkdir(target, 0o755)
			_ = os.Symlink(target, d.dir)
		case "dangling":
			_ = os.Symlink(target, d.dir)
		case "noparent":
			d.dir = filepath.Join(t.TempDir(), "missing", "out")
		case "file":
			_ = os.WriteFile(d.dir, []byte("x"), 0o600)
		}
		d.r.wantFail(d.run(), teaExitUsage, teaReasonUsage)
		teaNoRequests(t, d.f, d.cdn)
		if c == "link" {
			if es, _ := os.ReadDir(target); len(es) != 0 {
				t.Errorf("link: the target got %v", es)
			}
		}
		if c == "dangling" {
			if _, err := os.Lstat(target); !os.IsNotExist(err) {
				t.Errorf("dangling: the target was created")
			}
		}
	}
}

func TestTeaMachineDownloadHostileNames(t *testing.T) { // T-21
	d := newTeaDL(t)
	parent := filepath.Dir(d.dir)
	disposition := func(body string) http.HandlerFunc {
		return func(w http.ResponseWriter, q *http.Request) {
			w.Header().Set("Content-Disposition", `attachment; filename="../../evil"`)
			teaBody(body)(w, q)
		}
	}
	d.art(teaArtProd, 1, "BOM", `"../../etc/passwd"`,
		d.server(teaArtProd, 1, "../x", disposition("one"), teaSums("SHA-256", teaSumSHA256("one"))),
		teaEscapedExternal(d, disposition("two"), teaSums("SHA-256", teaSumSHA256("two"))))
	d.r.want(d.run(), teaExitOK)
	doc := d.r.doc()
	if d.row(doc, 0, "artifactName") != "../../etc/passwd" || d.row(doc, 0, "mediaType") != "../x" {
		t.Errorf("names not kept as data: %v", doc["files"])
	}
	if got := strings.Join(d.files(), ","); got != "000001.bin,000002.bin" {
		t.Errorf("directory %s", got)
	}
	es, _ := os.ReadDir(parent)
	if len(es) != 1 || es[0].Name() != "out" {
		t.Errorf("the parent lists %v", es)
	}
}

// teaEscapedExternal is a format whose URL path ends in /..%2f..%2fevil; the fixture routes
// by the decoded path.
func teaEscapedExternal(d *teaDL, h http.HandlerFunc, sums string) string {
	d.cdn.handle("/../../evil", h)
	return `{"mediaType":"a/json","url":"` + d.cdn.origin + `/..%2f..%2fevil","checksums":` + sums + `}`
}

func TestTeaMachineDownloadArtifactBound(t *testing.T) { // T-22
	for _, h := range []func(string) http.HandlerFunc{teaBody, teaChunked} {
		d := newTeaDL(t)
		body := strings.Repeat("a", 1025)
		d.art(teaArtProd, 1, "BOM", "", d.server(teaArtProd, 1, "a/json", h(body), teaSums("SHA-256", teaSumSHA256(body))))
		d.r.want(d.run("--max-artifact-bytes", "1024"), teaExitPartial)
		d.wantRow(d.r.doc(), 0, "FAILED", "ARTIFACT_BYTES")
		if len(d.files()) != 0 {
			t.Errorf("directory %v", d.files())
		}
	}
}

func TestTeaMachineDownloadTotalBound(t *testing.T) { // T-23
	const k = 1024
	body := func(n int, c string) string { return strings.Repeat(c, n) }
	type file struct {
		body    string
		chunked bool
		wrong   bool
	}
	run := func(name string, extra []string, files ...file) (*teaDL, map[string]any) {
		d := newTeaDL(t)
		var formats []string
		for i, f := range files {
			h := teaBody(f.body)
			if f.chunked {
				h = teaChunked(f.body)
			}
			sum := teaSumSHA256(f.body)
			if f.wrong {
				sum = teaSumSHA256("other")
			}
			formats = append(formats, d.server(teaArtProd, 1, "a/f"+strconv.Itoa(i+1), h, teaSums("SHA-256", sum)))
		}
		d.art(teaArtProd, 1, "BOM", "", formats...)
		d.r.want(d.run(append([]string{"--max-total-bytes", strconv.Itoa(100 * k)}, extra...)...), teaExitPartial)
		return d, d.r.doc()
	}
	requested := func(d *teaDL, media string) bool {
		for _, q := range d.f.requests() {
			if mt, _ := urlQueryValue(q.rawQuery, "mediaType"); mt == media {
				return true
			}
		}
		return false
	}
	// (a) three files of 60 KiB
	d, doc := run("a", nil, file{body: body(60*k, "a")}, file{body: body(60*k, "b")}, file{body: body(60*k, "c")})
	d.wantRow(doc, 0, "VERIFIED", nil)
	d.wantRow(doc, 1, "FAILED", "TOTAL_BYTES")
	d.wantRow(doc, 2, "NOT_ATTEMPTED", "TOTAL_BYTES")
	if requested(d, "a/f3") || doc["stoppedBy"] != "TOTAL_BYTES" || teaPath(doc, "summary.bytesReceived").(float64) > 100*k {
		t.Errorf("(a) %v %v", doc["stoppedBy"], doc["summary"])
	}
	// (b) the first fails its checksum: its bytes count
	d, doc = run("b", nil, file{body: body(60*k, "a"), wrong: true}, file{body: body(60*k, "b")}, file{body: body(60*k, "c")})
	d.wantRow(doc, 0, "FAILED", "CHECKSUM_MISMATCH")
	d.wantRow(doc, 1, "FAILED", "TOTAL_BYTES")
	// (c) two files fill the bound exactly; the third is never requested
	d, doc = run("c", nil, file{body: body(50*k, "a")}, file{body: body(50*k, "b")}, file{body: body(10, "c")})
	d.wantRow(doc, 0, "VERIFIED", nil)
	d.wantRow(doc, 1, "VERIFIED", nil)
	d.wantRow(doc, 2, "NOT_ATTEMPTED", "TOTAL_BYTES")
	if requested(d, "a/f3") || teaPath(doc, "summary.bytesReceived") != float64(100*k) || doc["stoppedBy"] != "TOTAL_BYTES" {
		t.Errorf("(c) %v %v", doc["stoppedBy"], doc["summary"])
	}
	// (d) 60 KiB then exactly 40 KiB; then 40 KiB and one byte
	d, doc = func() (*teaDL, map[string]any) {
		d := newTeaDL(t)
		d.art(teaArtProd, 1, "BOM", "", d.serverBody(teaArtProd, 1, "a/f1", body(60*k, "a")), d.serverBody(teaArtProd, 1, "a/f2", body(40*k, "b")))
		d.r.want(d.run("--max-total-bytes", strconv.Itoa(100*k)), teaExitOK)
		return d, d.r.doc()
	}()
	d.wantRow(doc, 1, "VERIFIED", nil)
	if teaPath(doc, "summary.bytesReceived") != float64(100*k) {
		t.Errorf("(d) %v", doc["summary"])
	}
	d, doc = run("d+1", nil, file{body: body(60*k, "a")}, file{body: body(40*k+1, "b")})
	d.wantRow(doc, 0, "VERIFIED", nil)
	d.wantRow(doc, 1, "FAILED", "TOTAL_BYTES")
	// (e) the tie: the artifact bound equals what is left
	d, doc = run("e", []string{"--max-artifact-bytes", strconv.Itoa(50 * k)},
		file{body: body(50*k, "a")}, file{body: body(50*k+1, "b"), chunked: true}, file{body: body(10, "c")})
	d.wantRow(doc, 0, "VERIFIED", nil)
	d.wantRow(doc, 1, "FAILED", "ARTIFACT_BYTES")
	d.wantRow(doc, 2, "NOT_ATTEMPTED", "TOTAL_BYTES")
	if requested(d, "a/f3") || doc["stoppedBy"] != "TOTAL_BYTES" {
		t.Errorf("(e) %v", doc["stoppedBy"])
	}
	// (f) one file of exactly the bound that fails its checksum
	d, doc = run("f", nil, file{body: body(100*k, "a"), wrong: true}, file{body: body(10, "b")})
	d.wantRow(doc, 0, "FAILED", "CHECKSUM_MISMATCH")
	d.wantRow(doc, 1, "NOT_ATTEMPTED", "TOTAL_BYTES")
	if requested(d, "a/f2") {
		t.Errorf("(f) the second file was requested")
	}
}

func TestTeaMachineDownloadFileCount(t *testing.T) { // T-24
	d := newTeaDL(t)
	var formats []string
	for i := 1; i <= 5; i++ {
		formats = append(formats, d.serverBody(teaArtProd, 1, "a/f"+strconv.Itoa(i), "x"))
	}
	d.art(teaArtProd, 1, "BOM", "", formats...)
	d.r.wantFail(d.run("--max-files", "4"), teaExitLimit, teaReasonFileCount)
	if _, err := os.Lstat(d.dir); !os.IsNotExist(err) {
		t.Errorf("the directory was created")
	}
	if d.count(d.downloadPath(teaArtProd, 1)) != 0 {
		t.Errorf("a download was requested")
	}
}

func TestTeaMachineDownloadDeadline(t *testing.T) { // T-25
	d := newTeaDL(t)
	stall := func(w http.ResponseWriter, q *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("start"))
		w.(http.Flusher).Flush()
		<-q.Context().Done()
	}
	d.art(teaArtProd, 1, "BOM", "",
		d.serverBody(teaArtProd, 1, "a/f1", "one"),
		d.server(teaArtProd, 1, "a/f2", stall, teaSums("SHA-256", teaSumSHA256("x"))),
		d.serverBody(teaArtProd, 1, "a/f3", "three"))
	start := time.Now()
	d.r.want(d.run("--deadline", "1s"), teaExitPartial)
	doc := d.r.doc()
	d.wantRow(doc, 0, "VERIFIED", nil)
	d.wantRow(doc, 1, "FAILED", "DEADLINE")
	d.wantRow(doc, 2, "NOT_ATTEMPTED", "DEADLINE")
	if doc["stoppedBy"] != "DEADLINE" || time.Since(start) > 10*time.Second {
		t.Errorf("stoppedBy %v after %s", doc["stoppedBy"], time.Since(start))
	}
	if got := strings.Join(d.files(), ","); got != "000001.bin" {
		t.Errorf("directory %s", got)
	}
	for _, q := range d.f.requests() {
		if mt, _ := urlQueryValue(q.rawQuery, "mediaType"); mt == "a/f3" {
			t.Errorf("the third file was requested")
		}
	}
}

func TestTeaMachineDownloadPerFileAnswers(t *testing.T) { // T-26
	d := newTeaDL(t)
	answer := func(a teaAnswer) http.HandlerFunc {
		return func(w http.ResponseWriter, q *http.Request) { writeTeaAnswer(w, a) }
	}
	sums := teaSums("SHA-256", teaSumSHA256("x"))
	d.art(teaArtProd, 1, "BOM", "",
		d.server(teaArtProd, 1, "a/404", answer(teaTEAError(http.StatusNotFound, "OBJECT_UNKNOWN")), sums),
		d.server(teaArtProd, 1, "a/406", answer(teaTEAError(http.StatusNotAcceptable, "NO_ACCEPTABLE_FORMAT")), sums),
		d.server(teaArtProd, 1, "a/401", answer(teaAnswer{status: http.StatusUnauthorized}), sums),
		d.server(teaArtProd, 1, "a/500", answer(teaTEAError(http.StatusInternalServerError, "X")), sums),
		d.serverBody(teaArtProd, 1, "a/ok", "x"))
	d.r.want(d.run(), teaExitPartial)
	doc := d.r.doc()
	for i, reason := range []string{"NOT_FOUND", "NO_ACCEPTABLE_FORMAT", "AUTH", "SERVER_ERROR"} {
		d.wantRow(doc, i, "FAILED", reason)
	}
	d.wantRow(doc, 4, "VERIFIED", nil)
}

func TestTeaMachineDownloadContentCoding(t *testing.T) { // T-27
	body := strings.Repeat("compressible ", 1000)
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	_, _ = zw.Write([]byte(body))
	_ = zw.Close()
	encoded := func(coding string, b []byte) http.HandlerFunc {
		return func(w http.ResponseWriter, q *http.Request) {
			if coding == "gzip" && !strings.Contains(q.Header.Get("Accept-Encoding"), "gzip") {
				t.Errorf("the transport did not ask for gzip")
			}
			w.Header().Set("Content-Encoding", coding)
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(b)
		}
	}
	sums := teaSums("SHA-256", teaSumSHA256(body))
	// (a) br; (b) gzip, verified over the decoded bytes
	d := newTeaDL(t)
	d.art(teaArtProd, 1, "BOM", "",
		d.server(teaArtProd, 1, "a/br", encoded("br", []byte(body)), sums),
		d.server(teaArtProd, 1, "a/gz", encoded("gzip", gz.Bytes()), sums))
	d.r.want(d.run(), teaExitPartial)
	doc := d.r.doc()
	d.wantRow(doc, 0, "FAILED", "INVALID_RESPONSE")
	d.wantRow(doc, 1, "VERIFIED", nil)
	if d.row(doc, 1, "sizeBytes") != float64(len(body)) {
		t.Errorf("(b) sizeBytes %v, want %d", d.row(doc, 1, "sizeBytes"), len(body))
	}
	// (c) the decoded bytes over the artifact bound
	d = newTeaDL(t)
	d.art(teaArtProd, 1, "BOM", "", d.server(teaArtProd, 1, "a/gz", encoded("gzip", gz.Bytes()), sums))
	d.r.want(d.run("--max-artifact-bytes", strconv.Itoa(len(body)-1)), teaExitPartial)
	d.wantRow(d.r.doc(), 0, "FAILED", "ARTIFACT_BYTES")
}

func TestTeaMachineDownloadCollectionVersion(t *testing.T) { // T-28
	d := newTeaDL(t)
	d.art(teaArtProd, 1, "BOM", "", d.serverBody(teaArtProd, 1, "a/json", "x"))
	d.f.route(d.f.api("/componentRelease/"+teaCR1+"/collection/2"), teaJSON(`{}`))
	r := d.r
	r.wantFail(r.exec(teaVerbDownload, "--api-root", d.f.root, "--component-release", teaCR1, "--collection-version", "3", "--dir", d.dir), teaExitNotResolved, teaReasonNotFound)
	if _, err := os.Lstat(d.dir); !os.IsNotExist(err) {
		t.Errorf("the directory was created")
	}
	r.wantFail(r.exec(teaVerbDownload, "--api-root", d.f.root, "--component-release", teaCR1, "--dir", d.dir), teaExitUsage, teaReasonUsage)
	r.wantFail(r.exec(teaVerbDownload, "--api-root", d.f.root, "--component-release", teaCR1, "--collection-version", "0", "--dir", d.dir), teaExitUsage, teaReasonUsage)
}
