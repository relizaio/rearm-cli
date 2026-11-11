package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// The JSON contract of the machine mode (task TPF-178, design section 4, rows T-1 to T-14
// and T-29 to T-35). The rig is teaMachineHarness_test.go.

func teaResolveArgs(f *teaFixture, id string) []string {
	return []string{"--tei", "tei://" + f.host + "/uuid/" + id, "--useport", f.port}
}

func TestTeaMachineResolveGolden(t *testing.T) { // T-1
	r := newTeaMRow(t)
	f := r.fixture("tea.example.com", true)
	f.route(f.api("/discovery"), teaJSON(teaDiscoveryJSON(f.root, teaPR1)))
	r.want(r.exec(teaVerbResolve, teaResolveArgs(f, teaPR1)...), teaExitOK)
	r.golden(`{"formatVersion":1,"kind":"TEA_RESOLUTION","tei":"tei://tea.example.com/uuid/` + teaPR1 + `","results":[{"productReleaseUuid":"` + teaPR1 +
		`","discoveredAt":{"rootUrl":"` + f.root + `","apiVersion":"1.0.0"},"servers":[{"rootUrl":"` + f.root + `","versions":["1.0.0"],"priority":null}]}]}`)
}

func TestTeaMachineResolveAmbiguityIsData(t *testing.T) { // T-2
	r := newTeaMRow(t)
	f := r.fixture("tea.example.com", true)
	var items []string
	ids := []string{teaPR2, teaPR1, teaCR3}
	for _, id := range ids {
		items = append(items, `{"productReleaseUuid":"`+id+`","servers":[{"rootUrl":"`+f.root+`","versions":["1.0.0"],"priority":0.5},{"rootUrl":"https://mirror.example.com/tea","versions":["1.0.0"]}]}`)
	}
	f.route(f.api("/discovery"), teaJSON("["+strings.Join(items, ",")+"]"))
	r.want(r.exec(teaVerbResolve, teaResolveArgs(f, teaPR1)...), teaExitOK)
	d := r.doc()
	res, _ := d["results"].([]any)
	if len(res) != 3 {
		t.Fatalf("results %v", d["results"])
	}
	for i, id := range ids {
		if teaPath(d, fmt.Sprintf("results.%d.productReleaseUuid", i)) != id ||
			teaPath(d, fmt.Sprintf("results.%d.servers.0.priority", i)) != 0.5 ||
			teaPath(d, fmt.Sprintf("results.%d.servers.1.rootUrl", i)) != "https://mirror.example.com/tea" ||
			teaPath(d, fmt.Sprintf("results.%d.servers.1.priority", i)) != nil {
			t.Errorf("result %d: %v", i, res[i])
		}
	}
	if !strings.Contains(r.out.String(), `"priority":0.5}`) {
		t.Errorf("priority not written as 0.5: %s", r.out.String())
	}
	for _, q := range f.requests() {
		if strings.Contains(q.path, "/productRelease") {
			t.Errorf("resolve read a product release: %s", q.path)
		}
	}
}

func TestTeaMachineResolveNotFound(t *testing.T) { // T-3
	r := newTeaMRow(t)
	f := r.fixture("tea.example.com", true)
	f.route(f.api("/discovery"), teaTEAError(http.StatusNotFound, "OBJECT_UNKNOWN"))
	r.wantFail(r.exec(teaVerbResolve, teaResolveArgs(f, teaPR1)...), teaExitNotResolved, teaReasonNotFound)

	// (b) An empty list: the library refuses it as an invalid discovery answer (a discovery
	// answer has one item or more) and retries, so the run fails after its attempts. This
	// is a departure from the design (which expected NOT_FOUND), named in the notes.
	r = newTeaMRow(t)
	f = r.fixture("tea.example.com", true)
	f.route(f.api("/discovery"), teaJSON(`[]`))
	e := r.wantFail(r.exec(teaVerbResolve, teaResolveArgs(f, teaPR1)...), teaExitFailed, teaReasonFailed)
	if !strings.Contains(e["message"].(string), "empty array") {
		t.Errorf("message %v", e["message"])
	}
}

