package cmd

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	tea "github.com/oolong-project/tea-client-go"
	"github.com/oolong-project/tea-client-go/teaapi"
	"github.com/oolong-project/tea-client-go/tei"
)

// Mutation round for the output contract: the reason of every error and the exit code of
// every reason (docs/tea.md, "Exit codes and the error object"), the error object, the
// ASCII encoding, the bound on server strings at every field, and the manifest helpers.

func TestTeaMachineReasonOfEveryError(t *testing.T) {
	done, stop := context.WithCancelCause(context.Background())
	stop(errTeaDeadline)
	cut, stopCut := context.WithCancelCause(context.Background())
	stopCut(context.Canceled)
	live := context.Background()
	refused := &teaAddressRefusedError{Address: netip.MustParseAddr("127.0.0.1"), Class: teaAddressLoopback, Host: "h"}
	connect := &teaConnectError{Phase: teaPhaseConnect, Host: "h", Err: errors.New("down")}
	wrap := func(err error) error { return fmt.Errorf("while reading: %w", err) }

	for _, c := range []struct {
		name  string
		ctx   context.Context
		err   error
		bound teaFileBound
		want  teaReason
	}{
		{"usage", live, teaUsage("bad"), teaFileBoundNone, teaReasonUsage},
		{"wrapped usage", live, wrap(teaUsage("bad")), teaFileBoundNone, teaReasonUsage},
		{"tei syntax", live, tei.ErrSyntax, teaFileBoundNone, teaReasonUsage},
		{"tei syntax wrapped", live, wrap(tei.ErrSyntax), teaFileBoundNone, teaReasonUsage},
		{"tei legacy", live, tei.ErrLegacyForm, teaFileBoundNone, teaReasonUsage},
		{"tei legacy wrapped", live, wrap(tei.ErrLegacyForm), teaFileBoundNone, teaReasonUsage},
		{"invalid option", live, tea.ErrInvalidOption, teaFileBoundNone, teaReasonUsage},
		{"invalid option wrapped", live, wrap(tea.ErrInvalidOption), teaFileBoundNone, teaReasonUsage},
		{"usage with the deadline passed", done, teaUsage("bad"), teaFileBoundNone, teaReasonUsage},
		{"deadline", done, errors.New("x"), teaFileBoundNone, teaReasonDeadline},
		{"deadline before a refusal", done, refused, teaFileBoundNone, teaReasonDeadline},
		{"deadline before a connect failure", done, connect, teaFileBoundNone, teaReasonDeadline},
		{"deadline before a timeout", done, tea.ErrTimeout, teaFileBoundNone, teaReasonDeadline},
		{"interrupt", cut, errors.New("x"), teaFileBoundNone, teaReasonInterrupted},
		{"interrupt before a refusal", cut, refused, teaFileBoundNone, teaReasonInterrupted},
		{"refusal", live, refused, teaFileBoundNone, teaReasonAddressRefused},
		{"refusal wrapped", live, wrap(refused), teaFileBoundNone, teaReasonAddressRefused},
		{"refusal and connect failure", live, errors.Join(connect, refused), teaFileBoundNone, teaReasonAddressRefused},
		{"connect failure", live, connect, teaFileBoundNone, teaReasonConnectFailed},
		{"connect failure wrapped", live, &url.Error{Op: "Get", URL: "https://h/x", Err: connect}, teaFileBoundNone, teaReasonConnectFailed},
		{"redirect limit", live, tea.ErrRedirectLimit, teaFileBoundNone, teaReasonRedirectLimit},
		{"insecure url", live, tea.ErrInsecureURL, teaFileBoundNone, teaReasonURLRefused},
		{"unauthorized", live, tea.ErrUnauthorized, teaFileBoundNone, teaReasonAuth},
		{"forbidden", live, tea.ErrForbidden, teaFileBoundNone, teaReasonAuth},
		{"token error", live, &tea.TokenError{Status: 401}, teaFileBoundNone, teaReasonAuth},
		{"auth error", live, &teaAuthError{msg: "m", err: errors.New("x")}, teaFileBoundNone, teaReasonAuth},
		{"not resolved", live, &tea.NotResolvedError{}, teaFileBoundNone, teaReasonNotFound},
		{"object unknown", live, tea.ErrObjectUnknown, teaFileBoundNone, teaReasonNotFound},
		{"no result", live, errTeaNoResult, teaFileBoundNone, teaReasonNotFound},
		{"too large, a document", live, tea.ErrResponseTooLarge, teaFileBoundNone, teaReasonDocumentBytes},
		{"too large, an artifact", live, tea.ErrResponseTooLarge, teaFileBoundArtifact, teaReasonArtifactBytes},
		{"too large, the total", live, tea.ErrResponseTooLarge, teaFileBoundTotal, teaReasonTotalBytes},
		{"too many results", live, tea.ErrTooManyResults, teaFileBoundNone, teaReasonDiscoveryResults},
		{"timeout", live, tea.ErrTimeout, teaFileBoundNone, teaReasonRequestTimeout},
		{"string bound", live, &teaLimitError{reason: teaReasonStringBytes, msg: "m"}, teaFileBoundNone, teaReasonStringBytes},
		{"output bound", live, &teaLimitError{reason: teaReasonOutputBytes, msg: "m"}, teaFileBoundNone, teaReasonOutputBytes},
		{"file count", live, &teaLimitError{reason: teaReasonFileCount, msg: "m"}, teaFileBoundNone, teaReasonFileCount},
		{"no usable checksum", live, tea.ErrNoUsableChecksum, teaFileBoundArtifact, teaReasonNoUsableChecksum},
		{"checksum mismatch", live, tea.ErrChecksumMismatch, teaFileBoundArtifact, teaReasonChecksumMismatch},
		{"no acceptable format", live, tea.ErrNoAcceptableFormat, teaFileBoundArtifact, teaReasonNoAcceptableFormat},
		{"server error", live, tea.ErrServer, teaFileBoundNone, teaReasonServerError},
		{"unexpected response", live, &tea.UnexpectedResponseError{Operation: "op", Status: 200, Reason: "r"}, teaFileBoundNone, teaReasonInvalidResponse},
		{"content coding", live, tea.ErrUnsupportedContentEncoding, teaFileBoundNone, teaReasonInvalidResponse},
		{"invalid argument", live, tea.ErrInvalidArgument, teaFileBoundNone, teaReasonInvalidResponse},
		{"certificate", live, &tls.CertificateVerificationError{Err: errors.New("bad")}, teaFileBoundNone, teaReasonTLSCertificate},
		{"path error", live, &fs.PathError{Op: "open", Path: "/x", Err: os.ErrPermission}, teaFileBoundNone, teaReasonLocalIO},
		{"link error", live, &os.LinkError{Op: "rename", Old: "a", New: "b", Err: os.ErrPermission}, teaFileBoundNone, teaReasonLocalIO},
		{"anything else", live, errors.New("odd"), teaFileBoundNone, teaReasonFailed},
	} {
		if got := teaMachineReason(c.ctx, c.err, c.bound); got != c.want {
			t.Errorf("%s: reason %s, want %s", c.name, got, c.want)
		}
	}
}

