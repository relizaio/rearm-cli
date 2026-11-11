package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

// Mutation round for the verbs: the bound on server strings at every field of every
// document, the page loop of `releases` at the exact edges of --max-output-bytes, the next
// page token, the bound on the document written, and the error object's url and trace.

// wantStringBound checks an exit 9 STRING_BYTES naming path and the bound.
func (r *teaMRow) wantStringBound(got teaExit, path string, max int) {
	r.t.Helper()
	e := r.wantFail(got, teaExitLimit, teaReasonStringBytes)
	want := fmt.Sprintf("the server string at %s is longer than --max-string-bytes (%d bytes)", path, max)
	if e["message"] != want {
		r.t.Errorf("message %q, want %q", e["message"], want)
	}
}

func teaA(n int) string { return strings.Repeat("a", n) }

// teaStrArgs bound every server string to 256 bytes; teaLong256 is one byte too long.
var teaStrArgs = []string{"--max-string-bytes", "256"}

func TestTeaMachineStringBoundProductAndComponent(t *testing.T) {
	ids := func(idType, idValue string) string {
		return `[{"idType":"PURL","idValue":"x"},{"idType":"` + idType + `","idValue":"` + idValue + `"}]`
	}
	for _, c := range []struct {
		verb teaMachineVerb
		path string
		body string
		kind string
		want string
	}{
		{teaVerbProduct, "/product/" + teaProduct, `{"uuid":"` + teaProduct + `","name":"` + teaA(257) + `","identifiers":[]}`, "", "product.name"},
		{teaVerbProduct, "/product/" + teaProduct, `{"uuid":"` + teaProduct + `","name":"n","identifiers":` + ids("PURL", teaA(257)) + `}`, "", "product.identifiers[1].idValue"},
		{teaVerbProduct, "/product/" + teaProduct, `{"uuid":"` + teaProduct + `","name":"n","identifiers":` + ids(teaA(257), "v") + `}`, "", "product.identifiers[1].idType"},
		{teaVerbComponent, "/component/" + teaComp1, `{"uuid":"` + teaComp1 + `","name":"` + teaA(257) + `","identifiers":[]}`, "", "component.name"},
		{teaVerbComponent, "/component/" + teaComp1, `{"uuid":"` + teaComp1 + `","name":"n","identifiers":` + ids("PURL", teaA(257)) + `}`, "", "component.identifiers[1].idValue"},
	} {
		r := newTeaMRow(t)
		f := r.fixture("tea.example.com", true)
		f.route(f.api(c.path), teaJSON(c.body))
		id := teaProduct
		if c.verb == teaVerbComponent {
			id = teaComp1
		}
		r.wantStringBound(r.exec(c.verb, append([]string{"--api-root", f.root, "--uuid", id}, teaStrArgs...)...), c.want, 256)
	}
	// 256 bytes are within the bound
	r := newTeaMRow(t)
	f := r.fixture("tea.example.com", true)
	f.route(f.api("/product/"+teaProduct), teaJSON(`{"uuid":"`+teaProduct+`","name":"`+teaA(256)+`","identifiers":[]}`))
	r.want(r.exec(teaVerbProduct, append([]string{"--api-root", f.root, "--uuid", teaProduct}, teaStrArgs...)...), teaExitOK)
}