func TestTeaMachineProductAndComponentGolden(t *testing.T) { // T-4
	r := newTeaMRow(t)
	f := r.fixture("tea.example.com", true)
	ids := `[{"idType":"PURL","idValue":"pkg:generic/example/product"},{"idType":"CPE","idValue":"cpe:2.3:a:example:product:*:*:*:*:*:*:*:*"},` +
		`{"idType":"TEI","idValue":"tei://tea.example.com/uuid/` + teaProduct + `"},{"idType":"SWID","idValue":"swid-tag-1"}]`
	f.route(f.api("/product/"+teaProduct), teaJSON(`{"uuid":"`+teaProduct+`","name":"Example Product","identifiers":`+ids+`}`))
	f.route(f.api("/component/"+teaComp1), teaJSON(`{"uuid":"`+teaComp1+`","name":"Example Component","identifiers":[]}`))
	server := `"server":{"rootUrl":"` + f.root + `","apiVersion":"1.0.0"}`

	r.want(r.exec(teaVerbProduct, "--api-root", f.root, "--uuid", strings.ToUpper(teaProduct)), teaExitOK)
	r.golden(`{"formatVersion":1,"kind":"TEA_PRODUCT",` + server + `,"product":{"uuid":"` + teaProduct + `","name":"Example Product","identifiers":` + ids + `}}`)
	if f.count(f.api("/product/"+teaProduct)) != 1 {
		t.Errorf("the uuid was not sent in canonical form: %+v", f.requests())
	}
	r.want(r.exec(teaVerbComponent, "--api-root", f.root, "--uuid", teaComp1), teaExitOK)
	r.golden(`{"formatVersion":1,"kind":"TEA_COMPONENT",` + server + `,"component":{"uuid":"` + teaComp1 + `","name":"Example Component","identifiers":[]}}`)
}

// teaListRelease is one product release of a list page.
func teaListRelease(id, version string) string {
	return `{"uuid":"` + id + `","product":"` + teaProduct + `","version":"` + version + `","createdDate":"2026-01-10T08:00:00Z","components":[]}`
}

func teaListPage(next string, items ...string) string {
	if next == "" {
		return `{"hasNext":false,"results":[` + strings.Join(items, ",") + `]}`
	}
	return `{"hasNext":true,"nextPageToken":` + teaQuote(next) + `,"results":[` + strings.Join(items, ",") + `]}`
}

func teaQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestTeaMachineReleasesPreReleaseAndExactStrings(t *testing.T) { // T-5
	r := newTeaMRow(t)
	f := r.fixture("tea.example.com", true)
	f.route(f.api("/product/"+teaProduct+"/releases"), teaJSON(`{"hasNext":false,"results":[`+
		`{"uuid":"`+teaPR1+`","product":"`+teaProduct+`","productName":"P","version":" 1.0 ","createdDate":"2026-01-10T10:00:00.123456789+02:00","preRelease":true,"components":[]},`+
		`{"uuid":"`+teaPR2+`","product":"`+teaProduct+`","version":"v1.0.0+build.7","createdDate":"2026-01-11T00:00:00Z","releaseDate":"2026-01-15T00:00:00Z","preRelease":false,"identifiers":[{"idType":"PURL","idValue":"pkg:x@1"}],"components":[]},`+
		`{"uuid":"`+teaCR1+`","product":"`+teaProduct+`","version":"2.0.0-rc.1","createdDate":"2026-02-01T09:30:00.5Z","components":[]}]}`))
	r.want(r.exec(teaVerbReleases, "--api-root", f.root, "--product", teaProduct), teaExitOK)
	r.golden(`{"formatVersion":1,"kind":"TEA_RELEASE_LIST","server":{"rootUrl":"` + f.root + `","apiVersion":"1.0.0"},"parent":{"kind":"PRODUCT","uuid":"` + teaProduct + `"},"releases":[` +
		`{"uuid":"` + teaPR1 + `","version":" 1.0 ","name":"P","createdDate":"2026-01-10T08:00:00.123456789Z","releaseDate":null,"preRelease":"YES","identifiers":[]},` +
		`{"uuid":"` + teaPR2 + `","version":"v1.0.0+build.7","name":null,"createdDate":"2026-01-11T00:00:00Z","releaseDate":"2026-01-15T00:00:00Z","preRelease":"NO","identifiers":[{"idType":"PURL","idValue":"pkg:x@1"}]},` +
		`{"uuid":"` + teaCR1 + `","version":"2.0.0-rc.1","name":null,"createdDate":"2026-02-01T09:30:00.5Z","releaseDate":null,"preRelease":"UNSTATED","identifiers":[]}],` +
		`"page":{"stoppedBy":"END","nextPageToken":null,"pagesRead":1}}`)
}

// teaThreePages serves a list of three pages chained by tokens on path.
func teaThreePages(f *teaFixture, path string, item func(id, v string) string) {
	f.routeFunc(path, func(q *http.Request) teaAnswer {
		switch q.URL.Query().Get("pageToken") {
		case "":
			return teaJSON(teaListPage("tok/2+= x", item(teaPR1, "1")))
		case "tok/2+= x":
			return teaJSON(teaListPage("tok-3", item(teaPR2, "2")))
		case "tok-3":
			return teaJSON(teaListPage("", item(teaCR1, "3")))
		}
		return teaTEAError(http.StatusBadRequest, "INVALID_PAGE_TOKEN")
	})
}

