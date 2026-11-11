package cmd

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"
)

// Mutation round for the address guard: the edges of the classification, the text of the
// refusal, and the dial loop driven directly (name resolution, the order of the steps, the
// bound on the addresses tried, the share of the time each gets).

func TestTeaMachineClassificationEdges(t *testing.T) {
	own := []netip.Addr{
		netip.MustParseAddr("10.9.8.7"), netip.MustParseAddr("fe80::7"),
		netip.MustParseAddr("::ffff:10.9.8.8"), netip.MustParseAddr("2001:db8::1%eth0"),
		netip.MustParseAddr("127.0.0.1"),
	}
	for class, addrs := range map[teaAddressClass][]string{
		// the neighbours of the metadata addresses are not metadata
		teaAddressAllowed: {"100.100.100.201", "100.100.100.199", "168.63.129.17", "168.63.129.15", "fd00:ec2::255", "fd00:ec2::24",
			"fd20:ce::255", "fd00:c1::a9fe:a9ff", "fd20:ce::253", "fd00:ec2::22",
			// link-local is fe80::/10 and 169.254.0.0/16 only: not site-local, not multicast
			"fec0::1", "ff02::1", "ff01::1", "169.253.255.255", "169.255.0.0", "1.0.0.0", "::2", "10.9.8.9", "203.0.113.5"},
		teaAddressLinkLocal:     {"169.254.0.0", "169.254.255.255", "fe80::", "febf:ffff::1", "fe80::7%eth0"},
		teaAddressUnspecified:   {"0.255.255.255", "0.0.0.1", "::ffff:0.0.0.0"},
		teaAddressLoopback:      {"127.0.0.2", "::ffff:127.255.255.255", "::1%lo"},
		teaAddressOwn:           {"10.9.8.7", "::ffff:10.9.8.7", "10.9.8.8", "::ffff:10.9.8.8", "2001:db8::1", "2001:db8::1%eth1"},
		teaAddressCloudMetadata: {"168.63.129.16", "fd00:ec2::254", "fd00:ec2::23", "fd20:ce::254", "fd00:c1::a9fe:a9fe", "100.100.100.200", "169.254.169.254"},
	} {
		for _, a := range addrs {
			addr, err := netip.ParseAddr(strings.TrimSuffix(a, "%x"))
			if err != nil {
				t.Fatal(err)
			}
			if got := classifyTeaAddress(addr, own); got != class {
				t.Errorf("%s: %q, want %q", a, got, class)
			}
		}
	}
	// an own loopback address is loopback, an own link-local one is link-local (first match wins)
	if got := classifyTeaAddress(netip.MustParseAddr("127.0.0.1"), own); got != teaAddressLoopback {
		t.Errorf("own 127.0.0.1: %q", got)
	}
	if got := classifyTeaAddress(netip.MustParseAddr("fe80::7"), own); got != teaAddressLinkLocal {
		t.Errorf("own fe80::7: %q", got)
	}
	// the metadata address is metadata whether or not it is also link-local
	if got := classifyTeaAddress(netip.MustParseAddr("169.254.169.254"), []netip.Addr{netip.MustParseAddr("169.254.169.254")}); got != teaAddressCloudMetadata {
		t.Errorf("own metadata address: %q", got)
	}
	if got := classifyTeaAddress(netip.MustParseAddr("10.9.8.7"), nil); got != teaAddressAllowed {
		t.Errorf("no own addresses: %q", got)
	}
}