func TestTeaMachineStringBoundResolve(t *testing.T) {
	item := func(id, server string) string {
		return `{"productReleaseUuid":"` + id + `","servers":[` + server + `]}`
	}
	ok := `{"rootUrl":"https://mirror.example.com/tea","versions":["1.0.0"]}`
	for _, c := range []struct {
		name  string
		items func(root string) string
		want  string
	}{
		{"second result's server url", func(root string) string {
			return "[" + item(teaPR1, ok) + "," + item(teaPR2, `{"rootUrl":"https://mirror.example.com/`+teaA(257)+`","versions":["1.0.0"]}`) + "]"
		}, "results[1].servers[0].rootUrl"},
		{"second server's url", func(root string) string {
			return "[" + item(teaPR1, ok+`,{"rootUrl":"https://mirror.example.com/`+teaA(257)+`","versions":["1.0.0"]}`) + "]"
		}, "results[0].servers[1].rootUrl"},
		{"second version", func(root string) string {
			return "[" + item(teaPR1, `{"rootUrl":"https://mirror.example.com/tea","versions":["1.0.0","1.0.0-`+teaA(257)+`"]}`) + "]"
		}, "results[0].servers[0].versions[1]"},
	} {
		r := newTeaMRow(t)
		f := r.fixture("tea.example.com", true)
		f.route(f.api("/discovery"), teaJSON(c.items(f.root)))
		r.wantStringBound(r.exec(teaVerbResolve, append(teaResolveArgs(f, teaPR1), teaStrArgs...)...), c.want, 256)
	}
	// the root the discovery was done at is bound too
	r := newTeaMRow(t)
	f := r.fixture("tea.example.com", true)
	long := f.origin + "/tea/" + teaA(300)
	f.route("/.well-known/tea", teaJSON(`{"schemaVersion":1,"endpoints":[{"url":"`+long+`","versions":["1.0.0"]}]}`))
	f.route("/tea/"+teaA(300)+"/v1.0.0/discovery", teaJSON(teaDiscoveryJSON(f.root, teaPR1)))
	r.wantStringBound(r.exec(teaVerbResolve, append(teaResolveArgs(f, teaPR1), teaStrArgs...)...), "results[0].discoveredAt.rootUrl", 256)

	// the identifier is written in the form the server wrote it; a mixed-case uuid is canonical lower case
	r = newTeaMRow(t)
	f = r.fixture("tea.example.com", true)
	const mixed = "ABCDEF01-2345-4678-89AB-CDEF01234567"
	f.route(f.api("/discovery"), teaJSON(teaDiscoveryJSON(f.root, mixed)))
	r.want(r.exec(teaVerbResolve, teaResolveArgs(f, teaPR1)...), teaExitOK)
	if got := teaPath(r.doc(), "results.0.productReleaseUuid"); got != strings.ToLower(mixed) {
		t.Errorf("productReleaseUuid %v", got)
	}
}

// teaListItem is a release of a list page; kind is "product" or "component".
func teaListItem(kind, id, version, name, idValue string) string {
	nameKey := "productName"
	if kind == "component" {
		nameKey = "componentName"
	}
	return `{"uuid":"` + id + `","` + kind + `":"` + teaProduct + `","` + nameKey + `":"` + name + `","version":"` + version +
		`","createdDate":"2026-01-10T08:00:00Z","identifiers":[{"idType":"PURL","idValue":"` + idValue + `"}],"components":[]}`
}

func TestTeaMachineStringBoundReleaseList(t *testing.T) {
	for _, kind := range []string{"product", "component"} {
		parent := teaProduct
		if kind == "component" {
			parent = teaComp1
		}
		for _, c := range []struct {
			name, version, idValue, want string
		}{
			{teaA(257), "1", "x", "releases[2].name"},
			{"n", teaA(257), "x", "releases[2].version"},
			{"n", "1", teaA(257), "releases[2].identifiers[0].idValue"},
		} {
			r := newTeaMRow(t)
			f := r.fixture("tea.example.com", true)
			path := f.api("/" + kind + "/" + parent + "/releases")
			f.routeFunc(path, func(q *http.Request) teaAnswer {
				if q.URL.Query().Get("pageToken") == "" {
					return teaJSON(teaListPage("t2", teaListItem(kind, teaPR1, "1", "n", "x"), teaListItem(kind, teaPR2, "2", "n", "x")))
				}
				return teaJSON(teaListPage("", teaListItem(kind, teaCR1, c.version, c.name, c.idValue)))
			})
			args := append([]string{"--api-root", f.root, "--" + kind, parent}, teaStrArgs...)
			r.wantStringBound(r.exec(teaVerbReleases, args...), c.want, 256)
		}
	}
	// a string in the first page of a run that is not stopped by the bound
	r := newTeaMRow(t)
	f := r.fixture("tea.example.com", true)
	f.route(f.api("/product/"+teaProduct+"/releases"), teaJSON(teaListPage("", teaListItem("product", teaPR1, "1", teaA(257), "x"))))
	r.wantStringBound(r.exec(teaVerbReleases, append([]string{"--api-root", f.root, "--product", teaProduct}, teaStrArgs...)...), "releases[0].name", 256)
}