func teaVersions(d map[string]any) []string {
	var out []string
	rels, _ := d["releases"].([]any)
	for _, x := range rels {
		out = append(out, x.(map[string]any)["version"].(string))
	}
	return out
}

func TestTeaMachineReleasesPaging(t *testing.T) { // T-6
	r := newTeaMRow(t)
	f := r.fixture("tea.example.com", true)
	teaThreePages(f, f.api("/product/"+teaProduct+"/releases"), teaListRelease)

	// (a) all three pages in one run
	r.want(r.exec(teaVerbReleases, "--api-root", f.root, "--product", teaProduct, "--max-pages", "10"), teaExitOK)
	d := r.doc()
	if got := strings.Join(teaVersions(d), ","); got != "1,2,3" || teaPath(d, "page.stoppedBy") != "END" ||
		teaPath(d, "page.pagesRead") != float64(3) || teaPath(d, "page.nextPageToken") != nil {
		t.Errorf("(a) %s", r.out.String())
	}
	for _, q := range f.requests() {
		if v, _ := urlQueryValue(q.rawQuery, "pageSize"); v != "100" {
			t.Errorf("(a) query %q", q.rawQuery)
		}
	}
	if len(f.requests()) != 3 {
		t.Fatalf("(a) %d requests", len(f.requests()))
	}

	// (b) two pages, then the rest with the token
	r.want(r.exec(teaVerbReleases, "--api-root", f.root, "--product", teaProduct, "--max-pages", "2"), teaExitOK)
	d = r.doc()
	if got := strings.Join(teaVersions(d), ","); got != "1,2" || teaPath(d, "page.stoppedBy") != "MAX_PAGES" ||
		teaPath(d, "page.nextPageToken") != "tok-3" || teaPath(d, "page.pagesRead") != float64(2) {
		t.Errorf("(b) %s", r.out.String())
	}
	r.want(r.exec(teaVerbReleases, "--api-root", f.root, "--product", teaProduct, "--page-token=tok-3"), teaExitOK)
	d = r.doc()
	if got := strings.Join(teaVersions(d), ","); got != "3" || teaPath(d, "page.stoppedBy") != "END" {
		t.Errorf("(b) continuation %s", r.out.String())
	}
	// the token with a slash, a plus, an equals sign and a space went back verbatim
	seen := false
	for _, q := range f.requests() {
		if q.path == f.api("/product/"+teaProduct+"/releases") && strings.Contains(q.rawQuery, "pageToken=") {
			v, _ := urlQueryValue(q.rawQuery, "pageToken")
			if v == "tok/2+= x" {
				seen = true
			}
		}
	}
	if !seen {
		t.Errorf("the token of page 2 was not passed back verbatim: %+v", f.requests())
	}

	// (c) a component
	r = newTeaMRow(t)
	f = r.fixture("tea.example.com", true)
	teaThreePages(f, f.api("/component/"+teaComp1+"/releases"), func(id, v string) string {
		return `{"uuid":"` + id + `","component":"` + teaComp1 + `","version":"` + v + `","createdDate":"2026-01-10T08:00:00Z"}`
	})
	r.want(r.exec(teaVerbReleases, "--api-root", f.root, "--component", teaComp1), teaExitOK)
	d = r.doc()
	if got := strings.Join(teaVersions(d), ","); got != "1,2,3" || teaPath(d, "parent.kind") != "COMPONENT" {
		t.Errorf("(c) %s", r.out.String())
	}
}

func urlQueryValue(rawQuery, key string) (string, bool) {
	q, err := url.ParseQuery(rawQuery)
	if err != nil || !q.Has(key) {
		return "", false
	}
	return q.Get(key), true
}

// teaBigPage is a page of 100 releases whose names are nameLen bytes.
func teaBigPage(next string, nameLen int) string {
	var items []string
	name := strings.Repeat("n", nameLen)
	for i := 0; i < 100; i++ {
		items = append(items, `{"uuid":"`+teaPR1+`","product":"`+teaProduct+`","productName":"`+name+`","version":"`+fmt.Sprint(i)+`","createdDate":"2026-01-10T08:00:00Z","components":[]}`)
	}
	return teaListPage(next, items...)
}

