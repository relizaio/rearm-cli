package cmd

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// The address refusal and the connection of the machine mode (task TPF-178, design
// section 4, rows T-34 and T-36 to T-48).

func TestTeaMachineClassification(t *testing.T) { // T-36
	own := []netip.Addr{netip.MustParseAddr("10.9.8.7"), netip.MustParseAddr("fe80::7")}
	for class, addrs := range map[teaAddressClass][]string{
		teaAddressLoopback:      {"127.0.0.1", "127.255.255.254", "::1", "::ffff:127.0.0.1"},
		teaAddressUnspecified:   {"0.0.0.0", "0.1.2.3", "::", "::ffff:0.0.0.0"},
		teaAddressLinkLocal:     {"169.254.1.1", "::ffff:169.254.1.1", "fe80::1", "fe80::1%eth0", "fe80::7", "169.254.170.23"},
		teaAddressCloudMetadata: {"169.254.169.254", "::ffff:169.254.169.254", "100.100.100.200", "168.63.129.16", "fd00:ec2::254", "fd00:ec2::23", "fd20:ce::254", "fd00:c1::a9fe:a9fe", "::ffff:100.100.100.200"},
		teaAddressOwn:           {"10.9.8.7", "::ffff:10.9.8.7"},
		// allowed: public, private not this host's, and the stated non-goals (NAT64, 6to4)
		teaAddressAllowed: {"203.0.113.10", "10.1.2.3", "2001:db8::1", "64:ff9b::7f00:1", "2002:7f00:1::", "192.168.1.1", "fd00:ec2::253"},
	} {
		for _, a := range addrs {
			if got := classifyTeaAddress(netip.MustParseAddr(a), own); got != class {
				t.Errorf("%s: %q, want %q", a, got, class)
			}
		}
	}
	// the flag lifts loopback and own addresses, nothing else
	for _, c := range []teaAddressClass{teaAddressCloudMetadata, teaAddressLinkLocal, teaAddressUnspecified} {
		if !teaRefused(c, true) || !teaRefused(c, false) {
			t.Errorf("%s is not refused under both settings", c)
		}
	}
	for _, c := range []teaAddressClass{teaAddressLoopback, teaAddressOwn} {
		if !teaRefused(c, false) || teaRefused(c, true) {
			t.Errorf("%s: refused %v without the flag, %v with it", c, teaRefused(c, false), teaRefused(c, true))
		}
	}
	if teaRefused(teaAddressAllowed, false) {
		t.Error("an allowed address is refused")
	}
}

// teaRefusedTargets are the targets of T-37: a literal host, or a name and what it
// resolves to; the address and class the error names.
var teaRefusedTargets = []struct {
	host, resolves, addr string
	class                teaAddressClass
}{
	{"127.0.0.1", "", "127.0.0.1", teaAddressLoopback},
	{"[::1]", "", "::1", teaAddressLoopback},
	{"evil.example.com", "127.0.0.1", "127.0.0.1", teaAddressLoopback},
	{"evil.example.com", "::ffff:127.0.0.1", "127.0.0.1", teaAddressLoopback},
	{"evil.example.com", "0.0.0.0", "0.0.0.0", teaAddressUnspecified},
	{"evil.example.com", "169.254.169.254", "169.254.169.254", teaAddressCloudMetadata},
	{"evil.example.com", "169.254.1.1", "169.254.1.1", teaAddressLinkLocal},
	{"evil.example.com", "fe80::1%eth0", "fe80::1", teaAddressLinkLocal},
	{"evil.example.com", "100.100.100.200", "100.100.100.200", teaAddressCloudMetadata},
	{"evil.example.com", "10.9.8.7", "10.9.8.7", teaAddressOwn},
}

// wantRefused checks an exit 8 naming addr and class, with nothing dialled at addr.
func (r *teaMRow) wantRefused(got teaExit, addr string, class teaAddressClass) {
	r.t.Helper()
	e := r.wantFail(got, teaExitAddressRefused, teaReasonAddressRefused)
	if e["address"] != addr || e["addressClass"] != string(class) || e["hint"] == nil {
		r.t.Errorf("error object %v, want %s %s", e, addr, class)
	}
	if r.m.connectedTo(addr) {
		r.t.Errorf("connect was called for %s: %v", addr, r.m.connected())
	}
}