func TestTeaMachineExitOfEveryReason(t *testing.T) {
	for reason, want := range map[teaReason]teaExit{
		teaReasonUsage: 2, teaReasonNotFound: 3, teaReasonURLRefused: 5, teaReasonRedirectLimit: 5, teaReasonAuth: 6,
		teaReasonAddressRefused: 8, teaReasonConnectFailed: 10,
		teaReasonDeadline: 9, teaReasonRequestTimeout: 9, teaReasonDocumentBytes: 9, teaReasonStringBytes: 9,
		teaReasonOutputBytes: 9, teaReasonFileCount: 9, teaReasonDiscoveryResults: 9,
		teaReasonFailed: 1, teaReasonServerError: 1, teaReasonInvalidResponse: 1, teaReasonTLSCertificate: 1,
		teaReasonLocalIO: 1, teaReasonInterrupted: 1,
		// the reasons only a manifest row carries never end a run
		teaReasonNoUsableChecksum: 1, teaReasonChecksumMismatch: 1, teaReasonNoAcceptableFormat: 1,
		teaReasonArtifactBytes: 1, teaReasonTotalBytes: 1,
	} {
		if got := reason.exit(); got != want {
			t.Errorf("%s exits with %d, want %d", reason, got, want)
		}
	}
	if teaExitOK != 0 || teaExitFailed != 1 || teaExitUsage != 2 || teaExitNotResolved != 3 || teaExitRefusedURL != 5 ||
		teaExitAuth != 6 || teaExitPartial != 7 || teaExitAddressRefused != 8 || teaExitLimit != 9 || teaExitConnect != 10 {
		t.Error("an exit code changed")
	}
}

func TestTeaMachineRedactURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://u:p@host.example.com:8443/a%20b/c?q=1&token=x#frag": "https://host.example.com:8443/a%20b/c",
		"http://host.example.com/":                                   "http://host.example.com/",
		"https://host.example.com":                                   "https://host.example.com",
		"https://host.example.com/a%2Fb?x":                           "https://host.example.com/a%2Fb",
		"https://[2001:db8::1]:8443/p":                               "https://[2001:db8::1]:8443/p",
		"HTTPS://Host.Example.com/p":                                 "https://Host.Example.com/p",
	} {
		got := teaRedactURL(in)
		if got == nil || *got != want {
			t.Errorf("teaRedactURL(%q) = %v, want %q", in, got, want)
		}
	}
	for _, in := range []string{"", "not a url", "/relative/path", "://bad", "https:///nohost", "mailto:x@example.com"} {
		if got := teaRedactURL(in); got != nil {
			t.Errorf("teaRedactURL(%q) = %q, want nil", in, *got)
		}
	}
}