func TestTeaMachineComponentReleaseListGolden(t *testing.T) {
	r := newTeaMRow(t)
	f := r.fixture("tea.example.com", true)
	f.route(f.api("/component/"+teaComp1+"/releases"), teaJSON(teaListPage("",
		teaListItem("component", teaCR1, "1.0", "Component One", "pkg:generic/c@1"))))
	r.want(r.exec(teaVerbReleases, "--api-root", f.root, "--component", teaComp1), teaExitOK)
	r.golden(`{"formatVersion":1,"kind":"TEA_RELEASE_LIST","server":{"rootUrl":"` + f.root + `","apiVersion":"1.0.0"},"parent":{"kind":"COMPONENT","uuid":"` + teaComp1 + `"},"releases":[` +
		`{"uuid":"` + teaCR1 + `","version":"1.0","name":"Component One","createdDate":"2026-01-10T08:00:00Z","releaseDate":null,"preRelease":"UNSTATED","identifiers":[{"idType":"PURL","idValue":"pkg:generic/c@1"}]}],` +
		`"page":{"stoppedBy":"END","nextPageToken":null,"pagesRead":1}}`)
	// the page size goes to the server
	for _, q := range f.requests() {
		if v, _ := urlQueryValue(q.rawQuery, "pageSize"); v != "100" {
			t.Errorf("query %q", q.rawQuery)
		}
	}
	r.want(r.exec(teaVerbReleases, "--api-root", f.root, "--component", teaComp1, "--page-size", "7"), teaExitOK)
	last := f.requests()[len(f.requests())-1]
	if v, _ := urlQueryValue(last.rawQuery, "pageSize"); v != "7" {
		t.Errorf("--page-size 7 sent as %q", last.rawQuery)
	}
	if _, ok := urlQueryValue(last.rawQuery, "pageToken"); ok {
		t.Errorf("a page token was sent on the first page: %q", last.rawQuery)
	}
}

func teaMutProductReleaseJSON(name, version, idValue string) string {
	return `{"uuid":"` + teaPR1 + `","product":"` + teaProduct + `","productName":"` + name + `","version":"` + version + `",` +
		`"createdDate":"2026-01-10T08:00:00Z","identifiers":[{"idType":"PURL","idValue":"` + idValue + `"}],"components":[],` +
		`"latestCollection":{"uuid":"` + teaPR1 + `","version":1,"createdDate":"2026-01-10T08:00:00Z","belongsTo":"PRODUCT_RELEASE","updateReason":{"type":"INITIAL_RELEASE"},"artifacts":[]}}`
}

func teaMutComponentReleaseJSON(name, version, idValue string) string {
	return `{"uuid":"` + teaCR1 + `","component":"` + teaComp1 + `","componentName":"` + name + `","version":"` + version + `",` +
		`"createdDate":"2026-01-10T08:00:00Z","identifiers":[{"idType":"PURL","idValue":"` + idValue + `"}],` +
		`"latestCollection":{"uuid":"` + teaCR1 + `","version":1,"createdDate":"2026-01-10T08:00:00Z","belongsTo":"COMPONENT_RELEASE","updateReason":{"type":"INITIAL_RELEASE"},"artifacts":[]}}`
}

func TestTeaMachineStringBoundRelease(t *testing.T) {
	for _, kind := range []string{"productRelease", "componentRelease"} {
		build, id, flag := teaMutProductReleaseJSON, teaPR1, "--product-release"
		if kind == "componentRelease" {
			build, id, flag = teaMutComponentReleaseJSON, teaCR1, "--component-release"
		}
		for _, c := range []struct{ name, version, idValue, want string }{
			{teaA(257), "1", "x", "release.name"},
			{"n", teaA(257), "x", "release.version"},
			{"n", "1", teaA(257), "release.identifiers[0].idValue"},
		} {
			r := newTeaMRow(t)
			f := r.fixture("tea.example.com", true)
			f.route(f.api("/"+kind+"/"+id), teaJSON(build(c.name, c.version, c.idValue)))
			r.wantStringBound(r.exec(teaVerbRelease, append([]string{"--api-root", f.root, flag, id}, teaStrArgs...)...), c.want, 256)
		}
		// the lifecycle is bound too, and a failure there is the run's
		r := newTeaMRow(t)
		f := r.fixture("tea.example.com", true)
		f.route(f.api("/"+kind+"/"+id), teaJSON(build("n", "1", "x")))
		f.route(f.api("/"+kind+"/"+id+"/cle"), teaJSON(`{"events":[{"id":1,"type":"`+teaA(257)+`","effective":"2026-01-15T00:00:00Z","published":"2026-01-15T00:00:00Z"}]}`))
		r.wantStringBound(r.exec(teaVerbRelease, append([]string{"--api-root", f.root, flag, id}, teaStrArgs...)...), "cle.events[0].type", 256)
	}
}