func TestTeaMachineRefusedFirstRequest(t *testing.T) { // T-37
	for _, c := range teaRefusedTargets {
		// product: --api-root
		r := newTeaMRow(t)
		f := r.fixture("tea.example.com", true)
		if c.resolves != "" {
			r.m.resolveTo(c.host, c.resolves)
		}
		r.wantRefused(r.exec(teaVerbProduct, "--api-root", "https://"+c.host+":"+f.port+"/tea", "--uuid", teaProduct), c.addr, c.class)
		teaNoRequests(t, f)
		if len(r.m.connected()) != 0 {
			t.Errorf("%s: connects %v", c.resolves, r.m.connected())
		}
		// resolve: the well-known host (the TEI parser takes names only)
		if c.resolves == "" {
			continue
		}
		r = newTeaMRow(t)
		f = r.fixture("tea.example.com", true)
		r.m.resolveTo(c.host, c.resolves)
		r.wantRefused(r.exec(teaVerbResolve, "--tei", "tei://"+c.host+"/uuid/"+teaPR1, "--useport", f.port), c.addr, c.class)
		teaNoRequests(t, f)
	}
}

func TestTeaMachineRefusedTokenEndpoint(t *testing.T) { // T-38
	r := newTeaMRow(t)
	f := r.fixture("tea.example.com", true)
	teaTokenFixture(f, true)
	r.m.resolveTo("evil.example.com", "127.0.0.1")
	root := "https://evil.example.com:" + f.port + "/tea"
	r.env[teaEnvAuthRoot] = root
	r.env[teaEnvKeyID] = teaKeyID
	r.env[teaEnvKey] = teaKeySecret
	r.wantRefused(r.exec(teaVerbProduct, "--api-root", root, "--uuid", teaProduct), "127.0.0.1", teaAddressLoopback)
	teaNoRequests(t, f)
}

func TestTeaMachineRefusedRedirect(t *testing.T) { // T-39
	for _, c := range teaRefusedTargets {
		r := newTeaMRow(t)
		f := r.fixture("tea.example.com", true)
		victim := r.fixture("victim.example.com", true)
		if c.resolves != "" {
			r.m.resolveTo(c.host, c.resolves)
		}
		f.route(f.api("/product/"+teaProduct), teaAnswer{status: http.StatusFound, location: "https://" + c.host + ":" + victim.port + "/x"})
		r.wantRefused(r.exec(teaVerbProduct, "--api-root", f.root, "--uuid", teaProduct), c.addr, c.class)
		if n := len(f.requests()); n != 1 {
			t.Errorf("%s: the first server received %d requests", c.host, n)
		}
		teaNoRequests(t, victim)
	}
}

func TestTeaMachineRefusedDiscoveryEndpoint(t *testing.T) { // T-40
	// (a) the only endpoint is refused
	r := newTeaMRow(t)
	f := r.fixture("tea.example.com", true)
	victim := r.fixture("victim.example.com", true)
	r.m.resolveTo("evil.example.com", "127.0.0.1")
	evil := "https://evil.example.com:" + victim.port + "/tea"
	f.route("/.well-known/tea", teaJSON(`{"schemaVersion":1,"endpoints":[{"url":"`+evil+`","versions":["1.0.0"]}]}`))
	r.wantRefused(r.exec(teaVerbResolve, teaResolveArgs(f, teaPR1)...), "127.0.0.1", teaAddressLoopback)
	teaNoRequests(t, victim)
	// (b) a refused endpoint first, an allowed one second: failover is a success
	r = newTeaMRow(t)
	f = r.fixture("tea.example.com", true)
	victim = r.fixture("victim.example.com", true)
	r.m.resolveTo("evil.example.com", "127.0.0.1")
	evil = "https://evil.example.com:" + victim.port + "/tea"
	f.route("/.well-known/tea", teaJSON(`{"schemaVersion":1,"endpoints":[{"url":"`+evil+`","versions":["1.0.0"],"priority":1},{"url":"`+f.root+`","versions":["1.0.0"],"priority":0.5}]}`))
	f.route(f.api("/discovery"), teaJSON(teaDiscoveryJSON(f.root, teaPR1)))
	r.want(r.exec(teaVerbResolve, teaResolveArgs(f, teaPR1)...), teaExitOK)
	teaNoRequests(t, victim)
}