func TestTeaMachineErrorURL(t *testing.T) {
	last := "https://last.example.com/tea/v1.0.0/product/x?secret=1"
	wantLast := "https://last.example.com/tea/v1.0.0/product/x"
	str := func(p *string) string {
		if p == nil {
			return "<nil>"
		}
		return *p
	}
	// the URL an error carries comes first, whatever the reason
	policy := &tea.URLPolicyError{URL: "https://policy.example.com/p?q=1", Reason: "r"}
	if got := str(teaErrorURL(policy, teaReasonURLRefused, last)); got != "https://policy.example.com/p" {
		t.Errorf("policy error: %s", got)
	}
	if got := str(teaErrorURL(fmt.Errorf("w: %w", policy), teaReasonUsage, "")); got != "https://policy.example.com/p" {
		t.Errorf("policy error, usage: %s", got)
	}
	ue := &url.Error{Op: "Get", URL: "https://u:p@urlerr.example.com/x?y=1#z", Err: errors.New("e")}
	if got := str(teaErrorURL(ue, teaReasonFailed, last)); got != "https://urlerr.example.com/x" {
		t.Errorf("url error: %s", got)
	}
	if got := str(teaErrorURL(ue, teaReasonDeadline, last)); got != "https://urlerr.example.com/x" {
		t.Errorf("url error, deadline: %s", got)
	}
	// otherwise the last request, unless the reason does not come from a request
	plain := errors.New("x")
	for _, r := range []teaReason{teaReasonUsage, teaReasonStringBytes, teaReasonOutputBytes, teaReasonFileCount, teaReasonLocalIO, teaReasonInterrupted, teaReasonDeadline} {
		if got := str(teaErrorURL(plain, r, last)); got != "<nil>" {
			t.Errorf("%s carries the URL %s", r, got)
		}
	}
	for _, r := range []teaReason{teaReasonFailed, teaReasonServerError, teaReasonInvalidResponse, teaReasonTLSCertificate, teaReasonNotFound,
		teaReasonURLRefused, teaReasonRedirectLimit, teaReasonAuth, teaReasonAddressRefused, teaReasonRequestTimeout,
		teaReasonDocumentBytes, teaReasonDiscoveryResults, teaReasonConnectFailed, teaReasonChecksumMismatch, teaReasonArtifactBytes} {
		if got := str(teaErrorURL(plain, r, last)); got != wantLast {
			t.Errorf("%s: URL %s, want %s", r, got, wantLast)
		}
		if got := str(teaErrorURL(plain, r, "")); got != "<nil>" {
			t.Errorf("%s with no request: %s", r, got)
		}
	}
}

