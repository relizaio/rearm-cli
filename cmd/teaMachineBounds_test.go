package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

// Mutation round for the machine mode: the ranges, defaults and messages of the flags, one
// value on each side of every bound (docs/tea.md, "Limits"), and the validation of every
// flag value. The rig is teaMachineHarness_test.go.

const teaBoundsRoot = "https://tea.example.com/tea"

// teaFlagsOf parses args with the real flag set of verb.
func teaFlagsOf(t *testing.T, verb teaMachineVerb, args ...string) teaMachineInputs {
	t.Helper()
	c := &cobra.Command{Use: string(verb)}
	addTeaMachineFlags(c, verb)
	if err := c.Flags().Parse(args); err != nil {
		t.Fatalf("parsing %v: %v", args, err)
	}
	return teaMachineInputsFromFlags(c, verb, c.Flags().Args())
}

// teaRequired are the arguments that make a run of verb valid; the tests append to them (a
// later occurrence of a flag wins).
func teaRequired(t *testing.T, verb teaMachineVerb) []string {
	t.Helper()
	switch verb {
	case teaVerbResolve:
		return []string{"--tei", "tei://tea.example.com/uuid/" + teaPR1}
	case teaVerbProduct:
		return []string{"--api-root", teaBoundsRoot, "--uuid", teaProduct}
	case teaVerbComponent:
		return []string{"--api-root", teaBoundsRoot, "--uuid", teaComp1}
	case teaVerbReleases:
		return []string{"--api-root", teaBoundsRoot, "--product", teaProduct}
	case teaVerbRelease:
		return []string{"--api-root", teaBoundsRoot, "--product-release", teaPR1}
	}
	return []string{"--api-root", teaBoundsRoot, "--product-release", teaPR1, "--collection-version", "1",
		"--dir", filepath.Join(t.TempDir(), "out")}
}

func teaValidateArgs(t *testing.T, verb teaMachineVerb, extra ...string) (teaMachineTarget, error) {
	t.Helper()
	args := append(teaRequired(t, verb), extra...)
	return validateTeaMachine(teaFlagsOf(t, verb, args...))
}

// teaWantUsage fails unless err is a usage error whose message is exactly msg.
func teaWantUsage(t *testing.T, what string, err error, msg string) {
	t.Helper()
	var u *teaUsageError
	if !errors.As(err, &u) {
		t.Errorf("%s: error %v, want the usage error %q", what, err, msg)
		return
	}
	if u.msg != msg {
		t.Errorf("%s: message %q, want %q", what, u.msg, msg)
	}
}

func TestTeaMachineLimitRanges(t *testing.T) {
	const day = int64(24 * time.Hour)
	ns := func(v int64) string { return strconv.FormatInt(v, 10) + "ns" }
	i := func(v int64) string { return strconv.FormatInt(v, 10) }
	cases := []struct {
		verb   teaMachineVerb
		flag   string
		lo, hi int64
		val    func(int64) string
		text   string // the range as the message writes it
	}{
		{teaVerbProduct, "connect-timeout", int64(time.Second), int64(time.Minute), ns, "1s to 1m0s"},
		{teaVerbProduct, "deadline", int64(time.Second), day, ns, "1s to 24h0m0s"},
		{teaVerbProduct, "max-document-bytes", 1 << 10, 256 << 20, i, "1024 to 268435456"},
		{teaVerbProduct, "max-redirects", 1, 20, i, "1 to 20"},
		{teaVerbProduct, "max-string-bytes", 256, 1 << 20, i, "256 to 1048576"},
		{teaVerbProduct, "max-output-bytes", 64 << 10, 1 << 30, i, "65536 to 1073741824"},
		{teaVerbDownload, "max-artifact-bytes", 1, 16 << 30, i, "1 to 17179869184"},
		{teaVerbDownload, "max-total-bytes", 1, 64 << 30, i, "1 to 68719476736"},
		{teaVerbDownload, "max-files", 1, 100000, i, "1 to 100000"},
		{teaVerbReleases, "page-size", 1, 100, i, "1 to 100"},
		{teaVerbReleases, "max-pages", 1, 1000, i, "1 to 1000"},
	}
	for _, c := range cases {
		for _, v := range []struct {
			n  int64
			ok bool
		}{{c.lo - 1, false}, {c.lo, true}, {c.hi, true}, {c.hi + 1, false}} {
			_, err := teaValidateArgs(t, c.verb, "--"+c.flag, c.val(v.n))
			if v.ok {
				if err != nil {
					t.Errorf("--%s %d: %v, want it accepted", c.flag, v.n, err)
				}
				continue
			}
			teaWantUsage(t, "--"+c.flag+" "+i(v.n), err, "--"+c.flag+" is out of range: it must be from "+c.text)
		}
	}
}