func TestTeaMachineReleasesOutputBound(t *testing.T) { // T-7
	// (a) pages of about 40 KiB under a bound of 100 KiB: two pages, the token of the third
	r := newTeaMRow(t)
	f := r.fixture("tea.example.com", true)
	f.routeFunc(f.api("/product/"+teaProduct+"/releases"), func(q *http.Request) teaAnswer {
		switch q.URL.Query().Get("pageToken") {
		case "":
			return teaJSON(teaBigPage("p2", 300))
		case "p2":
			return teaJSON(teaBigPage("p3", 300))
		case "p3":
			return teaJSON(teaBigPage("p4", 300))
		}
		return teaJSON(teaBigPage("", 300))
	})
	r.want(r.exec(teaVerbReleases, "--api-root", f.root, "--product", teaProduct, "--max-output-bytes", "102400"), teaExitOK)
	d := r.doc()
	if len(teaVersions(d)) != 200 || teaPath(d, "page.stoppedBy") != "OUTPUT_BYTES" || teaPath(d, "page.nextPageToken") != "p3" ||
		teaPath(d, "page.pagesRead") != float64(2) {
		t.Errorf("(a) %d releases, page %v", len(teaVersions(d)), d["page"])
	}
	if r.out.Len() > 102400 {
		t.Errorf("(a) stdout is %d bytes", r.out.Len())
	}

	// (b) a first page of about 200 KiB
	r = newTeaMRow(t)
	f = r.fixture("tea.example.com", true)
	f.route(f.api("/product/"+teaProduct+"/releases"), teaJSON(teaBigPage("", 2000)))
	r.wantFail(r.exec(teaVerbReleases, "--api-root", f.root, "--product", teaProduct, "--max-output-bytes", "102400"), teaExitLimit, teaReasonOutputBytes)
}

func TestTeaMachineReleasesBadToken(t *testing.T) { // T-8
	for _, page := range []string{
		`{"hasNext":true,"results":[]}`,
		`{"hasNext":true,"nextPageToken":"a\u0001b","results":[]}`,
	} {
		r := newTeaMRow(t)
		f := r.fixture("tea.example.com", true)
		f.route(f.api("/product/"+teaProduct+"/releases"), teaJSON(page))
		r.wantFail(r.exec(teaVerbReleases, "--api-root", f.root, "--product", teaProduct), teaExitFailed, teaReasonInvalidResponse)
	}
}

const (
	teaSHA1Of = "a94a8fe5ccb19ba61c4c0873d391e987982fbbd3"                         // SHA-1 of "test"
	teaSHA2Of = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08" // SHA-256 of "test"
)