func TestTeaMachineErrorText(t *testing.T) {
	plain := errors.New("the cause")
	u := "https://h.example.com/p"
	refused := &teaAddressRefusedError{Address: netip.MustParseAddr("169.254.169.254"), Class: teaAddressCloudMetadata, Host: "h"}
	for _, c := range []struct {
		name   string
		err    error
		reason teaReason
		url    *string
		want   string
	}{
		{"usage", teaUsage("%s is wrong", "--x"), teaReasonUsage, nil, "--x is wrong"},
		{"usage with a cause", &teaUsageError{msg: "short", err: plain}, teaReasonUsage, nil, "short"},
		{"refusal with a url", refused, teaReasonAddressRefused, &u, "refused to connect to 169.254.169.254 (cloud metadata) for https://h.example.com/p"},
		{"refusal without a url", refused, teaReasonAddressRefused, nil, "refused to connect to 169.254.169.254 (cloud metadata)"},
		{"deadline", plain, teaReasonDeadline, nil, "the run deadline (--deadline) was reached: the cause"},
		{"interrupt", plain, teaReasonInterrupted, nil, "interrupted: the cause"},
		{"other", plain, teaReasonFailed, &u, "the cause"},
	} {
		if got := teaErrorText(c.err, c.reason, c.url); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
	if errTeaDeadline.Error() != "the run deadline (--deadline) was reached" || errTeaNoResult.Error() != "discovery returned no result" {
		t.Error("a fixed error text changed")
	}
}

func TestTeaMachineErrorObject(t *testing.T) {
	live := context.Background()
	encode := func(d teaErrorDoc) map[string]any {
		b, err := teaEncode(d)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), `"ignoredProxyVariables":[`) || strings.Contains(string(b), `"ignoredProxyVariables":null`) {
			t.Errorf("ignoredProxyVariables is not a list: %s", b)
		}
		return m
	}
	// a plain failure: every optional key is null, the list is empty
	d := newTeaErrorDoc(live, errors.New("odd"), "", []string{"HTTPS_PROXY"})
	m := encode(d)
	if m["reason"] != "FAILED" || m["exitCode"] != 1.0 || m["kind"] != "TEA_ERROR" || m["formatVersion"] != 1.0 || m["message"] != "odd" {
		t.Errorf("plain failure %v", m)
	}
	for _, k := range []string{"url", "httpStatus", "address", "addressClass", "phase", "hint"} {
		if m[k] != nil {
			t.Errorf("%s = %v", k, m[k])
		}
	}
	if l, _ := m["ignoredProxyVariables"].([]any); len(l) != 0 {
		t.Errorf("proxy variables %v on a failure that is not a connect failure", l)
	}
	// the message is made printable
	d = newTeaErrorDoc(live, errors.New("a"+"\x1b[31mred\x07 \u00e9 \xff"), "", nil)
	if d.Message != "a\\x1b[31mred\\a "+"\u00e9"+" \\xff" {
		t.Errorf("message %q", d.Message)
	}
	// an HTTP status
	d = newTeaErrorDoc(live, &tea.APIError{Operation: "op", Status: 503}, "https://h.example.com/p?q=1", nil)
	if d.HTTPStatus == nil || *d.HTTPStatus != 503 || d.URL == nil || *d.URL != "https://h.example.com/p" {
		t.Errorf("api error %+v", d)
	}
	// a connect failure names its phase, the hint and the proxy variables
	conn := &teaConnectError{Phase: teaPhaseResolve, Host: "h", Err: errors.New("no")}
	d = newTeaErrorDoc(live, conn, "", []string{"HTTP_PROXY", "all_proxy"})
	m = encode(d)
	if m["phase"] != "RESOLVE" || m["hint"] != teaHintConnect || m["exitCode"] != 10.0 || m["address"] != nil || m["addressClass"] != nil {
		t.Errorf("connect failure %v", m)
	}
	if l, _ := m["ignoredProxyVariables"].([]any); len(l) != 2 || l[0] != "HTTP_PROXY" || l[1] != "all_proxy" {
		t.Errorf("proxy variables %v", l)
	}
	d = newTeaErrorDoc(live, &teaConnectError{Phase: teaPhaseConnect, Host: "h", Err: errors.New("no")}, "", nil)
	if d.Phase == nil || *d.Phase != "CONNECT" || d.Hint == nil {
		t.Errorf("connect phase %+v", d)
	}
	// a refusal, the hint following the class; the proxy list stays empty
	for class, hint := range map[teaAddressClass]string{
		teaAddressLoopback: teaHintThisHost, teaAddressOwn: teaHintThisHost,
		teaAddressLinkLocal: teaHintNever, teaAddressUnspecified: teaHintNever, teaAddressCloudMetadata: teaHintNever,
	} {
		e := &teaAddressRefusedError{Address: netip.MustParseAddr("10.1.2.3"), Class: class, Host: "h"}
		d = newTeaErrorDoc(live, e, "https://h.example.com/p", []string{"HTTP_PROXY"})
		m = encode(d)
		if m["hint"] != hint || m["address"] != "10.1.2.3" || m["addressClass"] != string(class) || m["phase"] != nil || m["exitCode"] != 8.0 {
			t.Errorf("%s: %v", class, m)
		}
		if l, _ := m["ignoredProxyVariables"].([]any); len(l) != 0 {
			t.Errorf("%s: proxy variables %v", class, l)
		}
	}
	// a run that ended by its deadline reports the deadline, never the address or the phase
	done, stop := context.WithCancelCause(context.Background())
	stop(errTeaDeadline)
	e := &teaAddressRefusedError{Address: netip.MustParseAddr("127.0.0.1"), Class: teaAddressLoopback, Host: "h"}
	d = newTeaErrorDoc(done, e, "https://h.example.com/p", nil)
	if d.Reason != teaReasonDeadline || d.Address != nil || d.AddressClass != nil || d.Hint != nil || d.URL != nil || d.ExitCode != 9 {
		t.Errorf("deadline over a refusal: %+v", d)
	}
	d = newTeaErrorDoc(done, conn, "", []string{"HTTP_PROXY"})
	if d.Reason != teaReasonDeadline || d.Phase != nil || d.Hint != nil || len(d.IgnoredProxyVariables) != 0 {
		t.Errorf("deadline over a connect failure: %+v", d)
	}
	if d.Message != "the run deadline (--deadline) was reached: could not resolve h: no" {
		t.Errorf("deadline message %q", d.Message)
	}
}

