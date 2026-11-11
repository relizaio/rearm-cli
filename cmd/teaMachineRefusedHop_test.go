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
	"fmt"
	"net/netip"
	"net/url"
	"testing"
)

// The URL a refusal names (task TPF-178, T-3 of test run 2): the refused hop of a redirect
// chain, never the chain's first request.

func TestTeaMachineRefusedHopURL(t *testing.T) {
	refused := func(host string) error {
		return fmt.Errorf("get: %w", &teaAddressRefusedError{Address: netip.MustParseAddr("127.0.0.1"), Class: teaAddressLoopback, Host: host})
	}
	// the library's error carries the first request; the trace saw the hop last
	chain := func(host string) error {
		return &url.Error{Op: "Get", URL: "https://tea.example.com/tea/v1.0.0/product/p", Err: refused(host)}
	}
	str := func(p *string) string {
		if p == nil {
			return "<nil>"
		}
		return *p
	}
	for _, c := range []struct {
		name, last string
		err        error
		want       string
	}{
		{"the hop, redacted", "https://user:pw@Evil.Example.com:8443/x?q=1#f", chain("evil.example.com"), "https://Evil.Example.com:8443/x"},
		{"an IPv6 literal hop", "https://[::1]:8443/x", chain("::1"), "https://[::1]:8443/x"},
		{"a zoned hop", "https://[fe80::1%25eth0]:8443/x", chain("fe80::1%eth0"), "https://[fe80::1%eth0]:8443/x"},
		{"the last request is another host: the error's URL", "https://other.example.com/y", chain("evil.example.com"), "https://tea.example.com/tea/v1.0.0/product/p"},
		{"no last request: the error's URL", "", chain("evil.example.com"), "https://tea.example.com/tea/v1.0.0/product/p"},
		{"a refusal without a URL error", "https://evil.example.com/z", refused("evil.example.com"), "https://evil.example.com/z"},
		{"not a refusal: the error's URL", "https://evil.example.com/z", &url.Error{Op: "Get", URL: "https://tea.example.com/a", Err: fmt.Errorf("boom")}, "https://tea.example.com/a"},
	} {
		got := teaErrorURL(c.err, teaReasonAddressRefused, c.last)
		if str(got) != c.want {
			t.Errorf("%s: %s, want %s", c.name, str(got), c.want)
		}
	}
	// the message names the hop too
	u := teaErrorURL(chain("evil.example.com"), teaReasonAddressRefused, "https://evil.example.com:8443/x")
	if got, want := teaErrorText(chain("evil.example.com"), teaReasonAddressRefused, u), "refused to connect to 127.0.0.1 (loopback) for https://evil.example.com:8443/x"; got != want {
		t.Errorf("message %q, want %q", got, want)
	}
}