func TestTeaMachineFlagDefaults(t *testing.T) {
	want := map[string]string{
		"connect-timeout": "5s", "deadline": "10m0s", "max-document-bytes": "16777216", "max-redirects": "5",
		"max-string-bytes": "8192", "max-output-bytes": "33554432", "max-artifact-bytes": "104857600",
		"max-total-bytes": "1073741824", "max-files": "200", "page-size": "100", "max-pages": "50", "useport": "443",
	}
	seen := map[string]bool{}
	for _, verb := range []teaMachineVerb{teaVerbResolve, teaVerbProduct, teaVerbComponent, teaVerbReleases, teaVerbRelease, teaVerbDownload} {
		c := &cobra.Command{Use: string(verb)}
		addTeaMachineFlags(c, verb)
		for name, def := range want {
			if f := c.Flags().Lookup(name); f != nil {
				seen[name] = true
				if f.DefValue != def {
					t.Errorf("%s --%s defaults to %s, want %s", verb, name, f.DefValue, def)
				}
			}
		}
		in := teaFlagsOf(t, verb)
		if in.connectTimeout != 5*time.Second || in.deadline != 10*time.Minute || in.maxDocumentBytes != 16<<20 ||
			in.maxRedirects != 5 || in.maxStringBytes != 8192 || in.maxOutputBytes != 32<<20 ||
			in.maxArtifactBytes != 100<<20 || in.maxTotalBytes != 1<<30 || in.maxFiles != 200 ||
			in.pageSize != 100 || in.maxPages != 50 || in.usePort != 443 || in.portSet {
			t.Errorf("%s: defaults %+v", verb, in)
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("no verb has --%s", name)
		}
	}
	if d := teaMachineInputsDefault(teaVerbProduct); d.usePort != 443 || d.pageSize != 100 || d.maxPages != 50 ||
		d.connectTimeout != 5*time.Second || d.deadline != 10*time.Minute || d.maxDocumentBytes != 16<<20 ||
		d.maxRedirects != 5 || d.maxStringBytes != 8192 || d.maxOutputBytes != 32<<20 ||
		d.maxArtifactBytes != 100<<20 || d.maxTotalBytes != 1<<30 || d.maxFiles != 200 {
		t.Errorf("teaMachineInputsDefault %+v", d)
	}
}

func TestTeaMachineUsePortRange(t *testing.T) {
	for _, c := range []struct {
		port string
		ok   bool
	}{{"0", false}, {"1", true}, {"443", true}, {"65535", true}, {"65536", false}} {
		_, err := teaValidateArgs(t, teaVerbResolve, "--useport", c.port)
		if c.ok != (err == nil) {
			t.Errorf("--useport %s: %v", c.port, err)
		}
		if !c.ok {
			teaWantUsage(t, "--useport "+c.port, err, "--useport is out of range: a port is 1 to 65535")
		}
	}
	// without --useport the default is never checked
	if _, err := teaValidateArgs(t, teaVerbResolve); err != nil {
		t.Errorf("no --useport: %v", err)
	}
}