func TestTeaMachineLifecycleNotAvailable(t *testing.T) {
	for _, kind := range []string{"productRelease", "componentRelease"} {
		build, id, flag := teaMutProductReleaseJSON, teaPR1, "--product-release"
		if kind == "componentRelease" {
			build, id, flag = teaMutComponentReleaseJSON, teaCR1, "--component-release"
		}
		for _, status := range []teaAnswer{teaTEAError(http.StatusNotFound, "OBJECT_UNKNOWN"), teaTEAError(http.StatusNotImplemented, "NOT_IMPLEMENTED")} {
			r := newTeaMRow(t)
			f := r.fixture("tea.example.com", true)
			f.route(f.api("/"+kind+"/"+id), teaJSON(build("n", "1", "x")))
			f.route(f.api("/"+kind+"/"+id+"/cle"), status)
			r.want(r.exec(teaVerbRelease, "--api-root", f.root, flag, id), teaExitOK)
			d := r.doc()
			if teaPath(d, "cle.status") != "NOT_AVAILABLE" || len(teaPath(d, "cle.events").([]any)) != 0 || len(teaPath(d, "cle.supportDefinitions").([]any)) != 0 {
				t.Errorf("%s: cle %v", kind, d["cle"])
			}
		}
		// the lifecycle of the kind asked for, not the other
		r := newTeaMRow(t)
		f := r.fixture("tea.example.com", true)
		f.route(f.api("/"+kind+"/"+id), teaJSON(build("n", "1", "x")))
		f.route(f.api("/"+kind+"/"+id+"/cle"), teaJSON(`{"events":[{"id":9,"type":"released","effective":"2026-01-15T00:00:00Z","published":"2026-01-15T00:00:00Z"}]}`))
		r.want(r.exec(teaVerbRelease, "--api-root", f.root, flag, id), teaExitOK)
		if d := r.doc(); teaPath(d, "cle.status") != "READ" || teaPath(d, "cle.events.0.id") != 9.0 {
			t.Errorf("%s: cle %v", kind, d["cle"])
		}
		if d := r.doc(); teaPath(d, "release.name") != "n" || teaPath(d, "release.identifiers.0.idValue") != "x" {
			t.Errorf("%s: release %v", kind, d["release"])
		}
	}
}

// teaLongPages serves pages of `per` releases, each with a name of nameLen bytes, chained
// by the tokens tok-2, tok-3, ..., the last page ending the list.
func teaLongPages(f *teaFixture, pages, per, nameLen int) {
	counts := make([]int, pages)
	for i := range counts {
		counts[i] = per
	}
	teaShapedPages(f, counts, nameLen)
}

// teaShapedPages serves pages whose sizes are counts, the tokens being tok-2, tok-3, ...
func teaShapedPages(f *teaFixture, counts []int, nameLen int) {
	name := teaA(nameLen)
	f.routeFunc(f.api("/product/"+teaProduct+"/releases"), func(q *http.Request) teaAnswer {
		n := 1
		if tok := q.URL.Query().Get("pageToken"); tok != "" {
			n, _ = strconv.Atoi(strings.TrimPrefix(tok, "tok-"))
		}
		var items []string
		for j := 0; j < counts[n-1]; j++ {
			items = append(items, `{"uuid":"`+teaPR1+`","product":"`+teaProduct+`","productName":"`+name+`","version":"`+fmt.Sprintf("p%di%d", n, j)+
				`","createdDate":"2026-01-10T08:00:00Z","components":[]}`)
		}
		if n >= len(counts) {
			return teaJSON(teaListPage("", items...))
		}
		return teaJSON(teaListPage("tok-"+strconv.Itoa(n+1), items...))
	})
}

