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
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	tea "github.com/oolong-project/tea-client-go"
	"github.com/oolong-project/tea-client-go/teaapi"
	"github.com/oolong-project/tea-client-go/tei"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// The TEA machine mode (task TPF-178): `rearm tea machine <verb>`, for a service (rebom)
// that reads supplier TEA servers. A machine run never prompts, writes one JSON document
// (format version 1) or nothing on stdout and exactly one error object line on stderr when
// it fails, bounds everything it reads, refuses this host, link-local and cloud metadata
// addresses at connect time (teaMachineGuard.go), and reads no configuration file and no
// REARM_<FLAG> variable: every setting is on its command line.

// teaMachineVerb is one verb of the group.
type teaMachineVerb string

const (
	teaVerbResolve   teaMachineVerb = "resolve"
	teaVerbProduct   teaMachineVerb = "product"
	teaVerbComponent teaMachineVerb = "component"
	teaVerbReleases  teaMachineVerb = "releases"
	teaVerbRelease   teaMachineVerb = "release"
	teaVerbDownload  teaMachineVerb = "download"
)

// Defaults and ranges of the limit flags (D4). No range includes zero: to the library zero
// means "the default", so no flag can switch a bound off.
const (
	kib = int64(1) << 10
	mib = int64(1) << 20
	gib = int64(1) << 30

	teaDefMaxDocumentBytes = 16 * mib
	teaMinMaxDocumentBytes = kib
	teaMaxMaxDocumentBytes = 256 * mib
	teaDefMaxRedirects     = 5
	teaMinMaxRedirects     = 1
	teaMaxMaxRedirects     = 20
	teaDefMaxStringBytes   = 8192
	teaMinMaxStringBytes   = 256
	teaMaxMaxStringBytes   = int(mib)
	teaDefMaxOutputBytes   = 32 * mib
	teaMinMaxOutputBytes   = 64 * kib
	teaMaxMaxOutputBytes   = gib
	teaDefMaxArtifactBytes = 100 * mib
	teaMinMaxArtifactBytes = 1
	teaMaxMaxArtifactBytes = 16 * gib
	teaDefMaxTotalBytes    = gib
	teaMinMaxTotalBytes    = 1
	teaMaxMaxTotalBytes    = 64 * gib
	teaDefMaxFiles         = 200
	teaMinMaxFiles         = 1
	teaMaxMaxFiles         = 100000
	teaDefPageSize         = 100
	teaMinPageSize         = 1
	teaMaxPageSize         = 100
	teaDefMaxPages         = 50
	teaMinMaxPages         = 1
	teaMaxMaxPages         = 1000
	teaDefConnectTimeout   = 5 * time.Second
	teaMinConnectTimeout   = time.Second
	teaMaxConnectTimeout   = 60 * time.Second
	teaDefDeadline         = 10 * time.Minute
	teaMinDeadline         = time.Second
	teaMaxDeadline         = 24 * time.Hour
)

// teaArtifactTypes are the eleven artifact types of TEA 1.0.0, the only values the type
// selection flags accept.
var teaArtifactTypes = []teaapi.ArtifactType{
	teaapi.ATTESTATION, teaapi.BOM, teaapi.BUILDMETA, teaapi.CERTIFICATION, teaapi.FORMULATION, teaapi.LICENSE,
	teaapi.OTHER, teaapi.RELEASENOTES, teaapi.SECURITYTXT, teaapi.THREATMODEL, teaapi.VULNERABILITIES,
}

// teaMachineInputs are the flag values of one machine run.
type teaMachineInputs struct {
	verb teaMachineVerb
	args []string // positional arguments: none is accepted

	tei     string
	useHTTP bool
	usePort int
	portSet bool

	apiRoot           string
	uuid              string
	product           string
	component         string
	productRelease    string
	componentRelease  string
	collectionVersion int
	dir               string

	artifactTypes        []string
	excludeArtifactTypes []string
	mediaTypes           []string

	pageToken    string
	pageTokenSet bool
	pageSize     int
	maxPages     int

	credentialStdin bool
	allowThisHost   bool
	debug           bool

	connectTimeout   time.Duration
	deadline         time.Duration
	maxDocumentBytes int64
	maxRedirects     int
	maxStringBytes   int
	maxOutputBytes   int64
	maxArtifactBytes int64
	maxTotalBytes    int64
	maxFiles         int

	set map[string]bool // flags given on the command line
}

// teaMachineInputsDefault are the inputs with every flag at its default.
func teaMachineInputsDefault(verb teaMachineVerb) teaMachineInputs {
	return teaMachineInputs{
		verb: verb, usePort: 443, pageSize: teaDefPageSize, maxPages: teaDefMaxPages,
		connectTimeout: teaDefConnectTimeout, deadline: teaDefDeadline,
		maxDocumentBytes: teaDefMaxDocumentBytes, maxRedirects: teaDefMaxRedirects,
		maxStringBytes: teaDefMaxStringBytes, maxOutputBytes: teaDefMaxOutputBytes,
		maxArtifactBytes: teaDefMaxArtifactBytes, maxTotalBytes: teaDefMaxTotalBytes, maxFiles: teaDefMaxFiles,
		set: map[string]bool{},
	}
}