func TestTeaMachineRefusedPerClassAndFlag(t *testing.T) {
	for _, c := range []struct {
		class         teaAddressClass
		withoutFlag   bool
		withFlag      bool
		label         string
		liftedByFlag  bool
		errorText     string
		hint, hintFor string
	}{
		{teaAddressAllowed, false, false, "allowed", false, "", "", ""},
		{teaAddressLoopback, true, false, "loopback", true, "refused to connect to 127.0.0.1 (loopback)", teaHintThisHost, ""},
		{teaAddressOwn, true, false, "an address of this host", true, "refused to connect to 10.9.8.7 (an address of this host)", teaHintThisHost, ""},
		{teaAddressLinkLocal, true, true, "link-local", false, "refused to connect to 169.254.1.1 (link-local)", teaHintNever, ""},
		{teaAddressUnspecified, true, true, "unspecified", false, "refused to connect to 0.0.0.0 (unspecified)", teaHintNever, ""},
		{teaAddressCloudMetadata, true, true, "cloud metadata", false, "refused to connect to 169.254.169.254 (cloud metadata)", teaHintNever, ""},
	} {
		if got := teaRefused(c.class, false); got != c.withoutFlag {
			t.Errorf("%q without the flag: refused %v", c.class, got)
		}
		if got := teaRefused(c.class, true); got != c.withFlag {
			t.Errorf("%q with the flag: refused %v", c.class, got)
		}
		if got := c.class.label(); got != c.label {
			t.Errorf("label of %q is %q, want %q", c.class, got, c.label)
		}
		if got := c.class.liftedByAllowThisHost(); got != c.liftedByFlag {
			t.Errorf("%q lifted by the flag: %v", c.class, got)
		}
		if c.errorText == "" {
			continue
		}
		addr := strings.Fields(strings.TrimPrefix(c.errorText, "refused to connect to "))[0]
		e := &teaAddressRefusedError{Address: netip.MustParseAddr(addr), Class: c.class, Host: "some.example.com"}
		if e.Error() != c.errorText {
			t.Errorf("error text %q, want %q", e.Error(), c.errorText)
		}
		d := newTeaErrorDoc(context.Background(), e, "", nil)
		if d.Hint == nil || *d.Hint != c.hint || d.Address == nil || *d.Address != addr || d.AddressClass == nil || *d.AddressClass != string(c.class) {
			t.Errorf("%q: error object %+v", c.class, d)
		}
		if d.ExitCode != 8 || d.Reason != teaReasonAddressRefused || d.Phase != nil {
			t.Errorf("%q: exit %d reason %s phase %v", c.class, d.ExitCode, d.Reason, d.Phase)
		}
	}
	if teaHintThisHost != "this host is refused as a target by default; --allow-this-host lifts it for local testing" ||
		teaHintNever != "this address class is never a TEA target" ||
		teaHintConnect != "the connection could not be made; the network policy of the deployment may block this address" {
		t.Error("a hint text changed")
	}
}

func TestTeaMachineConnectErrorText(t *testing.T) {
	cause := errors.New("boom")
	resolve := &teaConnectError{Phase: teaPhaseResolve, Host: "h.example.com", Err: cause}
	connect := &teaConnectError{Phase: teaPhaseConnect, Host: "h.example.com", Err: cause}
	if resolve.Error() != "could not resolve h.example.com: boom" {
		t.Errorf("resolve: %q", resolve.Error())
	}
	if connect.Error() != "could not connect to h.example.com: boom" {
		t.Errorf("connect: %q", connect.Error())
	}
	if !errors.Is(resolve, cause) || !errors.Is(connect, cause) || errors.Unwrap(connect) != cause {
		t.Error("the cause is not reachable through Unwrap")
	}
	if teaPhaseResolve != "RESOLVE" || teaPhaseConnect != "CONNECT" {
		t.Error("a phase name changed")
	}
}

// teaDial is the dial loop under a test seam that records what it is asked.
type teaDial struct {
	mu       sync.Mutex
	names    map[string][]string
	lookups  []string
	lookErr  error
	own      []netip.Addr
	ownErr   error
	ownReads int
	connects []netip.AddrPort
	connect  func(ctx context.Context, ap netip.AddrPort) (net.Conn, error)
	refused  []string
}

