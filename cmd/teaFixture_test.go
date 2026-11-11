package cmd

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/oolong-project/tea-client-go"
)

// An in-process TEA 1.0.0 server for the tea commands' tests, reached through a dialer that
// maps DNS names (the TEI parser refuses IP literals) to the fixtures' listeners and
// records every address dialled. Nothing here touches the network or the environment.

// teaRecorded is one request a fixture received.
type teaRecorded struct {
	method        string
	host          string
	path          string
	rawQuery      string
	authorization string
	body          string
}

// teaAnswer is what a fixture route answers.
type teaAnswer struct {
	status      int
	contentType string
	body        string
	location    string
}

func teaJSON(body string) teaAnswer {
	return teaAnswer{status: http.StatusOK, contentType: "application/json", body: body}
}

func teaTEAError(status int, code string) teaAnswer {
	return teaAnswer{status: status, contentType: "application/json", body: fmt.Sprintf(`{"error":%q}`, code)}
}

// teaFixture is one TEA server on one host name and port.
type teaFixture struct {
	t      *testing.T
	srv    *httptest.Server
	scheme string
	host   string // DNS name, e.g. tea.example.com
	port   string
	origin string // scheme://host:port
	root   string // origin + "/tea"

	mu            sync.Mutex
	reqs          []teaRecorded
	routes        map[string]func(r *http.Request) teaAnswer // full path -> answer
	requireBearer string                                     // when set, every path under /tea/ but the token endpoint needs it
	tokenKeyID    string                                     // the API key the token endpoint accepts
	tokenSecret   string
	tokenIssued   string // the token it issues
}

// teaNet is the dialer of one test: DNS name and port -> fixture listener.
type teaNet struct {
	mu     sync.Mutex
	addrs  map[string]string // host:port -> 127.0.0.1:port
	dialed []string
	pool   *x509.CertPool
}

func newTeaNet() *teaNet {
	return &teaNet{addrs: map[string]string{}, pool: x509.NewCertPool()}
}

func (n *teaNet) dialedAddrs() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string(nil), n.dialed...)
}

// client is the *http.Client the rows pass in teaIO.httpClient: no proxy, the fixtures'
// certificates trusted, every dial recorded and only fixture addresses reachable.
func (n *teaNet) client() *http.Client {
	d := &net.Dialer{Timeout: 5 * time.Second}
	tr := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			n.mu.Lock()
			n.dialed = append(n.dialed, addr)
			real, ok := n.addrs[addr]
			n.mu.Unlock()
			if !ok {
				return nil, fmt.Errorf("test dialer: %s is not a fixture", addr)
			}
			return d.DialContext(ctx, network, real)
		},
		TLSClientConfig: &tls.Config{RootCAs: n.pool},
	}
	return &http.Client{Transport: tr}
}

// newTeaFixture starts a plain-http (tlsOn false) or TLS fixture answering as host.
func newTeaFixture(t *testing.T, n *teaNet, host string, tlsOn bool) *teaFixture {
	t.Helper()
	f := &teaFixture{t: t, host: host, routes: map[string]func(*http.Request) teaAnswer{}}
	h := http.HandlerFunc(f.serve)
	if tlsOn {
		f.srv = httptest.NewTLSServer(h)
		f.scheme = "https"
		n.pool.AddCert(f.srv.Certificate())
	} else {
		f.srv = httptest.NewServer(h)
		f.scheme = "http"
	}
	t.Cleanup(f.srv.Close)
	_, port, err := net.SplitHostPort(f.srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	f.port = port
	f.origin = f.scheme + "://" + host + ":" + port
	f.root = f.origin + "/tea"
	n.mu.Lock()
	n.addrs[host+":"+port] = f.srv.Listener.Addr().String()
	n.mu.Unlock()
	// the default well-known document lists this fixture's own root
	f.route("/.well-known/tea", teaJSON(`{"schemaVersion":1,"endpoints":[{"url":"`+f.root+`","versions":["1.0.0"]}]}`))
	return f
}

// api is the path of an API operation under this fixture's root.
func (f *teaFixture) api(p string) string { return "/tea/v1.0.0" + p }

func (f *teaFixture) route(path string, a teaAnswer) {
	f.routeFunc(path, func(*http.Request) teaAnswer { return a })
}

func (f *teaFixture) routeFunc(path string, fn func(*http.Request) teaAnswer) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.routes[path] = fn
}

func (f *teaFixture) requests() []teaRecorded {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]teaRecorded(nil), f.reqs...)
}

// count is the number of recorded requests whose path is p.
func (f *teaFixture) count(p string) int {
	k := 0
	for _, r := range f.requests() {
		if r.path == p {
			k++
		}
	}
	return k
}

func (f *teaFixture) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	f.mu.Lock()
	f.reqs = append(f.reqs, teaRecorded{
		method: r.Method, host: r.Host, path: r.URL.Path, rawQuery: r.URL.RawQuery,
		authorization: r.Header.Get("Authorization"), body: string(body),
	})
	fn, ok := f.routes[r.URL.Path]
	need := f.requireBearer
	keyID, secret, issued := f.tokenKeyID, f.tokenSecret, f.tokenIssued
	f.mu.Unlock()

	tokenPath := f.api("/token")
	if r.URL.Path == tokenPath && !ok && issued != "" {
		id, sec, basic := r.BasicAuth()
		uid, _ := url.QueryUnescape(id)
		usec, _ := url.QueryUnescape(sec)
		if !basic || uid != keyID || usec != secret {
			writeTeaAnswer(w, teaAnswer{status: http.StatusUnauthorized, contentType: "application/json", body: `{"error":"invalid_client"}`})
			return
		}
		writeTeaAnswer(w, teaJSON(`{"access_token":"`+issued+`","token_type":"Bearer","expires_in":3600}`))
		return
	}
	if need != "" && strings.HasPrefix(r.URL.Path, "/tea/") && r.URL.Path != tokenPath &&
		r.Header.Get("Authorization") != "Bearer "+need {
		w.Header().Set("WWW-Authenticate", `Bearer realm="tea"`)
		writeTeaAnswer(w, teaAnswer{status: http.StatusUnauthorized})
		return
	}
	if !ok {
		writeTeaAnswer(w, teaTEAError(http.StatusNotFound, "OBJECT_UNKNOWN"))
		return
	}
	writeTeaAnswer(w, fn(r))
}