func TestTeaMachineReleaseGolden(t *testing.T) { // T-9
	r := newTeaMRow(t)
	f := r.fixture("tea.example.com", true)
	f.route(f.api("/productRelease/"+teaPR1), teaJSON(`{"uuid":"`+teaPR1+`","product":"`+teaProduct+`","productName":"Example Product","version":"1.2.3",`+
		`"createdDate":"2026-01-10T08:00:00Z","releaseDate":"2026-01-15T00:00:00Z","preRelease":false,`+
		`"identifiers":[{"idType":"PURL","idValue":"pkg:generic/example/product@1.2.3"}],`+
		`"components":[{"uuid":"`+teaComp1+`","release":"`+teaCR1+`"},{"uuid":"`+teaComp2+`"}],`+
		`"latestCollection":{"uuid":"`+teaPR1+`","version":2,"createdDate":"2026-01-16T10:00:00Z","belongsTo":"PRODUCT_RELEASE","updateReason":{"type":"VEX_UPDATED","comment":"new statement"},"artifacts":[`+
		`{"uuid":"`+teaArtProd+`","version":1,"name":"Product SBOM","type":"BOM","createdDate":"2026-01-10T08:05:00Z","formats":[`+
		`{"mediaType":"application/vnd.cyclonedx+json","description":"CycloneDX JSON","url":"https://cdn.example.com/p.json?sig=1","signatureUrl":"https://cdn.example.com/p.json.sig","checksums":[{"algType":"SHA-256","algValue":"`+teaSHA2Of+`"},{"algType":"SHA-1","algValue":"`+teaSHA1Of+`"}]}]},`+
		`{"uuid":"`+teaArtComp1+`","version":3,"type":"VULNERABILITIES","createdDate":"2026-01-11T08:00:00Z","formats":[`+
		`{"mediaType":"application/vnd.cyclonedx+json"},{"mediaType":"application/vnd.cyclonedx+xml","checksums":[]}]}]}}`))
	f.route(f.api("/productRelease/"+teaPR1+"/cle"), teaJSON(`{"events":[`+
		`{"id":1,"type":"released","effective":"2026-01-15T00:00:00Z","published":"2026-01-15T01:00:00+01:00","version":"1.2.3","versions":[{"version":"1.2.3"},{"range":"vers:generic/>=1.0"}],`+
		`"license":"MIT","supportId":"std","supersededByVersion":"1.2.4","description":"d","reason":"r","references":["https://example.com/r"],"identifiers":[{"idType":"PURL","idValue":"pkg:x"}],"eventId":7},`+
		`{"id":2,"type":"endOfLife","effective":"2027-01-15T00:00:00Z","published":"2026-01-15T00:00:00Z"}],`+
		`"definitions":{"support":[{"id":"std","description":"Standard","url":"https://example.com/support"},{"id":"none","description":"None"}]}}`))
	r.want(r.exec(teaVerbRelease, "--api-root", f.root, "--product-release", teaPR1), teaExitOK)
	r.golden(`{"formatVersion":1,"kind":"TEA_RELEASE","server":{"rootUrl":"` + f.root + `","apiVersion":"1.0.0"},` +
		`"release":{"kind":"PRODUCT_RELEASE","uuid":"` + teaPR1 + `","parentUuid":"` + teaProduct + `","name":"Example Product","version":"1.2.3","createdDate":"2026-01-10T08:00:00Z","releaseDate":"2026-01-15T00:00:00Z","preRelease":"NO",` +
		`"identifiers":[{"idType":"PURL","idValue":"pkg:generic/example/product@1.2.3"}],"components":[{"componentUuid":"` + teaComp1 + `","releaseUuid":"` + teaCR1 + `"},{"componentUuid":"` + teaComp2 + `","releaseUuid":null}]},` +
		`"collection":{"uuid":"` + teaPR1 + `","version":2,"createdDate":"2026-01-16T10:00:00Z","belongsTo":"PRODUCT_RELEASE","updateReason":{"type":"VEX_UPDATED","comment":"new statement"},"artifacts":[` +
		`{"uuid":"` + teaArtProd + `","version":1,"name":"Product SBOM","type":"BOM","createdDate":"2026-01-10T08:05:00Z","formats":[` +
		`{"mediaType":"application/vnd.cyclonedx+json","description":"CycloneDX JSON","externalUrl":"https://cdn.example.com/p.json?sig=1","signatureUrl":"https://cdn.example.com/p.json.sig","checksums":[{"algType":"SHA-256","algValue":"` + teaSHA2Of + `"},{"algType":"SHA-1","algValue":"` + teaSHA1Of + `"}]}]},` +
		`{"uuid":"` + teaArtComp1 + `","version":3,"name":null,"type":"VULNERABILITIES","createdDate":"2026-01-11T08:00:00Z","formats":[` +
		`{"mediaType":"application/vnd.cyclonedx+json","description":null,"externalUrl":null,"signatureUrl":null,"checksums":[]},` +
		`{"mediaType":"application/vnd.cyclonedx+xml","description":null,"externalUrl":null,"signatureUrl":null,"checksums":[]}]}]},` +
		`"cle":{"status":"READ","events":[` +
		`{"id":1,"type":"released","effective":"2026-01-15T00:00:00Z","published":"2026-01-15T00:00:00Z","version":"1.2.3","versions":[{"version":"1.2.3","range":null},{"version":null,"range":"vers:generic/\u003e=1.0"}],` +
		`"license":"MIT","supportId":"std","supersededByVersion":"1.2.4","description":"d","reason":"r","references":["https://example.com/r"],"identifiers":[{"idType":"PURL","idValue":"pkg:x"}],"eventId":7},` +
		`{"id":2,"type":"endOfLife","effective":"2027-01-15T00:00:00Z","published":"2026-01-15T00:00:00Z","version":null,"versions":[],"license":null,"supportId":null,"supersededByVersion":null,"description":null,"reason":null,"references":[],"identifiers":[],"eventId":null}],` +
		`"supportDefinitions":[{"id":"std","description":"Standard","url":"https://example.com/support"},{"id":"none","description":"None","url":null}]}}`)
	if n := len(f.requests()); n != 2 {
		t.Errorf("release made %d requests, want 2", n)
	}
}

func teaComponentReleaseFixture(f *teaFixture, extra string) {
	f.route(f.api("/componentRelease/"+teaCR1), teaJSON(`{"uuid":"`+teaCR1+`","component":"`+teaComp1+`","componentName":"C","version":"1.0",`+
		`"createdDate":"2026-01-10T08:00:00Z","latestCollection":{"uuid":"`+teaCR1+`","version":1,"createdDate":"2026-01-10T08:00:00Z",`+
		`"belongsTo":"COMPONENT_RELEASE","updateReason":{"type":"INITIAL_RELEASE"},"artifacts":[`+extra+`]}}`))
}

