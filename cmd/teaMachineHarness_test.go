package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

// The rig of the machine-mode tests (task TPF-178, design section 4). The fixtures of
// teaFixture_test.go listen on 127.0.0.1; the seam replaces what lies UNDER the guard: the
// test lookup answers names with addresses the row chooses (documentation addresses
// 203.0.113.0/24 for the fixtures, which the guard allows), and the test connect records
// every address it is asked for and maps an address on a fixture's port to that fixture's
// listener. So every ordinary row runs in the default configuration, without
// --allow-this-host, with the real guard in the path, and a refusal row that leaked would
// show in connects and in the fixture's requests.

// teaMNet is the seam of one row.
type teaMNet struct {
	t        *testing.T
	base     *teaNet
	mu       sync.Mutex
	names    map[string][]netip.Addr
	lookups  []string
	lookErr  error
	own      []netip.Addr
	ownErr   error
	ownNext  []netip.Addr // from the second interface read on, when set
	ownReads int
	connects []netip.AddrPort
	hook     func(ctx context.Context, ap netip.AddrPort) (net.Conn, error)
	next     int
}

func (n *teaMNet) seam() *teaNetSeam {
	return &teaNetSeam{
		lookup: func(ctx context.Context, host string) ([]netip.Addr, error) {
			n.mu.Lock()
			defer n.mu.Unlock()
			n.lookups = append(n.lookups, host)
			if n.lookErr != nil {
				return nil, n.lookErr
			}
			a, ok := n.names[strings.ToLower(host)]
			if !ok {
				return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
			}
			return append([]netip.Addr(nil), a...), nil
		},
		interfaceAddrs: func() ([]netip.Addr, error) {
			n.mu.Lock()
			defer n.mu.Unlock()
			n.ownReads++
			if n.ownErr != nil {
				return nil, n.ownErr
			}
			if n.ownNext != nil && n.ownReads > 1 {
				return append([]netip.Addr(nil), n.ownNext...), nil
			}
			return append([]netip.Addr(nil), n.own...), nil
		},
		connect: func(ctx context.Context, ap netip.AddrPort) (net.Conn, error) {
			n.mu.Lock()
			n.connects = append(n.connects, ap)
			hook := n.hook
			n.mu.Unlock()
			if hook != nil {
				return hook(ctx, ap)
			}
			real := n.listener(ap.Port())
			if real == "" {
				return nil, fmt.Errorf("test connect: no fixture on port %d", ap.Port())
			}
			var d net.Dialer
			return d.DialContext(ctx, "tcp", real)
		},
		rootCAs:      n.base.pool,
		firstBackoff: time.Millisecond,
	}
}

// listener is the 127.0.0.1 address of the fixture on port, "" when none.
func (n *teaMNet) listener(port uint16) string {
	n.base.mu.Lock()
	defer n.base.mu.Unlock()
	suffix := ":" + strconv.Itoa(int(port))
	for _, real := range n.base.addrs {
		if strings.HasSuffix(real, suffix) {
			return real
		}
	}
	return ""
}

func (n *teaMNet) connected() []netip.AddrPort {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]netip.AddrPort(nil), n.connects...)
}

// connectedTo reports whether connect was asked for addr (normal form).
func (n *teaMNet) connectedTo(addr string) bool {
	want := teaNormalAddr(netip.MustParseAddr(addr))
	for _, ap := range n.connected() {
		if teaNormalAddr(ap.Addr()) == want {
			return true
		}
	}
	return false
}

func (n *teaMNet) resolveTo(host string, addrs ...string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	var out []netip.Addr
	for _, a := range addrs {
		out = append(out, netip.MustParseAddr(a))
	}
	n.names[strings.ToLower(host)] = out
}

// teaMRow is one machine run against fixtures.
type teaMRow struct {
	*teaRow
	m     *teaMNet
	seam  *teaNetSeam
	debug bool
}

func newTeaMRow(t *testing.T) *teaMRow {
	row := newTeaRow(t)
	row.in = failingReader{t}
	m := &teaMNet{t: t, base: row.net, names: map[string][]netip.Addr{},
		own: []netip.Addr{netip.MustParseAddr("10.9.8.7"), netip.MustParseAddr("fe80::7")}}
	return &teaMRow{teaRow: row, m: m, seam: m.seam()}
}

// fixture starts a fixture on host that resolves to its own documentation address.
func (r *teaMRow) fixture(host string, tlsOn bool) *teaFixture {
	f := newTeaFixture(r.t, r.net, host, tlsOn)
	r.m.mu.Lock()
	r.m.next++
	addr := netip.AddrFrom4([4]byte{203, 0, 113, byte(9 + r.m.next)})
	r.m.names[strings.ToLower(host)] = []netip.Addr{addr}
	r.m.mu.Unlock()
	return f
}

func (r *teaMRow) io() teaIO {
	tio := r.teaRow.io()
	tio.extra = nil
	tio.machine = r.seam
	return tio
}