func (d *teaDial) guard(allow bool, timeout time.Duration) *teaGuard {
	g := newTeaGuard(allow, timeout, &teaNetSeam{
		lookup: func(ctx context.Context, host string) ([]netip.Addr, error) {
			d.mu.Lock()
			defer d.mu.Unlock()
			d.lookups = append(d.lookups, host)
			if d.lookErr != nil {
				return nil, d.lookErr
			}
			var out []netip.Addr
			for _, a := range d.names[host] {
				out = append(out, netip.MustParseAddr(a))
			}
			return out, nil
		},
		interfaceAddrs: func() ([]netip.Addr, error) {
			d.mu.Lock()
			defer d.mu.Unlock()
			d.ownReads++
			return d.own, d.ownErr
		},
		connect: func(ctx context.Context, ap netip.AddrPort) (net.Conn, error) {
			d.mu.Lock()
			d.connects = append(d.connects, ap)
			f := d.connect
			d.mu.Unlock()
			if f == nil {
				return nil, errors.New("no connect function")
			}
			return f(ctx, ap)
		},
	})
	g.refused = func(a netip.Addr, class teaAddressClass, host string) {
		d.mu.Lock()
		defer d.mu.Unlock()
		d.refused = append(d.refused, fmt.Sprintf("%s %s %s", a, class, host))
	}
	return g
}

func (d *teaDial) connected() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []string
	for _, ap := range d.connects {
		out = append(out, ap.String())
	}
	return out
}

func newTeaDial(names map[string][]string) *teaDial {
	return &teaDial{names: names, own: []netip.Addr{netip.MustParseAddr("10.9.8.7")}}
}

func TestTeaMachineDialAddressErrors(t *testing.T) {
	d := newTeaDial(nil)
	g := d.guard(false, time.Second)
	for _, c := range []struct{ address, host, text string }{
		{"nohostport", "nohostport", "could not resolve nohostport: address nohostport: missing port in address"},
		{"h.example.com:abc", "h.example.com", `could not resolve h.example.com: invalid port "abc"`},
		{"h.example.com:65536", "h.example.com", `could not resolve h.example.com: invalid port "65536"`},
		{"h.example.com:-1", "h.example.com", `could not resolve h.example.com: invalid port "-1"`},
		{"h.example.com:", "h.example.com", `could not resolve h.example.com: invalid port ""`},
	} {
		_, err := g.dialContext(context.Background(), "tcp", c.address)
		var ce *teaConnectError
		if !errors.As(err, &ce) || ce.Phase != teaPhaseResolve || ce.Host != c.host || ce.Error() != c.text {
			t.Errorf("%s: %v, want RESOLVE %q", c.address, err, c.text)
		}
	}
	if len(d.lookups) != 0 || len(d.connects) != 0 {
		t.Errorf("lookups %v connects %v for malformed addresses", d.lookups, d.connects)
	}
}