// teaBoundOracle runs the list under bounds taken from the documents that stop at
// MAX_PAGES: the same document stopped by OUTPUT_BYTES is 3 bytes longer (the name of the
// reason), the token being the same. It returns the sizes of those documents by number of
// pages and a function that checks one bound.
func teaBoundOracle(t *testing.T, r *teaMRow, f *teaFixture, maxPages int, nameLen int, counts []int) (map[int]int, func(bound, wantPages int, wantStop, wantNext string, wantLen int, extra ...string)) {
	t.Helper()
	base := []string{"--api-root", f.root, "--product", teaProduct, "--max-string-bytes", strconv.Itoa(nameLen + 100)}
	run := func(extra ...string) string {
		t.Helper()
		r.want(r.exec(teaVerbReleases, append(append([]string(nil), base...), extra...)...), teaExitOK)
		return r.out.String()
	}
	size := map[int]int{}
	for k := 1; k <= maxPages; k++ {
		size[k] = len(run("--max-pages", strconv.Itoa(k)))
	}
	return size, func(bound, wantPages int, wantStop, wantNext string, wantLen int, extra ...string) {
		t.Helper()
		out := run(append([]string{"--max-output-bytes", strconv.Itoa(bound)}, extra...)...)
		d := r.doc()
		items := 0
		for _, c := range counts[:wantPages] {
			items += c
		}
		if teaPath(d, "page.pagesRead") != float64(wantPages) || teaPath(d, "page.stoppedBy") != wantStop {
			t.Errorf("bound %d: %v, want %d pages stopped by %s", bound, d["page"], wantPages, wantStop)
		}
		if wantNext == "" && teaPath(d, "page.nextPageToken") != nil || wantNext != "" && teaPath(d, "page.nextPageToken") != wantNext {
			t.Errorf("bound %d: token %v, want %q", bound, teaPath(d, "page.nextPageToken"), wantNext)
		}
		if len(out) != wantLen || len(teaVersions(d)) != items {
			t.Errorf("bound %d: %d bytes and %d releases, want %d bytes and %d releases", bound, len(out), len(teaVersions(d)), wantLen, items)
		}
	}
}

func TestTeaMachineReleasesBoundWithAnEmptyPageAmongThem(t *testing.T) {
	r := newTeaMRow(t)
	f := r.fixture("tea.example.com", true)
	counts := []int{3, 0, 3, 3}
	teaShapedPages(f, counts, 11000)
	size, check := teaBoundOracle(t, r, f, 3, 11000, counts)
	if size[3]+2 < 65536 {
		t.Fatalf("three pages are %d bytes: too small for the smallest bound", size[3])
	}
	check(size[3]+3, 3, "OUTPUT_BYTES", "tok-4", size[3]+3)
	check(size[3]+2, 2, "OUTPUT_BYTES", "tok-3", size[2]+3)
	check(size[3], 3, "MAX_PAGES", "tok-4", size[3], "--max-pages", "3")
}

func TestTeaMachineReleasesBoundAtTenPages(t *testing.T) {
	r := newTeaMRow(t)
	f := r.fixture("tea.example.com", true)
	counts := make([]int, 12)
	for i := range counts {
		counts[i] = 1
	}
	teaShapedPages(f, counts, 6800)
	size, check := teaBoundOracle(t, r, f, 10, 6800, counts)
	if size[10]+2 < 65536 {
		t.Fatalf("ten pages are %d bytes: too small for the smallest bound", size[10])
	}
	// the number of pages is written with two digits from the tenth page on
	check(size[10]+3, 10, "OUTPUT_BYTES", "tok-11", size[10]+3)
	check(size[10]+2, 9, "OUTPUT_BYTES", "tok-10", size[9]+3)
	check(size[10], 10, "MAX_PAGES", "tok-11", size[10], "--max-pages", "10")
	check(size[10]-1, 9, "OUTPUT_BYTES", "tok-10", size[9]+3, "--max-pages", "10")
}