// teaMachineCmd is the group. It has its own PersistentPreRun, which does NOT call
// initConfig: cobra runs only the nearest hook, so no flag of a machine verb is ever set
// from the configuration file or a REARM_<FLAG> variable (D1 rule 1).
var teaMachineCmd = &cobra.Command{
	Use:   "machine",
	Short: "TEA commands for a service: JSON output, bounded reads, verified downloads",
	Long: `Non-interactive TEA 1.0.0 commands for a service that reads supplier TEA servers.
Each verb writes one JSON document (format version 1) on stdout, or nothing; on failure
it writes one JSON error object line on stderr. It never prompts, reads standard input
only with --credential-stdin, and reads no configuration file and no REARM_<FLAG>
variable. Connections to loopback, this host's own addresses, link-local, unspecified
and cloud metadata addresses are refused at connect time (--allow-this-host lifts the
loopback and own-address part for local testing). See docs/tea.md, 11.6.

Exit codes: 0 success, 1 failure, 2 usage, 3 not found, 5 refused by the URL policy,
6 authentication, 7 partial download (read the manifest), 8 address refused, 9 a limit
was hit, 10 could not connect.`,
	Args:             cobra.ArbitraryArgs,
	SilenceErrors:    true,
	SilenceUsage:     true,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {},
	Run: func(cmd *cobra.Command, args []string) {
		msg := "a verb is required: resolve, product, component, releases, release or download"
		if len(args) > 0 {
			msg = "unknown verb; the verbs are resolve, product, component, releases, release and download"
		}
		os.Exit(int(teaMachineFail(systemTeaIO(), context.Background(), teaUsage("%s", msg), "", nil)))
	},
}

// teaMachineFlagError makes a cobra flag error the mode's usage error (D1 rule 5).
func teaMachineFlagError(cmd *cobra.Command, err error) error {
	os.Exit(int(teaMachineFail(systemTeaIO(), context.Background(), teaUsage("%s", err.Error()), "", nil)))
	return err
}

func newTeaMachineVerbCmd(verb teaMachineVerb, short string) *cobra.Command {
	return &cobra.Command{
		Use:           string(verb),
		Short:         short,
		Args:          cobra.ArbitraryArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		Run: func(cmd *cobra.Command, args []string) {
			in := teaMachineInputsFromFlags(cmd, verb, args)
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			code := runTeaMachine(ctx, systemTeaIO(), in)
			stop()
			os.Exit(int(code))
		},
	}
}

func init() {
	verbs := []*cobra.Command{
		newTeaMachineVerbCmd(teaVerbResolve, "Resolve a TEI to its product release(s): TEA_RESOLUTION"),
		newTeaMachineVerbCmd(teaVerbProduct, "Read one product by uuid: TEA_PRODUCT"),
		newTeaMachineVerbCmd(teaVerbComponent, "Read one component by uuid: TEA_COMPONENT"),
		newTeaMachineVerbCmd(teaVerbReleases, "List the releases of a product or component, paged: TEA_RELEASE_LIST"),
		newTeaMachineVerbCmd(teaVerbRelease, "Read one release with its latest collection and lifecycle: TEA_RELEASE"),
		newTeaMachineVerbCmd(teaVerbDownload, "Download and verify the artifacts of one collection version: TEA_DOWNLOAD_MANIFEST"),
	}
	for _, c := range verbs {
		addTeaMachineFlags(c, teaMachineVerb(c.Use))
		teaMachineCmd.AddCommand(c)
	}
	teaMachineCmd.SetFlagErrorFunc(teaMachineFlagError)
	teaCmd.AddCommand(teaMachineCmd)
}

// addTeaMachineFlags registers the flags of one verb. The values are read back through the
// command's own flag set (teaMachineInputsFromFlags), so no package variable is shared.
func addTeaMachineFlags(c *cobra.Command, verb teaMachineVerb) {
	d := teaMachineInputsDefault(verb)
	f := c.Flags()
	f.Bool("credential-stdin", false, "Read the TEA token (or, with REARM_TEA_APIKEYID, the key secret) from the first line of standard input")
	f.Bool("usehttp", false, "Allow plain http: on the TEI's origin for resolve, on exactly the --api-root for the other verbs (for local testing)")
	f.Bool("allow-this-host", false, "Allow connections to loopback and to this host's own addresses (for local testing); link-local and metadata addresses stay refused")
	f.Duration("connect-timeout", d.connectTimeout, "Bound on name resolution and connecting, per connection (1s to 60s)")
	f.Duration("deadline", d.deadline, "Bound on the whole run (1s to 24h)")
	f.Int64("max-document-bytes", d.maxDocumentBytes, "Bound on one JSON answer, in bytes (1 KiB to 256 MiB)")
	f.Int("max-redirects", d.maxRedirects, "Redirects followed per request (1 to 20)")
	f.Int("max-string-bytes", d.maxStringBytes, "Bound on one server string in the output, in bytes (256 to 1 MiB)")
	f.Int64("max-output-bytes", d.maxOutputBytes, "Bound on the JSON document written on stdout, in bytes (64 KiB to 1 GiB)")
	switch verb {
	case teaVerbResolve:
		f.String("tei", "", "Transparency Exchange Identifier, tei://<domain>/<type>/<id>")
		f.Int("useport", 443, "Port of the well-known resource (default: 443, or 80 with --usehttp)")
	case teaVerbProduct, teaVerbComponent:
		f.String("api-root", "", "TEA API root of the server (the url of a well-known endpoint)")
		f.String("uuid", "", "uuid of the "+string(verb))
	case teaVerbReleases:
		f.String("api-root", "", "TEA API root of the server (the url of a well-known endpoint)")
		f.String("product", "", "uuid of the product whose releases are listed")
		f.String("component", "", "uuid of the component whose releases are listed")
		f.String("page-token", "", "Continue a list: the nextPageToken of the previous run, given as --page-token=<token>")
		f.Int("page-size", d.pageSize, "Releases per page (1 to 100)")
		f.Int("max-pages", d.maxPages, "Pages read in one run (1 to 1000)")
	case teaVerbRelease, teaVerbDownload:
		f.String("api-root", "", "TEA API root of the server (the url of a well-known endpoint)")
		f.String("product-release", "", "uuid of the product release")
		f.String("component-release", "", "uuid of the component release")
	}
	if verb == teaVerbDownload {
		f.Int("collection-version", 0, "Version of the release's collection to download (1 or more)")
		f.String("dir", "", "Directory to create and download into; it must not exist, its parent must")
		f.StringArray("artifact-type", nil, "Download only artifacts of this TEA type (repeatable)")
		f.StringArray("exclude-artifact-type", nil, "Download every artifact but those of this TEA type (repeatable)")
		f.StringArray("media-type", nil, "Download only formats of this media type (repeatable)")
		f.Int64("max-artifact-bytes", d.maxArtifactBytes, "Bound on one file, in bytes (1 to 16 GiB)")
		f.Int64("max-total-bytes", d.maxTotalBytes, "Bound on the bytes received in one run, in bytes (1 to 64 GiB)")
		f.Int("max-files", d.maxFiles, "Bound on the number of files selected (1 to 100000)")
	}
}