func TestTeaMachineRefusedArtifactURL(t *testing.T) { // T-41
	d := newTeaDL(t)
	victim := d.r.fixture("victim.example.com", true)
	d.r.m.resolveTo("evil.example.com", "127.0.0.1")
	formats := []string{d.serverBody(teaArtProd, 1, "a/ok", "ok")}
	var targets []string
	for i, c := range teaRefusedTargets {
		host := c.host
		if c.resolves != "" {
			host = fmt.Sprintf("evil%d.example.com", i)
			d.r.m.resolveTo(host, c.resolves)
		}
		targets = append(targets, c.addr+" "+string(c.class))
		formats = append(formats, `{"mediaType":"a/x`+fmt.Sprint(i)+`","url":"https://`+host+`:`+victim.port+`/x","checksums":`+teaSums("SHA-256", teaSumSHA256("x"))+`}`)
	}
	formats = append(formats, d.server(teaArtProd, 1, "a/redirect", func(w http.ResponseWriter, q *http.Request) {
		http.Redirect(w, q, "https://127.0.0.1:"+victim.port+"/x", http.StatusFound)
	}, teaSums("SHA-256", teaSumSHA256("x"))))
	targets = append(targets, "127.0.0.1 LOOPBACK")
	d.art(teaArtProd, 1, "BOM", "", formats...)
	d.r.want(d.run(), teaExitPartial)
	doc := d.r.doc()
	d.wantRow(doc, 0, "VERIFIED", nil)
	for i, want := range targets {
		d.wantRow(doc, i+1, "FAILED", "ADDRESS_REFUSED")
		if got := fmt.Sprint(d.row(doc, i+1, "address"), " ", d.row(doc, i+1, "addressClass")); got != want {
			t.Errorf("row %d: %s, want %s", i+2, got, want)
		}
	}
	teaNoRequests(t, victim)
	for _, c := range teaRefusedTargets {
		if d.r.m.connectedTo(c.addr) {
			t.Errorf("connect was called for %s", c.addr)
		}
	}
}

func TestTeaMachineMixedAnswers(t *testing.T) { // T-42
	// (a) one refused and one allowed address: only the allowed one is dialled
	r := newTeaMRow(t)
	f := r.fixture("tea.example.com", true)
	f.route(f.api("/product/"+teaProduct), teaJSON(`{"uuid":"`+teaProduct+`","name":"P","identifiers":[]}`))
	r.m.resolveTo("tea.example.com", "169.254.169.254", "203.0.113.10")
	r.want(r.exec(teaVerbProduct, "--api-root", f.root, "--uuid", teaProduct), teaExitOK)
	for _, ap := range r.m.connected() {
		if ap.Addr() != netip.MustParseAddr("203.0.113.10") {
			t.Errorf("connect saw %s", ap)
		}
	}
	// (b) every address refused: a refusal naming the first
	r = newTeaMRow(t)
	f = r.fixture("tea.example.com", true)
	r.m.resolveTo("tea.example.com", "127.0.0.1", "::1")
	r.wantRefused(r.exec(teaVerbProduct, "--api-root", f.root, "--uuid", teaProduct), "127.0.0.1", teaAddressLoopback)
	teaNoRequests(t, f)
}

func TestTeaMachineAllowThisHost(t *testing.T) { // T-43
	product := teaJSON(`{"uuid":"` + teaProduct + `","name":"P","identifiers":[]}`)
	// loopback and the host's own address: the request arrives
	for _, c := range []struct{ host, resolves string }{{"127.0.0.1", ""}, {"[::1]", ""}, {"own.example.com", "10.9.8.7"}} {
		r := newTeaMRow(t)
		f := r.fixture("tea.example.com", true)
		f.route(f.api("/product/"+teaProduct), product)
		if c.resolves != "" {
			r.m.resolveTo(c.host, c.resolves)
		}
		r.want(r.exec(teaVerbProduct, "--api-root", "https://"+c.host+":"+f.port+"/tea", "--uuid", teaProduct, "--allow-this-host"), teaExitOK)
		if f.count(f.api("/product/"+teaProduct)) != 1 {
			t.Errorf("%s: the request did not arrive", c.host)
		}
	}
	// everything else stays refused, as the first request and as a redirect target
	for _, c := range []struct {
		addr  string
		class teaAddressClass
	}{
		{"169.254.169.254", teaAddressCloudMetadata}, {"169.254.1.1", teaAddressLinkLocal}, {"fe80::1", teaAddressLinkLocal},
		{"0.0.0.0", teaAddressUnspecified}, {"100.100.100.200", teaAddressCloudMetadata}, {"fd00:ec2::254", teaAddressCloudMetadata},
	} {
		r := newTeaMRow(t)
		f := r.fixture("tea.example.com", true)
		r.m.resolveTo("evil.example.com", c.addr)
		r.wantRefused(r.exec(teaVerbProduct, "--api-root", "https://evil.example.com:"+f.port+"/tea", "--uuid", teaProduct, "--allow-this-host"), c.addr, c.class)
		teaNoRequests(t, f)

		r = newTeaMRow(t)
		f = r.fixture("tea.example.com", true)
		victim := r.fixture("victim.example.com", true)
		r.m.resolveTo("evil.example.com", c.addr)
		f.route(f.api("/product/"+teaProduct), teaAnswer{status: http.StatusFound, location: "https://evil.example.com:" + victim.port + "/x"})
		r.wantRefused(r.exec(teaVerbProduct, "--api-root", f.root, "--uuid", teaProduct, "--allow-this-host"), c.addr, c.class)
		teaNoRequests(t, victim)
	}
}

