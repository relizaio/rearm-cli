package cmd

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"

	tea "github.com/oolong-project/tea-client-go"
	"github.com/oolong-project/tea-client-go/tei"
)

// The tea commands against in-process TEA 1.0.0 fixtures (teaFixture_test.go). Row names
// follow the design of task TPF-175 (architecture section 8).

// teaSimple is a plain-http fixture on tea.example.com resolving to teaPR1, with a product
// release that has no components.
func teaSimple(t *testing.T, row *teaRow) *teaFixture {
	f := newTeaFixture(t, row.net, "tea.example.com", false)
	f.route(f.api("/discovery"), teaJSON(teaDiscoveryJSON(f.root, teaPR1)))
	f.route(f.api("/productRelease/"+teaPR1), teaJSON(teaProductReleaseJSON(teaPR1, `"Alpha"`, `"1.0"`, "", "")))
	f.route(f.api("/productRelease/"+teaPR1+"/cle"), teaJSON(`{"events":[]}`))
	return f
}

// teaTwoResults makes discovery on f answer teaPR1 and teaPR2.
func teaTwoResults(f *teaFixture) {
	f.route(f.api("/discovery"), teaJSON(teaDiscoveryJSON(f.root, teaPR1, teaPR2)))
	f.route(f.api("/productRelease/"+teaPR2), teaJSON(teaProductReleaseJSON(teaPR2, `"Beta"`, `"2.0"`, "", "")))
	f.route(f.api("/productRelease/"+teaPR2+"/cle"), teaJSON(`{"events":[]}`))
}

func teaLines(s string) []string {
	return strings.Split(strings.TrimRight(s, "\n"), "\n")
}

func teaNoAuthorization(t *testing.T, fs ...*teaFixture) {
	t.Helper()
	for _, f := range fs {
		for _, r := range f.requests() {
			if r.authorization != "" {
				t.Errorf("%s %s carried an Authorization header", r.method, r.path)
			}
		}
	}
}

func teaWantExit(t *testing.T, got, want teaExit, row *teaRow) {
	t.Helper()
	if got != want {
		t.Fatalf("exit %d, want %d; stdout %q; stderr %q", got, want, row.out.String(), row.errOut.String())
	}
}

// --- 8.1 Discovery and selection

func TestTeaDiscoveryOneResult(t *testing.T) { // T-1, T-10a
	row := newTeaRow(t)
	f := teaSimple(t, row)
	in := teaHTTPInputs(f, teaPR1)
	teaWantExit(t, row.discovery(in), teaExitOK, row)
	if row.out.String() != teaPR1+"\n" {
		t.Errorf("stdout %q", row.out.String())
	}
	if row.errOut.String() != "" {
		t.Errorf("stderr %q", row.errOut.String())
	}
	reqs := f.requests()
	if len(reqs) != 2 || reqs[0].path != "/.well-known/tea" || reqs[1].path != f.api("/discovery") {
		t.Fatalf("requests %+v", reqs)
	}
	q, err := url.ParseQuery(reqs[1].rawQuery)
	if err != nil || q.Get("tei") != in.tei {
		t.Errorf("discovery query %q", reqs[1].rawQuery)
	}
	teaNoAuthorization(t, f)
}

func TestTeaDiscoveryChoiceAtTerminal(t *testing.T) { // T-2
	row := newTeaRow(t)
	f := teaSimple(t, row)
	teaTwoResults(f)
	row.stdin = teaStdinTerminal
	row.in = strings.NewReader("2\n")
	teaWantExit(t, row.discovery(teaHTTPInputs(f, teaPR1)), teaExitOK, row)
	if row.out.String() != teaPR2+"\n" {
		t.Errorf("stdout %q", row.out.String())
	}
	want := "Multiple discovery results found (2 results).\n" +
		"  1) Alpha 1.0 (product release " + teaPR1 + ", server " + f.root + ")\n" +
		"  2) Beta 2.0 (product release " + teaPR2 + ", server " + f.root + ")\n" +
		"Enter your choice (number): "
	if row.errOut.String() != want {
		t.Errorf("stderr\n%q\nwant\n%q", row.errOut.String(), want)
	}
	if f.count(f.api("/productRelease/"+teaPR1)) != 1 || f.count(f.api("/productRelease/"+teaPR2)) != 1 {
		t.Errorf("product release requests %+v", f.requests())
	}
}

func TestTeaDiscoveryNoTerminalNeverReads(t *testing.T) { // T-3
	row := newTeaRow(t)
	f := teaSimple(t, row)
	teaTwoResults(f)
	row.stdin = teaStdinOther
	row.in = failingReader{t}
	teaWantExit(t, row.discovery(teaHTTPInputs(f, teaPR1)), teaExitAmbiguous, row)
	if row.out.String() != "" {
		t.Errorf("stdout %q", row.out.String())
	}
	lines := teaLines(row.errOut.String())
	if len(lines) != 4 || !strings.HasPrefix(lines[1], "  1) Alpha") || !strings.HasPrefix(lines[2], "  2) Beta") ||
		lines[3] != "Error: the TEI resolves to 2 product releases and standard input is not a terminal; run the command in a terminal to choose one" {
		t.Errorf("stderr %q", row.errOut.String())
	}
}

func TestTeaDiscoveryInvalidChoices(t *testing.T) { // T-4
	for _, input := range []string{"", "0\n", "3\n", "abc\n", "1 1\n", strings.Repeat("7", 10<<10)} {
		row := newTeaRow(t)
		f := teaSimple(t, row)
		teaTwoResults(f)
		row.stdin = teaStdinTerminal
		row.in = strings.NewReader(input)
		teaWantExit(t, row.discovery(teaHTTPInputs(f, teaPR1)), teaExitAmbiguous, row)
		if row.out.String() != "" {
			t.Errorf("%.10q: stdout %q", input, row.out.String())
		}
		var errLines []string
		for _, l := range teaLines(row.errOut.String()) {
			if strings.Contains(l, "Error:") {
				errLines = append(errLines, l)
			}
		}
		if len(errLines) != 1 || !strings.Contains(errLines[0], "from 1 to 2") {
			t.Errorf("%.10q: stderr %q", input, row.errOut.String())
		}
	}
}

func TestTeaDiscoveryNotResolved(t *testing.T) { // T-5
	row := newTeaRow(t)
	f := teaSimple(t, row)
	f.route(f.api("/discovery"), teaTEAError(http.StatusNotFound, "OBJECT_UNKNOWN"))
	teaWantExit(t, row.discovery(teaHTTPInputs(f, teaPR1)), teaExitNotResolved, row)
	lines := teaLines(row.errOut.String())
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "Error: TEI not resolved") {
		t.Errorf("stderr %q", row.errOut.String())
	}
	if n := f.count(f.api("/discovery")); n != 1 {
		t.Errorf("%d discovery requests, want 1", n)
	}
}

func TestTeaDiscoveryAttemptsUsedUp(t *testing.T) { // T-6
	for _, a := range []teaAnswer{
		{status: http.StatusNotFound, contentType: "text/html", body: "<html>not here</html>"},
		{status: http.StatusServiceUnavailable},
	} {
		row := newTeaRow(t)
		f := teaSimple(t, row)
		f.route(f.api("/discovery"), a)
		teaWantExit(t, row.discovery(teaHTTPInputs(f, teaPR1)), teaExitFailed, row)
		if lines := teaLines(row.errOut.String()); len(lines) != 1 || !strings.HasPrefix(lines[0], "Error: ") {
			t.Errorf("stderr %q", row.errOut.String())
		}
		if n := len(f.requests()); n != 4 {
			t.Errorf("%d requests, want 4", n)
		}
	}
}

