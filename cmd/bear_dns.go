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
	"net"
	"net/http"
	"strings"
	"time"
)

// buildBearHttpClient returns the http.Client used for outbound calls to
// BEAR.
//
// When --resilientDns is true (the default), the returned client uses a
// custom DialContext that side-steps two well-known pure-Go-resolver
// issues that show up when the CLI runs inside a Kubernetes pod:
//
//   - /etc/resolv.conf inside k8s carries ndots:5 plus 4+ search domains,
//     so every external lookup is expanded to ~5 sequential queries
//     against CoreDNS before the absolute name is tried. Under burst
//     load (rebom's enrichment scheduler fires up to 50 BOMs per cycle,
//     each calling the CLI which iterates batches) this surfaces as
//     "no such host" or "server misbehaving" even though `nslookup`
//     from the same pod resolves fine.
//   - Pure-Go fires A and AAAA in parallel on the same UDP source port,
//     and a single SERVFAIL on either fails the whole lookup.
//
// Mitigations:
//   - Treat the host as a fully-qualified name by appending a trailing
//     dot before resolving, which the pure-Go resolver honors by
//     skipping search-domain expansion (RFC 1034 absolute name).
//   - Retry the lookup once after a short backoff on transient errors.
//
// TLS SNI is unaffected: http.Transport derives ServerName from the
// request URL host (without the trailing dot), and the dial happens by
// IP literal after we resolve.
//
// When --resilientDns is false, the call falls back to a plain
// http.Client — needed when BEAR is in-cluster and only resolvable via
// the same search domains we are skipping (e.g.
// http://bear.rearm.svc.cluster.local).
func buildBearHttpClient(timeout time.Duration) *http.Client {
	if !resilientBearDns {
		return &http.Client{Timeout: timeout}
	}

	resolver := &net.Resolver{PreferGo: true, StrictErrors: false}
	dialer := &net.Dialer{
		Timeout:   15 * time.Second,
		KeepAlive: 30 * time.Second,
		Resolver:  resolver,
	}

	dialContext := func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return dialer.DialContext(ctx, network, addr)
		}
		// IP literals: nothing to resolve.
		if ip := net.ParseIP(host); ip != nil {
			return dialer.DialContext(ctx, network, addr)
		}

		fqdn := host
		if !strings.HasSuffix(fqdn, ".") {
			fqdn += "."
		}

		var lastErr error
		for attempt := 0; attempt < 2; attempt++ {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			ips, lookupErr := resolver.LookupIP(ctx, "ip", fqdn)
			if lookupErr == nil && len(ips) > 0 {
				conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
				if dialErr == nil {
					return conn, nil
				}
				lastErr = dialErr
			} else if lookupErr != nil {
				lastErr = lookupErr
			}
			if attempt == 0 {
				select {
				case <-time.After(200 * time.Millisecond):
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
		}
		return nil, lastErr
	}

	transport := &http.Transport{
		DialContext:           dialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		MaxIdleConns:          100,
	}
	return &http.Client{Transport: transport, Timeout: timeout}
}