// teaMachineInputsFromFlags reads the inputs of a verb from its flag set. pflag has parsed
// them already; a value that did not parse never reaches here (teaMachineFlagError).
func teaMachineInputsFromFlags(cmd *cobra.Command, verb teaMachineVerb, args []string) teaMachineInputs {
	in := teaMachineInputsDefault(verb)
	in.args = args
	in.debug = debug == "true"
	f := cmd.Flags()
	f.Visit(func(fl *pflag.Flag) { in.set[fl.Name] = true })
	in.credentialStdin, _ = f.GetBool("credential-stdin")
	in.useHTTP, _ = f.GetBool("usehttp")
	in.allowThisHost, _ = f.GetBool("allow-this-host")
	in.connectTimeout, _ = f.GetDuration("connect-timeout")
	in.deadline, _ = f.GetDuration("deadline")
	in.maxDocumentBytes, _ = f.GetInt64("max-document-bytes")
	in.maxRedirects, _ = f.GetInt("max-redirects")
	in.maxStringBytes, _ = f.GetInt("max-string-bytes")
	in.maxOutputBytes, _ = f.GetInt64("max-output-bytes")
	in.tei, _ = f.GetString("tei")
	if p, err := f.GetInt("useport"); err == nil {
		in.usePort, in.portSet = p, in.set["useport"]
	}
	in.apiRoot, _ = f.GetString("api-root")
	in.uuid, _ = f.GetString("uuid")
	in.product, _ = f.GetString("product")
	in.component, _ = f.GetString("component")
	in.productRelease, _ = f.GetString("product-release")
	in.componentRelease, _ = f.GetString("component-release")
	in.pageToken, _ = f.GetString("page-token")
	in.pageTokenSet = in.set["page-token"]
	if v, err := f.GetInt("page-size"); err == nil {
		in.pageSize = v
	}
	if v, err := f.GetInt("max-pages"); err == nil {
		in.maxPages = v
	}
	in.collectionVersion, _ = f.GetInt("collection-version")
	in.dir, _ = f.GetString("dir")
	in.artifactTypes, _ = f.GetStringArray("artifact-type")
	in.excludeArtifactTypes, _ = f.GetStringArray("exclude-artifact-type")
	in.mediaTypes, _ = f.GetStringArray("media-type")
	if v, err := f.GetInt64("max-artifact-bytes"); err == nil {
		in.maxArtifactBytes = v
	}
	if v, err := f.GetInt64("max-total-bytes"); err == nil {
		in.maxTotalBytes = v
	}
	if v, err := f.GetInt("max-files"); err == nil {
		in.maxFiles = v
	}
	return in
}

// teaMachineTarget is what a verb reads, parsed from its flags.
type teaMachineTarget struct {
	tei        tei.TEI
	base       tea.APIBase
	id         uuid.UUID // product, component, product release or component release
	kind       teaReleaseKind
	parentKind teaParentKind
	version    int // collection version (download)
}

// checkTeaRange refuses a value outside its range, naming the flag.
func checkTeaRange[T int | int64 | time.Duration](flag string, v, lo, hi T) error {
	if v < lo || v > hi {
		return teaUsage("--%s is out of range: it must be from %v to %v", flag, lo, hi)
	}
	return nil
}

