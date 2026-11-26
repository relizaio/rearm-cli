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
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

// The list verbs products and components of the TEA machine mode (task TPF-240, design
// section 4, rows T-780 to T-795). Every row runs both verbs unless it says otherwise.

// teaListVerb is one list verb with its TEA path and the key of its rows.
type teaListVerb struct {
	verb teaMachineVerb
	path string // under the API root
	key  string // the rows of the document
	kind string
}

var teaListVerbs = []teaListVerb{
	{teaVerbProducts, "/products", "products", "TEA_PRODUCT_LIST"},
	{teaVerbComponents, "/components", "components", "TEA_COMPONENT_LIST"},
}

// teaListRowJSON is a product or a component of a list answer.
func teaListRowJSON(id, name, identifiers string) string {
	return `{"uuid":"` + id + `","name":` + teaQuote(name) + `,"identifiers":` + identifiers + `}`
}

// teaListAnswerJSON is a list answer: hasNext with the token when next is set.
func teaListAnswerJSON(next string, rows ...string) string {
	if next == "" {
		return `{"hasNext":false,"results":[` + strings.Join(rows, ",") + `]}`
	}
	return `{"hasNext":true,"nextPageToken":` + teaQuote(next) + `,"results":[` + strings.Join(rows, ",") + `]}`
}

// teaListRows are n rows named name-1 .. name-n with uuids that keep their order visible.
func teaListRows(n int) []string {
	var rows []string
	for i := 1; i <= n; i++ {
		rows = append(rows, teaListRowJSON(fmt.Sprintf("77777777-7777-4777-8777-%012d", i), fmt.Sprintf("name-%d", i), `[]`))
	}
	return rows
}

// teaListNames are the names of the rows of a list document.
func teaListNames(d map[string]any, key string) []string {
	var out []string
	rows, _ := d[key].([]any)
	for _, x := range rows {
		out = append(out, x.(map[string]any)["name"].(string))
	}
	return out
}

// teaListRun is a row with a fixture serving body on the list path of v.
func teaListRun(t *testing.T, v teaListVerb, body string) (*teaMRow, *teaFixture) {
	r := newTeaMRow(t)
	f := r.fixture("tea.example.com", true)
	f.route(f.api(v.path), teaJSON(body))
	return r, f
}

func TestTeaMachineListOneRequestGolden(t *testing.T) { // T-780
	for _, v := range teaListVerbs {
		ids := `[{"idType":"PURL","idValue":"pkg:generic/example/a"},{"idType":"CPE","idValue":"cpe:2.3:a:example:a:*:*:*:*:*:*:*:*"},` +
			`{"idType":"TEI","idValue":"tei://tea.example.com/uuid/` + teaProduct + `"},{"idType":"SWID","idValue":"swid-tag-1"}]`
		rows := teaListRowJSON(teaProduct, "Example A", ids) + "," + teaListRowJSON(teaComp1, "Example B", `[]`)
		r, f := teaListRun(t, v, teaListAnswerJSON("b3BhcXVl", rows))
		r.want(r.exec(v.verb, "--api-root", f.root), teaExitOK)
		r.golden(`{"formatVersion":1,"kind":"` + v.kind + `","server":{"rootUrl":"` + f.root + `","apiVersion":"1.0.0"},"` + v.key + `":[` + rows +
			`],"page":{"nextPageToken":"b3BhcXVl","rowsReceived":2,"cutBy":"NONE"}}`)
		reqs := f.requests()
		if len(reqs) != 1 || reqs[0].method != http.MethodGet || reqs[0].path != f.api(v.path) || reqs[0].rawQuery != "pageSize=100" {
			t.Errorf("%s: requests %+v, want one GET %s?pageSize=100", v.verb, reqs, f.api(v.path))
		}
	}
}