const teaLegacyLine = "Error: the urn:tei: form is no longer accepted; write the TEI as tei://<domain>/<type>/<id>\n"

func TestTeaLegacyFormRefused(t *testing.T) { // T-7, T-40 in-process
	for _, s := range []string{
		"urn:tei:uuid:tea.example.com:" + teaPR1,
		"URN:TEI:uuid:tea.example.com:" + teaPR1,
		"urn:tei:purl:tea.example.com:pkg:npm/x@1",
	} {
		for _, run := range []func(*teaRow, teaInputs) teaExit{(*teaRow).discovery, (*teaRow).flow} {
			row := newTeaRow(t)
			f := teaSimple(t, row)
			in := teaHTTPInputs(f, teaPR1)
			in.tei = s
			teaWantExit(t, run(row, in), teaExitUsage, row)
			if row.errOut.String() != teaLegacyLine || row.out.String() != "" {
				t.Errorf("%s: stdout %q stderr %q", s, row.out.String(), row.errOut.String())
			}
			if len(f.requests()) != 0 || len(row.net.dialedAddrs()) != 0 {
				t.Errorf("%s: something was requested", s)
			}
		}
	}
}

func TestTeaInvalidTEI(t *testing.T) { // T-8
	for _, s := range []string{"", "tei://127.0.0.1/uuid/x", "tei://tea.example.com/uuid/a b", "tei://tea.example.com/uuid", "https://tea.example.com/x"} {
		row := newTeaRow(t)
		in := teaInputs{tei: s}
		teaWantExit(t, row.discovery(in), teaExitUsage, row)
		lines := teaLines(row.errOut.String())
		if len(lines) != 1 || !strings.HasPrefix(lines[0], "Error: invalid TEI:") {
			t.Errorf("%q: stderr %q", s, row.errOut.String())
		}
		if len(row.net.dialedAddrs()) != 0 {
			t.Errorf("%q: dialled %v", s, row.net.dialedAddrs())
		}
	}
}

// TestTeaExitThroughExecute runs the real Execute in a child of the test binary, because
// the commands end the process (T-9).
func TestTeaExitThroughExecute(t *testing.T) {
	if args := os.Getenv("REARM_TEST_TEA_ARGS"); args != "" {
		var argv []string
		if err := json.Unmarshal([]byte(args), &argv); err != nil {
			os.Exit(99)
		}
		rootCmd.SetArgs(argv)
		Execute()
		os.Exit(0)
	}
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "REARM_") && !strings.HasPrefix(kv, "HOME=") {
			env = append(env, kv)
		}
	}
	for _, c := range []struct {
		args   []string
		code   int
		stderr string
	}{
		{[]string{"tea", "discovery", "--tei", "urn:tei:uuid:a.example.com:x"}, 2, teaLegacyLine},
		{[]string{"tea", "discovery"}, 1, `required flag(s) "tei" not set`},
	} {
		argv, _ := json.Marshal(c.args)
		cmd := exec.Command(os.Args[0], "-test.run=^TestTeaExitThroughExecute$")
		cmd.Env = append(append([]string(nil), env...), "HOME="+t.TempDir(), "REARM_TEST_TEA_ARGS="+string(argv))
		var out, errOut strings.Builder
		cmd.Stdout, cmd.Stderr = &out, &errOut
		err := cmd.Run()
		code := 0
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		if code != c.code || !strings.Contains(errOut.String(), c.stderr) || out.String() != "" {
			t.Errorf("%v: exit %d stdout %q stderr %q", c.args, code, out.String(), errOut.String())
		}
		if c.code == 2 && errOut.String() != teaLegacyLine {
			t.Errorf("%v: stderr %q, want only the legacy line", c.args, errOut.String())
		}
	}
}

// --- 8.2 The http flags and the URL policy

func TestTeaHTTPEndpointOnOtherOriginRefused(t *testing.T) { // T-10b
	row := newTeaRow(t)
	f := teaSimple(t, row)
	other := newTeaFixture(t, row.net, "other.example.com", false)
	f.route("/.well-known/tea", teaJSON(`{"schemaVersion":1,"endpoints":[{"url":"`+other.root+`","versions":["1.0.0"]}]}`))
	teaWantExit(t, row.discovery(teaHTTPInputs(f, teaPR1)), teaExitRefusedURL, row)
	if lines := teaLines(row.errOut.String()); len(lines) != 1 || !strings.HasPrefix(lines[0], "Error: refused by the URL policy") ||
		!strings.Contains(lines[0], "only the origin given with --usehttp and --useport") {
		t.Errorf("stderr %q", row.errOut.String())
	}
	if len(other.requests()) != 0 {
		t.Errorf("the other origin was asked: %+v", other.requests())
	}
}

func TestTeaHTTPServerRootOnOtherOriginRefused(t *testing.T) { // T-10c
	row := newTeaRow(t)
	f := teaSimple(t, row)
	other := newTeaFixture(t, row.net, "other.example.com", false)
	f.route(f.api("/discovery"), teaJSON(teaDiscoveryJSON(other.root, teaPR1)))
	teaWantExit(t, row.flow(teaHTTPInputs(f, teaPR1)), teaExitRefusedURL, row)
	if len(other.requests()) != 0 {
		t.Errorf("the other origin was asked: %+v", other.requests())
	}
}

func TestTeaNoHTTPWithoutUseHTTP(t *testing.T) { // T-10d
	row := newTeaRow(t)
	secure := newTeaFixture(t, row.net, "tea.example.com", true)
	plain := newTeaFixture(t, row.net, "plain.example.com", false)
	secure.route("/.well-known/tea", teaJSON(`{"schemaVersion":1,"endpoints":[{"url":"`+plain.root+`","versions":["1.0.0"]}]}`))
	in := teaHTTPInputs(secure, teaPR1)
	in.useHTTP = false
	teaWantExit(t, row.discovery(in), teaExitRefusedURL, row)
	if secure.count("/.well-known/tea") == 0 {
		t.Errorf("the well-known request did not arrive over TLS")
	}
	if len(plain.requests()) != 0 {
		t.Errorf("plain http was used: %+v", plain.requests())
	}
}

func teaDialedFor(t *testing.T, in teaInputs) []string {
	row := newTeaRow(t)
	row.discovery(in)
	return row.net.dialedAddrs()
}

func TestTeaHTTPWithoutPortUses80(t *testing.T) { // T-10e
	got := teaDialedFor(t, teaInputs{tei: "tei://tea.example.com/uuid/" + teaPR1, useHTTP: true, usePort: 443})
	if len(got) == 0 || got[0] != "tea.example.com:80" {
		t.Errorf("dialled %v", got)
	}
}

func TestTeaInvalidPort(t *testing.T) { // T-10f
	for _, p := range []int{0, 70000} {
		row := newTeaRow(t)
		teaWantExit(t, row.discovery(teaInputs{tei: "tei://tea.example.com/uuid/" + teaPR1, usePort: p, portSet: true}), teaExitUsage, row)
		if len(row.net.dialedAddrs()) != 0 {
			t.Errorf("%d: dialled %v", p, row.net.dialedAddrs())
		}
	}
}