func TestTeaMachinePrivateAddressAllowed(t *testing.T) { // T-44
	r := newTeaMRow(t)
	f := r.fixture("tea.example.com", true)
	f.route(f.api("/product/"+teaProduct), teaJSON(`{"uuid":"`+teaProduct+`","name":"P","identifiers":[]}`))
	r.m.resolveTo("tea.example.com", "10.1.2.3")
	r.want(r.exec(teaVerbProduct, "--api-root", f.root, "--uuid", teaProduct), teaExitOK)
}

func TestTeaMachineInterfaces(t *testing.T) { // T-45
	// (a) the interfaces cannot be listed: fail closed
	r := newTeaMRow(t)
	f := r.fixture("tea.example.com", true)
	r.m.ownErr = errors.New("netlink: permission denied")
	e := r.wantFail(r.exec(teaVerbProduct, "--api-root", f.root, "--uuid", teaProduct), teaExitFailed, teaReasonFailed)
	if !strings.Contains(e["message"].(string), "interfaces of this host could not be listed") || len(r.m.connected()) != 0 {
		t.Errorf("message %v, connects %v", e["message"], r.m.connected())
	}
	// (b) the list is read at every dial: an address added after the first dial is refused
	r = newTeaMRow(t)
	f = r.fixture("tea.example.com", true)
	victim := r.fixture("victim.example.com", true)
	r.m.resolveTo("new.example.com", "10.20.30.40")
	r.m.ownNext = []netip.Addr{netip.MustParseAddr("10.9.8.7"), netip.MustParseAddr("10.20.30.40")}
	f.route(f.api("/product/"+teaProduct), teaAnswer{status: http.StatusFound, location: "https://new.example.com:" + victim.port + "/x"})
	r.wantRefused(r.exec(teaVerbProduct, "--api-root", f.root, "--uuid", teaProduct), "10.20.30.40", teaAddressOwn)
	teaNoRequests(t, victim)
}

// teaCountingListener accepts and counts connections, then closes them.
type teaCountingListener struct {
	ln      net.Listener
	accepts atomic.Int64
}

func newTeaCountingListener(t *testing.T) *teaCountingListener {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	c := &teaCountingListener{ln: ln}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			c.accepts.Add(1)
			_ = conn.Close()
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return c
}

func (c *teaCountingListener) port() string {
	_, p, _ := net.SplitHostPort(c.ln.Addr().String())
	return p
}

func TestTeaMachineProxyIgnored(t *testing.T) { // T-46
	proxy := newTeaCountingListener(t)
	names := []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "ALL_PROXY"}
	for _, k := range names {
		t.Setenv(k, "http://127.0.0.1:"+proxy.port())
	}
	r := newTeaMRow(t)
	for _, k := range names {
		r.env[k] = "http://127.0.0.1:" + proxy.port()
	}
	f := r.fixture("tea.example.com", true)
	f.route(f.api("/product/"+teaProduct), teaJSON(`{"uuid":"`+teaProduct+`","name":"P","identifiers":[]}`))
	r.want(r.exec(teaVerbProduct, "--api-root", f.root, "--uuid", teaProduct), teaExitOK)
	if proxy.accepts.Load() != 0 {
		t.Errorf("the proxy received %d connections", proxy.accepts.Load())
	}
	r.m.hook = func(ctx context.Context, ap netip.AddrPort) (net.Conn, error) { return nil, syscall.ECONNREFUSED }
	e := r.wantFail(r.exec(teaVerbProduct, "--api-root", f.root, "--uuid", teaProduct), teaExitConnect, teaReasonConnectFailed)
	if fmt.Sprint(e["ignoredProxyVariables"]) != "[HTTP_PROXY HTTPS_PROXY ALL_PROXY https_proxy]" {
		t.Errorf("ignoredProxyVariables %v", e["ignoredProxyVariables"])
	}
}