func TestTeaMachineReleasesFitTheBoundExactly(t *testing.T) {
	r := newTeaMRow(t)
	f := r.fixture("tea.example.com", true)
	counts := []int{3, 3, 3, 3}
	teaShapedPages(f, counts, 11000)
	size, check := teaBoundOracle(t, r, f, 3, 11000, counts)
	r.want(r.exec(teaVerbReleases, "--api-root", f.root, "--product", teaProduct, "--max-string-bytes", "20000"), teaExitOK)
	fullSize := r.out.Len()
	if teaPath(r.doc(), "page.stoppedBy") != "END" || teaPath(r.doc(), "page.pagesRead") != 4.0 {
		t.Fatalf("the full list: %v", r.doc()["page"])
	}
	if size[2]+3 < 65536 {
		t.Fatalf("two pages are %d bytes: too small for the smallest bound", size[2])
	}
	// the list ends where it ends
	check(fullSize, 4, "END", "", fullSize)
	check(fullSize-1, 3, "OUTPUT_BYTES", "tok-4", size[3]+3)
	// stopped by the number of pages, the document just fits
	check(size[3], 3, "MAX_PAGES", "tok-4", size[3], "--max-pages", "3")
	check(size[3]-1, 2, "OUTPUT_BYTES", "tok-3", size[2]+3, "--max-pages", "3")
	check(size[2], 2, "MAX_PAGES", "tok-3", size[2], "--max-pages", "2")
	// stopped by the bound after k pages: the page after them does not fit
	check(size[3]+3, 3, "OUTPUT_BYTES", "tok-4", size[3]+3)
	check(size[3]+2, 2, "OUTPUT_BYTES", "tok-3", size[2]+3)
	check(size[2]+3, 2, "OUTPUT_BYTES", "tok-3", size[2]+3)
	check(size[2]+2, 1, "OUTPUT_BYTES", "tok-2", size[1]+3)
}

func TestTeaMachineFirstReleasesPageFitsTheBoundExactly(t *testing.T) {
	r := newTeaMRow(t)
	f := r.fixture("tea.example.com", true)
	teaLongPages(f, 1, 3, 22000)
	base := []string{"--api-root", f.root, "--product", teaProduct, "--max-string-bytes", "30000"}
	r.want(r.exec(teaVerbReleases, base...), teaExitOK)
	n := r.out.Len()
	if n < 65536 {
		t.Fatalf("the page is %d bytes: too small for the smallest bound", n)
	}
	r.want(r.exec(teaVerbReleases, append(append([]string(nil), base...), "--max-output-bytes", strconv.Itoa(n))...), teaExitOK)
	if r.out.Len() != n {
		t.Errorf("document of %d bytes under a bound of %d", r.out.Len(), n)
	}
	e := r.wantFail(r.exec(teaVerbReleases, append(append([]string(nil), base...), "--max-output-bytes", strconv.Itoa(n-1))...), teaExitLimit, teaReasonOutputBytes)
	if e["message"] != fmt.Sprintf("the first page of releases does not fit --max-output-bytes (%d bytes)", n-1) {
		t.Errorf("message %v", e["message"])
	}
}

func TestTeaMachineNextPageTokenMustBePassable(t *testing.T) {
	for _, c := range []struct {
		name, page, reason string
		args               []string
	}{
		{"control character", `{"hasNext":true,"nextPageToken":"a\u0001b","results":[]}`, "it has a control character", nil},
		{"too long", `{"hasNext":true,"nextPageToken":"` + teaA(257) + `","results":[]}`, "it is longer than --max-string-bytes (256 bytes)", teaStrArgs},
	} {
		r := newTeaMRow(t)
		f := r.fixture("tea.example.com", true)
		f.route(f.api("/product/"+teaProduct+"/releases"), teaJSON(c.page))
		got := r.exec(teaVerbReleases, append([]string{"--api-root", f.root, "--product", teaProduct}, c.args...)...)
		if c.reason == "" {
			continue
		}
		e := r.wantFail(got, teaExitFailed, teaReasonInvalidResponse)
		want := "listing releases: unexpected response (HTTP 200): the nextPageToken cannot be passed back: " + c.reason
		if e["message"] != want {
			t.Errorf("%s: message %q, want %q", c.name, e["message"], want)
		}
	}
	// a token that is as long as the bound is passed on
	r := newTeaMRow(t)
	f := r.fixture("tea.example.com", true)
	f.route(f.api("/product/"+teaProduct+"/releases"), teaJSON(teaListPage(teaA(256), teaListItem("product", teaPR1, "1", "n", "x"))))
	r.want(r.exec(teaVerbReleases, append([]string{"--api-root", f.root, "--product", teaProduct, "--max-pages", "1"}, teaStrArgs...)...), teaExitOK)
	if teaPath(r.doc(), "page.nextPageToken") != teaA(256) || teaPath(r.doc(), "page.stoppedBy") != "MAX_PAGES" {
		t.Errorf("page %v", r.doc()["page"])
	}
}