func TestTeaMachineEncodeIsPrintableASCII(t *testing.T) {
	for in, want := range map[string]string{
		"plain ~ text":     `"plain ~ text"`,
		"\u00e9":           `"\u00e9"`,
		"\u00ff\u0100":     `"\u00ff\u0100"`,
		"\u007f":           `"\u007f"`,
		"\u0080":           `"\u0080"`,
		"\uffff":           `"\uffff"`,
		"\ud7ff\ue000":     `"\ud7ff\ue000"`,
		"\U00010000":       `"\ud800\udc00"`,
		"\U0001f600":       `"\ud83d\ude00"`,
		"\U0010ffff":       `"\udbff\udfff"`,
		"<&>":              `"\u003c\u0026\u003e"`,
		"\u2028\u2029":     `"\u2028\u2029"`,
		"tab\tnl\n\x01":    `"tab\tnl\n\u0001"`,
		"quote\" back\\":   `"quote\" back\\"`,
		"bad \xff utf-8":   `"bad \ufffd utf-8"`,
		"":                 `""`,
		"\u00e9\U0001f600": `"\u00e9\ud83d\ude00"`,
	} {
		b, err := teaEncode(in)
		if err != nil {
			t.Fatal(err)
		}
		got := string(b)
		if got != want {
			t.Errorf("%q encodes to %s, want %s", in, got, want)
		}
		for i := 0; i < len(b); i++ {
			if b[i] < 0x20 || b[i] > 0x7e {
				t.Errorf("%q: byte %d of %s is 0x%02x", in, i, got, b[i])
			}
		}
		var back string
		if err := json.Unmarshal(b, &back); err != nil {
			t.Errorf("%s does not decode: %v", got, err)
		}
	}
	if _, err := teaEncode(make(chan int)); err == nil {
		t.Error("a value that cannot be encoded was encoded")
	}
}

func TestTeaMachineRowMessage(t *testing.T) {
	for in, want := range map[string]string{
		"plain":                 "plain",
		"caf\u00e9":             `caf\u00e9`,
		"\U0001f600 and \uffff": `\ud83d\ude00 and \uffff`,
		"\U00010000":            `\ud800\udc00`,
		"a\x1bb":                `a\x1bb`,
		"tab\there":             `tab\there`,
		"del\x7f":               `del\x7f`,
		"bad \xff":              `bad \xff`,
		"\u202e":                `\u202e`,
		"~":                     "~",
		"":                      "",
		"\u00e9\u00e9\u00e9":    `\u00e9\u00e9\u00e9`,
	} {
		if got := teaRowMessage(in); got != want {
			t.Errorf("teaRowMessage(%q) = %q, want %q", in, got, want)
		}
	}
	// the bound is on the bytes of the escaped text
	e := "\u00e9"
	esc := `\u00e9`
	for _, c := range []struct {
		in   string
		want string
	}{
		{strings.Repeat("a", 1023), strings.Repeat("a", 1023)},
		{strings.Repeat("a", 1024), strings.Repeat("a", 1024)},
		{strings.Repeat("a", 1025), strings.Repeat("a", 1021) + "..."},
		{strings.Repeat("a", 5000), strings.Repeat("a", 1021) + "..."},
		{strings.Repeat(e, 170) + "ab", strings.Repeat(esc, 170) + "ab"},
		// the cut is on bytes, so it can fall inside an escape
		{strings.Repeat(e, 171), strings.Repeat(esc, 170) + `\...`},
	} {
		got := teaRowMessage(c.in)
		if got != c.want || len(got) > 1024 {
			t.Errorf("teaRowMessage of %d bytes = %d bytes %.30q, want %d bytes", len(c.in), len(got), got, len(c.want))
		}
	}
	// the text goes through teaText first
	long := strings.Repeat("x", teaTextMax+50)
	if got := teaRowMessage(long); got != strings.Repeat("x", 1021)+"..." {
		t.Errorf("a long text: %d bytes", len(got))
	}
}

func TestTeaMachineResponseMediaType(t *testing.T) {
	for _, c := range []struct {
		in string
		ok bool
	}{
		{"", false}, {"application/json", true}, {"text/plain; charset=utf-8", false}, {"text/plain;charset=utf-8", true},
		{"a b", false}, {"a\tb", false}, {"a\x7fb", false}, {"a\x1fb", false}, {"a\u00e9b", false}, {"!", true}, {"~", true},
		{" ", false}, {"\x21\x7e", true}, {strings.Repeat("a", 255), true}, {strings.Repeat("a", 256), false},
	} {
		got := teaResponseMediaType(c.in)
		if (got != nil) != c.ok || (got != nil && *got != c.in) {
			t.Errorf("teaResponseMediaType(%q) = %v, want ok=%v", c.in, got, c.ok)
		}
	}
}