func TestTeaMachineDialNameErrors(t *testing.T) {
	// no address at all
	d := newTeaDial(map[string][]string{"empty.example.com": nil})
	_, err := d.guard(false, time.Second).dialContext(context.Background(), "tcp", "empty.example.com:443")
	var ce *teaConnectError
	if !errors.As(err, &ce) || ce.Phase != teaPhaseResolve || ce.Host != "empty.example.com" || ce.Error() != "could not resolve empty.example.com: no address" {
		t.Errorf("empty answer: %v", err)
	}
	if len(d.connects) != 0 {
		t.Errorf("connects %v", d.connects)
	}
	// the resolver fails
	d = newTeaDial(nil)
	d.lookErr = errors.New("servfail")
	_, err = d.guard(false, time.Second).dialContext(context.Background(), "tcp", "x.example.com:443")
	if !errors.As(err, &ce) || ce.Phase != teaPhaseResolve || !errors.Is(err, d.lookErr) || ce.Error() != "could not resolve x.example.com: servfail" {
		t.Errorf("resolver error: %v", err)
	}
	// a literal is never looked up
	d = newTeaDial(nil)
	d.connect = func(ctx context.Context, ap netip.AddrPort) (net.Conn, error) { return nil, errors.New("down") }
	_, err = d.guard(false, time.Second).dialContext(context.Background(), "tcp", "203.0.113.5:8443")
	if len(d.lookups) != 0 || strings.Join(d.connected(), ",") != "203.0.113.5:8443" {
		t.Errorf("literal: lookups %v connects %v", d.lookups, d.connected())
	}
	if !errors.As(err, &ce) || ce.Phase != teaPhaseConnect || ce.Host != "203.0.113.5" || ce.Error() != "could not connect to 203.0.113.5: down" {
		t.Errorf("literal error: %v", err)
	}
	// a bracketed IPv6 literal
	d = newTeaDial(nil)
	d.connect = func(ctx context.Context, ap netip.AddrPort) (net.Conn, error) { return nil, errors.New("down") }
	_, _ = d.guard(false, time.Second).dialContext(context.Background(), "tcp", "[2001:db8::5]:443")
	if strings.Join(d.connected(), ",") != "[2001:db8::5]:443" {
		t.Errorf("v6 literal: connects %v", d.connected())
	}
}

func TestTeaMachineDialInterfaceListFailsClosed(t *testing.T) {
	for _, host := range []string{"203.0.113.5", "name.example.com"} {
		d := newTeaDial(map[string][]string{"name.example.com": {"203.0.113.5"}})
		d.ownErr = errors.New("no netlink")
		_, err := d.guard(false, time.Second).dialContext(context.Background(), "tcp", host+":443")
		if !errors.Is(err, errTeaInterfaces) || !strings.Contains(err.Error(), "no netlink") {
			t.Errorf("%s: %v", host, err)
		}
		var ce *teaConnectError
		if errors.As(err, &ce) {
			t.Errorf("%s: an interface failure is not a connect failure: %v", host, err)
		}
		if len(d.connects) != 0 {
			t.Errorf("%s: connects %v", host, d.connected())
		}
		if got := teaMachineReason(context.Background(), err, teaFileBoundNone); got != teaReasonFailed {
			t.Errorf("%s: reason %s", host, got)
		}
	}
	// the list is read once per dial
	d := newTeaDial(map[string][]string{"name.example.com": {"203.0.113.5", "203.0.113.6"}})
	d.connect = func(ctx context.Context, ap netip.AddrPort) (net.Conn, error) { return nil, errors.New("down") }
	g := d.guard(false, time.Second)
	for i := 1; i <= 2; i++ {
		_, _ = g.dialContext(context.Background(), "tcp", "name.example.com:443")
		if d.ownReads != i {
			t.Errorf("after %d dials the interfaces were read %d times", i, d.ownReads)
		}
	}
}