// validateTeaMachine checks every flag before any request (D1, D4). No error repeats a
// value the user gave except a uuid flag's name.
func validateTeaMachine(in teaMachineInputs) (teaMachineTarget, error) {
	var t teaMachineTarget
	if len(in.args) > 0 {
		return t, teaUsage("tea machine %s takes no positional arguments", in.verb)
	}
	for _, r := range []error{
		checkTeaRange("connect-timeout", in.connectTimeout, teaMinConnectTimeout, teaMaxConnectTimeout),
		checkTeaRange("deadline", in.deadline, teaMinDeadline, teaMaxDeadline),
		checkTeaRange("max-document-bytes", in.maxDocumentBytes, teaMinMaxDocumentBytes, teaMaxMaxDocumentBytes),
		checkTeaRange("max-redirects", in.maxRedirects, teaMinMaxRedirects, teaMaxMaxRedirects),
		checkTeaRange("max-string-bytes", in.maxStringBytes, teaMinMaxStringBytes, teaMaxMaxStringBytes),
		checkTeaRange("max-output-bytes", in.maxOutputBytes, teaMinMaxOutputBytes, teaMaxMaxOutputBytes),
	} {
		if r != nil {
			return t, r
		}
	}
	if in.verb == teaVerbResolve {
		if in.tei == "" {
			return t, teaUsage("--tei is required")
		}
		parsed, err := tei.Parse(in.tei)
		if err != nil {
			if errors.Is(err, tei.ErrLegacyForm) {
				return t, &teaUsageError{msg: "the urn:tei: form is not accepted; write the TEI as tei://<domain>/<type>/<id>", err: err}
			}
			return t, &teaUsageError{msg: "invalid TEI: " + err.Error(), err: err}
		}
		if in.portSet && (in.usePort < 1 || in.usePort > 65535) {
			return t, teaUsage("--useport is out of range: a port is 1 to 65535")
		}
		t.tei = parsed
		return t, nil
	}
	base, err := checkTeaAPIRoot(in.apiRoot, in.useHTTP)
	if err != nil {
		return t, err
	}
	t.base = base
	switch in.verb {
	case teaVerbProduct, teaVerbComponent:
		t.id, err = teaUUIDFlag("uuid", in.uuid)
		return t, err
	case teaVerbReleases:
		flag, value, err := teaOneOf(in, "product", "component")
		if err != nil {
			return t, err
		}
		t.parentKind = teaParentProduct
		if flag == "component" {
			t.parentKind = teaParentComponent
		}
		if t.id, err = teaUUIDFlag(flag, value); err != nil {
			return t, err
		}
		if in.pageTokenSet {
			if err := checkTeaPageToken(in.pageToken, in.maxStringBytes); err != nil {
				return t, teaUsage("--page-token %s", err.Error())
			}
		}
		if err := checkTeaRange("page-size", in.pageSize, teaMinPageSize, teaMaxPageSize); err != nil {
			return t, err
		}
		return t, checkTeaRange("max-pages", in.maxPages, teaMinMaxPages, teaMaxMaxPages)
	}
	// release and download
	flag, value, err := teaOneOf(in, "product-release", "component-release")
	if err != nil {
		return t, err
	}
	t.kind = teaReleaseKindProduct
	if flag == "component-release" {
		t.kind = teaReleaseKindComponent
	}
	if t.id, err = teaUUIDFlag(flag, value); err != nil {
		return t, err
	}
	if in.verb == teaVerbRelease {
		return t, nil
	}
	if !in.set["collection-version"] {
		return t, teaUsage("--collection-version is required")
	}
	if in.collectionVersion < 1 {
		return t, teaUsage("--collection-version must be 1 or more")
	}
	t.version = in.collectionVersion
	for _, r := range []error{
		checkTeaRange("max-artifact-bytes", in.maxArtifactBytes, teaMinMaxArtifactBytes, teaMaxMaxArtifactBytes),
		checkTeaRange("max-total-bytes", in.maxTotalBytes, teaMinMaxTotalBytes, teaMaxMaxTotalBytes),
		checkTeaRange("max-files", in.maxFiles, teaMinMaxFiles, teaMaxMaxFiles),
	} {
		if r != nil {
			return t, r
		}
	}
	if len(in.artifactTypes) > 0 && len(in.excludeArtifactTypes) > 0 {
		return t, teaUsage("--artifact-type and --exclude-artifact-type cannot be combined")
	}
	for _, list := range [][]string{in.artifactTypes, in.excludeArtifactTypes} {
		for _, v := range list {
			if !teaKnownArtifactType(v) {
				return t, teaUsage("an artifact type must be one of the TEA 1.0.0 types (%s)", teaArtifactTypeList())
			}
		}
	}
	for _, m := range in.mediaTypes {
		if teaMediaTypeKey(m) == "" {
			return t, teaUsage("--media-type must be a type/subtype")
		}
	}
	return t, checkTeaDir(in.dir)
}

// teaOneOf returns the one of two uuid flags that was given.
func teaOneOf(in teaMachineInputs, a, b string) (string, string, error) {
	va, vb := teaFlagString(in, a), teaFlagString(in, b)
	switch {
	case va != "" && vb != "":
		return "", "", teaUsage("--%s and --%s cannot be combined", a, b)
	case va != "":
		return a, va, nil
	case vb != "":
		return b, vb, nil
	}
	return "", "", teaUsage("one of --%s and --%s is required", a, b)
}

func teaFlagString(in teaMachineInputs, name string) string {
	switch name {
	case "product":
		return in.product
	case "component":
		return in.component
	case "product-release":
		return in.productRelease
	case "component-release":
		return in.componentRelease
	}
	return ""
}

// teaUUIDFlag parses a uuid flag; the value is sent in canonical form.
func teaUUIDFlag(flag, v string) (uuid.UUID, error) {
	if v == "" {
		return uuid.Nil, teaUsage("--%s is required", flag)
	}
	id, err := uuid.Parse(v)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, teaUsage("--%s is not a uuid", flag)
	}
	return id, nil
}

