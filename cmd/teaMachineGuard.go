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
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"syscall"
	"time"
)

// The address refusal of the TEA machine mode (task TPF-178, design D6) and the one HTTP
// client of a machine run (D7). Every connection of a run, whatever URL led to it (the
// well-known document, discovery, API calls, the token request, redirects, artifact URLs),
// is made by teaGuard.dialContext: it resolves the host, classifies every address, never
// dials a refused one and dials the classified literal, so no second resolution can answer
// differently. The interactive verbs do not use this client.

// teaAddressClass is the class of a refused address; teaAddressAllowed is every other one.
type teaAddressClass string

const (
	teaAddressAllowed       teaAddressClass = ""
	teaAddressCloudMetadata teaAddressClass = "CLOUD_METADATA"
	teaAddressLinkLocal     teaAddressClass = "LINK_LOCAL"
	teaAddressUnspecified   teaAddressClass = "UNSPECIFIED"
	teaAddressLoopback      teaAddressClass = "LOOPBACK"
	teaAddressOwn           teaAddressClass = "OWN_ADDRESS"
)

// label is the class as the error message names it.
func (c teaAddressClass) label() string {
	switch c {
	case teaAddressCloudMetadata:
		return "cloud metadata"
	case teaAddressLinkLocal:
		return "link-local"
	case teaAddressUnspecified:
		return "unspecified"
	case teaAddressLoopback:
		return "loopback"
	case teaAddressOwn:
		return "an address of this host"
	}
	return "allowed"
}

// liftedByAllowThisHost reports whether --allow-this-host lifts the class: loopback and the
// host's own addresses, nothing else.
func (c teaAddressClass) liftedByAllowThisHost() bool {
	return c == teaAddressLoopback || c == teaAddressOwn
}

// teaCloudMetadata is the fixed list of cloud metadata (or credential) endpoints, checked
// before every other class: 169.254.169.254 (AWS, Google, Azure, Oracle and others over
// IPv4), 100.100.100.200 (Alibaba Cloud), 168.63.129.16 (Azure platform, WireServer),
// fd00:ec2::254 (AWS instance metadata over IPv6), fd00:ec2::23 (EKS Pod Identity over
// IPv6), fd20:ce::254 (Google Compute Engine metadata over IPv6), fd00:c1::a9fe:a9fe
// (Oracle Cloud instance metadata over IPv6).
var teaCloudMetadata = []netip.Addr{
	netip.MustParseAddr("169.254.169.254"),
	netip.MustParseAddr("100.100.100.200"),
	netip.MustParseAddr("168.63.129.16"),
	netip.MustParseAddr("fd00:ec2::254"),
	netip.MustParseAddr("fd00:ec2::23"),
	netip.MustParseAddr("fd20:ce::254"),
	netip.MustParseAddr("fd00:c1::a9fe:a9fe"),
}

// teaNormalAddr is the form an address is classified in: without its zone, and an
// IPv4-mapped IPv6 address as its IPv4 address.
func teaNormalAddr(a netip.Addr) netip.Addr {
	return a.WithZone("").Unmap()
}

// classifyTeaAddress is the one classification of the machine mode (D6). own are the host's
// interface addresses; the first matching class wins, so a link-local own address is
// LINK_LOCAL, which --allow-this-host does not lift. NAT64, 6to4, Teredo and the
// IPv4-compatible form are not decoded (a stated non-goal of the design).
func classifyTeaAddress(addr netip.Addr, own []netip.Addr) teaAddressClass {
	a := teaNormalAddr(addr)
	for _, m := range teaCloudMetadata {
		if a == m {
			return teaAddressCloudMetadata
		}
	}
	switch {
	case a.IsLinkLocalUnicast():
		return teaAddressLinkLocal
	case a.IsUnspecified(), a.Is4() && a.As4()[0] == 0:
		return teaAddressUnspecified
	case a.IsLoopback():
		return teaAddressLoopback
	}
	for _, o := range own {
		if a == teaNormalAddr(o) {
			return teaAddressOwn
		}
	}
	return teaAddressAllowed
}

// teaRefused reports whether the guard refuses the class with or without --allow-this-host.
func teaRefused(c teaAddressClass, allowThisHost bool) bool {
	if c == teaAddressAllowed {
		return false
	}
	return !(allowThisHost && c.liftedByAllowThisHost())
}

// teaAddressRefusedError is a connection the guard refused: every address of the host was
// in a refused class. Address is the first refused address in its normal form.
type teaAddressRefusedError struct {
	Address netip.Addr
	Class   teaAddressClass
	Host    string
}

func (e *teaAddressRefusedError) Error() string {
	return fmt.Sprintf("refused to connect to %s (%s)", e.Address, e.Class.label())
}

// teaConnectPhase is where a connection that could not be made failed.
type teaConnectPhase string