func TestTeaMachineReleaseLifecycleAnswers(t *testing.T) { // T-10
	for _, c := range []struct {
		name   string
		answer teaAnswer
		code   teaExit
		reason teaReason
	}{
		{"not implemented", teaTEAError(http.StatusNotImplemented, "NOT_IMPLEMENTED"), teaExitOK, ""},
		{"object unknown", teaTEAError(http.StatusNotFound, "OBJECT_UNKNOWN"), teaExitOK, ""},
		{"server error", teaTEAError(http.StatusInternalServerError, "INTERNAL"), teaExitFailed, teaReasonServerError},
	} {
		r := newTeaMRow(t)
		f := r.fixture("tea.example.com", true)
		teaComponentReleaseFixture(f, "")
		f.route(f.api("/componentRelease/"+teaCR1+"/cle"), c.answer)
		got := r.exec(teaVerbRelease, "--api-root", f.root, "--component-release", teaCR1)
		if c.code != teaExitOK {
			r.wantFail(got, c.code, c.reason)
			continue
		}
		r.want(got, teaExitOK)
		d := r.doc()
		if teaPath(d, "cle.status") != "NOT_AVAILABLE" || fmt.Sprint(teaPath(d, "cle.events")) != "[]" ||
			fmt.Sprint(teaPath(d, "release.components")) != "[]" || teaPath(d, "release.kind") != "COMPONENT_RELEASE" ||
			teaPath(d, "release.parentUuid") != teaComp1 {
			t.Errorf("%s: %s", c.name, r.out.String())
		}
	}
}

func TestTeaMachineOpenEnumerations(t *testing.T) { // T-11
	r := newTeaMRow(t)
	f := r.fixture("tea.example.com", true)
	f.route(f.api("/componentRelease/"+teaCR1), teaJSON(`{"uuid":"`+teaCR1+`","component":"`+teaComp1+`","version":"1.0","createdDate":"2026-01-10T08:00:00Z",`+
		`"latestCollection":{"uuid":"`+teaCR1+`","version":1,"createdDate":"2026-01-10T08:00:00Z","belongsTo":"COMPONENT_RELEASE","updateReason":{"type":"NEW_REASON"},`+
		`"artifacts":[{"uuid":"`+teaArtComp1+`","version":1,"type":"SOMETHING_NEW","createdDate":"2026-01-10T08:00:00Z","formats":[{"mediaType":"x/y","checksums":[{"algType":"SHA-999","algValue":"zz"}]}]}]}}`))
	f.route(f.api("/componentRelease/"+teaCR1+"/cle"), teaJSON(`{"events":[{"id":1,"type":"newEvent","effective":"2026-01-10T08:00:00Z","published":"2026-01-10T08:00:00Z"}]}`))
	r.want(r.exec(teaVerbRelease, "--api-root", f.root, "--component-release", teaCR1), teaExitOK)
	d := r.doc()
	for path, want := range map[string]string{
		"collection.updateReason.type":                         "NEW_REASON",
		"collection.artifacts.0.type":                          "SOMETHING_NEW",
		"collection.artifacts.0.formats.0.checksums.0.algType": "SHA-999",
		"cle.events.0.type":                                    "newEvent",
	} {
		if teaPath(d, path) != want {
			t.Errorf("%s = %v, want %s", path, teaPath(d, path), want)
		}
	}
}

// teaHostile holds every kind of string a terminal or a log must not act on.
const teaHostile = "a\x1b[31mb\r\nc\u202ed\u2028e\"f\\g</script>h\U0001F600i\x7fj&k"

func TestTeaMachineASCIILossless(t *testing.T) { // T-12
	r := newTeaMRow(t)
	f := r.fixture("tea.example.com", true)
	f.route(f.api("/product/"+teaProduct), teaJSON(`{"uuid":"`+teaProduct+`","name":`+teaQuote(teaHostile)+
		`,"identifiers":[{"idType":`+teaQuote(teaHostile)+`,"idValue":`+teaQuote("https://x.example.com/"+teaHostile)+`}]}`))
	r.want(r.exec(teaVerbProduct, "--api-root", f.root, "--uuid", teaProduct), teaExitOK)
	d := r.doc()
	if teaPath(d, "product.name") != teaHostile || teaPath(d, "product.identifiers.0.idType") != teaHostile ||
		teaPath(d, "product.identifiers.0.idValue") != "https://x.example.com/"+teaHostile {
		t.Errorf("strings changed: %v", d["product"])
	}
	for _, bad := range []string{"<", ">", "&"} {
		if strings.Contains(r.out.String(), bad) {
			t.Errorf("stdout carries %q raw", bad)
		}
	}
}