// checkTeaAPIRoot checks --api-root like REARM_TEA_AUTH_ROOT (never echoing a refused
// value), then as a TEA 1.0.0 API root; plain http needs --usehttp and --usehttp needs it.
func checkTeaAPIRoot(root string, useHTTP bool) (tea.APIBase, error) {
	if root == "" {
		return tea.APIBase{}, teaUsage("--api-root is required")
	}
	u, err := url.Parse(root)
	switch {
	case err != nil:
		return tea.APIBase{}, teaUsage("--api-root is not a valid URL")
	case u.User != nil:
		return tea.APIBase{}, teaUsage("--api-root must not carry a user name or password")
	case u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(root, "#"):
		return tea.APIBase{}, teaUsage("--api-root must not carry a query or a fragment")
	case strings.HasSuffix(root, "/"):
		return tea.APIBase{}, teaUsage("--api-root must not end with a slash")
	}
	switch scheme := strings.ToLower(u.Scheme); {
	case scheme == "http" && !useHTTP:
		return tea.APIBase{}, teaUsage("--api-root is plain http: give --usehttp for a local server")
	case scheme != "http" && useHTTP:
		return tea.APIBase{}, teaUsage("--usehttp allows plain http on the --api-root only: the root must then be http://")
	}
	base, err := tea.ParseAPIBase(root, teaAPIVersion)
	if err != nil {
		return tea.APIBase{}, &teaUsageError{msg: "--api-root is not a TEA API root: an absolute https URL without query, fragment, dot segments or trailing slash", err: err}
	}
	return base, nil
}

// checkTeaPageToken accepts a page token that can be passed back on a command line: not
// empty (empty means the first page to the library), valid UTF-8, no control character, at
// most max bytes.
func checkTeaPageToken(tok string, max int) error {
	switch {
	case tok == "":
		return errors.New("must not be empty")
	case len(tok) > max:
		return fmt.Errorf("is longer than --max-string-bytes (%d bytes)", max)
	case !utf8.ValidString(tok):
		return errors.New("is not valid UTF-8")
	}
	for _, r := range tok {
		if unicode.IsControl(r) {
			return errors.New("has a control character")
		}
	}
	return nil
}

func teaKnownArtifactType(v string) bool {
	for _, t := range teaArtifactTypes {
		if string(t) == v {
			return true
		}
	}
	return false
}

func teaArtifactTypeList() string {
	names := make([]string, 0, len(teaArtifactTypes))
	for _, t := range teaArtifactTypes {
		names = append(names, string(t))
	}
	return strings.Join(names, ", ")
}

// teaMediaTypeKey is the lowercase type/subtype of a media type, parameters dropped; "" when
// it has no type/subtype.
func teaMediaTypeKey(m string) string {
	base, _, _ := strings.Cut(m, ";")
	base = strings.ToLower(strings.TrimSpace(base))
	typ, sub, ok := strings.Cut(base, "/")
	if !ok || typ == "" || sub == "" {
		return ""
	}
	return base
}

// checkTeaDir: the parent of --dir must be an existing directory and nothing may exist at
// --dir itself (Lstat, so a symbolic link counts as existing).
func checkTeaDir(dir string) error {
	if dir == "" {
		return teaUsage("--dir is required")
	}
	parent := teaParentDir(dir)
	st, err := os.Stat(parent)
	if err != nil || !st.IsDir() {
		return teaUsage("the parent of --dir must be an existing directory")
	}
	if _, err := os.Lstat(dir); err == nil {
		return teaUsage("--dir must not exist: the command creates it")
	} else if !errors.Is(err, os.ErrNotExist) {
		return teaUsage("--dir cannot be checked")
	}
	return nil
}

// teaMachineLimits are the library limits of a machine run: the flags, and the library's
// time bounds from the seam (0 keeps their defaults). DownloadBytes is the artifact bound,
// so no download can fall back to the library's 512 MiB default.
func teaMachineLimits(in teaMachineInputs, seam *teaNetSeam) tea.Limits {
	l := tea.Limits{JSONBytes: in.maxDocumentBytes, Redirects: in.maxRedirects, DownloadBytes: in.maxArtifactBytes}
	if seam != nil {
		l.AttemptTimeout, l.CallTimeout, l.IdleTimeout = seam.attemptTimeout, seam.callTimeout, seam.idleTimeout
	}
	return l
}

// teaMachineRun is the state of one machine run.
type teaMachineRun struct {
	tio    teaIO
	in     teaMachineInputs
	run    *teaRun // credential, trace, last request
	ctx    context.Context
	target teaMachineTarget
}

// runTeaMachine runs one verb and returns its exit code. Every output goes through tio.
func runTeaMachine(ctx context.Context, tio teaIO, in teaMachineInputs) teaExit {
	m := &teaMachineRun{tio: tio, in: in, ctx: ctx,
		run: &teaRun{tio: tio, in: teaInputs{debug: in.debug, credentialStdin: in.credentialStdin}}}
	target, err := validateTeaMachine(in)
	if err != nil {
		return m.fail(err)
	}
	if in.credentialStdin && tio.stdin == teaStdinTerminal {
		return m.fail(teaUsage("--credential-stdin reads a line from a pipe; standard input is a terminal and the machine mode never prompts"))
	}
	m.target = target
	runCtx, cancel := context.WithTimeoutCause(ctx, in.deadline, errTeaDeadline)
	defer cancel()
	m.ctx = runCtx

	switch in.verb {
	case teaVerbResolve:
		return m.resolve()
	case teaVerbProduct, teaVerbComponent:
		return m.product()
	case teaVerbReleases:
		return m.releases()
	case teaVerbRelease:
		return m.release()
	case teaVerbDownload:
		return m.download()
	}
	return m.fail(teaUsage("unknown verb"))
}