const (
	teaPhaseResolve teaConnectPhase = "RESOLVE"
	teaPhaseConnect teaConnectPhase = "CONNECT"
)

// teaConnectError is a connection that could not be made: the name did not resolve, or no
// allowed address answered within the connect timeout. It claims neither a refusal nor a
// timeout (D5): the cause is in Err.
type teaConnectError struct {
	Phase teaConnectPhase
	Host  string
	Err   error
}

func (e *teaConnectError) Error() string {
	if e.Phase == teaPhaseResolve {
		return fmt.Sprintf("could not resolve %s: %v", e.Host, e.Err)
	}
	return fmt.Sprintf("could not connect to %s: %v", e.Host, e.Err)
}

func (e *teaConnectError) Unwrap() error { return e.Err }

// errTeaInterfaces: the host's interface addresses could not be listed, so no dial is made
// (the own-address refusal fails closed).
var errTeaInterfaces = errors.New("the network interfaces of this host could not be listed, so no connection is made")

// errTeaNoTimeLeft: the connect timeout ended before an address could be tried.
var errTeaNoTimeLeft = errors.New("connect timed out")

// teaMaxDialAddresses bounds the allowed addresses tried for one dial.
const teaMaxDialAddresses = 8

// teaNetSeam is what lies UNDER the guard, replaced by tests; nil in production. The
// guard's own code (classification, the order of its steps, its errors) is the same in
// tests and in production.
type teaNetSeam struct {
	lookup         func(ctx context.Context, host string) ([]netip.Addr, error)
	interfaceAddrs func() ([]netip.Addr, error)
	connect        func(ctx context.Context, ap netip.AddrPort) (net.Conn, error)
	rootCAs        *x509.CertPool
	attemptTimeout time.Duration // the library's time bounds, 0 for its defaults
	callTimeout    time.Duration
	idleTimeout    time.Duration
	firstBackoff   time.Duration // the library's first discovery backoff, 0 for its default
}

// teaGuard dials every connection of a machine run.
type teaGuard struct {
	allowThisHost  bool
	connectTimeout time.Duration
	lookup         func(ctx context.Context, host string) ([]netip.Addr, error)
	interfaceAddrs func() ([]netip.Addr, error)
	connect        func(ctx context.Context, ap netip.AddrPort) (net.Conn, error)
	refused        func(addr netip.Addr, class teaAddressClass, host string) // trace, may be nil
}

// newTeaGuard builds the guard with the production lookup, interface list and connect, each
// replaced by the seam's when set.
func newTeaGuard(allowThisHost bool, connectTimeout time.Duration, seam *teaNetSeam) *teaGuard {
	g := &teaGuard{allowThisHost: allowThisHost, connectTimeout: connectTimeout}
	g.lookup = func(ctx context.Context, host string) ([]netip.Addr, error) {
		return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	}
	g.interfaceAddrs = teaInterfaceAddrs
	g.connect = g.dialChecked
	if seam != nil {
		if seam.lookup != nil {
			g.lookup = seam.lookup
		}
		if seam.interfaceAddrs != nil {
			g.interfaceAddrs = seam.interfaceAddrs
		}
		if seam.connect != nil {
			g.connect = seam.connect
		}
	}
	return g
}

// teaInterfaceAddrs lists the addresses of this host's network interfaces.
func teaInterfaceAddrs() ([]netip.Addr, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}
	out := make([]netip.Addr, 0, len(addrs))
	for _, a := range addrs {
		var ip net.IP
		switch v := a.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		if na, ok := netip.AddrFromSlice(ip); ok {
			out = append(out, na.Unmap())
		}
	}
	return out, nil
}

// classify classifies addr against the host's interface addresses as they are now.
func (g *teaGuard) classify(addr netip.Addr) (teaAddressClass, error) {
	own, err := g.interfaceAddrs()
	if err != nil {
		return teaAddressAllowed, fmt.Errorf("%w: %v", errTeaInterfaces, err)
	}
	return classifyTeaAddress(addr, own), nil
}