func TestTeaNoFlagUsesHTTPS443(t *testing.T) { // T-10g
	got := teaDialedFor(t, teaInputs{tei: "tei://tea.example.com/uuid/" + teaPR1, usePort: 443})
	if len(got) == 0 || got[0] != "tea.example.com:443" {
		t.Errorf("dialled %v", got)
	}
}

func TestTeaRedirectDoesNotWidenHTTP(t *testing.T) { // T-10h
	row := newTeaRow(t)
	f := teaSimple(t, row)
	other := newTeaFixture(t, row.net, "other.example.com", false)
	f.route(f.api("/productRelease/"+teaPR1), teaAnswer{status: http.StatusFound, location: other.root + "/v1.0.0/productRelease/" + teaPR1})
	teaWantExit(t, row.flow(teaHTTPInputs(f, teaPR1)), teaExitRefusedURL, row)
	if len(other.requests()) != 0 {
		t.Errorf("the redirect was followed: %+v", other.requests())
	}
}

// --- 8.3 The flow and its output

// teaFlowFixture is the T-11 data: a product release with a name, a version, a CLE of two
// events setting every optional field and one product-level artifact; component 1 pinned
// with a CLE and two formats (one with url and signatureUrl, one with neither); component
// 2 unpinned, its newest release read from the listing, with no artifact.
func teaFlowFixture(t *testing.T, row *teaRow, extraComps string) *teaFixture {
	f := newTeaFixture(t, row.net, "tea.example.com", false)
	f.route(f.api("/discovery"), teaJSON(teaDiscoveryJSON(f.root, teaPR1)))
	prodArt := `{"uuid":"` + teaArtProd + `","version":3,"name":"Product SBOM","type":"BOM","createdDate":"2026-01-15T00:00:00Z","formats":[` +
		`{"mediaType":"application/vnd.cyclonedx+json","description":"CycloneDX JSON","url":"https://cdn.example.com/p.json","checksums":[{"algType":"SHA-256","algValue":"` + teaSHA256 + `"}]}]}`
	comps := `{"uuid":"` + teaComp1 + `","release":"` + teaCR1 + `"},{"uuid":"` + teaComp2 + `"}` + extraComps
	f.route(f.api("/productRelease/"+teaPR1), teaJSON(teaProductReleaseJSON(teaPR1, `"Sample Product"`, `"1.0.0"`, comps, prodArt)))
	f.route(f.api("/productRelease/"+teaPR1+"/cle"), teaJSON(`{"events":[`+
		`{"id":2,"type":"released","effective":"2026-01-15T00:00:00Z","published":"2026-01-15T00:00:00Z","version":"1.0.0","license":"Apache-2.0","description":"First \"GA\" release","references":["https://example.com/notes","https://example.com/more"]},`+
		`{"id":1,"type":"supersededBy","effective":"2026-01-10T00:00:00Z","published":"2026-01-11T12:30:00.5Z","versions":[{"version":"0.9.0"},{"range":"vers:generic/<0.9.0"}],"supportId":"standard","supersededByVersion":"1.0.0","reason":"replaced"}]}`))
	compArt := `{"uuid":"` + teaArtComp1 + `","version":2,"name":"Build SBOM","type":"BOM","createdDate":"2026-01-15T00:00:00Z","formats":[` +
		`{"mediaType":"application/vnd.cyclonedx+json","description":"CycloneDX JSON","url":"https://cdn.example.com/c.json","signatureUrl":"https://cdn.example.com/c.json.sig","checksums":[{"algType":"SHA-256","algValue":"` + teaSHA256 + `"}]},` +
		`{"mediaType":"application/vnd.cyclonedx+xml"}]},` +
		`{"uuid":"` + teaArtComp1 + `","version":1,"type":"LICENSE","createdDate":"2026-01-15T00:00:00Z","formats":[{"mediaType":"text/plain","description":"License"}]}`
	f.route(f.api("/componentRelease/"+teaCR1), teaJSON(teaComponentReleaseJSON(teaCR1, teaComp1, `"Core Lib"`, "2.0.0", compArt)))
	f.route(f.api("/componentRelease/"+teaCR1+"/cle"), teaJSON(`{"events":[{"id":1,"type":"endOfLife","effective":"2027-01-01T00:00:00Z","published":"2026-01-15T00:00:00Z"}]}`))
	f.route(f.api("/component/"+teaComp2), teaJSON(`{"uuid":"`+teaComp2+`","name":"Free Part","identifiers":[]}`))
	f.route(f.api("/component/"+teaComp2+"/releases"), teaJSON(`{"timestamp":"2026-01-15T00:00:00Z","pageStartIndex":0,"pageSize":1,"totalResults":2,"hasNext":true,"nextPageToken":"t",`+
		`"results":[{"uuid":"`+teaCR2+`","component":"`+teaComp2+`","version":"3.1.0","createdDate":"2026-01-15T00:00:00Z"}]}`))
	f.route(f.api("/componentRelease/"+teaCR2), teaJSON(teaComponentReleaseJSON(teaCR2, teaComp2, "", "3.1.0", "")))
	f.route(f.api("/componentRelease/"+teaCR2+"/cle"), teaJSON(`{"events":[]}`))
	return f
}

func teaGoldenFlow(f *teaFixture) string {
	return `
=== Product Information ===
Product Name: Sample Product
Version: 1.0.0
  Lifecycle Events (CLE):
    [2] type=released effective=2026-01-15T00:00:00Z published=2026-01-15T00:00:00Z version=1.0.0 license=Apache-2.0 description="First \"GA\" release" references=https://example.com/notes,https://example.com/more
    [1] type=supersededBy effective=2026-01-10T00:00:00Z published=2026-01-11T12:30:00.5Z versions=0.9.0,vers:generic/<0.9.0 supportId=standard supersededBy=1.0.0 reason="replaced"

  Artifact: Product SBOM (BOM)
    - Description: CycloneDX JSON
      Media Type: application/vnd.cyclonedx+json
      URL: https://cdn.example.com/p.json


--- Component: Core Lib ---
Version: 2.0.0
  Lifecycle Events (CLE):
    [1] type=endOfLife effective=2027-01-01T00:00:00Z published=2026-01-15T00:00:00Z

  Artifact: Build SBOM (BOM)
    - Description: CycloneDX JSON
      Media Type: application/vnd.cyclonedx+json
      URL: https://cdn.example.com/c.json
      Signature URL: https://cdn.example.com/c.json.sig
    - Description: ` + `
      Media Type: application/vnd.cyclonedx+xml
      URL: ` + f.root + `/v1.0.0/artifact/` + teaArtComp1 + `/2/download?mediaType=application%2Fvnd.cyclonedx%2Bxml

  Artifact: LICENSE
    - Description: License
      Media Type: text/plain
      URL: ` + f.root + `/v1.0.0/artifact/` + teaArtComp1 + `/1/download?mediaType=text%2Fplain
Note: Component 'Free Part' does not have a pinned release. Selecting latest available release.

--- Component: Free Part ---
Version: 3.1.0
  No artifacts in the latest collection of this component release.
`
}

func TestTeaFlowGoldenOutput(t *testing.T) { // T-11, T-16
	row := newTeaRow(t)
	f := teaFlowFixture(t, row, "")
	teaWantExit(t, row.flow(teaHTTPInputs(f, teaPR1)), teaExitOK, row)
	if got, want := row.out.String(), teaGoldenFlow(f); got != want {
		t.Errorf("stdout\n%s\nwant\n%s", got, want)
	}
	if row.errOut.String() != "" {
		t.Errorf("stderr %q", row.errOut.String())
	}
}