func TestTeaMachineConnectBackstop(t *testing.T) { // T-47
	ln := newTeaCountingListener(t)
	ap := netip.MustParseAddrPort("127.0.0.1:" + ln.port())
	// without the flag: the Control backstop refuses before connect(2)
	g := newTeaGuard(false, 5*time.Second, nil)
	conn, err := g.dialChecked(context.Background(), ap)
	var refused *teaAddressRefusedError
	if err == nil || !errors.As(err, &refused) || refused.Class != teaAddressLoopback {
		if conn != nil {
			_ = conn.Close()
		}
		t.Fatalf("dial: %v, want the address refusal", err)
	}
	time.Sleep(100 * time.Millisecond)
	if ln.accepts.Load() != 0 {
		t.Errorf("the listener accepted %d connections", ln.accepts.Load())
	}
	// with it: connected
	g = newTeaGuard(true, 5*time.Second, nil)
	conn, err = g.dialChecked(context.Background(), ap)
	if err != nil {
		t.Fatalf("dial with --allow-this-host: %v", err)
	}
	_ = conn.Close()
}

// teaFailingTransport fails the test when it is used.
type teaFailingTransport struct{ t *testing.T }

func (f teaFailingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	f.t.Error("the caller's HTTP client was used")
	return nil, errors.New("not this client")
}

func TestTeaMachineOwnClient(t *testing.T) { // T-48
	r := newTeaMRow(t)
	f := r.fixture("tea.example.com", true)
	f.route(f.api("/product/"+teaProduct), teaJSON(`{"uuid":"`+teaProduct+`","name":"P","identifiers":[]}`))
	tio := r.io()
	tio.httpClient = &http.Client{Transport: teaFailingTransport{t}}
	in := teaMachineInputsDefault(teaVerbProduct)
	in.apiRoot, in.uuid = f.root, teaProduct
	if code := runTeaMachine(context.Background(), tio, in); code != teaExitOK {
		t.Fatalf("exit %d: %s", code, r.errOut.String())
	}
}

func TestTeaMachineConnectFailures(t *testing.T) { // T-34
	type row struct {
		name    string
		verb    teaMachineVerb
		setup   func(r *teaMRow)
		phase   string
		fast    bool
		connect int
	}
	var mu sync.Mutex
	var ended []bool
	block := func(ctx context.Context, ap netip.AddrPort) (net.Conn, error) {
		<-ctx.Done()
		mu.Lock()
		ended = append(ended, ctx.Err() != nil)
		mu.Unlock()
		return nil, ctx.Err()
	}
	for _, c := range []row{
		{"refused", teaVerbProduct, func(r *teaMRow) {
			r.m.hook = func(context.Context, netip.AddrPort) (net.Conn, error) { return nil, syscall.ECONNREFUSED }
		}, "CONNECT", true, 1},
		{"blocks", teaVerbProduct, func(r *teaMRow) { r.m.hook = block }, "CONNECT", true, 1},
		{"lookup fails", teaVerbProduct, func(r *teaMRow) { r.m.lookErr = errors.New("server misbehaving") }, "RESOLVE", true, 0},
		{"two addresses block", teaVerbProduct, func(r *teaMRow) {
			r.m.resolveTo("tea.example.com", "203.0.113.10", "203.0.113.11")
			r.m.hook = block
		}, "CONNECT", true, 2},
		{"refused on resolve", teaVerbResolve, func(r *teaMRow) {
			r.m.hook = func(context.Context, netip.AddrPort) (net.Conn, error) { return nil, syscall.ECONNREFUSED }
		}, "CONNECT", false, 4},
	} {
		ended = nil
		r := newTeaMRow(t)
		f := r.fixture("tea.example.com", true)
		c.setup(r)
		args := []string{"--api-root", f.root, "--uuid", teaProduct, "--connect-timeout", "1s"}
		if c.verb == teaVerbResolve {
			args = append(teaResolveArgs(f, teaPR1), "--connect-timeout", "1s")
		}
		start := time.Now()
		e := r.wantFail(r.exec(c.verb, args...), teaExitConnect, teaReasonConnectFailed)
		took := time.Since(start)
		if e["phase"] != c.phase || e["hint"] != teaHintConnect {
			t.Errorf("%s: %v", c.name, e)
		}
		if c.fast && took > 3*time.Second {
			t.Errorf("%s: took %s", c.name, took)
		}
		if n := len(r.m.connected()); n != c.connect {
			t.Errorf("%s: connect called %d times, want %d", c.name, n, c.connect)
		}
		if c.name == "two addresses block" {
			mu.Lock()
			if len(ended) != 2 || !ended[0] || !ended[1] {
				t.Errorf("%s: contexts ended %v", c.name, ended)
			}
			mu.Unlock()
		}
	}
}