func TestTeaMachineDialRefusals(t *testing.T) {
	d := newTeaDial(map[string][]string{
		"evil.example.com":  {"::ffff:127.0.0.1", "169.254.169.254", "fe80::1%eth0"},
		"mixed.example.com": {"127.0.0.1", "::ffff:203.0.113.5", "10.9.8.7", "203.0.113.6"},
	})
	d.connect = func(ctx context.Context, ap netip.AddrPort) (net.Conn, error) { return nil, errors.New("down") }
	g := d.guard(false, time.Second)

	// every address refused: the first one is named, in its normal form; each is traced
	_, err := g.dialContext(context.Background(), "tcp", "evil.example.com:443")
	var re *teaAddressRefusedError
	if !errors.As(err, &re) || re.Address != netip.MustParseAddr("127.0.0.1") || re.Class != teaAddressLoopback || re.Host != "evil.example.com" {
		t.Fatalf("all refused: %#v", err)
	}
	want := "127.0.0.1 LOOPBACK evil.example.com|169.254.169.254 CLOUD_METADATA evil.example.com|fe80::1 LINK_LOCAL evil.example.com"
	if got := strings.Join(d.refused, "|"); got != want {
		t.Errorf("trace %q, want %q", got, want)
	}
	if len(d.connects) != 0 {
		t.Errorf("a refused address was dialled: %v", d.connected())
	}

	// some allowed: only they are dialled, as normal literals, in order
	d.refused = nil
	_, err = g.dialContext(context.Background(), "tcp", "mixed.example.com:8443")
	var ce *teaConnectError
	if !errors.As(err, &ce) || ce.Phase != teaPhaseConnect {
		t.Errorf("mixed: %v", err)
	}
	if got := strings.Join(d.connected(), ","); got != "203.0.113.5:8443,203.0.113.6:8443" {
		t.Errorf("dialled %q", got)
	}
	if got := strings.Join(d.refused, "|"); got != "127.0.0.1 LOOPBACK mixed.example.com|10.9.8.7 OWN_ADDRESS mixed.example.com" {
		t.Errorf("trace %q", got)
	}

	// with the flag the host's own addresses are dialled
	d.connects, d.refused = nil, nil
	_, _ = d.guard(true, time.Second).dialContext(context.Background(), "tcp", "mixed.example.com:8443")
	if got := strings.Join(d.connected(), ","); got != "127.0.0.1:8443,203.0.113.5:8443,10.9.8.7:8443,203.0.113.6:8443" {
		t.Errorf("with the flag dialled %q", got)
	}
	if len(d.refused) != 0 {
		t.Errorf("traced under the flag: %v", d.refused)
	}
	// the metadata address stays refused under the flag
	d.connects = nil
	d.names["meta.example.com"] = []string{"169.254.169.254"}
	_, err = d.guard(true, time.Second).dialContext(context.Background(), "tcp", "meta.example.com:80")
	if !errors.As(err, &re) || re.Class != teaAddressCloudMetadata || len(d.connects) != 0 {
		t.Errorf("metadata under the flag: %v %v", err, d.connected())
	}
	// no trace function is allowed
	g = d.guard(false, time.Second)
	g.refused = nil
	if _, err = g.dialContext(context.Background(), "tcp", "evil.example.com:443"); !errors.As(err, &re) {
		t.Errorf("without a trace function: %v", err)
	}
}

func TestTeaMachineDialAtMostEightAddresses(t *testing.T) {
	for _, n := range []int{7, 8, 9, 20} {
		var addrs []string
		for i := 0; i < n; i++ {
			addrs = append(addrs, fmt.Sprintf("203.0.113.%d", i+1))
		}
		d := newTeaDial(map[string][]string{"many.example.com": addrs})
		d.connect = func(ctx context.Context, ap netip.AddrPort) (net.Conn, error) {
			return nil, fmt.Errorf("down %s", ap.Addr())
		}
		_, err := d.guard(false, 5*time.Second).dialContext(context.Background(), "tcp", "many.example.com:443")
		want := min(n, 8)
		got := d.connected()
		if len(got) != want {
			t.Errorf("%d addresses: %d dialled, want %d", n, len(got), want)
		}
		for i, g := range got {
			if g != fmt.Sprintf("203.0.113.%d:443", i+1) {
				t.Errorf("%d addresses: dial %d is %s", n, i, g)
			}
		}
		var ce *teaConnectError
		if !errors.As(err, &ce) || ce.Error() != "could not connect to many.example.com: down 203.0.113.1" {
			t.Errorf("%d addresses: %v (the first failure is the cause)", n, err)
		}
	}
}