func TestTeaMachineListNextTokenOnlyWithHasNext(t *testing.T) { // T-781
	for _, v := range teaListVerbs {
		for _, c := range []struct {
			name, body string
			rows       int
		}{
			{"(a) hasNext false", teaListAnswerJSON("", teaListRows(2)...), 2},
			{"(b) a token without hasNext", `{"hasNext":false,"nextPageToken":"stray","results":[` + strings.Join(teaListRows(2), ",") + `]}`, 2},
			{"(c) no rows", `{"hasNext":false,"results":[]}`, 0},
			{"(d) no results key", `{"hasNext":false}`, 0},
		} {
			r, f := teaListRun(t, v, c.body)
			r.want(r.exec(v.verb, "--api-root", f.root), teaExitOK)
			d := r.doc()
			if p, ok := d["page"].(map[string]any); !ok || p["nextPageToken"] != nil || p["rowsReceived"] != float64(c.rows) || p["cutBy"] != "NONE" {
				t.Errorf("%s %s: page %v", v.verb, c.name, d["page"])
			}
			if len(teaListNames(d, v.key)) != c.rows {
				t.Errorf("%s %s: %s", v.verb, c.name, r.out.String())
			}
			if c.rows == 0 && !strings.Contains(r.out.String(), `"`+v.key+`":[],`) {
				t.Errorf("%s %s: no empty list: %s", v.verb, c.name, r.out.String())
			}
			if !strings.Contains(r.out.String(), `"nextPageToken":null`) {
				t.Errorf("%s %s: the token is not null: %s", v.verb, c.name, r.out.String())
			}
		}
	}
}

func TestTeaMachineListFlagValuesAreQueryValues(t *testing.T) { // T-782
	value := "pkg:generic/a&b=c#d?e%f+g/h i\u00e9"
	token := "-tok&x=1=2"
	for _, v := range teaListVerbs {
		r, f := teaListRun(t, v, teaListAnswerJSON("", teaListRows(1)...))
		r.want(r.exec(v.verb, "--api-root", f.root, "--id-type", "PURL", "--id-value="+value, "--page-size", "7",
			"--page-token="+token, "--sort-order", "desc"), teaExitOK)
		reqs := f.requests()
		if len(reqs) != 1 || reqs[0].path != f.api(v.path) {
			t.Fatalf("%s: requests %+v", v.verb, reqs)
		}
		q, err := url.ParseQuery(reqs[0].rawQuery)
		if err != nil {
			t.Fatalf("%s: query %q: %v", v.verb, reqs[0].rawQuery, err)
		}
		want := map[string]string{"idType": "PURL", "idValue": value, "pageSize": "7", "pageToken": token, "sortOrder": "desc"}
		if len(q) != len(want) {
			t.Errorf("%s: query keys %v, want %v", v.verb, q, want)
		}
		for k, w := range want {
			if got := q[k]; len(got) != 1 || got[0] != w {
				t.Errorf("%s: %s = %q, want exactly %q", v.verb, k, got, w)
			}
		}
		if q.Has("sortField") {
			t.Errorf("%s: sortField was sent", v.verb)
		}
	}
}