func TestTeaMachineStringBound(t *testing.T) { // T-13
	for _, c := range []struct {
		n    int
		args []string
		code teaExit
	}{
		{teaDefMaxStringBytes + 1, nil, teaExitLimit},
		{teaDefMaxStringBytes, nil, teaExitOK},
		{teaDefMaxStringBytes + 1, []string{"--max-string-bytes", "9000"}, teaExitOK},
	} {
		r := newTeaMRow(t)
		f := r.fixture("tea.example.com", true)
		name := strings.Repeat("N", c.n)
		f.route(f.api("/componentRelease/"+teaCR1), teaJSON(`{"uuid":"`+teaCR1+`","component":"`+teaComp1+`","componentName":"`+name+`","version":"1.0",`+
			`"createdDate":"2026-01-10T08:00:00Z","latestCollection":{"uuid":"`+teaCR1+`","version":1,"createdDate":"2026-01-10T08:00:00Z",`+
			`"belongsTo":"COMPONENT_RELEASE","updateReason":{"type":"INITIAL_RELEASE"},"artifacts":[]}}`))
		f.route(f.api("/componentRelease/"+teaCR1+"/cle"), teaJSON(`{"events":[]}`))
		got := r.exec(teaVerbRelease, append([]string{"--api-root", f.root, "--component-release", teaCR1}, c.args...)...)
		if c.code == teaExitOK {
			r.want(got, teaExitOK)
			continue
		}
		e := r.wantFail(got, c.code, teaReasonStringBytes)
		msg := e["message"].(string)
		if !strings.Contains(msg, "release.name") || !strings.Contains(msg, "8192") || strings.Contains(msg, "NNNN") {
			t.Errorf("message %q", msg)
		}
	}
}

func TestTeaMachineServerErrorText(t *testing.T) { // T-14
	hostile := "bad\x1b[31m\r\nX" + strings.Repeat("\u202e", 3000)
	for _, c := range []struct {
		status int
		code   teaExit
		reason teaReason
	}{
		{http.StatusBadRequest, teaExitFailed, teaReasonFailed},
		{http.StatusInternalServerError, teaExitFailed, teaReasonServerError},
		{http.StatusUnauthorized, teaExitAuth, teaReasonAuth},
	} {
		r := newTeaMRow(t)
		f := r.fixture("tea.example.com", true)
		status := c.status
		f.handle(f.api("/product/"+teaProduct), func(w http.ResponseWriter, q *http.Request) {
			if status == http.StatusUnauthorized {
				// a header line cannot carry ESC or CR LF (the transport refuses it as
				// malformed); the bidirectional control can
				w.Header().Set("WWW-Authenticate", `Bearer error="bad`+strings.Repeat("\u202e", 3000)+`"`)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":"INVALID_REQUEST","message":` + teaQuote(hostile) + `}`))
		})
		e := r.wantFail(r.exec(teaVerbProduct, "--api-root", f.root, "--uuid", teaProduct), c.code, c.reason)
		if n := len([]rune(e["message"].(string))); n > 2051 {
			t.Errorf("%d: message of %d runes", c.status, n)
		}
		if e["httpStatus"] != float64(c.status) {
			t.Errorf("%d: httpStatus %v", c.status, e["httpStatus"])
		}
	}
}

// --- Limits

func TestTeaMachineDocumentBytes(t *testing.T) { // T-29
	// (a)
	r := newTeaMRow(t)
	f := r.fixture("tea.example.com", true)
	f.route(f.api("/product/"+teaProduct), teaJSON(`{"uuid":"`+teaProduct+`","name":"`+strings.Repeat("x", 2048)+`","identifiers":[]}`))
	r.wantFail(r.exec(teaVerbProduct, "--api-root", f.root, "--uuid", teaProduct, "--max-document-bytes", "1024"), teaExitLimit, teaReasonDocumentBytes)

	// (b) the limits the library gets
	for _, c := range []struct{ doc, art int64 }{{teaMinMaxDocumentBytes, 1}, {teaDefMaxDocumentBytes, teaDefMaxArtifactBytes}, {teaMaxMaxDocumentBytes, teaMaxMaxArtifactBytes}} {
		in := teaMachineInputsDefault(teaVerbDownload)
		in.maxDocumentBytes, in.maxArtifactBytes, in.maxRedirects = c.doc, c.art, 7
		l := teaMachineLimits(in, nil)
		if l.JSONBytes != c.doc || l.DownloadBytes != c.art || l.Redirects != 7 || l.JSONBytes == 0 || l.DownloadBytes == 0 {
			t.Errorf("%+v from %+v", l, c)
		}
	}
	in := teaMachineInputsDefault(teaVerbProduct)
	if l := teaMachineLimits(in, nil); l.DownloadBytes != teaDefMaxArtifactBytes {
		t.Errorf("default download bound %d", l.DownloadBytes)
	}
}

func TestTeaMachineRedirectLimit(t *testing.T) { // T-30
	r := newTeaMRow(t)
	f := r.fixture("tea.example.com", true)
	f.route(f.api("/product/"+teaProduct), teaAnswer{status: http.StatusFound, location: "/r1"})
	f.route("/r1", teaAnswer{status: http.StatusFound, location: "/r2"})
	f.route("/r2", teaAnswer{status: http.StatusFound, location: "/r3"})
	f.route("/r3", teaJSON(`{"uuid":"`+teaProduct+`","name":"P","identifiers":[]}`))
	r.wantFail(r.exec(teaVerbProduct, "--api-root", f.root, "--uuid", teaProduct, "--max-redirects", "2"), teaExitRefusedURL, teaReasonRedirectLimit)
	r.want(r.exec(teaVerbProduct, "--api-root", f.root, "--uuid", teaProduct, "--max-redirects", "3"), teaExitOK)
}

// teaStall answers nothing until the request ends.
func teaStall(w http.ResponseWriter, q *http.Request) {
	<-q.Context().Done()
}

func TestTeaMachineDeadline(t *testing.T) { // T-31
	r := newTeaMRow(t)
	f := r.fixture("tea.example.com", true)
	f.handle(f.api("/product/"+teaProduct), teaStall)
	start := time.Now()
	r.wantFail(r.exec(teaVerbProduct, "--api-root", f.root, "--uuid", teaProduct, "--deadline", "1s"), teaExitLimit, teaReasonDeadline)
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("took %s", d)
	}
}