// dialContext is the DialContext of the machine mode's transport (D6 steps 1 to 5).
func (g *teaGuard) dialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return nil, &teaConnectError{Phase: teaPhaseResolve, Host: address, Err: err}
	}
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil {
		return nil, &teaConnectError{Phase: teaPhaseResolve, Host: host, Err: fmt.Errorf("invalid port %q", portText)}
	}
	// 1. The whole dial, name resolution included, within the connect timeout.
	ctx, cancel := context.WithTimeout(ctx, g.connectTimeout)
	defer cancel()

	// 2. The literal itself, or every address the resolver answers, both families.
	var addrs []netip.Addr
	if lit, perr := netip.ParseAddr(host); perr == nil {
		addrs = []netip.Addr{lit}
	} else {
		addrs, err = g.lookup(ctx, host)
		if err != nil {
			return nil, &teaConnectError{Phase: teaPhaseResolve, Host: host, Err: err}
		}
		if len(addrs) == 0 {
			return nil, &teaConnectError{Phase: teaPhaseResolve, Host: host, Err: errors.New("no address")}
		}
	}

	// 3. The interface addresses, read at every dial; fail closed.
	own, err := g.interfaceAddrs()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errTeaInterfaces, err)
	}

	// 4. Classify every address; a refused one is never dialled.
	var allowed []netip.Addr
	var first *teaAddressRefusedError
	for _, a := range addrs {
		class := classifyTeaAddress(a, own)
		if !teaRefused(class, g.allowThisHost) {
			allowed = append(allowed, teaNormalAddr(a))
			continue
		}
		if g.refused != nil {
			g.refused(teaNormalAddr(a), class, host)
		}
		if first == nil {
			first = &teaAddressRefusedError{Address: teaNormalAddr(a), Class: class, Host: host}
		}
	}
	if len(allowed) == 0 {
		return nil, first
	}
	if len(allowed) > teaMaxDialAddresses {
		allowed = allowed[:teaMaxDialAddresses]
	}
	deadline, _ := ctx.Deadline()
	var firstErr error
	for i, a := range allowed {
		left := time.Until(deadline)
		if left <= 0 || ctx.Err() != nil {
			break
		}
		// Each address gets an equal share of the time left, as a context deadline: never
		// as net.Dialer.Timeout, where zero would mean no timeout.
		actx, acancel := context.WithTimeout(ctx, left/time.Duration(len(allowed)-i))
		conn, cerr := g.connect(actx, netip.AddrPortFrom(a, uint16(port)))
		acancel()
		if cerr == nil {
			return conn, nil
		}
		var refused *teaAddressRefusedError
		if errors.As(cerr, &refused) || errors.Is(cerr, errTeaInterfaces) {
			return nil, cerr
		}
		if firstErr == nil {
			firstErr = cerr
		}
	}
	if firstErr == nil {
		firstErr = errTeaNoTimeLeft
	}
	// 5. No connection was made.
	return nil, &teaConnectError{Phase: teaPhaseConnect, Host: host, Err: firstErr}
}

// dialChecked is the production connect: a TCP connection to the classified literal, by a
// dialer whose Control classifies the socket's address once more before connect(2) (D6 step
// 6), so nothing connects to a refused address whatever a later change does above it.
func (g *teaGuard) dialChecked(ctx context.Context, ap netip.AddrPort) (net.Conn, error) {
	d := &net.Dialer{Control: g.control}
	return d.DialContext(ctx, "tcp", ap.String())
}

// control is the backstop of dialChecked.
func (g *teaGuard) control(_, address string, _ syscall.RawConn) error {
	ap, err := netip.ParseAddrPort(address)
	if err != nil {
		return fmt.Errorf("the socket address %q is not an address literal", address)
	}
	class, err := g.classify(ap.Addr())
	if err != nil {
		return err
	}
	if teaRefused(class, g.allowThisHost) {
		return &teaAddressRefusedError{Address: teaNormalAddr(ap.Addr()), Class: class, Host: ap.Addr().String()}
	}
	return nil
}

// Fixed bounds of the machine mode's transport (D7): never computed, never zero.
const (
	teaMachineHandshakeTimeout = 10 * time.Second
	teaMachineHeaderTimeout    = 30 * time.Second
	teaMachineHeaderBytes      = 64 << 10
	teaMachineIdleConns        = 4
	teaMachineIdleConnTimeout  = 30 * time.Second
)

// teaProxyVariables are the proxy variables the machine mode ignores (D7).
var teaProxyVariables = []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"}

// newTeaMachineHTTPClient is the one HTTP client of a machine run: the guard dials, no
// proxy, the system trust store (or the seam's pool), HTTP/1.1 only, bounded headers.
func newTeaMachineHTTPClient(g *teaGuard, seam *teaNetSeam) *http.Client {
	tlsConfig := &tls.Config{}
	if seam != nil && seam.rootCAs != nil {
		tlsConfig.RootCAs = seam.rootCAs
	}
	var protocols http.Protocols
	protocols.SetHTTP1(true)
	tr := &http.Transport{
		Proxy:                  nil,
		DialContext:            g.dialContext,
		TLSClientConfig:        tlsConfig,
		TLSHandshakeTimeout:    teaMachineHandshakeTimeout,
		ResponseHeaderTimeout:  teaMachineHeaderTimeout,
		MaxResponseHeaderBytes: teaMachineHeaderBytes,
		MaxIdleConns:           teaMachineIdleConns,
		MaxIdleConnsPerHost:    teaMachineIdleConns,
		IdleConnTimeout:        teaMachineIdleConnTimeout,
		ForceAttemptHTTP2:      false,
		Protocols:              &protocols,
	}
	return &http.Client{Transport: tr}
}