func TestTeaFlowUnpinnedTakesNewest(t *testing.T) { // T-12
	row := newTeaRow(t)
	f := teaFlowFixture(t, row, "")
	teaWantExit(t, row.flow(teaHTTPInputs(f, teaPR1)), teaExitOK, row)
	var listing *teaRecorded
	for _, r := range f.requests() {
		if r.path == f.api("/component/"+teaComp2+"/releases") {
			listing = &r
		}
	}
	if listing == nil {
		t.Fatal("no listing request")
	}
	q, _ := url.ParseQuery(listing.rawQuery)
	if q.Get("pageSize") != "1" || q.Get("sortField") != "createdDate" || q.Get("sortOrder") != "desc" {
		t.Errorf("listing query %q", listing.rawQuery)
	}
	if f.count(f.api("/componentRelease/"+teaCR2)) != 1 ||
		!strings.Contains(row.out.String(), "Note: Component 'Free Part' does not have a pinned release.") {
		t.Errorf("stdout %q", row.out.String())
	}
}

func TestTeaFlowComponentFailures(t *testing.T) { // T-13
	row := newTeaRow(t)
	extra := `,{"uuid":"` + teaComp3 + `","release":"` + teaCR3 + `"},{"uuid":"` + teaComp4 + `","release":"` + teaCR4 + `"}`
	f := teaFlowFixture(t, row, extra)
	f.route(f.api("/componentRelease/"+teaCR4), teaJSON(teaComponentReleaseJSON(teaCR4, teaComp4, `"Four"`, "4.0.0", "")))
	f.route(f.api("/componentRelease/"+teaCR4+"/cle"), teaAnswer{status: http.StatusInternalServerError})
	teaWantExit(t, row.flow(teaHTTPInputs(f, teaPR1)), teaExitPartial, row)
	out := row.out.String()
	for _, s := range []string{"=== Product Information ===", "--- Component: Core Lib ---", "--- Component: Free Part ---", "--- Component: Four ---\nVersion: 4.0.0\n  No artifacts"} {
		if !strings.Contains(out, s) {
			t.Errorf("stdout lacks %q", s)
		}
	}
	if strings.Contains(out, teaCR3) || strings.Count(out, "Lifecycle Events") != 2 {
		t.Errorf("stdout %q", out)
	}
	lines := teaLines(row.errOut.String())
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "Warning: component "+teaComp3+": ") || !strings.HasPrefix(lines[1], "Warning: component "+teaComp4+": ") {
		t.Errorf("stderr %q", row.errOut.String())
	}
}

func TestTeaFlowLifecycleAnswers(t *testing.T) { // T-14
	for _, c := range []struct {
		cle  teaAnswer
		code teaExit
	}{
		{teaJSON(`{"events":[]}`), teaExitOK},
		{teaTEAError(http.StatusNotFound, "NOT_IMPLEMENTED"), teaExitOK},
		{teaAnswer{status: http.StatusInternalServerError}, teaExitFailed},
	} {
		row := newTeaRow(t)
		f := teaSimple(t, row)
		f.route(f.api("/productRelease/"+teaPR1+"/cle"), c.cle)
		teaWantExit(t, row.flow(teaHTTPInputs(f, teaPR1)), c.code, row)
		if c.code == teaExitOK && strings.Contains(row.out.String(), "Lifecycle Events") {
			t.Errorf("stdout %q", row.out.String())
		}
		if c.code != teaExitOK && row.out.String() != "" {
			t.Errorf("stdout %q", row.out.String())
		}
	}
}

func TestTeaFlowTimestampsUTC(t *testing.T) { // T-15
	row := newTeaRow(t)
	f := teaSimple(t, row)
	f.route(f.api("/productRelease/"+teaPR1+"/cle"), teaJSON(`{"events":[{"id":1,"type":"released","effective":"2026-01-15T10:00:00+02:00","published":"2026-01-15T10:00:00+02:00"}]}`))
	teaWantExit(t, row.flow(teaHTTPInputs(f, teaPR1)), teaExitOK, row)
	if !strings.Contains(row.out.String(), "effective=2026-01-15T08:00:00Z published=2026-01-15T08:00:00Z") {
		t.Errorf("stdout %q", row.out.String())
	}
}

func TestTeaFlowChoice(t *testing.T) { // T-17
	row := newTeaRow(t)
	f := teaSimple(t, row)
	teaTwoResults(f)
	row.stdin = teaStdinTerminal
	row.in = strings.NewReader("1\n")
	teaWantExit(t, row.flow(teaHTTPInputs(f, teaPR1)), teaExitOK, row)
	if !strings.Contains(row.out.String(), "Product Name: Alpha\nVersion: 1.0\n") {
		t.Errorf("stdout %q", row.out.String())
	}
	if n := f.count(f.api("/productRelease/" + teaPR1)); n != 1 {
		t.Errorf("%d requests for the chosen release, want 1 (the candidate's is reused)", n)
	}

	row = newTeaRow(t)
	f = teaSimple(t, row)
	teaTwoResults(f)
	row.in = failingReader{t}
	teaWantExit(t, row.flow(teaHTTPInputs(f, teaPR1)), teaExitAmbiguous, row)
	if row.out.String() != "" {
		t.Errorf("stdout %q", row.out.String())
	}
}

func TestTeaFlowNotResolved(t *testing.T) { // T-18
	for _, setup := range []func(*teaFixture){
		func(f *teaFixture) { f.route(f.api("/discovery"), teaTEAError(http.StatusNotFound, "OBJECT_UNKNOWN")) },
		func(f *teaFixture) {
			f.route(f.api("/productRelease/"+teaPR1), teaTEAError(http.StatusNotFound, "OBJECT_UNKNOWN"))
		},
	} {
		row := newTeaRow(t)
		f := teaSimple(t, row)
		setup(f)
		teaWantExit(t, row.flow(teaHTTPInputs(f, teaPR1)), teaExitNotResolved, row)
		if row.out.String() != "" {
			t.Errorf("stdout %q", row.out.String())
		}
	}
}

// teaHostileName carries ESC [2J, CR, LF and a forged header line, BEL, U+202E and U+009B.
const teaHostileName = "Evil\x1b[2J\r\n=== Product Information ===\a\u202e\u009b"

func teaCheckClean(t *testing.T, what, s string) {
	t.Helper()
	for _, r := range s {
		if (r < 0x20 && r != '\n') || r == 0x7f || r == 0x202e || r == 0x9b {
			t.Errorf("%s carries %U", what, r)
		}
	}
}