func TestTeaMachineStringBoundIsFirstInBytes(t *testing.T) {
	c := &teaStrings{max: 10}
	if got := c.s("a", strings.Repeat("x", 10)); len(got) != 10 || c.err != nil {
		t.Fatalf("a string of exactly the bound: %v", c.err)
	}
	if c.s("b", strings.Repeat("\u00e9", 6)); c.err == nil {
		t.Fatal("twelve bytes in six runes passed a bound of ten")
	}
	first := c.err
	c.s("c", strings.Repeat("x", 99))
	c.p("d", strPtr(strings.Repeat("x", 99)))
	if c.err != first {
		t.Errorf("a later string replaced the first error: %v", c.err)
	}
	var le *teaLimitError
	if !errors.As(c.err, &le) || le.reason != teaReasonStringBytes ||
		c.err.Error() != "the server string at b is longer than --max-string-bytes (10 bytes)" {
		t.Errorf("the error: %v", c.err)
	}
	c = &teaStrings{max: 10}
	if got := c.p("p", nil); got != nil || c.err != nil {
		t.Errorf("a nil string: %v %v", got, c.err)
	}
	v := "short"
	if got := c.p("p", &v); got == nil || *got != "short" || got == &v && false {
		t.Errorf("a short string: %v", got)
	}
}

func strPtr(s string) *string { return &s }

// teaWantStringPath fails unless build leaves c with a string-bound error at path.
func teaWantStringPath(t *testing.T, name, path string, build func(c *teaStrings)) {
	t.Helper()
	c := &teaStrings{max: 20}
	build(c)
	want := "the server string at " + path + " is longer than --max-string-bytes (20 bytes)"
	if c.err == nil || c.err.Error() != want {
		t.Errorf("%s: error %v, want %q", name, c.err, want)
	}
}

func teaLong() string { return strings.Repeat("L", 21) }

func teaFullCollection() teaapi.Collection {
	s := func(v string) *string { return &v }
	sums := func() *[]teaapi.Checksum {
		return &[]teaapi.Checksum{{AlgType: "SHA-256", AlgValue: "aa"}, {AlgType: "SHA-1", AlgValue: "bb"}}
	}
	format := func() teaapi.ArtifactFormat {
		return teaapi.ArtifactFormat{MediaType: "a/b", Description: s("d"), Url: s("u"), SignatureUrl: s("g"), Checksums: sums()}
	}
	art := func() teaapi.Artifact {
		return teaapi.Artifact{Uuid: uuid.MustParse(teaArtProd), Version: 1, Name: s("n"), Type: "BOM",
			CreatedDate: time.Unix(0, 0), Formats: []teaapi.ArtifactFormat{format(), format()}}
	}
	return teaapi.Collection{Uuid: uuid.MustParse(teaPR1), Version: 1, CreatedDate: time.Unix(0, 0), BelongsTo: "PRODUCT_RELEASE",
		UpdateReason: teaapi.CollectionUpdateReason{Type: "INITIAL_RELEASE", Comment: s("c")}, Artifacts: []teaapi.Artifact{art(), art()}}
}

func TestTeaMachineStringBoundNamesTheCollectionField(t *testing.T) {
	long := teaLong()
	for _, c := range []struct {
		path string
		mod  func(col *teaapi.Collection)
	}{
		{"x.belongsTo", func(col *teaapi.Collection) { col.BelongsTo = teaapi.CollectionBelongsToType(long) }},
		{"x.updateReason.type", func(col *teaapi.Collection) { col.UpdateReason.Type = teaapi.CollectionUpdateReasonType(long) }},
		{"x.updateReason.comment", func(col *teaapi.Collection) { col.UpdateReason.Comment = &long }},
		{"x.artifacts[1].name", func(col *teaapi.Collection) { col.Artifacts[1].Name = &long }},
		{"x.artifacts[1].type", func(col *teaapi.Collection) { col.Artifacts[1].Type = teaapi.ArtifactType(long) }},
		{"x.artifacts[1].formats[1].mediaType", func(col *teaapi.Collection) { col.Artifacts[1].Formats[1].MediaType = long }},
		{"x.artifacts[1].formats[1].description", func(col *teaapi.Collection) { col.Artifacts[1].Formats[1].Description = &long }},
		{"x.artifacts[1].formats[1].externalUrl", func(col *teaapi.Collection) { col.Artifacts[1].Formats[1].Url = &long }},
		{"x.artifacts[1].formats[1].signatureUrl", func(col *teaapi.Collection) { col.Artifacts[1].Formats[1].SignatureUrl = &long }},
		{"x.artifacts[1].formats[1].checksums[1].algType", func(col *teaapi.Collection) {
			(*col.Artifacts[1].Formats[1].Checksums)[1].AlgType = teaapi.ChecksumType(long)
		}},
		{"x.artifacts[1].formats[1].checksums[1].algValue", func(col *teaapi.Collection) {
			(*col.Artifacts[1].Formats[1].Checksums)[1].AlgValue = long
		}},
	} {
		col := teaFullCollection()
		c.mod(&col)
		teaWantStringPath(t, c.path, c.path, func(sc *teaStrings) { sc.collection("x", col) })
	}
	// nothing too long: no error, and the shapes are lists even when empty
	sc := &teaStrings{max: 20}
	out := sc.collection("x", teaFullCollection())
	if sc.err != nil || len(out.Artifacts) != 2 || len(out.Artifacts[1].Formats) != 2 || out.Artifacts[1].Formats[1].Checksums[1].AlgValue != "bb" {
		t.Errorf("a full collection: %v %+v", sc.err, out)
	}
	if out.Artifacts[1].Formats[1].ExternalURL == nil || *out.Artifacts[1].Formats[1].ExternalURL != "u" || *out.Artifacts[1].Formats[1].SignatureURL != "g" ||
		*out.Artifacts[1].Formats[1].Description != "d" || *out.Artifacts[1].Name != "n" || out.UpdateReason.Comment == nil || *out.UpdateReason.Comment != "c" {
		t.Errorf("optional fields were lost: %+v", out)
	}
	col := teaFullCollection()
	col.Artifacts = []teaapi.Artifact{{Uuid: uuid.MustParse(teaArtProd), Version: 4, Type: "BOM", CreatedDate: time.Unix(0, 0), Formats: []teaapi.ArtifactFormat{{MediaType: "a/b"}}}}
	out = (&teaStrings{max: 20}).collection("x", col)
	b, _ := teaEncode(out)
	if !strings.Contains(string(b), `"artifacts":[{"uuid":"`+teaArtProd+`","version":4,"name":null,"type":"BOM"`) ||
		!strings.Contains(string(b), `"formats":[{"mediaType":"a/b","description":null,"externalUrl":null,"signatureUrl":null,"checksums":[]}]`) {
		t.Errorf("an artifact without optional fields: %s", b)
	}
}