// fail writes the error object of err and returns its exit code; stdout stays empty.
func (m *teaMachineRun) fail(err error) teaExit {
	return teaMachineFail(m.tio, m.ctx, err, m.run.lastRequest, teaSetProxyVariables(m.tio))
}

// teaMachineFail writes the one error object line on stderr.
func teaMachineFail(tio teaIO, runCtx context.Context, err error, lastRequest string, proxies []string) teaExit {
	d := newTeaErrorDoc(runCtx, err, lastRequest, proxies)
	b, encErr := teaEncode(d)
	if encErr != nil {
		b = []byte(`{"formatVersion":1,"kind":"TEA_ERROR","exitCode":1,"reason":"FAILED","message":"the error could not be encoded","url":null,"httpStatus":null,"address":null,"addressClass":null,"phase":null,"hint":null,"ignoredProxyVariables":[]}`)
		d.ExitCode = int(teaExitFailed)
	}
	_, _ = tio.errOut.Write(append(b, '\n'))
	return teaExit(d.ExitCode)
}

// teaSetProxyVariables are the proxy variables set in the environment, which the mode ignores.
func teaSetProxyVariables(tio teaIO) []string {
	out := []string{}
	if tio.getenv == nil {
		return out
	}
	for _, k := range teaProxyVariables {
		if v, ok := tio.getenv(k); ok && v != "" {
			out = append(out, k)
		}
	}
	return out
}

// emit writes a document on stdout when it fits --max-output-bytes.
func (m *teaMachineRun) emit(doc any, code teaExit) teaExit {
	b, err := teaEncode(doc)
	if err != nil {
		return m.fail(err)
	}
	if int64(len(b)+1) > m.in.maxOutputBytes {
		return m.fail(&teaLimitError{reason: teaReasonOutputBytes,
			msg: fmt.Sprintf("the document is larger than --max-output-bytes (%d bytes)", m.in.maxOutputBytes)})
	}
	if _, err := m.tio.out.Write(append(b, '\n')); err != nil {
		return m.fail(err)
	}
	return code
}

// client builds the client of the run: the guard under the one HTTP client, the limits,
// the trace, and the credential of TPF-175.
func (m *teaMachineRun) client(cfg teaClientConfig) (*tea.Client, error) {
	seam := m.tio.machine
	g := newTeaGuard(m.in.allowThisHost, m.in.connectTimeout, seam)
	if m.in.debug {
		g.refused = func(a netip.Addr, class teaAddressClass, host string) {
			fmt.Fprintf(m.tio.errOut, "TEA address refused: %s %s for %s\n", a, class, teaText(host))
		}
	}
	limits := teaMachineLimits(m.in, seam)
	cfg.httpClient = newTeaMachineHTTPClient(g, seam)
	cfg.limits = &limits
	cfg.trace = m.run.trace
	cfg.extra = nil
	if seam != nil && seam.firstBackoff > 0 {
		cfg.extra = []tea.Option{tea.WithDiscoveryAttempts(4, seam.firstBackoff)}
	}
	return m.run.credentialClient(m.ctx, cfg)
}

// api builds the client and the typed API at --api-root.
func (m *teaMachineRun) api() (*tea.API, error) {
	cfg := teaClientConfig{}
	if m.in.useHTTP {
		cfg.insecureBase = m.in.apiRoot
	}
	c, err := m.client(cfg)
	if err != nil {
		return nil, err
	}
	api, err := c.API(m.target.base)
	if err != nil {
		return nil, &teaUsageError{msg: "--api-root is refused by the URL policy", err: err}
	}
	return api, nil
}

func (m *teaMachineRun) server() teaServerJSON {
	return teaServerJSON{RootURL: m.in.apiRoot, APIVersion: teaAPIVersion}
}

// stringsChecker checks server strings against --max-string-bytes.
func (m *teaMachineRun) stringsChecker() *teaStrings {
	return &teaStrings{max: m.in.maxStringBytes}
}

// resolve: discovery only; several results are the answer (ambiguity is data).
func (m *teaMachineRun) resolve() teaExit {
	t := m.target.tei
	c, err := m.client(teaClientConfig{domain: t.Domain(), useHTTP: m.in.useHTTP, usePort: m.in.usePort, portSet: m.in.portSet})
	if err != nil {
		return m.fail(err)
	}
	results, err := c.Discover(m.ctx, t)
	if err != nil {
		return m.fail(err)
	}
	if len(results) == 0 {
		return m.fail(errTeaNoResult)
	}
	sc := m.stringsChecker()
	doc := teaResolutionDoc{FormatVersion: teaFormatVersion, Kind: teaDocResolution, TEI: t.String(), Results: []teaResolutionResult{}}
	for i, r := range results {
		p := fmt.Sprintf("results[%d]", i)
		res := teaResolutionResult{
			ProductReleaseUUID: r.ProductReleaseUUID.String(),
			DiscoveredAt:       teaServerJSON{RootURL: sc.s(p+".discoveredAt.rootUrl", r.DiscoveredAt.RootURL()), APIVersion: sc.s(p+".discoveredAt.apiVersion", r.DiscoveredAt.Version())},
			Servers:            []teaResolutionServer{},
		}
		for j, s := range r.Servers {
			sp := fmt.Sprintf("%s.servers[%d]", p, j)
			srv := teaResolutionServer{RootURL: sc.s(sp+".rootUrl", s.RootUrl), Versions: []string{}, Priority: s.Priority}
			for k, v := range s.Versions {
				srv.Versions = append(srv.Versions, sc.s(fmt.Sprintf("%s.versions[%d]", sp, k), v))
			}
			res.Servers = append(res.Servers, srv)
		}
		doc.Results = append(doc.Results, res)
	}
	if sc.err != nil {
		return m.fail(sc.err)
	}
	return m.emit(doc, teaExitOK)
}