func writeTeaAnswer(w http.ResponseWriter, a teaAnswer) {
	if a.contentType != "" {
		w.Header().Set("Content-Type", a.contentType)
	}
	if a.location != "" {
		w.Header().Set("Location", a.location)
	}
	w.WriteHeader(a.status)
	_, _ = w.Write([]byte(a.body))
}

// Sample data. Product release, component and component release uuids.
const (
	teaPR1      = "11111111-1111-4111-8111-111111111111"
	teaPR2      = "22222222-2222-4222-8222-222222222222"
	teaProduct  = "33333333-3333-4333-8333-333333333333"
	teaComp1    = "44444444-4444-4444-8444-444444444441"
	teaComp2    = "44444444-4444-4444-8444-444444444442"
	teaComp3    = "44444444-4444-4444-8444-444444444443"
	teaComp4    = "44444444-4444-4444-8444-444444444444"
	teaCR1      = "55555555-5555-4555-8555-555555555551"
	teaCR2      = "55555555-5555-4555-8555-555555555552"
	teaCR3      = "55555555-5555-4555-8555-555555555553"
	teaCR4      = "55555555-5555-4555-8555-555555555554"
	teaArtProd  = "66666666-6666-4666-8666-666666666661"
	teaArtComp1 = "66666666-6666-4666-8666-666666666662"
	teaSHA256   = "e04c9d55986d7194822eaa4f8115a77f801844d807ad6e0d454ac31dd41861e5"
)

func teaCollectionJSON(id, belongsTo, artifacts string) string {
	return `{"uuid":"` + id + `","version":1,"belongsTo":"` + belongsTo + `","createdDate":"2026-01-15T00:00:00Z",` +
		`"updateReason":{"type":"INITIAL_RELEASE"},"artifacts":[` + artifacts + `]}`
}

// teaProductReleaseJSON: name is a JSON string literal (with quotes) or "" for no name.
func teaProductReleaseJSON(id, nameJSON, versionJSON, comps, artifacts string) string {
	name := ""
	if nameJSON != "" {
		name = `"productName":` + nameJSON + `,`
	}
	return `{"uuid":"` + id + `","product":"` + teaProduct + `",` + name + `"version":` + versionJSON +
		`,"createdDate":"2026-01-15T00:00:00Z","components":[` + comps + `],"latestCollection":` +
		teaCollectionJSON(id, "PRODUCT_RELEASE", artifacts) + `}`
}

func teaComponentReleaseJSON(id, comp, nameJSON, version, artifacts string) string {
	name := ""
	if nameJSON != "" {
		name = `"componentName":` + nameJSON + `,`
	}
	return `{"uuid":"` + id + `","component":"` + comp + `",` + name + `"version":"` + version +
		`","createdDate":"2026-01-15T00:00:00Z","latestCollection":` + teaCollectionJSON(id, "COMPONENT_RELEASE", artifacts) + `}`
}

func teaDiscoveryJSON(root string, ids ...string) string {
	var parts []string
	for _, id := range ids {
		parts = append(parts, `{"productReleaseUuid":"`+id+`","servers":[{"rootUrl":"`+root+`","versions":["1.0.0"]}]}`)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// teaRow is one run of a command against fixtures.
type teaRow struct {
	t      *testing.T
	net    *teaNet
	env    map[string]string
	stdin  teaStdin
	in     io.Reader
	secret func(string) (string, error)
	out    bytes.Buffer
	errOut bytes.Buffer
}

func newTeaRow(t *testing.T) *teaRow {
	return &teaRow{t: t, net: newTeaNet(), env: map[string]string{}, in: strings.NewReader("")}
}

func (r *teaRow) io() teaIO {
	secret := r.secret
	if secret == nil {
		secret = func(string) (string, error) {
			r.t.Error("readSecret called")
			return "", errors.New("not a terminal")
		}
	}
	return teaIO{
		in:         r.in,
		out:        &r.out,
		errOut:     &r.errOut,
		stdin:      r.stdin,
		readSecret: secret,
		getenv: func(k string) (string, bool) {
			v, ok := r.env[k]
			return v, ok
		},
		httpClient: r.net.client(),
		extra:      []tea.Option{tea.WithDiscoveryAttempts(4, time.Millisecond)},
	}
}

func (r *teaRow) discovery(in teaInputs) teaExit {
	return runTeaDiscovery(context.Background(), r.io(), in)
}

func (r *teaRow) flow(in teaInputs) teaExit {
	return runTeaFullFlow(context.Background(), r.io(), in)
}

// teaHTTPInputs are the flags --usehttp --useport <the fixture's port> for a TEI on f.
func teaHTTPInputs(f *teaFixture, id string) teaInputs {
	port := 0
	fmt.Sscan(f.port, &port)
	return teaInputs{tei: "tei://" + f.host + "/uuid/" + id, useHTTP: true, usePort: port, portSet: true}
}

// failingReader fails the test when it is read.
type failingReader struct{ t *testing.T }

func (f failingReader) Read([]byte) (int, error) {
	f.t.Error("standard input was read")
	return 0, io.EOF
}