func TestTeaMachineListRefusedFlags(t *testing.T) { // T-783
	const secret = "SECRETVALUE"
	typeMsg := "--id-type must be one of the TEA 1.0.0 identifier types (CPE, TEI, PURL, COMPLIANCE_DOCUMENT)"
	cases := []struct {
		args []string
		msg  string
	}{
		{[]string{"--id-type", "PURL"}, "--id-type and --id-value are given together or not at all"},
		{[]string{"--id-value=" + secret}, "--id-type and --id-value are given together or not at all"},
		{[]string{"--id-type", "purl", "--id-value=" + secret}, typeMsg},
		{[]string{"--id-type", "SWID", "--id-value=" + secret}, typeMsg},
		{[]string{"--id-type=", "--id-value=" + secret}, typeMsg},
		{[]string{"--id-type", "PURL", "--id-value="}, "--id-value must not be empty"},
		{[]string{"--id-type", "PURL", "--id-value=" + secret + "\x01"}, "--id-value has a control character"},
		{[]string{"--id-type", "PURL", "--id-value=" + secret + "\xff"}, "--id-value is not valid UTF-8"},
		{[]string{"--id-type", "PURL", "--id-value=" + secret + strings.Repeat("Z", 250), "--max-string-bytes", "256"}, "--id-value is longer than --max-string-bytes (256 bytes)"},
		{[]string{"--sort-order", "ASC"}, "--sort-order must be asc or desc"},
		{[]string{"--sort-order", "random"}, "--sort-order must be asc or desc"},
		{[]string{"--sort-order="}, "--sort-order must be asc or desc"},
		{[]string{"--page-token="}, "--page-token must not be empty"},
		{[]string{"--page-token=" + secret + "\x01"}, "--page-token has a control character"},
		{[]string{"extra"}, ""},
		{[]string{"--uuid", teaProduct}, "unknown flag: --uuid"},
		{[]string{"--max-pages", "2"}, "unknown flag: --max-pages"},
	}
	for _, v := range teaListVerbs {
		r := newTeaMRow(t)
		f := r.fixture("tea.example.com", true)
		f.route(f.api(v.path), teaJSON(teaListAnswerJSON("", teaListRows(1)...)))
		for _, c := range cases {
			e := r.wantFail(r.exec(v.verb, append([]string{"--api-root", f.root}, c.args...)...), teaExitUsage, teaReasonUsage)
			msg, _ := e["message"].(string)
			want := c.msg
			if want == "" {
				want = "tea machine " + string(v.verb) + " takes no positional arguments"
			}
			if msg != want {
				t.Errorf("%s %q: message %q, want %q", v.verb, c.args, msg, want)
			}
			if strings.Contains(msg, secret) || strings.Contains(msg, "ZZZZ") {
				t.Errorf("%s %q: the message repeats the value: %q", v.verb, c.args, msg)
			}
		}
		teaNoRequests(t, f)
		if len(r.m.connected()) != 0 {
			t.Errorf("%s: connects %v", v.verb, r.m.connected())
		}
	}
}

func TestTeaMachineListFlagsOfEveryVerb(t *testing.T) { // T-784, the part beside the bounds tables
	common := []string{"credential-stdin", "usehttp", "allow-this-host", "connect-timeout", "deadline", "max-document-bytes",
		"max-redirects", "max-string-bytes", "max-output-bytes"}
	own := []string{"api-root", "id-type", "id-value", "sort-order", "page-token", "page-size", "max-rows"}
	for _, v := range teaListVerbs {
		c := &cobra.Command{Use: string(v.verb)}
		addTeaMachineFlags(c, v.verb)
		for _, name := range append(append([]string(nil), common...), own...) {
			if c.Flags().Lookup(name) == nil {
				t.Errorf("%s has no --%s", v.verb, name)
			}
		}
		for _, name := range []string{"uuid", "product", "component", "max-pages", "tei", "dir"} {
			if c.Flags().Lookup(name) != nil {
				t.Errorf("%s has --%s", v.verb, name)
			}
		}
		in := teaFlagsOf(t, v.verb)
		if in.pageSize != 100 || in.maxRows != 1000 || in.pageTokenSet || in.idType != "" || in.idValue != "" || in.sortOrder != "" {
			t.Errorf("%s: defaults %+v", v.verb, in)
		}
	}
	// a verb without --max-rows keeps the default, so nothing of it leaks into another verb
	if in := teaFlagsOf(t, teaVerbReleases, "--product", teaProduct); in.maxRows != teaDefMaxRows {
		t.Errorf("releases: maxRows %d", in.maxRows)
	}
}