func TestTeaMachineDialStopsOnRefusalOrInterfaceFailure(t *testing.T) {
	addrs := map[string][]string{"two.example.com": {"203.0.113.1", "203.0.113.2"}}
	for _, c := range []struct {
		name string
		err  error
		is   func(error) bool
	}{
		{"refused", &teaAddressRefusedError{Address: netip.MustParseAddr("127.0.0.1"), Class: teaAddressLoopback, Host: "x"}, func(e error) bool {
			var re *teaAddressRefusedError
			return errors.As(e, &re)
		}},
		{"interfaces", fmt.Errorf("%w: gone", errTeaInterfaces), func(e error) bool { return errors.Is(e, errTeaInterfaces) }},
	} {
		d := newTeaDial(addrs)
		d.connect = func(ctx context.Context, ap netip.AddrPort) (net.Conn, error) { return nil, c.err }
		_, err := d.guard(false, time.Second).dialContext(context.Background(), "tcp", "two.example.com:443")
		if err != c.err || !c.is(err) || len(d.connects) != 1 {
			t.Errorf("%s: %v after %d dials", c.name, err, len(d.connects))
		}
	}
	// any other failure goes on to the next address, and the first failure is the cause
	d := newTeaDial(addrs)
	n := 0
	d.connect = func(ctx context.Context, ap netip.AddrPort) (net.Conn, error) {
		n++
		return nil, fmt.Errorf("failure %d", n)
	}
	_, err := d.guard(false, time.Second).dialContext(context.Background(), "tcp", "two.example.com:443")
	if n != 2 || err == nil || err.Error() != "could not connect to two.example.com: failure 1" {
		t.Errorf("%d dials, error %v", n, err)
	}
	// a connection made by the second address is returned
	d = newTeaDial(addrs)
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()
	k := 0
	d.connect = func(ctx context.Context, ap netip.AddrPort) (net.Conn, error) {
		k++
		if k == 1 {
			return nil, errors.New("down")
		}
		return c1, nil
	}
	conn, err := d.guard(false, time.Second).dialContext(context.Background(), "tcp", "two.example.com:443")
	if err != nil || conn != c1 || k != 2 {
		t.Errorf("second address: %v %v after %d dials", conn, err, k)
	}
}

func TestTeaMachineDialStopsWhenTheCallerGivesUp(t *testing.T) {
	d := newTeaDial(map[string][]string{"two.example.com": {"203.0.113.1", "203.0.113.2", "203.0.113.3"}})
	ctx, cancel := context.WithCancel(context.Background())
	d.connect = func(c context.Context, ap netip.AddrPort) (net.Conn, error) {
		cancel()
		return nil, errors.New("down")
	}
	_, err := d.guard(false, 10*time.Second).dialContext(ctx, "tcp", "two.example.com:443")
	if len(d.connects) != 1 {
		t.Errorf("%d dials after the caller gave up: %v", len(d.connects), d.connected())
	}
	var ce *teaConnectError
	if !errors.As(err, &ce) || ce.Phase != teaPhaseConnect {
		t.Errorf("error %v", err)
	}
}

func TestTeaMachineDialTimeShares(t *testing.T) {
	const total = 600 * time.Millisecond
	for _, n := range []int{1, 2, 3} {
		var addrs []string
		for i := 0; i < n; i++ {
			addrs = append(addrs, fmt.Sprintf("203.0.113.%d", i+1))
		}
		d := newTeaDial(map[string][]string{"slow.example.com": addrs})
		var mu sync.Mutex
		var budgets []time.Duration
		d.connect = func(ctx context.Context, ap netip.AddrPort) (net.Conn, error) {
			dl, ok := ctx.Deadline()
			if !ok {
				return nil, errors.New("no deadline on the attempt")
			}
			mu.Lock()
			budgets = append(budgets, time.Until(dl))
			mu.Unlock()
			<-ctx.Done()
			return nil, ctx.Err()
		}
		start := time.Now()
		_, err := d.guard(false, total).dialContext(context.Background(), "tcp", "slow.example.com:443")
		elapsed := time.Since(start)
		var ce *teaConnectError
		if !errors.As(err, &ce) || ce.Phase != teaPhaseConnect || !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("%d addresses: %v", n, err)
		}
		if elapsed < total*85/100 || elapsed > total*130/100 {
			t.Errorf("%d addresses: the dial took %v, want about %v", n, elapsed, total)
		}
		if len(budgets) != n {
			t.Fatalf("%d addresses: %d attempts", n, len(budgets))
		}
		share := total / time.Duration(n)
		for i, b := range budgets {
			if b < share*70/100 || b > share*125/100 {
				t.Errorf("%d addresses: attempt %d had %v, want about %v", n, i, b, share)
			}
		}
	}
}