func TestTeaMachineResolveFlagMessages(t *testing.T) {
	_, err := validateTeaMachine(teaFlagsOf(t, teaVerbResolve))
	teaWantUsage(t, "no --tei", err, "--tei is required")
	_, err = teaValidateArgs(t, teaVerbResolve, "--tei", "urn:tei:uuid:example.com:abc")
	teaWantUsage(t, "urn form", err, "the urn:tei: form is not accepted; write the TEI as tei://<domain>/<type>/<id>")
	_, err = teaValidateArgs(t, teaVerbResolve, "--tei", "tei://x")
	var u *teaUsageError
	if !errors.As(err, &u) || !strings.HasPrefix(u.msg, "invalid TEI: ") || errors.Unwrap(u) == nil {
		t.Errorf("a TEI that does not parse: %v", err)
	}
	if _, err = teaValidateArgs(t, teaVerbProduct, "stray"); err == nil {
		t.Error("a positional argument was accepted")
	} else {
		teaWantUsage(t, "positional", err, "tea machine product takes no positional arguments")
	}
}

func TestTeaMachineAPIRootMessages(t *testing.T) {
	for _, c := range []struct {
		root, flag, msg string
	}{
		{"", "", "--api-root is required"},
		{"https://user:pw@tea.example.com/tea", "", "--api-root must not carry a user name or password"},
		{"https://tea.example.com/tea?x=1", "", "--api-root must not carry a query or a fragment"},
		{"https://tea.example.com/tea?", "", "--api-root must not carry a query or a fragment"},
		{"https://tea.example.com/tea#f", "", "--api-root must not carry a query or a fragment"},
		{"https://tea.example.com/tea#", "", "--api-root must not carry a query or a fragment"},
		{"https://tea.example.com/tea/", "", "--api-root must not end with a slash"},
		{"http://tea.example.com/tea", "", "--api-root is plain http: give --usehttp for a local server"},
		{"HTTP://tea.example.com/tea", "", "--api-root is plain http: give --usehttp for a local server"},
		{"https://tea.example.com/tea", "--usehttp", "--usehttp allows plain http on the --api-root only: the root must then be http://"},
		{"http://[::1", "", "--api-root is not a valid URL"},
	} {
		args := []string{"--api-root", c.root}
		if c.flag != "" {
			args = append(args, c.flag)
		}
		_, err := teaValidateArgs(t, teaVerbProduct, args...)
		teaWantUsage(t, "root "+c.root, err, c.msg)
	}
	// a root that is not a TEA API root keeps the library's error
	_, err := teaValidateArgs(t, teaVerbProduct, "--api-root", "ftp://tea.example.com/tea")
	var u *teaUsageError
	if !errors.As(err, &u) || !strings.HasPrefix(u.msg, "--api-root is not a TEA API root") || errors.Unwrap(u) == nil {
		t.Errorf("ftp root: %v", err)
	}
	// plain http is accepted with --usehttp
	if _, err := teaValidateArgs(t, teaVerbProduct, "--api-root", "http://127.0.0.1:8080/tea", "--usehttp"); err != nil {
		t.Errorf("http root with --usehttp: %v", err)
	}
}