func TestTeaMachineStringBoundNamesTheLifecycleField(t *testing.T) {
	long := teaLong()
	s := func(v string) *string { return &v }
	full := func() teaapi.Cle {
		ev := func() teaapi.CleEvent {
			return teaapi.CleEvent{Id: 1, Type: "released", Effective: time.Unix(0, 0), Published: time.Unix(0, 0),
				Version: s("v"), Versions: &[]teaapi.CleVersionSpecifier{{Version: s("1")}, {Range: s("r")}}, License: s("l"),
				SupportId: s("sid"), SupersededByVersion: s("sv"), Description: s("d"), Reason: s("r"),
				References: &[]string{"r0", "r1"}, Identifiers: &[]teaapi.Identifier{{IdType: "PURL", IdValue: "a"}, {IdType: "CPE", IdValue: "b"}}, EventId: new(int)}
		}
		return teaapi.Cle{Events: []teaapi.CleEvent{ev(), ev()}, Definitions: &teaapi.CleDefinitions{Support: &[]teaapi.CleSupportDefinition{
			{Id: "a", Description: "d", Url: s("u")}, {Id: "b", Description: "d", Url: s("u")}}}}
	}
	for _, c := range []struct {
		path string
		mod  func(cle *teaapi.Cle)
	}{
		{"x.events[1].type", func(cle *teaapi.Cle) { cle.Events[1].Type = teaapi.CleEventType(long) }},
		{"x.events[1].version", func(cle *teaapi.Cle) { cle.Events[1].Version = &long }},
		{"x.events[1].license", func(cle *teaapi.Cle) { cle.Events[1].License = &long }},
		{"x.events[1].supportId", func(cle *teaapi.Cle) { cle.Events[1].SupportId = &long }},
		{"x.events[1].supersededByVersion", func(cle *teaapi.Cle) { cle.Events[1].SupersededByVersion = &long }},
		{"x.events[1].description", func(cle *teaapi.Cle) { cle.Events[1].Description = &long }},
		{"x.events[1].reason", func(cle *teaapi.Cle) { cle.Events[1].Reason = &long }},
		{"x.events[1].identifiers[1].idType", func(cle *teaapi.Cle) { (*cle.Events[1].Identifiers)[1].IdType = teaapi.IdentifierType(long) }},
		{"x.events[1].identifiers[1].idValue", func(cle *teaapi.Cle) { (*cle.Events[1].Identifiers)[1].IdValue = long }},
		{"x.events[1].versions[1].version", func(cle *teaapi.Cle) { (*cle.Events[1].Versions)[1].Version = &long }},
		{"x.events[1].versions[1].range", func(cle *teaapi.Cle) { (*cle.Events[1].Versions)[1].Range = &long }},
		{"x.events[1].references[1]", func(cle *teaapi.Cle) { (*cle.Events[1].References)[1] = long }},
		{"x.supportDefinitions[1].id", func(cle *teaapi.Cle) { (*cle.Definitions.Support)[1].Id = long }},
		{"x.supportDefinitions[1].description", func(cle *teaapi.Cle) { (*cle.Definitions.Support)[1].Description = long }},
		{"x.supportDefinitions[1].url", func(cle *teaapi.Cle) { (*cle.Definitions.Support)[1].Url = &long }},
	} {
		cle := full()
		c.mod(&cle)
		teaWantStringPath(t, c.path, c.path, func(sc *teaStrings) { sc.cle("x", &cle) })
	}
	sc := &teaStrings{max: 20}
	cle := full()
	out := sc.cle("x", &cle)
	if sc.err != nil || out.Status != teaCLERead || len(out.Events) != 2 || len(out.SupportDefinitions) != 2 {
		t.Errorf("a full lifecycle: %v %+v", sc.err, out)
	}
	if ev := out.Events[1]; ev.SupportID == nil || *ev.SupportID != "sid" || ev.SupersededByVersion == nil || *ev.SupersededByVersion != "sv" ||
		ev.Description == nil || *ev.Description != "d" || ev.Reason == nil || *ev.Reason != "r" || ev.License == nil || ev.EventID == nil ||
		len(ev.References) != 2 || len(ev.Identifiers) != 2 || len(ev.Versions) != 2 || *ev.Versions[1].Range != "r" || *ev.Versions[0].Version != "1" {
		t.Errorf("optional lifecycle fields were lost: %+v", ev)
	}
	if sd := out.SupportDefinitions[1]; sd.ID != "b" || sd.Description != "d" || sd.URL == nil || *sd.URL != "u" {
		t.Errorf("support definition %+v", sd)
	}
	// a lifecycle with no definitions, or definitions with no support list, has none
	for _, cle := range []teaapi.Cle{{Events: nil}, {Events: nil, Definitions: &teaapi.CleDefinitions{}}} {
		out = (&teaStrings{max: 20}).cle("x", &cle)
		if out.Status != teaCLERead || len(out.SupportDefinitions) != 0 || out.SupportDefinitions == nil || out.Events == nil {
			t.Errorf("empty lifecycle %+v", out)
		}
	}
	out = (&teaStrings{max: 20}).cle("x", nil)
	if out.Status != teaCLENotAvailable || out.Events == nil || out.SupportDefinitions == nil || len(out.Events) != 0 {
		t.Errorf("no lifecycle %+v", out)
	}
	// an event with none of the optional fields keeps null and empty-list shapes
	bare := teaapi.Cle{Events: []teaapi.CleEvent{{Id: 3, Type: "endOfLife", Effective: time.Unix(0, 0), Published: time.Unix(0, 0)}}}
	b, _ := teaEncode((&teaStrings{max: 20}).cle("x", &bare))
	if !strings.Contains(string(b), `"version":null,"versions":[],"license":null,"supportId":null,"supersededByVersion":null,"description":null,"reason":null,"references":[],"identifiers":[],"eventId":null`) {
		t.Errorf("a bare event: %s", b)
	}
}