func TestTeaMachineDialNoTimeLeft(t *testing.T) {
	d := newTeaDial(map[string][]string{"x.example.com": {"203.0.113.1"}})
	d.connect = func(ctx context.Context, ap netip.AddrPort) (net.Conn, error) { return nil, errors.New("not reached") }
	_, err := d.guard(false, -time.Millisecond).dialContext(context.Background(), "tcp", "x.example.com:443")
	var ce *teaConnectError
	if !errors.As(err, &ce) || ce.Phase != teaPhaseConnect || ce.Err == nil || ce.Error() != "could not connect to x.example.com: connect timed out" {
		t.Errorf("no time left: %v", err)
	}
	if len(d.connects) != 0 {
		t.Errorf("dialled with no time left: %v", d.connected())
	}
}

func TestTeaMachineControlBackstop(t *testing.T) {
	d := newTeaDial(nil)
	g := d.guard(false, time.Second)
	err := g.control("tcp", "localhost:80", nil)
	if err == nil || err.Error() != `the socket address "localhost:80" is not an address literal` {
		t.Errorf("a name: %v", err)
	}
	for _, c := range []struct {
		address string
		class   teaAddressClass
		allowed bool // under the flag
	}{
		{"127.0.0.1:80", teaAddressLoopback, true},
		{"[::1]:80", teaAddressLoopback, true},
		{"10.9.8.7:80", teaAddressOwn, true},
		{"169.254.169.254:80", teaAddressCloudMetadata, false},
		{"169.254.1.1:80", teaAddressLinkLocal, false},
		{"0.0.0.0:80", teaAddressUnspecified, false},
		{"[::]:80", teaAddressUnspecified, false},
	} {
		var re *teaAddressRefusedError
		err := g.control("tcp", c.address, nil)
		if !errors.As(err, &re) || re.Class != c.class {
			t.Errorf("%s without the flag: %v", c.address, err)
		}
		if re != nil {
			if want := netip.MustParseAddrPort(c.address).Addr().String(); re.Address.String() != want || re.Host != want {
				t.Errorf("%s: address %q host %q", c.address, re.Address, re.Host)
			}
		}
		err = d.guard(true, time.Second).control("tcp", c.address, nil)
		if c.allowed && err != nil {
			t.Errorf("%s with the flag: %v", c.address, err)
		}
		if !c.allowed && !errors.As(err, &re) {
			t.Errorf("%s with the flag: %v", c.address, err)
		}
	}
	if err := g.control("tcp", "203.0.113.5:80", nil); err != nil {
		t.Errorf("an allowed address: %v", err)
	}
	d.ownErr = errors.New("no netlink")
	err = g.control("tcp", "203.0.113.5:80", nil)
	if !errors.Is(err, errTeaInterfaces) {
		t.Errorf("interface failure: %v", err)
	}
	_, cerr := g.classify(netip.MustParseAddr("203.0.113.5"))
	if !errors.Is(cerr, errTeaInterfaces) || !strings.Contains(cerr.Error(), "no netlink") {
		t.Errorf("classify with an interface failure: %v", cerr)
	}
}