func TestTeaHostileText(t *testing.T) { // T-19
	nameJSON, _ := json.Marshal(teaHostileName)
	longVersion, _ := json.Marshal(strings.Repeat("v", 100000))

	// the flow, with the name on the product and on a component whose read fails
	row := newTeaRow(t)
	f := teaSimple(t, row)
	comps := `{"uuid":"` + teaComp1 + `","release":"` + teaCR1 + `"}`
	f.route(f.api("/productRelease/"+teaPR1), teaJSON(teaProductReleaseJSON(teaPR1, string(nameJSON), string(longVersion), comps, "")))
	f.route(f.api("/componentRelease/"+teaCR1), teaJSON(teaComponentReleaseJSON(teaCR1, teaComp1, string(nameJSON), "1", "")))
	f.route(f.api("/componentRelease/"+teaCR1+"/cle"), teaTEAError(http.StatusBadRequest, "INVALID_REQUEST"))
	teaWantExit(t, row.flow(teaHTTPInputs(f, teaPR1)), teaExitPartial, row)
	out := row.out.String()
	teaCheckClean(t, "stdout", out)
	teaCheckClean(t, "stderr", row.errOut.String())
	if strings.Count(out, "\n=== Product Information ===") != 1 {
		t.Errorf("a forged header line reached stdout: %q", out)
	}
	escaped := `Evil\x1b[2J\r\n=== Product Information ===\a\u202e\u009b`
	if !strings.Contains(out, "Product Name: "+escaped+"\n") || !strings.Contains(out, "--- Component: "+escaped+" ---\n") {
		t.Errorf("the name is not escaped on one line: %q", out)
	}
	if !strings.Contains(out, "Version: "+strings.Repeat("v", teaTextMax)+"...\n") {
		t.Errorf("the version was not cut at %d", teaTextMax)
	}

	// the candidate list, not a terminal
	row = newTeaRow(t)
	f = teaSimple(t, row)
	teaTwoResults(f)
	f.route(f.api("/productRelease/"+teaPR2), teaJSON(teaProductReleaseJSON(teaPR2, string(nameJSON), `"2"`, "", "")))
	teaWantExit(t, row.discovery(teaHTTPInputs(f, teaPR1)), teaExitAmbiguous, row)
	teaCheckClean(t, "stderr", row.errOut.String())
	if !strings.Contains(row.errOut.String(), "  2) "+escaped+" 2 (product release") {
		t.Errorf("stderr %q", row.errOut.String())
	}
}

func TestTeaTextEscapes(t *testing.T) { // T-20
	for _, c := range []struct{ in, want string }{
		{"plain ASCII 1.0.0 (x)", "plain ASCII 1.0.0 (x)"},
		{"Gr\u00fc\u00dfe \u65e5\u672c", "Gr\u00fc\u00dfe \u65e5\u672c"},
		{"a\x1bb", `a\x1bb`},
		{"a\rb\nc\td", `a\rb\nc\td`},
		{"\x00\x07\x7f", `\x00\a\x7f`},
		{"x\u009by", `x\u009by`},
		{"x\u202ey\u200bz", `x\u202ey\u200bz`},
		{"a\u00a0b", `a\u00a0b`},
		{"bad\xff\xfeutf8", `bad\xff\xfeutf8`},
		{"\xc3", `\xc3`},
		{strings.Repeat("a", teaTextMax), strings.Repeat("a", teaTextMax)},
		{strings.Repeat("a", teaTextMax+1), strings.Repeat("a", teaTextMax) + "..."},
		{strings.Repeat("\x1b", teaTextMax+5), strings.Repeat(`\x1b`, teaTextMax) + "..."},
	} {
		if got := teaText(c.in); got != c.want {
			t.Errorf("teaText(%.40q) = %.80q, want %.80q", c.in, got, c.want)
		}
	}
	if got := teaCut(strings.Repeat("\u00fc", teaTextMax+1)); got != strings.Repeat("\u00fc", teaTextMax)+"..." {
		t.Errorf("teaCut %d runes", len([]rune(got)))
	}
}

// --- 8.4 Credentials

const teaKeyID = "key:id %1 x"
const teaKeySecret = "s3cr:et %2 y"

// teaTokenFixture makes f issue tok-2 for the API key and require it on everything else.
func teaTokenFixture(f *teaFixture, require bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tokenKeyID, f.tokenSecret, f.tokenIssued = teaKeyID, teaKeySecret, "tok-2"
	if require {
		f.requireBearer = "tok-2"
	}
}

func teaKeyEnv(row *teaRow, f *teaFixture) {
	row.env[teaEnvAuthRoot] = f.root
	row.env[teaEnvKeyID] = teaKeyID
	row.env[teaEnvKey] = teaKeySecret
}

func teaNoSecretIn(t *testing.T, row *teaRow, secrets ...string) {
	t.Helper()
	for _, s := range secrets {
		if strings.Contains(row.out.String(), s) || strings.Contains(row.errOut.String(), s) {
			t.Errorf("output carries %q", s)
		}
	}
}

func TestTeaCredentialRootElsewhere(t *testing.T) { // T-21
	row := newTeaRow(t)
	f := teaSimple(t, row)
	row.env[teaEnvAuthRoot] = "https://elsewhere.example.com/tea"
	row.env[teaEnvToken] = "tok-1"
	teaWantExit(t, row.flow(teaHTTPInputs(f, teaPR1)), teaExitOK, row)
	teaNoAuthorization(t, f)
}

func teaCheckBearer(t *testing.T, f *teaFixture, token string) {
	t.Helper()
	for _, r := range f.requests() {
		switch {
		case r.path == "/.well-known/tea":
			if r.authorization != "" {
				t.Errorf("the well-known request carried %q", r.authorization)
			}
		case r.path == f.api("/token"):
		case strings.HasPrefix(r.path, "/tea/"):
			if r.authorization != "Bearer "+token {
				t.Errorf("%s carried %q", r.path, r.authorization)
			}
		}
	}
}

func TestTeaBearerToken(t *testing.T) { // T-22
	row := newTeaRow(t)
	f := teaFlowFixture(t, row, "")
	f.requireBearer = "tok-1"
	row.env[teaEnvAuthRoot] = f.root
	row.env[teaEnvToken] = "tok-1"
	teaWantExit(t, row.flow(teaHTTPInputs(f, teaPR1)), teaExitOK, row)
	teaCheckBearer(t, f, "tok-1")
}

// teaCheckExchange: exactly one token request, before the first discovery request, with
// the key as HTTP Basic, and the key nowhere else.
func teaCheckExchange(t *testing.T, f *teaFixture) {
	t.Helper()
	reqs := f.requests()
	tokenAt, discoveryAt := -1, -1
	for i, r := range reqs {
		if r.path == f.api("/token") {
			if tokenAt != -1 {
				t.Errorf("a second token request")
			}
			tokenAt = i
			if r.method != http.MethodPost || r.body != "grant_type=client_credentials" {
				t.Errorf("token request %s %q", r.method, r.body)
			}
			want := "Basic " + base64.StdEncoding.EncodeToString([]byte(url.QueryEscape(teaKeyID)+":"+url.QueryEscape(teaKeySecret)))
			if r.authorization != want {
				t.Errorf("token request Authorization %q, want %q", r.authorization, want)
			}
			continue
		}
		if r.path == f.api("/discovery") && discoveryAt == -1 {
			discoveryAt = i
		}
		for _, s := range []string{teaKeyID, teaKeySecret, url.QueryEscape(teaKeyID), url.QueryEscape(teaKeySecret)} {
			if strings.Contains(r.authorization+r.rawQuery+r.path+r.body, s) {
				t.Errorf("%s carried the key", r.path)
			}
		}
	}
	if tokenAt == -1 || (discoveryAt != -1 && tokenAt > discoveryAt) {
		t.Errorf("token request at %d, first discovery at %d", tokenAt, discoveryAt)
	}
}