// product: one product or component by uuid, with its name and identifiers (item 19).
func (m *teaMachineRun) product() teaExit {
	api, err := m.api()
	if err != nil {
		return m.fail(err)
	}
	sc := m.stringsChecker()
	if m.in.verb == teaVerbProduct {
		p, err := api.GetProduct(m.ctx, m.target.id)
		if err != nil {
			return m.fail(err)
		}
		doc := teaProductDoc{FormatVersion: teaFormatVersion, Kind: teaDocProduct, Server: m.server(),
			Product: teaNamedJSON{UUID: p.Uuid.String(), Name: sc.s("product.name", p.Name), Identifiers: sc.identifiers("product.identifiers", p.Identifiers)}}
		if sc.err != nil {
			return m.fail(sc.err)
		}
		return m.emit(doc, teaExitOK)
	}
	c, err := api.GetComponent(m.ctx, m.target.id)
	if err != nil {
		return m.fail(err)
	}
	doc := teaComponentDoc{FormatVersion: teaFormatVersion, Kind: teaDocComponent, Server: m.server(),
		Component: teaNamedJSON{UUID: c.Uuid.String(), Name: sc.s("component.name", c.Name), Identifiers: sc.identifiers("component.identifiers", c.Identifiers)}}
	if sc.err != nil {
		return m.fail(sc.err)
	}
	return m.emit(doc, teaExitOK)
}

// teaReleasePage is one page of releases as the list needs it.
type teaReleasePage struct {
	items   []teaReleaseItem
	hasNext bool
	next    *string
}

// page reads one page of the parent's releases.
func (m *teaMachineRun) page(api *tea.API, sc *teaStrings, token string, offset int) (teaReleasePage, error) {
	pg := tea.ReleasePage{Page: tea.Page{PageSize: m.in.pageSize, PageToken: token}}
	var out teaReleasePage
	item := func(i int, id uuid.UUID, version string, name *string, created time.Time, released *time.Time, pre *bool, ids *[]teaapi.Identifier) teaReleaseItem {
		p := fmt.Sprintf("releases[%d]", offset+i)
		return teaReleaseItem{UUID: id.String(), Version: sc.s(p+".version", version), Name: sc.p(p+".name", name),
			CreatedDate: teaInstant(created), ReleaseDate: teaInstantP(released), PreRelease: teaPreReleaseOf(pre),
			Identifiers: sc.identifiersP(p+".identifiers", ids)}
	}
	if m.target.parentKind == teaParentProduct {
		r, err := api.ListProductReleases(m.ctx, m.target.id, pg)
		if err != nil {
			return out, err
		}
		for i, rel := range r.Results {
			out.items = append(out.items, item(i, rel.Uuid, rel.Version, rel.ProductName, rel.CreatedDate, rel.ReleaseDate, rel.PreRelease, rel.Identifiers))
		}
		out.hasNext, out.next = r.HasNext, r.NextPageToken
	} else {
		r, err := api.ListComponentReleases(m.ctx, m.target.id, pg)
		if err != nil {
			return out, err
		}
		for i, rel := range r.Results {
			out.items = append(out.items, item(i, rel.Uuid, rel.Version, rel.ComponentName, rel.CreatedDate, rel.ReleaseDate, rel.PreRelease, rel.Identifiers))
		}
		out.hasNext, out.next = r.HasNext, r.NextPageToken
	}
	if !out.hasNext {
		out.next = nil
		return out, nil
	}
	if err := checkTeaPageToken(*out.next, m.in.maxStringBytes); err != nil {
		return out, &tea.UnexpectedResponseError{Operation: "listing releases", Status: 200, Reason: "the nextPageToken cannot be passed back: it " + err.Error()}
	}
	return out, nil
}