func TestTeaMachineDialCheckedReachesAnIPv6Literal(t *testing.T) {
	ln, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skipf("no IPv6 loopback: %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	g := newTeaGuard(true, time.Second, nil)
	conn, err := g.dialChecked(context.Background(), netip.AddrPortFrom(netip.MustParseAddr("::1"), uint16(port)))
	if err != nil {
		t.Fatalf("dial [::1]:%d: %v", port, err)
	}
	conn.Close()
	// the production dial path, with a name that is a literal
	conn, err = g.dialContext(context.Background(), "tcp", net.JoinHostPort("::1", fmt.Sprint(port)))
	if err != nil {
		t.Fatalf("dialContext [::1]:%d: %v", port, err)
	}
	conn.Close()
}

func TestTeaMachineInterfaceAddrsAreNormal(t *testing.T) {
	addrs, err := teaInterfaceAddrs()
	if err != nil {
		t.Skipf("no interface list: %v", err)
	}
	loop := false
	for _, a := range addrs {
		if a.Is4In6() {
			t.Errorf("interface address %s is in the mapped form", a)
		}
		if a == netip.MustParseAddr("127.0.0.1") {
			loop = true
		}
	}
	if !loop {
		t.Errorf("127.0.0.1 is not among the interface addresses %v", addrs)
	}
}

func TestTeaMachineHTTPClientConfiguration(t *testing.T) {
	g := newTeaGuard(false, time.Second, nil)
	c := newTeaMachineHTTPClient(g, nil)
	tr, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport %T", c.Transport)
	}
	if tr.Proxy != nil {
		t.Error("the transport has a proxy function")
	}
	if tr.DialContext == nil || tr.Dial != nil || tr.DialTLS != nil || tr.DialTLSContext != nil {
		t.Error("the connection is not made by the guard alone")
	}
	if tr.TLSHandshakeTimeout != 10*time.Second || tr.ResponseHeaderTimeout != 30*time.Second || tr.MaxResponseHeaderBytes != 64<<10 ||
		tr.MaxIdleConns != 4 || tr.MaxIdleConnsPerHost != 4 || tr.IdleConnTimeout != 30*time.Second {
		t.Errorf("transport bounds %v %v %d %d %d %v", tr.TLSHandshakeTimeout, tr.ResponseHeaderTimeout, tr.MaxResponseHeaderBytes,
			tr.MaxIdleConns, tr.MaxIdleConnsPerHost, tr.IdleConnTimeout)
	}
	if tr.ForceAttemptHTTP2 || tr.Protocols == nil || !tr.Protocols.HTTP1() || tr.Protocols.HTTP2() || tr.Protocols.UnencryptedHTTP2() {
		t.Errorf("protocols %v, force HTTP/2 %v", tr.Protocols, tr.ForceAttemptHTTP2)
	}
	if tr.TLSClientConfig == nil || tr.TLSClientConfig.InsecureSkipVerify || tr.TLSClientConfig.RootCAs != nil {
		t.Errorf("TLS configuration %+v", tr.TLSClientConfig)
	}
	pool := x509.NewCertPool()
	c = newTeaMachineHTTPClient(g, &teaNetSeam{rootCAs: pool})
	if tr = c.Transport.(*http.Transport); tr.TLSClientConfig.RootCAs != pool {
		t.Error("the seam's trust pool was not used")
	}
}

func TestTeaMachineResponseHeadersAreBounded(t *testing.T) {
	for _, c := range []struct {
		size int
		ok   bool
	}{{20 << 10, true}, {60 << 10, true}, {70 << 10, false}, {300 << 10, false}} {
		r := newTeaMRow(t)
		f := r.fixture("tea.example.com", true)
		pad := strings.Repeat("a", c.size)
		f.handle(f.api("/product/"+teaProduct), func(w http.ResponseWriter, q *http.Request) {
			w.Header().Set("X-Pad", pad)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"uuid":"` + teaProduct + `","name":"P","identifiers":[]}`))
		})
		got := r.exec(teaVerbProduct, "--api-root", f.root, "--uuid", teaProduct)
		if c.ok {
			r.want(got, teaExitOK)
			continue
		}
		if got == teaExitOK {
			t.Errorf("a header of %d bytes was accepted", c.size)
			continue
		}
		r.want(got, got)
	}
}