func TestTeaAPIKeyExchange(t *testing.T) { // T-23
	row := newTeaRow(t)
	f := teaFlowFixture(t, row, "")
	teaTokenFixture(f, true)
	teaKeyEnv(row, f)
	teaWantExit(t, row.flow(teaHTTPInputs(f, teaPR1)), teaExitOK, row)
	teaCheckExchange(t, f)
	teaCheckBearer(t, f, "tok-2")
	teaNoSecretIn(t, row, teaKeyID, teaKeySecret, "tok-2")
}

func TestTeaAPIKeyTokenPresentedWithoutChallenge(t *testing.T) { // T-24
	row := newTeaRow(t)
	f := teaFlowFixture(t, row, "")
	teaTokenFixture(f, false)
	f.routeFunc(f.api("/productRelease/"+teaPR1), func(r *http.Request) teaAnswer {
		comps := `{"uuid":"` + teaComp1 + `","release":"` + teaCR1 + `"}`
		if r.Header.Get("Authorization") == "Bearer tok-2" {
			comps += `,{"uuid":"` + teaComp2 + `"}`
		}
		return teaJSON(teaProductReleaseJSON(teaPR1, `"Sample Product"`, `"1.0.0"`, comps, ""))
	})
	teaKeyEnv(row, f)
	teaWantExit(t, row.flow(teaHTTPInputs(f, teaPR1)), teaExitOK, row)
	if !strings.Contains(row.out.String(), "--- Component: Free Part ---") {
		t.Errorf("the component listed only for the token is missing: %q", row.out.String())
	}
}

func TestTeaAPIKeyProtectedDiscovery(t *testing.T) { // T-25
	row := newTeaRow(t)
	f := teaSimple(t, row)
	teaTokenFixture(f, true)
	teaKeyEnv(row, f)
	teaWantExit(t, row.discovery(teaHTTPInputs(f, teaPR1)), teaExitOK, row)
	if row.out.String() != teaPR1+"\n" {
		t.Errorf("stdout %q", row.out.String())
	}
}

func TestTeaWrongToken(t *testing.T) { // T-26a
	row := newTeaRow(t)
	f := teaSimple(t, row)
	f.requireBearer = "tok-1"
	row.env[teaEnvAuthRoot] = f.root
	row.env[teaEnvToken] = "wrong-SECRET"
	teaWantExit(t, row.discovery(teaHTTPInputs(f, teaPR1)), teaExitAuth, row)
	lines := teaLines(row.errOut.String())
	if len(lines) != 1 || !strings.Contains(lines[0], "HTTP 401") || !strings.Contains(lines[0], "only under "+f.root) ||
		!strings.Contains(lines[0], "went to "+f.root+"/v1.0.0/discovery") {
		t.Errorf("stderr %q", row.errOut.String())
	}
	teaNoSecretIn(t, row, "wrong-SECRET")
}

func TestTeaTokenEndpointAnswers(t *testing.T) { // T-26b, T-26c, T-26d
	// invalid_client: the wrong secret
	row := newTeaRow(t)
	f := teaSimple(t, row)
	teaTokenFixture(f, true)
	teaKeyEnv(row, f)
	row.env[teaEnvKey] = "wrong secret"
	teaWantExit(t, row.discovery(teaHTTPInputs(f, teaPR1)), teaExitAuth, row)
	if l := row.errOut.String(); !strings.HasPrefix(l, "Error: the TEA token endpoint refused the API key: ") || !strings.Contains(l, "invalid_client") {
		t.Errorf("stderr %q", l)
	}
	if reqs := f.requests(); len(reqs) != 1 || reqs[0].path != f.api("/token") {
		t.Errorf("requests %+v", reqs)
	}
	teaNoSecretIn(t, row, teaKeyID, "wrong secret")

	// 404: no token endpoint
	row = newTeaRow(t)
	f = teaSimple(t, row)
	teaKeyEnv(row, f)
	f.route(f.api("/token"), teaTEAError(http.StatusNotFound, "OBJECT_UNKNOWN"))
	teaWantExit(t, row.discovery(teaHTTPInputs(f, teaPR1)), teaExitAuth, row)
	if l := row.errOut.String(); l != "Error: the TEA server at "+f.root+" has no token endpoint (HTTP 404): unset REARM_TEA_APIKEYID and REARM_TEA_APIKEY to call it without a credential\n" {
		t.Errorf("stderr %q", l)
	}

	// a redirect, and a token type that is not Bearer
	for _, mac := range []bool{false, true} {
		row = newTeaRow(t)
		f = teaSimple(t, row)
		other := newTeaFixture(t, row.net, "other.example.com", false)
		teaKeyEnv(row, f)
		if mac {
			f.route(f.api("/token"), teaJSON(`{"access_token":"x","token_type":"mac"}`))
		} else {
			f.route(f.api("/token"), teaAnswer{status: http.StatusFound, location: other.root + "/v1.0.0/token"})
		}
		teaWantExit(t, row.discovery(teaHTTPInputs(f, teaPR1)), teaExitAuth, row)
		if !strings.HasPrefix(row.errOut.String(), "Error: no TEA access token") {
			t.Errorf("stderr %q", row.errOut.String())
		}
		if len(other.requests()) != 0 {
			t.Errorf("the token redirect was followed")
		}
	}
}

func TestTeaServerRequiresCredential(t *testing.T) { // T-26e, T-26f
	row := newTeaRow(t)
	f := teaSimple(t, row)
	f.requireBearer = "tok-1"
	teaWantExit(t, row.discovery(teaHTTPInputs(f, teaPR1)), teaExitAuth, row)
	if l := row.errOut.String(); !strings.HasPrefix(l, "Error: the TEA server requires a credential (HTTP 401)") ||
		!strings.Contains(l, "REARM_TEA_AUTH_ROOT with REARM_TEA_TOKEN, or with REARM_TEA_APIKEYID and REARM_TEA_APIKEY") {
		t.Errorf("stderr %q", l)
	}

	row = newTeaRow(t)
	f = teaSimple(t, row)
	f.route(f.api("/productRelease/"+teaPR1), teaAnswer{status: http.StatusForbidden})
	row.env[teaEnvAuthRoot] = f.root
	row.env[teaEnvToken] = "tok-1"
	teaWantExit(t, row.flow(teaHTTPInputs(f, teaPR1)), teaExitAuth, row)
	if l := row.errOut.String(); !strings.Contains(l, "HTTP 403") || !strings.Contains(l, "went to "+f.root+"/v1.0.0/productRelease/"+teaPR1) {
		t.Errorf("stderr %q", l)
	}
}

func TestTeaCredentialNeverLeavesItsRoot(t *testing.T) { // T-27
	row := newTeaRow(t)
	f := teaSimple(t, row)
	secure := newTeaFixture(t, row.net, "secure.example.com", true)
	f.requireBearer = "tok-1"
	f.route(f.api("/productRelease/"+teaPR1), teaAnswer{status: http.StatusFound, location: secure.root + "/v1.0.0/productRelease/" + teaPR1})
	secure.route(secure.api("/productRelease/"+teaPR1), teaJSON(teaProductReleaseJSON(teaPR1, `"Alpha"`, `"1.0"`, "", "")))
	row.env[teaEnvAuthRoot] = f.root
	row.env[teaEnvToken] = "tok-1"
	teaWantExit(t, row.flow(teaHTTPInputs(f, teaPR1)), teaExitOK, row)
	if f.count(f.api("/productRelease/"+teaPR1)) != 1 {
		t.Fatalf("requests %+v", f.requests())
	}
	teaCheckBearer(t, f, "tok-1")
	if secure.count(secure.api("/productRelease/"+teaPR1)) != 1 {
		t.Fatalf("the redirect target was not asked: %+v", secure.requests())
	}
	teaNoAuthorization(t, secure)
}