// teaPaddedProduct serves a product whose name is n bytes long.
func teaPaddedProduct(f *teaFixture, n int) {
	f.route(f.api("/product/"+teaProduct), teaJSON(`{"uuid":"`+teaProduct+`","name":"`+teaA(n)+`","identifiers":[]}`))
}

func TestTeaMachineDocumentFitsTheBoundExactly(t *testing.T) {
	r := newTeaMRow(t)
	f := r.fixture("tea.example.com", true)
	teaPaddedProduct(f, 65536)
	base := []string{"--api-root", f.root, "--uuid", teaProduct, "--max-string-bytes", "70000"}
	r.want(r.exec(teaVerbProduct, base...), teaExitOK)
	n := r.out.Len()
	r.want(r.exec(teaVerbProduct, append(append([]string(nil), base...), "--max-output-bytes", strconv.Itoa(n))...), teaExitOK)
	e := r.wantFail(r.exec(teaVerbProduct, append(append([]string(nil), base...), "--max-output-bytes", strconv.Itoa(n-1))...), teaExitLimit, teaReasonOutputBytes)
	if e["message"] != fmt.Sprintf("the document is larger than --max-output-bytes (%d bytes)", n-1) {
		t.Errorf("message %v", e["message"])
	}
}

type teaFailWriter struct{}