func TestTeaMachineListServerIgnoresThePageSize(t *testing.T) { // T-785
	for _, v := range teaListVerbs {
		r, f := teaListRun(t, v, teaListAnswerJSON("", teaListRows(5)...))
		r.want(r.exec(v.verb, "--api-root", f.root, "--page-size", "2"), teaExitOK)
		d := r.doc()
		if got := strings.Join(teaListNames(d, v.key), ","); got != "name-1,name-2,name-3,name-4,name-5" ||
			teaPath(d, "page.rowsReceived") != float64(5) || teaPath(d, "page.cutBy") != "NONE" {
			t.Errorf("%s: %s", v.verb, r.out.String())
		}
		if reqs := f.requests(); len(reqs) != 1 || reqs[0].rawQuery != "pageSize=2" {
			t.Errorf("%s: requests %+v", v.verb, reqs)
		}
	}
}

func TestTeaMachineListCutByMaxRows(t *testing.T) { // T-786
	long := strings.Repeat("N", teaDefMaxStringBytes+1)
	fourthLong := teaListRows(5)
	fourthLong[3] = teaListRowJSON("77777777-7777-4777-8777-000000000004", long, `[{"idType":"PURL","idValue":`+teaQuote(long)+`}]`)
	for _, v := range teaListVerbs {
		for _, c := range []struct {
			name     string
			body     string
			names    string
			received int
			cut      string
			next     any
		}{
			{"(a) five rows with a token", teaListAnswerJSON("tok-2", teaListRows(5)...), "name-1,name-2,name-3", 5, "MAX_ROWS", "tok-2"},
			{"(b) five rows", teaListAnswerJSON("", teaListRows(5)...), "name-1,name-2,name-3", 5, "MAX_ROWS", nil},
			{"(c) exactly three rows", teaListAnswerJSON("", teaListRows(3)...), "name-1,name-2,name-3", 3, "NONE", nil},
			{"(d) a long string in a row left out", teaListAnswerJSON("", fourthLong...), "name-1,name-2,name-3", 5, "MAX_ROWS", nil},
		} {
			r, f := teaListRun(t, v, c.body)
			r.want(r.exec(v.verb, "--api-root", f.root, "--max-rows", "3"), teaExitOK)
			d := r.doc()
			if got := strings.Join(teaListNames(d, v.key), ","); got != c.names || teaPath(d, "page.rowsReceived") != float64(c.received) ||
				teaPath(d, "page.cutBy") != c.cut || teaPath(d, "page.nextPageToken") != c.next {
				t.Errorf("%s %s: %s", v.verb, c.name, r.out.String())
			}
		}
	}
}

func TestTeaMachineListLimits(t *testing.T) { // T-787
	long := strings.Repeat("L", teaDefMaxStringBytes+1)
	for _, v := range teaListVerbs {
		// (a) the answer is larger than --max-document-bytes
		r, f := teaListRun(t, v, teaListAnswerJSON("", teaListRowJSON(teaProduct, strings.Repeat("x", 2048), `[]`)))
		r.wantFail(r.exec(v.verb, "--api-root", f.root, "--max-document-bytes", "1024"), teaExitLimit, teaReasonDocumentBytes)

		// (b) a string of a kept row is longer than --max-string-bytes
		for _, c := range []struct{ row, path string }{
			{teaListRowJSON(teaComp2, long, `[]`), ".name"},
			{teaListRowJSON(teaComp2, "B", `[{"idType":"PURL","idValue":"ok"},{"idType":"PURL","idValue":`+teaQuote(long)+`}]`), ".identifiers[1].idValue"},
			{teaListRowJSON(teaComp2, "B", `[{"idType":`+teaQuote(long)+`,"idValue":"ok"}]`), ".identifiers[0].idType"},
		} {
			r, f = teaListRun(t, v, teaListAnswerJSON("", teaListRowJSON(teaComp1, "A", `[]`), c.row))
			e := r.wantFail(r.exec(v.verb, "--api-root", f.root), teaExitLimit, teaReasonStringBytes)
			msg, _ := e["message"].(string)
			want := "the server string at " + v.key + "[1]" + c.path + " is longer than --max-string-bytes (8192 bytes)"
			if msg != want || strings.Contains(msg, "LLLL") {
				t.Errorf("%s: message %q, want %q", v.verb, msg, want)
			}
		}

		// (c) rows that fit both bounds, in a document larger than --max-output-bytes
		var rows []string
		for i := 0; i < 20; i++ {
			rows = append(rows, teaListRowJSON(teaComp1, strings.Repeat("o", 4000), `[]`))
		}
		r, f = teaListRun(t, v, teaListAnswerJSON("", rows...))
		r.wantFail(r.exec(v.verb, "--api-root", f.root, "--max-output-bytes", "65536"), teaExitLimit, teaReasonOutputBytes)
	}
}