func TestTeaCredentialInputsRefused(t *testing.T) { // T-28
	for _, c := range []struct {
		name  string
		env   map[string]string
		stdin bool
		http  bool
		names string
	}{
		{"token and key id", map[string]string{teaEnvAuthRoot: "https://tea.example.com/tea", teaEnvToken: "tok-SECRET", teaEnvKeyID: "id-SECRET"}, false, false, teaEnvToken},
		{"key id without secret", map[string]string{teaEnvAuthRoot: "https://tea.example.com/tea", teaEnvKeyID: "id-SECRET"}, false, false, teaEnvKey},
		{"secret without key id", map[string]string{teaEnvAuthRoot: "https://tea.example.com/tea", teaEnvKey: "key-SECRET"}, false, false, teaEnvKeyID},
		{"no root", map[string]string{teaEnvToken: "tok-SECRET"}, false, false, teaEnvAuthRoot},
		{"userinfo root", map[string]string{teaEnvAuthRoot: "https://user:pw-SECRET@tea.example.com/tea", teaEnvToken: "tok-SECRET"}, false, false, teaEnvAuthRoot},
		{"root with query", map[string]string{teaEnvAuthRoot: "https://tea.example.com/tea?k=q-SECRET", teaEnvToken: "tok-SECRET"}, false, false, teaEnvAuthRoot},
		{"root with trailing slash", map[string]string{teaEnvAuthRoot: "https://tea.example.com/tea/", teaEnvToken: "tok-SECRET"}, false, false, teaEnvAuthRoot},
		{"http root without --usehttp", map[string]string{teaEnvAuthRoot: "http://tea.example.com/tea", teaEnvToken: "tok-SECRET"}, false, false, teaEnvAuthRoot},
		{"http root on another origin", map[string]string{teaEnvAuthRoot: "http://other.example.com/tea", teaEnvToken: "tok-SECRET"}, false, true, teaEnvAuthRoot},
		{"--credential-stdin with the token variable", map[string]string{teaEnvAuthRoot: "https://tea.example.com/tea", teaEnvToken: "tok-SECRET"}, true, false, teaEnvToken},
		{"--credential-stdin with the key variable", map[string]string{teaEnvAuthRoot: "https://tea.example.com/tea", teaEnvKeyID: "id-SECRET", teaEnvKey: "key-SECRET"}, true, false, teaEnvKey},
		{"empty token", map[string]string{teaEnvAuthRoot: "https://tea.example.com/tea", teaEnvToken: ""}, false, false, teaEnvToken},
	} {
		row := newTeaRow(t)
		row.env = c.env
		row.in = failingReader{t}
		in := teaInputs{tei: "tei://tea.example.com/uuid/" + teaPR1, credentialStdin: c.stdin, useHTTP: c.http}
		teaWantExit(t, row.discovery(in), teaExitUsage, row)
		lines := teaLines(row.errOut.String())
		if len(lines) != 1 || !strings.HasPrefix(lines[0], "Error: ") || !strings.Contains(lines[0], c.names) {
			t.Errorf("%s: stderr %q", c.name, row.errOut.String())
		}
		if strings.Contains(row.errOut.String(), "SECRET") {
			t.Errorf("%s: stderr repeats a value: %q", c.name, row.errOut.String())
		}
		if len(row.net.dialedAddrs()) != 0 {
			t.Errorf("%s: dialled %v", c.name, row.net.dialedAddrs())
		}
	}
}

func TestTeaCredentialFromPipedStdin(t *testing.T) { // T-29
	// (a) a token
	row := newTeaRow(t)
	f := teaFlowFixture(t, row, "")
	f.requireBearer = "tok-1"
	row.env[teaEnvAuthRoot] = f.root
	row.in = strings.NewReader("tok-1\n")
	in := teaHTTPInputs(f, teaPR1)
	in.credentialStdin = true
	teaWantExit(t, row.flow(in), teaExitOK, row)
	teaCheckBearer(t, f, "tok-1")

	// (b) the key secret, CR LF
	row = newTeaRow(t)
	f = teaFlowFixture(t, row, "")
	teaTokenFixture(f, true)
	row.env[teaEnvAuthRoot] = f.root
	row.env[teaEnvKeyID] = teaKeyID
	row.in = strings.NewReader(teaKeySecret + "\r\n")
	in = teaHTTPInputs(f, teaPR1)
	in.credentialStdin = true
	teaWantExit(t, row.flow(in), teaExitOK, row)
	teaCheckExchange(t, f)
	teaCheckBearer(t, f, "tok-2")

	// (c) as (a) with two results: the list, exit 4, the token sent on the candidates
	row = newTeaRow(t)
	f = teaSimple(t, row)
	teaTwoResults(f)
	f.requireBearer = "tok-1"
	row.env[teaEnvAuthRoot] = f.root
	row.in = strings.NewReader("tok-1\n")
	in = teaHTTPInputs(f, teaPR1)
	in.credentialStdin = true
	teaWantExit(t, row.flow(in), teaExitAmbiguous, row)
	if !strings.Contains(row.errOut.String(), "Multiple discovery results found (2 results).") {
		t.Errorf("stderr %q", row.errOut.String())
	}
	if f.count(f.api("/productRelease/"+teaPR2)) != 1 {
		t.Errorf("candidates not loaded")
	}
	teaCheckBearer(t, f, "tok-1")
}

func TestTeaCredentialFromTerminal(t *testing.T) { // T-30
	row := newTeaRow(t)
	f := teaSimple(t, row)
	teaTwoResults(f)
	f.requireBearer = "tok-1"
	row.env[teaEnvAuthRoot] = f.root
	row.stdin = teaStdinTerminal
	row.in = strings.NewReader("2\n")
	var prompts []string
	row.secret = func(p string) (string, error) {
		prompts = append(prompts, p)
		return "tok-1", nil
	}
	in := teaHTTPInputs(f, teaPR1)
	in.credentialStdin = true
	teaWantExit(t, row.discovery(in), teaExitOK, row)
	if len(prompts) != 1 || prompts[0] != "TEA credential: " {
		t.Errorf("readSecret prompts %q", prompts)
	}
	if row.out.String() != teaPR2+"\n" {
		t.Errorf("stdout %q", row.out.String())
	}
	teaNoSecretIn(t, row, "tok-1")
}

func TestTeaReARMKeyNeverSent(t *testing.T) { // T-31
	savedKey, savedID := apiKey, apiKeyId
	t.Cleanup(func() { apiKey, apiKeyId = savedKey, savedID })
	apiKey, apiKeyId = "rearm-key-VALUE", "rearm-id-VALUE"
	row := newTeaRow(t)
	f := teaFlowFixture(t, row, "")
	for _, k := range []string{"REARM_APIKEY", "REARM_API_KEY", "REARM_APIKEYID", "REARM_API_ID"} {
		row.env[k] = k + "-VALUE"
	}
	teaWantExit(t, row.flow(teaHTTPInputs(f, teaPR1)), teaExitOK, row)
	teaNoAuthorization(t, f)
	for _, r := range f.requests() {
		if strings.Contains(r.path+r.rawQuery+r.body, "VALUE") {
			t.Errorf("%s carried a ReARM credential", r.path)
		}
	}
}