// releases: the CLI's own page loop over the single-page operation, so it always holds the
// token of the next page (D2). A page is added whole or not at all.
func (m *teaMachineRun) releases() teaExit {
	api, err := m.api()
	if err != nil {
		return m.fail(err)
	}
	sc := m.stringsChecker()
	doc := teaReleaseListDoc{FormatVersion: teaFormatVersion, Kind: teaDocReleases, Server: m.server(),
		Parent: teaParentJSON{Kind: m.target.parentKind, UUID: m.target.id.String()}, Releases: []teaReleaseItem{}}
	token := ""
	if m.in.pageTokenSet {
		token = m.in.pageToken
	}
	var itemsBytes int64 // encoded items, with the commas between them
	for pages := 0; ; {
		if err := m.ctx.Err(); err != nil {
			return m.fail(err)
		}
		pg, err := m.page(api, sc, token, len(doc.Releases))
		if err != nil {
			return m.fail(err)
		}
		if sc.err != nil {
			return m.fail(sc.err)
		}
		pageBytes, err := teaItemsBytes(pg.items)
		if err != nil {
			return m.fail(err)
		}
		if len(doc.Releases) > 0 && len(pg.items) > 0 {
			pageBytes++ // the comma before this page's first item
		}
		// The page block the document ends with if the run stops after this page: END,
		// MAX_PAGES with the next token, or OUTPUT_BYTES with the next token when a later
		// page is dropped.
		block := teaReleaseListPage{StoppedBy: teaListEnd, PagesRead: pages + 1}
		switch {
		case !pg.hasNext:
		case pages+1 == m.in.maxPages:
			block.StoppedBy, block.NextPageToken = teaListMaxPages, pg.next
		default:
			block.StoppedBy, block.NextPageToken = teaListOutputBytes, pg.next
		}
		size, err := teaSkeletonBytes(doc, block)
		if err != nil {
			return m.fail(err)
		}
		if size+itemsBytes+pageBytes+1 > m.in.maxOutputBytes {
			if pages == 0 {
				return m.fail(&teaLimitError{reason: teaReasonOutputBytes,
					msg: fmt.Sprintf("the first page of releases does not fit --max-output-bytes (%d bytes)", m.in.maxOutputBytes)})
			}
			t := token
			doc.Page = teaReleaseListPage{StoppedBy: teaListOutputBytes, NextPageToken: &t, PagesRead: pages}
			return m.emit(doc, teaExitOK)
		}
		pages++
		doc.Releases = append(doc.Releases, pg.items...)
		itemsBytes += pageBytes
		if !pg.hasNext {
			doc.Page = teaReleaseListPage{StoppedBy: teaListEnd, PagesRead: pages}
			return m.emit(doc, teaExitOK)
		}
		if pages == m.in.maxPages {
			doc.Page = teaReleaseListPage{StoppedBy: teaListMaxPages, NextPageToken: pg.next, PagesRead: pages}
			return m.emit(doc, teaExitOK)
		}
		token = *pg.next
	}
}

// teaItemsBytes is the encoded size of items as they appear inside a JSON array.
func teaItemsBytes(items []teaReleaseItem) (int64, error) {
	var n int64
	for i, it := range items {
		b, err := teaEncode(it)
		if err != nil {
			return 0, err
		}
		n += int64(len(b))
		if i > 0 {
			n++
		}
	}
	return n, nil
}

// teaSkeletonBytes is the encoded size of the list document without its releases, with
// the page block given.
func teaSkeletonBytes(doc teaReleaseListDoc, block teaReleaseListPage) (int64, error) {
	doc.Releases = []teaReleaseItem{}
	doc.Page = block
	b, err := teaEncode(doc)
	return int64(len(b)), err
}

// release: one release with its latest collection and lifecycle (two requests).
func (m *teaMachineRun) release() teaExit {
	api, err := m.api()
	if err != nil {
		return m.fail(err)
	}
	sc := m.stringsChecker()
	doc := teaReleaseDoc{FormatVersion: teaFormatVersion, Kind: teaDocRelease, Server: m.server()}
	var cle *teaapi.Cle
	var cleErr error
	if m.target.kind == teaReleaseKindProduct {
		r, err := api.GetProductRelease(m.ctx, m.target.id)
		if err != nil {
			return m.fail(err)
		}
		doc.Release = teaReleaseJSON{Kind: teaReleaseKindProduct, UUID: r.Uuid.String(), ParentUUID: r.Product.String(),
			Name: sc.p("release.name", r.ProductName), Version: sc.s("release.version", r.Version),
			CreatedDate: teaInstant(r.CreatedDate), ReleaseDate: teaInstantP(r.ReleaseDate), PreRelease: teaPreReleaseOf(r.PreRelease),
			Identifiers: sc.identifiersP("release.identifiers", r.Identifiers), Components: []teaComponentRef{}}
		for _, ref := range r.Components {
			cr := teaComponentRef{ComponentUUID: ref.Uuid.String()}
			if ref.Release != nil {
				s := ref.Release.String()
				cr.ReleaseUUID = &s
			}
			doc.Release.Components = append(doc.Release.Components, cr)
		}
		doc.Collection = sc.collection("collection", r.LatestCollection)
		cle, cleErr = api.GetProductReleaseCLE(m.ctx, m.target.id)
	} else {
		r, err := api.GetComponentRelease(m.ctx, m.target.id)
		if err != nil {
			return m.fail(err)
		}
		doc.Release = teaReleaseJSON{Kind: teaReleaseKindComponent, UUID: r.Uuid.String(), ParentUUID: r.Component.String(),
			Name: sc.p("release.name", r.ComponentName), Version: sc.s("release.version", r.Version),
			CreatedDate: teaInstant(r.CreatedDate), ReleaseDate: teaInstantP(r.ReleaseDate), PreRelease: teaPreReleaseOf(r.PreRelease),
			Identifiers: sc.identifiersP("release.identifiers", r.Identifiers), Components: []teaComponentRef{}}
		doc.Collection = sc.collection("collection", r.LatestCollection)
		cle, cleErr = api.GetComponentReleaseCLE(m.ctx, m.target.id)
	}
	if cleErr != nil {
		if !errors.Is(cleErr, tea.ErrNotImplemented) && !errors.Is(cleErr, tea.ErrObjectUnknown) {
			return m.fail(cleErr)
		}
		cle = nil
	}
	doc.CLE = sc.cle("cle", cle)
	if sc.err != nil {
		return m.fail(sc.err)
	}
	return m.emit(doc, teaExitOK)
}