func TestTeaMachineUUIDFlagMessages(t *testing.T) {
	for _, c := range []struct {
		verb  teaMachineVerb
		flag  string
		extra []string
		msg   string
	}{
		{teaVerbProduct, "uuid", []string{"--uuid", ""}, "--uuid is required"},
		{teaVerbProduct, "uuid", []string{"--uuid", "not-a-uuid-secret"}, "--uuid is not a uuid"},
		{teaVerbComponent, "uuid", []string{"--uuid", "00000000-0000-0000-0000-000000000000"}, "--uuid is not a uuid"},
		{teaVerbReleases, "product", []string{"--product", "xx"}, "--product is not a uuid"},
		{teaVerbReleases, "component", []string{"--product", "", "--component", "00000000-0000-0000-0000-000000000000"}, "--component is not a uuid"},
		{teaVerbRelease, "product-release", []string{"--product-release", "xx"}, "--product-release is not a uuid"},
		{teaVerbRelease, "component-release", []string{"--product-release", "", "--component-release", "xx"}, "--component-release is not a uuid"},
		{teaVerbReleases, "", []string{"--component", teaComp1}, "--product and --component cannot be combined"},
		{teaVerbReleases, "", []string{"--product", "", "--component", ""}, "one of --product and --component is required"},
		{teaVerbRelease, "", []string{"--component-release", teaCR1}, "--product-release and --component-release cannot be combined"},
		{teaVerbRelease, "", []string{"--product-release", ""}, "one of --product-release and --component-release is required"},
	} {
		_, err := teaValidateArgs(t, c.verb, c.extra...)
		teaWantUsage(t, string(c.verb)+" "+strings.Join(c.extra, " "), err, c.msg)
	}
	// the kind follows the flag that was given
	tg, err := teaValidateArgs(t, teaVerbReleases, "--product", "", "--component", teaComp1)
	if err != nil || tg.parentKind != teaParentComponent || tg.id.String() != teaComp1 {
		t.Errorf("releases --component: %+v %v", tg, err)
	}
	tg, err = teaValidateArgs(t, teaVerbReleases)
	if err != nil || tg.parentKind != teaParentProduct || tg.id.String() != teaProduct {
		t.Errorf("releases --product: %+v %v", tg, err)
	}
	tg, err = teaValidateArgs(t, teaVerbRelease, "--product-release", "", "--component-release", teaCR1)
	if err != nil || tg.kind != teaReleaseKindComponent || tg.id.String() != teaCR1 {
		t.Errorf("release --component-release: %+v %v", tg, err)
	}
	tg, err = teaValidateArgs(t, teaVerbRelease)
	if err != nil || tg.kind != teaReleaseKindProduct || tg.id.String() != teaPR1 {
		t.Errorf("release --product-release: %+v %v", tg, err)
	}
	tg, err = teaValidateArgs(t, teaVerbDownload, "--product-release", "", "--component-release", teaCR1, "--collection-version", "7")
	if err != nil || tg.kind != teaReleaseKindComponent || tg.version != 7 {
		t.Errorf("download --component-release: %+v %v", tg, err)
	}
}

func TestTeaMachinePageTokenFlag(t *testing.T) {
	long := func(n int) string { return strings.Repeat("a", n) }
	for _, c := range []struct {
		args []string
		msg  string // "" for accepted
	}{
		{[]string{"--page-token="}, "--page-token must not be empty"},
		{[]string{"--page-token=a\x01b"}, "--page-token has a control character"},
		{[]string{"--page-token=a\x7fb"}, "--page-token has a control character"},
		{[]string{"--page-token=a\xffb"}, "--page-token is not valid UTF-8"},
		{[]string{"--page-token=" + long(257), "--max-string-bytes", "256"}, "--page-token is longer than --max-string-bytes (256 bytes)"},
		{[]string{"--page-token=" + long(256), "--max-string-bytes", "256"}, ""},
		{[]string{"--page-token=" + long(255), "--max-string-bytes", "256"}, ""},
		{[]string{"--page-token=" + long(9000), "--max-string-bytes", "20000"}, ""},
		{[]string{"--page-token=" + long(300)}, ""},
		{[]string{"--page-token=" + long(8193)}, "--page-token is longer than --max-string-bytes (8192 bytes)"},
		{[]string{"--page-token=tok/2+= x"}, ""},
	} {
		_, err := teaValidateArgs(t, teaVerbReleases, c.args...)
		if c.msg == "" {
			if err != nil {
				t.Errorf("%.40q: %v", c.args[0], err)
			}
			continue
		}
		teaWantUsage(t, c.args[0], err, c.msg)
	}
	// without the flag no token is checked and none is sent
	in := teaFlagsOf(t, teaVerbReleases, "--product", teaProduct)
	if in.pageTokenSet {
		t.Error("pageTokenSet without --page-token")
	}
}