func (teaFailWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

// teaExecWith runs verb in-process like exec, but with tio.
func (r *teaMRow) execWith(tio teaIO, verb teaMachineVerb, args ...string) teaExit {
	r.errOut.Reset()
	c := &cobra.Command{Use: string(verb)}
	addTeaMachineFlags(c, verb)
	if err := c.Flags().Parse(args); err != nil {
		r.t.Fatal(err)
	}
	return runTeaMachine(context.Background(), tio, teaMachineInputsFromFlags(c, verb, c.Flags().Args()))
}

func TestTeaMachineStdoutWriteFailure(t *testing.T) {
	r := newTeaMRow(t)
	f := r.fixture("tea.example.com", true)
	teaPaddedProduct(f, 3)
	tio := r.io()
	tio.out = teaFailWriter{}
	got := r.execWith(tio, teaVerbProduct, "--api-root", f.root, "--uuid", teaProduct)
	e := r.wantFail(got, teaExitFailed, teaReasonFailed)
	if e["message"] != "disk full" {
		t.Errorf("message %v", e["message"])
	}
}

func TestTeaMachineStandardInputIsOnlyReadOnRequest(t *testing.T) {
	r := newTeaMRow(t)
	f := r.fixture("tea.example.com", true)
	teaPaddedProduct(f, 3)
	r.stdin = teaStdinTerminal
	// a terminal on standard input is no hindrance when it is not read
	r.want(r.exec(teaVerbProduct, "--api-root", f.root, "--uuid", teaProduct), teaExitOK)
	// and it is refused when it should be read
	e := r.wantFail(r.exec(teaVerbProduct, "--api-root", f.root, "--uuid", teaProduct, "--credential-stdin"), teaExitUsage, teaReasonUsage)
	if e["message"] != "--credential-stdin reads a line from a pipe; standard input is a terminal and the machine mode never prompts" {
		t.Errorf("message %v", e["message"])
	}
}

func TestTeaMachineDeadlineIsTheFlag(t *testing.T) {
	r := newTeaMRow(t)
	f := r.fixture("tea.example.com", true)
	f.handle(f.api("/product/"+teaProduct), teaStall)
	start := time.Now()
	e := r.wantFail(r.exec(teaVerbProduct, "--api-root", f.root, "--uuid", teaProduct, "--deadline", "1s"), teaExitLimit, teaReasonDeadline)
	if d := time.Since(start); d < 900*time.Millisecond || d > 1800*time.Millisecond {
		t.Errorf("a deadline of 1s ended the run after %v", d)
	}
	if m, _ := e["message"].(string); !strings.HasPrefix(m, "the run deadline (--deadline) was reached: ") {
		t.Errorf("message %q", m)
	}
	if e["url"] != nil || e["phase"] != nil || e["address"] != nil {
		t.Errorf("error object %v", e)
	}
}

func TestTeaMachineErrorObjectNamesTheFailingRequest(t *testing.T) {
	r := newTeaMRow(t)
	f := r.fixture("tea.example.com", true)
	f.route(f.api("/product/"+teaProduct), teaTEAError(http.StatusInternalServerError, "INTERNAL_ERROR"))
	e := r.wantFail(r.exec(teaVerbProduct, "--api-root", f.root, "--uuid", teaProduct), teaExitFailed, teaReasonServerError)
	if e["url"] != f.root+"/v1.0.0/product/"+teaProduct || e["httpStatus"] != 500.0 {
		t.Errorf("error object %v", e)
	}
	// a request that was refused names the address and the url
	r = newTeaMRow(t)
	f = r.fixture("tea.example.com", true)
	r.m.resolveTo("evil.example.com", "127.0.0.1")
	root := "https://evil.example.com:" + f.port + "/tea"
	e = r.wantFail(r.exec(teaVerbProduct, "--api-root", root, "--uuid", teaProduct), teaExitAddressRefused, teaReasonAddressRefused)
	if e["message"] != "refused to connect to 127.0.0.1 (loopback) for "+root+"/v1.0.0/product/"+teaProduct || e["url"] != root+"/v1.0.0/product/"+teaProduct {
		t.Errorf("error object %v", e)
	}
	if e["hint"] != teaHintThisHost {
		t.Errorf("hint %v", e["hint"])
	}
}

func TestTeaMachineTraceWithDebug(t *testing.T) {
	r := newTeaMRow(t)
	r.debug = true
	f := r.fixture("tea.example.com", true)
	teaPaddedProduct(f, 3)
	r.want(r.exec(teaVerbProduct, "--api-root", f.root, "--uuid", teaProduct), teaExitOK)
	errOut := r.errOut.String()
	if !strings.Contains(errOut, "TEA request") || !strings.Contains(errOut, "GET "+f.root+"/v1.0.0/product/"+teaProduct) ||
		!strings.Contains(errOut, "TEA response") || !strings.Contains(errOut, "200 "+f.root+"/v1.0.0/product/"+teaProduct) {
		t.Errorf("trace %q", errOut)
	}

	// a refused address is traced
	r = newTeaMRow(t)
	r.debug = true
	f = r.fixture("tea.example.com", true)
	r.m.resolveTo("evil.example.com", "169.254.169.254")
	root := "https://evil.example.com:" + f.port + "/tea"
	if got := r.exec(teaVerbProduct, "--api-root", root, "--uuid", teaProduct); got != teaExitAddressRefused {
		t.Fatalf("exit %d: %s", got, r.errOut.String())
	}
	if !strings.Contains(r.errOut.String(), "TEA address refused: 169.254.169.254 CLOUD_METADATA for evil.example.com\n") {
		t.Errorf("trace %q", r.errOut.String())
	}
}

func TestTeaMachineRequestTimeoutNamesTheRequest(t *testing.T) {
	r := newTeaMRow(t)
	r.seam.callTimeout = 200 * time.Millisecond
	f := r.fixture("tea.example.com", true)
	f.handle(f.api("/product/"+teaProduct), teaStall)
	e := r.wantFail(r.exec(teaVerbProduct, "--api-root", f.root, "--uuid", teaProduct), teaExitLimit, teaReasonRequestTimeout)
	if e["url"] != f.root+"/v1.0.0/product/"+teaProduct {
		t.Errorf("url %v", e["url"])
	}
}

// The manifest reports the collection as the server describes it. A server that answers
// another version than the one asked for shows in the manifest.
func TestTeaMachineDownloadManifestReportsTheServersCollectionVersion(t *testing.T) {
	d := newTeaDL(t)
	d.art(teaArtProd, 1, "BOM", "", d.serverBody(teaArtProd, 1, "a/one", "one"))
	d.f.route(d.f.api("/componentRelease/"+teaCR1+"/collection/2"), teaJSON(`{"uuid":"`+teaCR1+`","version":5,"createdDate":"2026-01-16T10:00:00Z",`+
		`"belongsTo":"COMPONENT_RELEASE","updateReason":{"type":"ARTIFACT_ADDED"},"artifacts":[`+strings.Join(d.arts, ",")+`]}`))
	d.r.want(d.exec(), teaExitOK)
	if got := teaPath(d.r.doc(), "collection.version"); got != 5.0 {
		t.Errorf("collection.version %v", got)
	}
}