// exec parses args with the verb's real flag set and runs it in-process.
func (r *teaMRow) exec(verb teaMachineVerb, args ...string) teaExit {
	r.out.Reset()
	r.errOut.Reset()
	return r.execCtx(context.Background(), verb, args...)
}

func (r *teaMRow) execCtx(ctx context.Context, verb teaMachineVerb, args ...string) teaExit {
	c := &cobra.Command{Use: string(verb)}
	addTeaMachineFlags(c, verb)
	if err := c.Flags().Parse(args); err != nil {
		return teaMachineFail(r.io(), ctx, teaUsage("%s", err.Error()), "", nil)
	}
	in := teaMachineInputsFromFlags(c, verb, c.Flags().Args())
	in.debug = r.debug
	return runTeaMachine(ctx, r.io(), in)
}

// want checks the exit code and the shape of both streams: on 0 and 7 one ASCII document
// line and an empty stderr (unless debugging), else an empty stdout and one error object
// as the last stderr line.
func (r *teaMRow) want(got, code teaExit) {
	r.t.Helper()
	if got != code {
		r.t.Fatalf("exit %d, want %d; stdout %q; stderr %q", got, code, r.out.String(), r.errOut.String())
	}
	out, errOut := r.out.String(), r.errOut.String()
	if code == teaExitOK || code == teaExitPartial {
		teaWantASCIILine(r.t, "stdout", out)
		if !r.debug && errOut != "" {
			r.t.Errorf("stderr %q, want empty", errOut)
		}
		return
	}
	if out != "" {
		r.t.Errorf("stdout %q, want empty", out)
	}
	lines := teaLines(errOut)
	last := lines[len(lines)-1] + "\n"
	teaWantASCIILine(r.t, "stderr", last)
	if !r.debug && len(lines) != 1 {
		r.t.Errorf("stderr has %d lines: %q", len(lines), errOut)
	}
	for _, l := range lines[:len(lines)-1] {
		if strings.HasPrefix(l, "{") {
			r.t.Errorf("a stderr line before the error object starts with {: %q", l)
		}
	}
}

// wantFail checks the exit code and the reason of the error object, and returns the object.
func (r *teaMRow) wantFail(got, code teaExit, reason teaReason) map[string]any {
	r.t.Helper()
	r.want(got, code)
	e := r.errObj()
	if e["reason"] != string(reason) || e["exitCode"] != float64(code) || e["kind"] != "TEA_ERROR" || e["formatVersion"] != float64(1) {
		r.t.Fatalf("error object %v, want reason %s exit %d", e, reason, code)
	}
	return e
}

func (r *teaMRow) errObj() map[string]any {
	r.t.Helper()
	lines := teaLines(r.errOut.String())
	var m map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &m); err != nil {
		r.t.Fatalf("error object %q: %v", lines[len(lines)-1], err)
	}
	for _, k := range []string{"formatVersion", "kind", "exitCode", "reason", "message", "url", "httpStatus", "address", "addressClass", "phase", "hint", "ignoredProxyVariables"} {
		if _, ok := m[k]; !ok {
			r.t.Errorf("error object without %q: %v", k, m)
		}
	}
	return m
}

func (r *teaMRow) doc() map[string]any {
	r.t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(r.out.String()), &m); err != nil {
		r.t.Fatalf("stdout %q: %v", r.out.String(), err)
	}
	return m
}

// golden compares stdout with the expected document byte for byte.
func (r *teaMRow) golden(want string) {
	r.t.Helper()
	if got := r.out.String(); got != want+"\n" {
		r.t.Errorf("stdout\n got %s\nwant %s", got, want)
	}
}

// teaWantASCIILine: exactly one line, a final line feed, every other byte 0x20 to 0x7E.
func teaWantASCIILine(t *testing.T, what, s string) {
	t.Helper()
	if !strings.HasSuffix(s, "\n") || strings.Count(s, "\n") != 1 {
		t.Errorf("%s is not one line: %q", what, s)
		return
	}
	for i := 0; i < len(s)-1; i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			t.Errorf("%s byte %d is 0x%02x", what, i, s[i])
			return
		}
	}
}

// teaNoRequests fails when any fixture received a request.
func teaNoRequests(t *testing.T, fs ...*teaFixture) {
	t.Helper()
	for _, f := range fs {
		if reqs := f.requests(); len(reqs) != 0 {
			t.Errorf("%s received %d request(s): %+v", f.host, len(reqs), reqs)
		}
	}
}

// teaPath is a dotted path into a decoded document: "files.0.status".
func teaPath(v any, path string) any {
	for _, p := range strings.Split(path, ".") {
		switch x := v.(type) {
		case map[string]any:
			v = x[p]
		case []any:
			i, err := strconv.Atoi(p)
			if err != nil || i < 0 || i >= len(x) {
				return nil
			}
			v = x[i]
		default:
			return nil
		}
	}
	return v
}

var errTeaTestRefused = errors.New("connection refused")