func TestTeaMachineDownloadFlagMessages(t *testing.T) {
	_, err := validateTeaMachine(teaFlagsOf(t, teaVerbDownload, "--api-root", teaBoundsRoot, "--product-release", teaPR1,
		"--dir", filepath.Join(t.TempDir(), "o")))
	teaWantUsage(t, "no version", err, "--collection-version is required")
	_, err = teaValidateArgs(t, teaVerbDownload, "--collection-version", "0")
	teaWantUsage(t, "version 0", err, "--collection-version must be 1 or more")
	_, err = teaValidateArgs(t, teaVerbDownload, "--collection-version", "-3")
	teaWantUsage(t, "version -3", err, "--collection-version must be 1 or more")
	if tg, err := teaValidateArgs(t, teaVerbDownload, "--collection-version", "1"); err != nil || tg.version != 1 {
		t.Errorf("version 1: %+v %v", tg, err)
	}

	types := "ATTESTATION, BOM, BUILD_META, CERTIFICATION, FORMULATION, LICENSE, OTHER, RELEASE_NOTES, SECURITY_TXT, THREAT_MODEL, VULNERABILITIES"
	typeMsg := "an artifact type must be one of the TEA 1.0.0 types (" + types + ")"
	for _, name := range strings.Split(types, ", ") {
		for _, flag := range []string{"--artifact-type", "--exclude-artifact-type"} {
			if _, err := teaValidateArgs(t, teaVerbDownload, flag, name); err != nil {
				t.Errorf("%s %s: %v", flag, name, err)
			}
		}
	}
	for _, bad := range []string{"bom", "BO", "BOMS", "Bom", "x", "BUILDMETA"} {
		for _, flag := range []string{"--artifact-type", "--exclude-artifact-type"} {
			_, err := teaValidateArgs(t, teaVerbDownload, flag, bad)
			teaWantUsage(t, flag+" "+bad, err, typeMsg)
		}
	}
	_, err = teaValidateArgs(t, teaVerbDownload, "--artifact-type", "BOM", "--exclude-artifact-type", "OTHER")
	teaWantUsage(t, "both", err, "--artifact-type and --exclude-artifact-type cannot be combined")
	_, err = teaValidateArgs(t, teaVerbDownload, "--artifact-type", "BOM", "--artifact-type", "bogus")
	teaWantUsage(t, "second type", err, typeMsg)

	for _, ok := range []string{"application/json", "Application/JSON; charset=utf-8", " text/plain ", "application/vnd.cyclonedx+json"} {
		if _, err := teaValidateArgs(t, teaVerbDownload, "--media-type", ok); err != nil {
			t.Errorf("--media-type %q: %v", ok, err)
		}
	}
	for _, bad := range []string{"nonsense", "/json", "application/", "a b", ";charset=x"} {
		_, err := teaValidateArgs(t, teaVerbDownload, "--media-type", bad)
		teaWantUsage(t, "--media-type "+bad, err, "--media-type must be a type/subtype")
	}
	_, err = teaValidateArgs(t, teaVerbDownload, "--media-type", "text/plain", "--media-type", "nonsense")
	teaWantUsage(t, "second media type", err, "--media-type must be a type/subtype")
}

func TestTeaMachineDirChecks(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "afile")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(root, "exists")
	if err := os.Mkdir(existing, 0o700); err != nil {
		t.Fatal(err)
	}
	dangling := filepath.Join(root, "dangling")
	if err := os.Symlink(filepath.Join(root, "nowhere"), dangling); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, dir, msg string
	}{
		{"empty", "", "--dir is required"},
		{"missing parent", filepath.Join(root, "nope", "out"), "the parent of --dir must be an existing directory"},
		{"parent is a file", filepath.Join(file, "out"), "the parent of --dir must be an existing directory"},
		{"exists", existing, "--dir must not exist: the command creates it"},
		{"dangling link", dangling, "--dir must not exist: the command creates it"},
		{"a file", file, "--dir must not exist: the command creates it"},
		{"exists with a slash", existing + "/", "--dir must not exist: the command creates it"},
	} {
		_, err := teaValidateArgs(t, teaVerbDownload, "--dir", c.dir)
		teaWantUsage(t, c.name, err, c.msg)
	}
	for _, dir := range []string{filepath.Join(root, "new"), filepath.Join(root, "new2") + "/", filepath.Join(root, ".", "new3")} {
		if _, err := teaValidateArgs(t, teaVerbDownload, "--dir", dir); err != nil {
			t.Errorf("--dir %s: %v", dir, err)
		}
	}
	// a relative --dir: the parent is the working directory
	t.Chdir(root)
	if _, err := teaValidateArgs(t, teaVerbDownload, "--dir", "relative"); err != nil {
		t.Errorf("relative --dir: %v", err)
	}
	if _, err := teaValidateArgs(t, teaVerbDownload, "--dir", "relative/"); err != nil {
		t.Errorf("relative --dir with a slash: %v", err)
	}
	if got := teaParentDir("a/b/"); got != "a" {
		t.Errorf("parent of a/b/ is %q", got)
	}
	if got := teaParentDir("a/b"); got != "a" {
		t.Errorf("parent of a/b is %q", got)
	}
	if got := teaParentDir("out"); got != "." {
		t.Errorf("parent of out is %q", got)
	}
}