func TestTeaMachineStringBoundNamesTheSharedField(t *testing.T) {
	long := teaLong()
	teaWantStringPath(t, "identifiers", "x[1].idType", func(sc *teaStrings) {
		sc.identifiers("x", []teaapi.Identifier{{IdType: "PURL", IdValue: "a"}, {IdType: teaapi.IdentifierType(long), IdValue: "b"}})
	})
	teaWantStringPath(t, "identifiers", "x[1].idValue", func(sc *teaStrings) {
		sc.identifiers("x", []teaapi.Identifier{{IdType: "PURL", IdValue: "a"}, {IdType: "CPE", IdValue: long}})
	})
	teaWantStringPath(t, "identifiersP", "x[0].idValue", func(sc *teaStrings) {
		sc.identifiersP("x", &[]teaapi.Identifier{{IdType: "PURL", IdValue: long}})
	})
	teaWantStringPath(t, "checksums", "x[1].algType", func(sc *teaStrings) {
		sc.checksums("x", &[]teaapi.Checksum{{AlgType: "SHA-1", AlgValue: "a"}, {AlgType: teaapi.ChecksumType(long), AlgValue: "b"}})
	})
	teaWantStringPath(t, "checksums", "x[1].algValue", func(sc *teaStrings) {
		sc.checksums("x", &[]teaapi.Checksum{{AlgType: "SHA-1", AlgValue: "a"}, {AlgType: "SHA-1", AlgValue: long}})
	})
	teaWantStringPath(t, "updateReason type", "r.type", func(sc *teaStrings) {
		sc.updateReason("r", teaapi.CollectionUpdateReason{Type: teaapi.CollectionUpdateReasonType(long)})
	})
	teaWantStringPath(t, "updateReason comment", "r.comment", func(sc *teaStrings) {
		sc.updateReason("r", teaapi.CollectionUpdateReason{Type: "X", Comment: &long})
	})
	sc := &teaStrings{max: 20}
	if got := sc.identifiersP("x", nil); got == nil || len(got) != 0 {
		t.Errorf("no identifiers: %#v", got)
	}
	if got := sc.checksums("x", nil); got == nil || len(got) != 0 {
		t.Errorf("no checksums: %#v", got)
	}
	if got := sc.identifiers("x", nil); got == nil || len(got) != 0 {
		t.Errorf("empty identifiers: %#v", got)
	}
}