func TestTeaMachineRequestTimeout(t *testing.T) { // T-32
	r := newTeaMRow(t)
	r.seam.callTimeout = 200 * time.Millisecond
	f := r.fixture("tea.example.com", true)
	f.handle(f.api("/product/"+teaProduct), teaStall)
	r.wantFail(r.exec(teaVerbProduct, "--api-root", f.root, "--uuid", teaProduct), teaExitLimit, teaReasonRequestTimeout)
}

func TestTeaMachineDiscoveryResults(t *testing.T) { // T-33
	r := newTeaMRow(t)
	f := r.fixture("tea.example.com", true)
	ids := make([]string, 33)
	for i := range ids {
		ids[i] = fmt.Sprintf("11111111-1111-4111-8111-%012d", i)
	}
	f.route(f.api("/discovery"), teaJSON(teaDiscoveryJSON(f.root, ids...)))
	r.wantFail(r.exec(teaVerbResolve, teaResolveArgs(f, teaPR1)...), teaExitLimit, teaReasonDiscoveryResults)
}

func TestTeaMachineLimitFlags(t *testing.T) { // T-35
	common := map[string][]string{
		"connect-timeout":    {"0s", "-1s", "61s"},
		"deadline":           {"0s", "-1s", "25h"},
		"max-document-bytes": {"0", "-1", "268435457"},
		"max-redirects":      {"0", "-1", "21"},
		"max-string-bytes":   {"0", "-1", "1048577"},
		"max-output-bytes":   {"0", "-1", "1073741825"},
	}
	r := newTeaMRow(t)
	f := r.fixture("tea.example.com", true)
	check := func(verb teaMachineVerb, base []string, flag, v string) {
		t.Helper()
		e := r.wantFail(r.exec(verb, append(append([]string(nil), base...), "--"+flag+"="+v)...), teaExitUsage, teaReasonUsage)
		if !strings.Contains(e["message"].(string), "--"+flag) {
			t.Errorf("--%s=%s: message %v", flag, v, e["message"])
		}
	}
	product := []string{"--api-root", f.root, "--uuid", teaProduct}
	for flag, vs := range common {
		for _, v := range vs {
			check(teaVerbProduct, product, flag, v)
		}
	}
	releases := []string{"--api-root", f.root, "--product", teaProduct}
	for flag, vs := range map[string][]string{"page-size": {"0", "-1", "101"}, "max-pages": {"0", "-1", "1001"}, "page-token": {""}} {
		for _, v := range vs {
			check(teaVerbReleases, releases, flag, v)
		}
	}
	dir := t.TempDir() + "/out"
	download := []string{"--api-root", f.root, "--component-release", teaCR1, "--collection-version", "1", "--dir", dir}
	for flag, vs := range map[string][]string{
		"max-artifact-bytes": {"0", "-1", "17179869185"},
		"max-total-bytes":    {"0", "-1", "68719476737"},
		"max-files":          {"0", "-1", "100001"},
	} {
		for _, v := range vs {
			check(teaVerbDownload, download, flag, v)
		}
	}
	// an unknown duration is a flag error of the same kind
	e := r.wantFail(r.exec(teaVerbProduct, append(product, "--deadline", "soon")...), teaExitUsage, teaReasonUsage)
	if !strings.Contains(e["message"].(string), "--deadline") {
		t.Errorf("message %v", e["message"])
	}
	teaNoRequests(t, f)
	if len(r.m.connected()) != 0 {
		t.Errorf("connects %v", r.m.connected())
	}
}