func TestTeaDebugTrace(t *testing.T) { // T-32
	type debugRow struct {
		name    string
		setup   func(row *teaRow) (*teaFixture, teaInputs)
		code    teaExit
		secrets []string
	}
	for _, c := range []debugRow{
		{"bearer", func(row *teaRow) (*teaFixture, teaInputs) {
			f := teaFlowFixture(t, row, "")
			f.requireBearer = "tok-1"
			row.env[teaEnvAuthRoot] = f.root
			row.env[teaEnvToken] = "tok-1"
			return f, teaHTTPInputs(f, teaPR1)
		}, teaExitOK, []string{"tok-1"}},
		{"api key", func(row *teaRow) (*teaFixture, teaInputs) {
			f := teaFlowFixture(t, row, "")
			teaTokenFixture(f, true)
			teaKeyEnv(row, f)
			return f, teaHTTPInputs(f, teaPR1)
		}, teaExitOK, []string{"tok-2", teaKeyID, teaKeySecret, url.QueryEscape(teaKeyID), url.QueryEscape(teaKeySecret)}},
		{"refused", func(row *teaRow) (*teaFixture, teaInputs) {
			f := teaFlowFixture(t, row, "")
			f.requireBearer = "tok-1"
			row.env[teaEnvAuthRoot] = f.root
			row.env[teaEnvToken] = "wrong-SECRET"
			return f, teaHTTPInputs(f, teaPR1)
		}, teaExitAuth, []string{"wrong-SECRET"}},
	} {
		plain := newTeaRow(t)
		_, in := c.setup(plain)
		teaWantExit(t, plain.flow(in), c.code, plain)

		row := newTeaRow(t)
		f, in := c.setup(row)
		in.debug = true
		teaWantExit(t, row.flow(in), c.code, row)
		// the fixtures of the two runs listen on different ports
		wantOut := strings.ReplaceAll(plain.out.String(), ":"+teaPortOf(plain)+"/", ":"+f.port+"/")
		if row.out.String() != wantOut {
			t.Errorf("%s: stdout differs with --debug:\n%s\nwithout:\n%s", c.name, row.out.String(), wantOut)
		}
		requests, responses := 0, 0
		for _, l := range teaLines(row.errOut.String()) {
			if strings.HasPrefix(l, "TEA request [") {
				requests++
			}
			if strings.HasPrefix(l, "TEA response [") {
				responses++
			}
			for _, bad := range append([]string{"?", "tei=", "Authorization", "Bearer", "Basic"}, c.secrets...) {
				if strings.Contains(l, bad) {
					t.Errorf("%s: debug line %q carries %q", c.name, l, bad)
				}
			}
		}
		if n := len(f.requests()); requests != n || responses != n {
			t.Errorf("%s: %d request and %d response lines for %d requests", c.name, requests, responses, n)
		}
		if c.name == "api key" && !strings.Contains(row.errOut.String(), "TEA request [token]: POST "+f.root+"/v1.0.0/token") {
			t.Errorf("%s: no token request line: %q", c.name, row.errOut.String())
		}
	}

	// (d) a userinfo root under --debug is refused before anything echoes it
	row := newTeaRow(t)
	row.env[teaEnvAuthRoot] = "https://user:pw-SECRET@tea.example.com/tea"
	row.env[teaEnvToken] = "tok-1"
	teaWantExit(t, row.discovery(teaInputs{tei: "tei://tea.example.com/uuid/" + teaPR1, debug: true}), teaExitUsage, row)
	teaNoSecretIn(t, row, "pw-SECRET", "tok-1")
}

// teaPortOf is the port of the tea.example.com fixture of a row.
func teaPortOf(row *teaRow) string {
	row.net.mu.Lock()
	defer row.net.mu.Unlock()
	for k := range row.net.addrs {
		if strings.HasPrefix(k, "tea.example.com:") {
			return strings.TrimPrefix(k, "tea.example.com:")
		}
	}
	return ""
}

// --- 8.5 Mapping and repository

func TestTeaExitFor(t *testing.T) { // T-33
	_, policyErr := tea.ParseAPIBase("https://tea.example.com/a/../b", teaAPIVersion)
	var pe *tea.URLPolicyError
	if !errors.As(policyErr, &pe) {
		t.Fatalf("no URL policy error to test with: %v", policyErr)
	}
	_, syntaxErr := tei.Parse("tei://")
	for _, c := range []struct {
		name string
		err  error
		want teaExit
	}{
		{"usage wrapping a policy refusal", &teaUsageError{msg: "x", err: policyErr}, teaExitUsage},
		{"policy refusal inside a discovery error", &tea.DiscoveryError{Stage: tea.StageWellKnown, Attempts: []tea.Attempt{{Err: policyErr}}}, teaExitRefusedURL},
		{"policy refusal", fmt.Errorf("x: %w", policyErr), teaExitRefusedURL},
		{"API 401", &tea.APIError{Status: 401}, teaExitAuth},
		{"API 403", &tea.APIError{Status: 403}, teaExitAuth},
		{"API 500", &tea.APIError{Status: 500}, teaExitFailed},
		{"token error", &tea.TokenError{Status: 401, Code: "invalid_client"}, teaExitAuth},
		{"exchange error", &teaAuthError{msg: "x"}, teaExitAuth},
		{"not resolved", &tea.NotResolvedError{Code: tea.ErrorCodeObjectUnknown}, teaExitNotResolved},
		{"object unknown", fmt.Errorf("x: %w", tea.ErrObjectUnknown), teaExitNotResolved},
		{"library ambiguous", &tea.AmbiguousError{}, teaExitAmbiguous},
		{"choice not made, as FullFlow wraps it", fmt.Errorf("full flow: selecting a candidate: %w", &teaChoiceError{msg: "x"}), teaExitAmbiguous},
		{"TEI syntax", syntaxErr, teaExitUsage},
		{"invalid option", fmt.Errorf("x: %w", tea.ErrInvalidOption), teaExitUsage},
		{"canceled", context.Canceled, teaExitFailed},
		{"time bound", fmt.Errorf("x: %w", tea.ErrTimeout), teaExitFailed},
		{"redirect limit", fmt.Errorf("x: %w", tea.ErrRedirectLimit), teaExitRefusedURL},
	} {
		if got := teaExitFor(c.err); got != c.want {
			t.Errorf("%s: %d, want %d", c.name, got, c.want)
		}
	}
}

func TestTeaLibraryPin(t *testing.T) { // T-34
	b, err := os.ReadFile("../go.mod")
	if err != nil {
		t.Fatal(err)
	}
	pinned := false
	for _, l := range strings.Split(string(b), "\n") {
		f := strings.Fields(l)
		if len(f) >= 2 && f[0] == "github.com/oolong-project/tea-client-go" && f[1] == "v0.0.0" && len(f) == 2 {
			pinned = true
		}
		if strings.HasPrefix(strings.TrimSpace(l), "replace") && strings.Contains(l, "tea-client-go") {
			t.Errorf("replace directive: %q", l)
		}
	}
	if !pinned {
		t.Error("go.mod does not require github.com/oolong-project/tea-client-go v0.0.0 directly")
	}
	if strings.Contains(string(b), "replace (") {
		t.Error("go.mod has a replace block")
	}
	if _, err := os.Stat("../schema/teaspec_openapi.yaml"); !os.IsNotExist(err) {
		t.Errorf("schema/teaspec_openapi.yaml still exists (%v)", err)
	}
}
