package cmd

import (
	"fmt"
	"net/netip"
	"net/url"
	"strings"
	"testing"
)

// Mutation round for the URL a refusal names, and for the trace flag read through the
// command line.

func TestTeaMachineRefusedHopURLFallsBackToTheErrorsURL(t *testing.T) {
	const first = "https://tea.example.com/tea/v1.0.0/product/p"
	refused := &teaAddressRefusedError{Address: netip.MustParseAddr("127.0.0.1"), Class: teaAddressLoopback, Host: "evil.example.com"}
	chain := &url.Error{Op: "Get", URL: first, Err: fmt.Errorf("get: %w", refused)}
	str := func(p *string) string {
		if p == nil {
			return "<nil>"
		}
		return *p
	}
	for _, c := range []struct {
		name, last, want string
	}{
		{"a last request that does not parse", "https://evil.example.com:bad/x", first},
		{"a last request with a control character", "https://evil.example.com/\x7f", first},
		{"a last request that is no URL", "::::", first},
		{"a last request of the refused host", "https://evil.example.com:8443/x?q=1", "https://evil.example.com:8443/x"},
		{"a last request of a host that only starts like the refused one", "https://evil.example.com.other.net/x", first},
		{"a last request of a host that ends like the refused one", "https://notevil.example.com/x", first},
		{"a last request that only names the refused host in its path", "https://other.example.com/evil.example.com", first},
	} {
		if got := teaErrorURL(chain, teaReasonAddressRefused, c.last); str(got) != c.want {
			t.Errorf("%s: %s, want %s", c.name, str(got), c.want)
		}
	}
	// an address literal is the host the guard refuses when the hop is written as one
	lit := &teaAddressRefusedError{Address: netip.MustParseAddr("169.254.169.254"), Class: teaAddressCloudMetadata, Host: "169.254.169.254"}
	litChain := &url.Error{Op: "Get", URL: first, Err: lit}
	if got := teaErrorURL(litChain, teaReasonAddressRefused, "https://169.254.169.254/latest/meta-data"); str(got) != "https://169.254.169.254/latest/meta-data" {
		t.Errorf("a literal hop: %s", str(got))
	}
	// the name of the refused host is what is compared, not its address
	named := &teaAddressRefusedError{Address: netip.MustParseAddr("169.254.169.254"), Class: teaAddressCloudMetadata, Host: "evil.example.com"}
	if got := teaErrorURL(&url.Error{Op: "Get", URL: first, Err: named}, teaReasonAddressRefused, "https://169.254.169.254/x"); str(got) != first {
		t.Errorf("a hop written as the address of a refused name: %s", str(got))
	}
}

func TestTeaMachineDebugFlagThroughTheCommandLine(t *testing.T) {
	row := newTeaRow(t)
	f := newTeaFixture(t, row.net, "127.0.0.1", false)
	f.route(f.api("/product/"+teaProduct), teaJSON(`{"uuid":"`+teaProduct+`","name":"P","identifiers":[]}`))
	args := []string{"tea", "machine", "product", "--api-root", f.root, "--uuid", teaProduct, "--allow-this-host", "--usehttp"}
	code, out, errOut := teaChild(t, t.TempDir(), nil, append(append([]string(nil), args...), "-d", "true")...)
	if code != 0 || !strings.Contains(out, `"kind":"TEA_PRODUCT"`) {
		t.Fatalf("exit %d stdout %q stderr %q", code, out, errOut)
	}
	if !strings.Contains(errOut, "TEA request") || !strings.Contains(errOut, "TEA response") || !strings.Contains(errOut, f.root+"/v1.0.0/product/"+teaProduct) {
		t.Errorf("no trace lines on stderr with -d true: %q", errOut)
	}
	// without it nothing is written on stderr
	code, _, errOut = teaChild(t, t.TempDir(), nil, args...)
	if code != 0 || errOut != "" {
		t.Errorf("without -d: exit %d stderr %q", code, errOut)
	}
}