// teaStallBody sends the headers and the start of a list answer, then nothing until the
// request ends.
func teaStallBody(w http.ResponseWriter, q *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"hasNext":false,"results":[`))
	if fl, ok := w.(http.Flusher); ok {
		fl.Flush()
	}
	<-q.Context().Done()
}

func TestTeaMachineListTimeouts(t *testing.T) { // T-788
	for _, v := range teaListVerbs {
		// (a) the call timeout
		r := newTeaMRow(t)
		r.seam.callTimeout = 200 * time.Millisecond
		f := r.fixture("tea.example.com", true)
		f.handle(f.api(v.path), teaStallBody)
		r.wantFail(r.exec(v.verb, "--api-root", f.root), teaExitLimit, teaReasonRequestTimeout)

		// (b) the deadline
		r = newTeaMRow(t)
		f = r.fixture("tea.example.com", true)
		f.handle(f.api(v.path), teaStallBody)
		start := time.Now()
		r.wantFail(r.exec(v.verb, "--api-root", f.root, "--deadline", "1s"), teaExitLimit, teaReasonDeadline)
		if d := time.Since(start); d > 5*time.Second {
			t.Errorf("%s: the deadline took %s", v.verb, d)
		}
	}
}

func TestTeaMachineListTokenMustBePassable(t *testing.T) { // T-789
	for _, v := range teaListVerbs {
		for _, body := range []string{
			`{"hasNext":true,"results":[]}`,
			`{"hasNext":true,"nextPageToken":"a\u0001b","results":[]}`,
			teaListAnswerJSON(strings.Repeat("t", teaDefMaxStringBytes+1), teaListRows(1)...),
		} {
			r, f := teaListRun(t, v, body)
			r.wantFail(r.exec(v.verb, "--api-root", f.root), teaExitFailed, teaReasonInvalidResponse)
		}
	}
}

func TestTeaMachineListStringsAreData(t *testing.T) { // T-790
	name := "x\u0001y\nz\x1b[31m<script>alert(\"q\")</script>'>\u202eabc\u00e9\u4e2d"
	value := "pkg:generic/<b>\"v\"</b>\u202e\r\n\u00e9"
	token := "<i>\"next\"</i>&\u00e9\u4e2d"
	for _, v := range teaListVerbs {
		rows := teaListRowJSON(teaComp1, name, `[{"idType":`+teaQuote(teaHostile)+`,"idValue":`+teaQuote(value)+`}]`) + "," +
			teaListRowJSON(teaComp2, teaHostile, `[]`)
		r, f := teaListRun(t, v, teaListAnswerJSON(token, rows))
		r.want(r.exec(v.verb, "--api-root", f.root), teaExitOK) // one line of printable ASCII
		d := r.doc()
		if teaPath(d, v.key+".0.name") != name || teaPath(d, v.key+".0.identifiers.0.idType") != teaHostile ||
			teaPath(d, v.key+".0.identifiers.0.idValue") != value || teaPath(d, v.key+".1.name") != teaHostile ||
			teaPath(d, "page.nextPageToken") != token {
			t.Errorf("%s: strings changed: %s", v.verb, r.out.String())
		}
		for _, bad := range []string{"<", ">", "&"} {
			if strings.Contains(r.out.String(), bad) {
				t.Errorf("%s: stdout carries %q raw", v.verb, bad)
			}
		}
	}
}

func TestTeaMachineListServerAnswers(t *testing.T) { // T-791
	for _, v := range teaListVerbs {
		for _, c := range []struct {
			name   string
			answer teaAnswer
			args   []string
			code   teaExit
			reason teaReason
			status any
		}{
			{"(a) an expired token", teaTEAError(http.StatusBadRequest, "INVALID_PAGE_TOKEN"), []string{"--page-token=expired"}, teaExitFailed, teaReasonFailed, float64(400)},
			{"(b) 401", teaAnswer{status: http.StatusUnauthorized}, nil, teaExitAuth, teaReasonAuth, float64(401)},
			{"(c) 500", teaTEAError(http.StatusInternalServerError, "INTERNAL"), nil, teaExitFailed, teaReasonServerError, float64(500)},
			{"(d) not JSON", teaAnswer{status: http.StatusOK, contentType: "application/json", body: "this is not json"}, nil, teaExitFailed, teaReasonInvalidResponse, nil},
		} {
			r := newTeaMRow(t)
			f := r.fixture("tea.example.com", true)
			f.route(f.api(v.path), c.answer)
			e := r.wantFail(r.exec(v.verb, append([]string{"--api-root", f.root}, c.args...)...), c.code, c.reason)
			if e["httpStatus"] != c.status {
				t.Errorf("%s %s: httpStatus %v, want %v", v.verb, c.name, e["httpStatus"], c.status)
			}
			if u, _ := e["url"].(string); strings.Contains(u, "?") || strings.Contains(u, "expired") {
				t.Errorf("%s %s: url %q carries the query", v.verb, c.name, u)
			}
		}
	}
}

func TestTeaMachineListRefusedTargets(t *testing.T) { // T-792
	for _, v := range teaListVerbs {
		for _, c := range teaRefusedTargets {
			r := newTeaMRow(t)
			f := r.fixture("tea.example.com", true)
			if c.resolves != "" {
				r.m.resolveTo(c.host, c.resolves)
			}
			r.wantRefused(r.exec(v.verb, "--api-root", "https://"+c.host+":"+f.port+"/tea"), c.addr, c.class)
			teaNoRequests(t, f)
			if len(r.m.connected()) != 0 {
				t.Errorf("%s %s: connects %v", v.verb, c.host, r.m.connected())
			}
		}
	}
}

func TestTeaMachineListCredentialStaysOnItsRoot(t *testing.T) { // T-793
	const secret = "tok-list-1"
	page := teaListAnswerJSON("", teaListRows(1)...)
	for _, v := range teaListVerbs {
		noLeak := func(r *teaMRow, what string) {
			t.Helper()
			if strings.Contains(r.out.String(), secret) || strings.Contains(r.errOut.String(), secret) {
				t.Errorf("%s %s: the credential is on stdout or stderr", v.verb, what)
			}
		}
		// (a) the list under the root carries the credential
		r, f := teaListRun(t, v, page)
		r.env[teaEnvAuthRoot], r.env[teaEnvToken] = f.root, secret
		r.want(r.exec(v.verb, "--api-root", f.root), teaExitOK)
		if reqs := f.requests(); len(reqs) != 1 || reqs[0].authorization != "Bearer "+secret {
			t.Errorf("%s (a): requests %+v", v.verb, reqs)
		}
		noLeak(r, "(a)")

		// (b) a redirect to another host loses it
		r = newTeaMRow(t)
		f = r.fixture("tea.example.com", true)
		other := r.fixture("other.example.com", true)
		f.route(f.api(v.path), teaAnswer{status: http.StatusFound, location: other.origin + "/moved"})
		other.route("/moved", teaJSON(page))
		r.env[teaEnvAuthRoot], r.env[teaEnvToken] = f.root, secret
		r.want(r.exec(v.verb, "--api-root", f.root), teaExitOK)
		if f.count(f.api(v.path)) != 1 || other.count("/moved") != 1 {
			t.Errorf("%s (b): requests %+v %+v", v.verb, f.requests(), other.requests())
		}
		teaNoAuthorization(t, other)
		noLeak(r, "(b)")

		// (c) an --api-root on a host other than the credential's
		r = newTeaMRow(t)
		f = r.fixture("tea.example.com", true)
		other = r.fixture("other.example.com", true)
		other.route(other.api(v.path), teaJSON(page))
		r.env[teaEnvAuthRoot], r.env[teaEnvToken] = f.root, secret
		r.want(r.exec(v.verb, "--api-root", other.root), teaExitOK)
		teaNoAuthorization(t, f, other)
		if other.count(other.api(v.path)) != 1 {
			t.Errorf("%s (c): requests %+v", v.verb, other.requests())
		}
		noLeak(r, "(c)")
	}
}

func TestTeaMachineListTokenIsNotAURL(t *testing.T) { // T-794
	const token = "https://evil.example.com/next?x=1"
	for _, v := range teaListVerbs {
		r, f := teaListRun(t, v, teaListAnswerJSON(token, teaListRows(1)...))
		r.want(r.exec(v.verb, "--api-root", f.root), teaExitOK)
		if teaPath(r.doc(), "page.nextPageToken") != token {
			t.Errorf("%s: the token was not passed on: %s", v.verb, r.out.String())
		}
		r.want(r.exec(v.verb, "--api-root", f.root, "--page-token="+token), teaExitOK)
		reqs := f.requests()
		if len(reqs) != 2 || reqs[1].path != f.api(v.path) {
			t.Fatalf("%s: requests %+v", v.verb, reqs)
		}
		if got, _ := urlQueryValue(reqs[1].rawQuery, "pageToken"); got != token {
			t.Errorf("%s: pageToken %q, want %q", v.verb, got, token)
		}
		r.m.mu.Lock()
		lookups := append([]string(nil), r.m.lookups...)
		r.m.mu.Unlock()
		for _, h := range lookups {
			if strings.EqualFold(h, "evil.example.com") {
				t.Errorf("%s: evil.example.com was resolved", v.verb)
			}
		}
		for _, d := range r.net.dialedAddrs() {
			if strings.Contains(d, "evil") {
				t.Errorf("%s: %s was dialled", v.verb, d)
			}
		}
	}
}

func TestTeaMachineListCapabilityCheck(t *testing.T) { // T-795
	for _, c := range []struct {
		args []string
		msg  string
	}{
		{[]string{"tea", "machine", "products"}, "--api-root is required"},
		{[]string{"tea", "machine", "components"}, "--api-root is required"},
		{[]string{"tea", "machine"}, "a verb is required: resolve, product, component, products, components, releases, release or download"},
		{[]string{"tea", "machine", "nosuchverb"}, "unknown verb; the verbs are resolve, product, component, products, components, releases, release and download"},
	} {
		code, out, errOut := teaChild(t, t.TempDir(), nil, c.args...)
		if code != 2 || out != "" {
			t.Errorf("%v: exit %d stdout %q", c.args, code, out)
		}
		var e map[string]any
		if strings.Count(errOut, "\n") != 1 || json.Unmarshal([]byte(errOut), &e) != nil || e["reason"] != "USAGE" ||
			e["kind"] != "TEA_ERROR" || e["formatVersion"] != float64(1) || e["message"] != c.msg {
			t.Errorf("%v: stderr %q, want the message %q", c.args, errOut, c.msg)
		}
	}
}