func TestTeaMachineMediaTypeKey(t *testing.T) {
	for in, want := range map[string]string{
		"application/json":                  "application/json",
		"Application/JSON; charset=utf-8":   "application/json",
		" text/plain ":                      "text/plain",
		"\ttext/plain\t;q=1":                "text/plain",
		"application/vnd.cyclonedx+json":    "application/vnd.cyclonedx+json",
		"text":                              "",
		"/x":                                "",
		"x/":                                "",
		"":                                  "",
		";charset=utf-8":                    "",
		"text/plain;charset=a/b":            "text/plain",
		"image/svg+xml":                     "image/svg+xml",
		"application/octet-stream ;x=\"y\"": "application/octet-stream",
	} {
		if got := teaMediaTypeKey(in); got != want {
			t.Errorf("teaMediaTypeKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTeaMachineProxyVariablesNamed(t *testing.T) {
	if got := teaSetProxyVariables(teaIO{}); got == nil || len(got) != 0 {
		t.Errorf("no getenv: %#v, want an empty list", got)
	}
	all := map[string]string{"HTTP_PROXY": "a", "HTTPS_PROXY": "b", "ALL_PROXY": "c", "http_proxy": "d", "https_proxy": "e", "all_proxy": "f"}
	get := func(env map[string]string) func(string) (string, bool) {
		return func(k string) (string, bool) { v, ok := env[k]; return v, ok }
	}
	got := teaSetProxyVariables(teaIO{getenv: get(all)})
	if strings.Join(got, ",") != "HTTP_PROXY,HTTPS_PROXY,ALL_PROXY,http_proxy,https_proxy,all_proxy" {
		t.Errorf("all set: %v", got)
	}
	for name := range all {
		env := map[string]string{name: "x"}
		if got := teaSetProxyVariables(teaIO{getenv: get(env)}); len(got) != 1 || got[0] != name {
			t.Errorf("only %s set: %v", name, got)
		}
	}
	got = teaSetProxyVariables(teaIO{getenv: get(map[string]string{"HTTP_PROXY": "", "NO_PROXY": "x", "https_proxy": "e"})})
	if len(got) != 1 || got[0] != "https_proxy" {
		t.Errorf("empty and unrelated values: %v", got)
	}
}

func TestTeaMachineLibraryLimitsFromFlags(t *testing.T) {
	in := teaFlagsOf(t, teaVerbDownload, "--max-document-bytes", "5000", "--max-redirects", "7", "--max-artifact-bytes", "123456",
		"--max-total-bytes", "999999", "--max-output-bytes", "70000")
	l := teaMachineLimits(in, nil)
	if l.JSONBytes != 5000 || l.Redirects != 7 || l.DownloadBytes != 123456 || l.AttemptTimeout != 0 || l.CallTimeout != 0 || l.IdleTimeout != 0 {
		t.Errorf("limits %+v", l)
	}
	l = teaMachineLimits(in, &teaNetSeam{attemptTimeout: 1 * time.Second, callTimeout: 2 * time.Second, idleTimeout: 3 * time.Second})
	if l.AttemptTimeout != time.Second || l.CallTimeout != 2*time.Second || l.IdleTimeout != 3*time.Second {
		t.Errorf("limits with the seam %+v", l)
	}
}
