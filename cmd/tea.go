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
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"

	tea "github.com/oolong-project/tea-client-go"
	"github.com/oolong-project/tea-client-go/tei"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// The TEA commands are a client of the Transparency Exchange API 1.0.0, built on
// github.com/oolong-project/tea-client-go: everything on the wire (TEI parsing, the
// well-known document, endpoint and server selection, discovery, the typed API calls,
// the URL policy and the credential rules) is the library's. What stays here is the
// command line: flags, credentials from the environment or standard input, the choice
// among several discovery results, printing (teaOutput.go) and exit codes.

// teaCmd represents the tea command
var teaCmd = &cobra.Command{
	Use:   "tea",
	Short: "Transparency Exchange API commands",
	Long: `Commands for reading a Transparency Exchange API (TEA) 1.0.0 server: resolve a
Transparency Exchange Identifier (TEI) and read the product release it names.`,
}

// The flag values of both commands. teaTeiFlag is not called tei: a package-level tei
// would collide with the import of package tei.
var teaTeiFlag string
var teaUseHTTPFlag bool
var teaUsePortFlag int
var teaCredentialStdinFlag bool

const teaCommandsHelp = `
A TEI has the form tei://<domain>/<type>/<id>, for example
  tei://demo.rearmhq.com/uuid/b9f115fb-6688-4a3f-a262-3acc04e1fc9e
The legacy urn:tei: form is not accepted.

Plain http is used only with --usehttp, and only on the origin http://<domain>[:<useport>].

Credentials for a server that requires them come from the environment or standard input,
never from a flag value: REARM_TEA_AUTH_ROOT with REARM_TEA_TOKEN, or with
REARM_TEA_APIKEYID and REARM_TEA_APIKEY; --credential-stdin reads the token (or the key
secret) from standard input. The ReARM API key is never sent to a TEA server.

Exit codes: 0 success, 1 failure, 2 usage, 3 not resolved, 4 several product releases
and none chosen, 5 refused by the URL policy, 6 authentication, 7 a component could not
be read.`

// discoveryCmd represents the discovery command
var discoveryCmd = &cobra.Command{
	Use:   "discovery",
	Short: "Discover product release UUID from TEI",
	Long: `Resolves a Transparency Exchange Identifier (TEI) to a product release UUID with TEA 1.0.0
discovery: the well-known document of the TEI's domain, then the discovery endpoint it names.
Prints the product release UUID. When the TEI resolves to several product releases, the
command lists them on stderr and asks which one to print (only when standard input is a
terminal).
` + teaCommandsHelp,
	Run: func(cmd *cobra.Command, args []string) {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		code := runTeaDiscovery(ctx, systemTeaIO(), teaInputsFromFlags(cmd))
		stop()
		os.Exit(int(code))
	},
}

// fullTeaFlowCmd represents the full_tea_flow command
var fullTeaFlowCmd = &cobra.Command{
	Use:   "full_tea_flow",
	Short: "Perform complete TEA discovery and data retrieval flow",
	Long: `Discovers a product release from a TEI and reads it from the TEA 1.0.0 server, printing:
- product name and version, its lifecycle events (CLE) and its artifacts
- each component release with its version, lifecycle events and artifacts
  (an unpinned component shows its newest release)
` + teaCommandsHelp,
	Run: func(cmd *cobra.Command, args []string) {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		code := runTeaFullFlow(ctx, systemTeaIO(), teaInputsFromFlags(cmd))
		stop()
		os.Exit(int(code))
	},
}

func init() {
	for _, c := range []*cobra.Command{discoveryCmd, fullTeaFlowCmd} {
		c.Flags().StringVar(&teaTeiFlag, "tei", "", "Transparency Exchange Identifier (TEI) to resolve, tei://<domain>/<type>/<id>")
		c.MarkFlagRequired("tei")
		c.Flags().BoolVar(&teaUseHTTPFlag, "usehttp", false, "Allow plain http, on the origin http://<domain>[:<useport>] only (for local testing)")
		c.Flags().IntVar(&teaUsePortFlag, "useport", 443, "Port of the well-known resource (default: 443, or 80 with --usehttp)")
		c.Flags().BoolVar(&teaCredentialStdinFlag, "credential-stdin", false, "Read the TEA token (or, with REARM_TEA_APIKEYID, the key secret) from standard input")
	}

	teaCmd.AddCommand(discoveryCmd)
	teaCmd.AddCommand(fullTeaFlowCmd)
}

// teaExit is the exit code of a TEA command (task text: typed exit codes). A machine mode
// adds codes; it does not renumber these.
type teaExit int

const (
	teaExitOK          teaExit = 0
	teaExitFailed      teaExit = 1
	teaExitUsage       teaExit = 2
	teaExitNotResolved teaExit = 3
	teaExitAmbiguous   teaExit = 4
	teaExitRefusedURL  teaExit = 5
	teaExitAuth        teaExit = 6
	teaExitPartial     teaExit = 7
)

// teaStdin says whether standard input is a terminal.
type teaStdin int

const (
	teaStdinOther teaStdin = iota
	teaStdinTerminal
)

// teaCredentialKind is the kind of TEA credential the user configured.
type teaCredentialKind int

const (
	teaCredentialNone teaCredentialKind = iota
	teaCredentialBearer
	teaCredentialAPIKey
)

// The environment variables of a TEA credential. They are read with teaIO.getenv, not
// through viper, so no flag and no configuration file key exists for them.
const (
	teaEnvAuthRoot = "REARM_TEA_AUTH_ROOT"
	teaEnvToken    = "REARM_TEA_TOKEN"
	teaEnvKeyID    = "REARM_TEA_APIKEYID"
	teaEnvKey      = "REARM_TEA_APIKEY"
)

// teaAPIVersion is the one TEA version the commands speak.
const teaAPIVersion = "1.0.0"

const (
	teaChoiceMaxBytes = 64       // the answer read at the prompt
	teaSecretMaxBytes = 16 << 10 // a secret read from standard input that is not a terminal
)

// teaIO is everything a TEA command reads and writes, so tests run the commands in-process.
type teaIO struct {
	in          io.Reader // standard input
	out, errOut io.Writer
	stdin       teaStdin
	readSecret  func(prompt string) (string, error) // no-echo read on a terminal
	getenv      func(string) (string, bool)
	httpClient  *http.Client // nil: the library's default transport
	extra       []tea.Option // appended last; tests shorten the discovery backoff here
}

// teaInputs are the flag values of one run.
type teaInputs struct {
	tei             string
	useHTTP         bool
	usePort         int
	portSet         bool
	debug           bool
	credentialStdin bool
}

func systemTeaIO() teaIO {
	stdin := teaStdinOther
	if term.IsTerminal(int(os.Stdin.Fd())) {
		stdin = teaStdinTerminal
	}
	return teaIO{
		in:     os.Stdin,
		out:    os.Stdout,
		errOut: os.Stderr,
		stdin:  stdin,
		readSecret: func(prompt string) (string, error) {
			fmt.Fprint(os.Stderr, prompt)
			b, err := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Fprintln(os.Stderr)
			return string(b), err
		},
		getenv: os.LookupEnv,
	}
}

func teaInputsFromFlags(cmd *cobra.Command) teaInputs {
	return teaInputs{
		tei:             teaTeiFlag,
		useHTTP:         teaUseHTTPFlag,
		usePort:         teaUsePortFlag,
		portSet:         cmd.Flags().Changed("useport"),
		debug:           debug == "true",
		credentialStdin: teaCredentialStdinFlag,
	}
}

// teaUsageError marks a failure of the preparation (flags, TEI, credential variables,
// client options), whatever it wraps: exit 2. Its text is the CLI's own.
type teaUsageError struct {
	msg string
	err error
}

func (e *teaUsageError) Error() string { return e.msg }
func (e *teaUsageError) Unwrap() error { return e.err }

func teaUsage(format string, args ...any) error {
	return &teaUsageError{msg: fmt.Sprintf(format, args...)}
}

// teaAuthError is a failed token exchange, rendered with its own line: exit 6.
type teaAuthError struct {
	msg string
	err error
}

func (e *teaAuthError) Error() string { return e.msg }
func (e *teaAuthError) Unwrap() error { return e.err }

// errTeaAmbiguous: several product releases and none chosen.
var errTeaAmbiguous = errors.New("no product release chosen")

// teaChoiceError is a choice among several discovery results that was not made; its text
// is the whole error line.
type teaChoiceError struct {
	msg string
}

func (e *teaChoiceError) Error() string { return e.msg }
func (e *teaChoiceError) Unwrap() error { return errTeaAmbiguous }

// teaCredential is the credential of one run, resolved and validated.
type teaCredential struct {
	kind   teaCredentialKind
	root   string
	token  string // teaCredentialBearer
	keyID  string // teaCredentialAPIKey
	secret string // teaCredentialAPIKey
}

func (c teaCredential) label() string {
	switch c.kind {
	case teaCredentialBearer:
		return "bearer token"
	case teaCredentialAPIKey:
		return "API key"
	default:
		return "none"
	}
}

// teaBearer is the token the client sends under its root.
type teaBearer struct {
	root  string
	token string
}

// teaClientConfig is the one input of newTeaClient.
type teaClientConfig struct {
	domain     string // the TEI's domain, for the well-known override
	useHTTP    bool
	usePort    int
	portSet    bool
	bearer     *teaBearer // nil: anonymous
	trace      func(tea.TraceEvent)
	httpClient *http.Client
	extra      []tea.Option
}

// teaHTTPOrigin is the one plain-http origin the flags allow, "" without --usehttp.
func (cfg teaClientConfig) teaHTTPOrigin() string {
	if !cfg.useHTTP {
		return ""
	}
	d := strings.ToLower(cfg.domain)
	if cfg.portSet {
		return "http://" + d + ":" + strconv.Itoa(cfg.usePort)
	}
	return "http://" + d
}

// newTeaClient is the only place a tea.Client is built in the CLI.
func newTeaClient(cfg teaClientConfig) (*tea.Client, error) {
	var opts []tea.Option
	d := strings.ToLower(cfg.domain)
	switch {
	case cfg.useHTTP:
		o := cfg.teaHTTPOrigin()
		opts = append(opts, tea.WithInsecureHTTPBase(o), tea.WithWellKnownLocation(d, o))
	case cfg.portSet:
		opts = append(opts, tea.WithWellKnownLocation(d, "https://"+d+":"+strconv.Itoa(cfg.usePort)))
	}
	if cfg.bearer != nil {
		opts = append(opts, tea.WithBearerToken(cfg.bearer.root, cfg.bearer.token))
	}
	if cfg.trace != nil {
		opts = append(opts, tea.WithTrace(cfg.trace))
	}
	if cfg.httpClient != nil {
		opts = append(opts, tea.WithHTTPClient(cfg.httpClient))
	}
	opts = append(opts, cfg.extra...)
	return tea.New(opts...)
}

// teaRun is the state of one command run: what the error line and the debug trace need.
type teaRun struct {
	tio         teaIO
	in          teaInputs
	cred        teaCredential
	lastRefusal string // URL of the last response answered 401 or 403
}

// trace is the WithTrace observer of every client of the run: it remembers the last
// refused URL for the error line and, with --debug, prints each event on stderr. An event
// carries scheme, host, port and path only, never a header, query string or credential.
func (r *teaRun) trace(ev tea.TraceEvent) {
	if ev.Kind == tea.TraceResponse && (ev.Status == http.StatusUnauthorized || ev.Status == http.StatusForbidden) {
		r.lastRefusal = ev.URL
	}
	if !r.in.debug {
		return
	}
	w := r.tio.errOut
	switch ev.Kind {
	case tea.TraceRequest:
		fmt.Fprintf(w, "TEA request [%s]: %s %s\n", ev.Stage, teaText(ev.Method), teaText(ev.URL))
	case tea.TraceResponse:
		fmt.Fprintf(w, "TEA response [%s]: %d %s\n", ev.Stage, ev.Status, teaText(ev.URL))
	case tea.TraceAttemptFailed:
		fmt.Fprintf(w, "TEA attempt failed [%s]: %s: %s\n", ev.Stage, teaText(ev.URL), teaText(ev.Reason))
	case tea.TraceBackoff:
		fmt.Fprintf(w, "TEA backoff [%s]: %s\n", ev.Stage, ev.Wait)
	}
}

func (r *teaRun) clientConfig(t tei.TEI) teaClientConfig {
	return teaClientConfig{
		domain:     t.Domain(),
		useHTTP:    r.in.useHTTP,
		usePort:    r.in.usePort,
		portSet:    r.in.portSet,
		trace:      r.trace,
		httpClient: r.tio.httpClient,
		extra:      r.tio.extra,
	}
}

// prepare parses the TEI, checks the flags, resolves the credential and builds the client
// the run works with. Every failure here is a usage error (exit 2), except a token
// exchange the server refused (exit 6) or could not answer (exit 1). Nothing is printed
// in debug before it has been validated.
func (r *teaRun) prepare(ctx context.Context) (*tea.Client, tei.TEI, error) {
	t, err := tei.Parse(r.in.tei)
	if err != nil {
		if errors.Is(err, tei.ErrLegacyForm) {
			return nil, tei.TEI{}, &teaUsageError{msg: "the urn:tei: form is no longer accepted; write the TEI as tei://<domain>/<type>/<id>", err: err}
		}
		return nil, tei.TEI{}, &teaUsageError{msg: "invalid TEI: " + err.Error(), err: err}
	}
	if r.in.debug {
		fmt.Fprintln(r.tio.errOut, "Resolving TEI:", teaText(t.String()))
	}
	if r.in.portSet && (r.in.usePort < 1 || r.in.usePort > 65535) {
		return nil, tei.TEI{}, teaUsage("invalid --useport %d: a port is 1 to 65535", r.in.usePort)
	}
	cred, err := resolveTeaCredential(r.tio, r.in)
	if err != nil {
		return nil, tei.TEI{}, err
	}
	r.cred = cred

	cfg := r.clientConfig(t)
	anon, err := newTeaClient(cfg)
	if err != nil {
		return nil, tei.TEI{}, &teaUsageError{msg: "invalid TEA client option: " + err.Error(), err: err}
	}
	if cred.kind == teaCredentialNone {
		if r.in.debug {
			fmt.Fprintln(r.tio.errOut, "TEA credential: none")
		}
		return anon, t, nil
	}
	base, err := tea.ParseAPIBase(cred.root, teaAPIVersion)
	if err != nil {
		return nil, tei.TEI{}, &teaUsageError{msg: teaEnvAuthRoot + " is not a TEA API root: an absolute https (or allowed http) URL without query, fragment or trailing slash", err: err}
	}
	api, err := anon.API(base)
	if err != nil {
		return nil, tei.TEI{}, &teaUsageError{msg: teaEnvAuthRoot + " is plain http outside the origin --usehttp and --useport allow; plain http is for a local server only", err: err}
	}
	if r.in.debug {
		fmt.Fprintf(r.tio.errOut, "TEA credential: %s for %s\n", cred.label(), teaText(cred.root))
	}
	token := cred.token
	if cred.kind == teaCredentialAPIKey {
		token, err = r.exchange(ctx, api)
		if err != nil {
			return nil, tei.TEI{}, err
		}
	}
	cfg.bearer = &teaBearer{root: cred.root, token: token}
	c, err := newTeaClient(cfg)
	if err != nil {
		return nil, tei.TEI{}, &teaUsageError{msg: "invalid TEA client option for " + teaEnvAuthRoot, err: err}
	}
	return c, t, nil
}

// exchange requests a token for the API key, once, before the first request: one POST to
// <root>/v1.0.0/token, built from the user's root, following no redirect. tea.WithAPIKey
// is not used: it requests a token only on a challenge, never for discovery, and a server
// that answers an anonymous caller with less instead of a challenge would go unnoticed.
func (r *teaRun) exchange(ctx context.Context, api *tea.API) (string, error) {
	tok, err := api.RequestToken(ctx, r.cred.keyID, r.cred.secret)
	if err == nil {
		return tok.AccessToken, nil
	}
	var tokErr *tea.TokenError
	var apiErr *tea.APIError
	var unexpected *tea.UnexpectedResponseError
	switch {
	case errors.As(err, &tokErr):
		return "", &teaAuthError{msg: "the TEA token endpoint refused the API key: " + err.Error(), err: err}
	case errors.Is(err, tea.ErrNotImplemented):
		return "", &teaAuthError{msg: fmt.Sprintf("the TEA server at %s has no token endpoint (HTTP 404): unset %s and %s to call it without a credential",
			r.cred.root, teaEnvKeyID, teaEnvKey), err: err}
	case errors.As(err, &unexpected):
		return "", &teaAuthError{msg: "no TEA access token: " + err.Error(), err: err}
	case errors.As(err, &apiErr) && !errors.Is(err, tea.ErrServer):
		return "", &teaAuthError{msg: "no TEA access token: " + err.Error(), err: err}
	}
	return "", err
}

// resolveTeaCredential reads the TEA credential from the environment, and the secret from
// standard input with --credential-stdin. Contradictions are refused before anything is
// read; no error names a value, only the variables.
func resolveTeaCredential(tio teaIO, in teaInputs) (teaCredential, error) {
	root, hasRoot := tio.getenv(teaEnvAuthRoot)
	token, hasToken := tio.getenv(teaEnvToken)
	keyID, hasID := tio.getenv(teaEnvKeyID)
	secret, hasKey := tio.getenv(teaEnvKey)

	if hasToken && (hasID || hasKey) {
		return teaCredential{}, teaUsage("%s cannot be combined with %s or %s: set one credential", teaEnvToken, teaEnvKeyID, teaEnvKey)
	}
	if in.credentialStdin {
		switch {
		case hasID && hasKey:
			return teaCredential{}, teaUsage("--credential-stdin reads the key secret in place of %s: unset %s", teaEnvKey, teaEnvKey)
		case hasToken:
			return teaCredential{}, teaUsage("--credential-stdin reads the token in place of %s: unset %s", teaEnvToken, teaEnvToken)
		case hasKey:
			return teaCredential{}, teaUsage("%s is set without %s", teaEnvKey, teaEnvKeyID)
		}
	} else {
		switch {
		case hasID && !hasKey:
			return teaCredential{}, teaUsage("%s is set without %s", teaEnvKeyID, teaEnvKey)
		case hasKey && !hasID:
			return teaCredential{}, teaUsage("%s is set without %s", teaEnvKey, teaEnvKeyID)
		}
	}
	kind := teaCredentialNone
	switch {
	case hasID:
		kind = teaCredentialAPIKey
	case hasToken || in.credentialStdin:
		kind = teaCredentialBearer
	}
	if kind == teaCredentialNone {
		// REARM_TEA_AUTH_ROOT alone, with no credential, is ignored.
		return teaCredential{}, nil
	}
	if !hasRoot || root == "" {
		return teaCredential{}, teaUsage("a TEA credential needs %s, the API root it belongs to (the url in https://<domain>/.well-known/tea)", teaEnvAuthRoot)
	}
	if err := checkTeaAuthRoot(root); err != nil {
		return teaCredential{}, err
	}
	if in.credentialStdin {
		s, err := readTeaSecret(tio)
		if err != nil {
			return teaCredential{}, err
		}
		if kind == teaCredentialAPIKey {
			secret = s
		} else {
			token = s
		}
	}
	switch kind {
	case teaCredentialAPIKey:
		if keyID == "" {
			return teaCredential{}, teaUsage("%s is empty", teaEnvKeyID)
		}
		if secret == "" {
			if in.credentialStdin {
				return teaCredential{}, teaUsage("the key secret read from standard input is empty")
			}
			return teaCredential{}, teaUsage("%s is empty", teaEnvKey)
		}
		return teaCredential{kind: kind, root: root, keyID: keyID, secret: secret}, nil
	default:
		if token == "" {
			if in.credentialStdin {
				return teaCredential{}, teaUsage("the token read from standard input is empty")
			}
			return teaCredential{}, teaUsage("%s is empty", teaEnvToken)
		}
		return teaCredential{kind: kind, root: root, token: token}, nil
	}
}

// checkTeaAuthRoot refuses a root the library would refuse with a text that repeats it:
// a user who wrote https://user:secret@host/tea must not see that on screen or in a log.
func checkTeaAuthRoot(root string) error {
	u, err := url.Parse(root)
	if err != nil {
		return teaUsage("%s is not a valid URL", teaEnvAuthRoot)
	}
	if u.User != nil {
		return teaUsage("%s must not carry a user name or password", teaEnvAuthRoot)
	}
	if u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(root, "#") {
		return teaUsage("%s must not carry a query or a fragment", teaEnvAuthRoot)
	}
	if strings.HasSuffix(root, "/") {
		return teaUsage("%s must not end with a slash", teaEnvAuthRoot)
	}
	return nil
}

// readTeaSecret reads the secret of --credential-stdin: without echo on a terminal, else
// the first line (at most teaSecretMaxBytes, the trailing CR LF removed).
func readTeaSecret(tio teaIO) (string, error) {
	if tio.stdin == teaStdinTerminal {
		s, err := tio.readSecret("TEA credential: ")
		if err != nil {
			return "", teaUsage("reading the credential from the terminal failed")
		}
		return s, nil
	}
	line, err := readTeaLine(tio.in, teaSecretMaxBytes)
	switch {
	case errors.Is(err, errTeaLineTooLong):
		return "", teaUsage("the credential read from standard input is longer than %d bytes", teaSecretMaxBytes)
	case err != nil && !errors.Is(err, io.EOF):
		return "", teaUsage("reading the credential from standard input failed")
	}
	return line, nil
}

var errTeaLineTooLong = errors.New("line too long")

// readTeaLine reads one line of at most max bytes, one byte at a time so nothing past the
// line is consumed. The line feed and a CR before it are removed. A line longer than max
// is errTeaLineTooLong after max+1 bytes, so the read is bounded; end of input before a
// line feed returns what was read with io.EOF.
func readTeaLine(r io.Reader, max int) (string, error) {
	var b []byte
	one := make([]byte, 1)
	for {
		n, err := r.Read(one)
		if n == 1 {
			if one[0] == '\n' {
				return strings.TrimSuffix(string(b), "\r"), nil
			}
			if len(b) == max {
				return "", errTeaLineTooLong
			}
			b = append(b, one[0])
		}
		if err != nil {
			return strings.TrimSuffix(string(b), "\r"), err
		}
	}
}

// runTeaDiscovery is tea discovery: print the product release uuid the TEI resolves to.
func runTeaDiscovery(ctx context.Context, tio teaIO, in teaInputs) teaExit {
	r := &teaRun{tio: tio, in: in}
	c, t, err := r.prepare(ctx)
	if err != nil {
		return r.fail(err)
	}
	results, err := c.Discover(ctx, t)
	if err != nil {
		return r.fail(err)
	}
	if len(results) == 0 {
		return r.fail(errors.New("discovery returned no result"))
	}
	chosen := results[0]
	if len(results) > 1 {
		cands, err := c.Candidates(ctx, results)
		if err != nil {
			return r.fail(err)
		}
		i, err := tio.chooseTeaCandidate(ctx, cands)
		if err != nil {
			return r.fail(err)
		}
		chosen = cands[i].Result
	}
	fmt.Fprintln(tio.out, chosen.ProductReleaseUUID.String())
	return teaExitOK
}

// runTeaFullFlow is tea full_tea_flow: print the product release, its lifecycle, its
// artifacts and its components.
func runTeaFullFlow(ctx context.Context, tio teaIO, in teaInputs) teaExit {
	r := &teaRun{tio: tio, in: in}
	c, t, err := r.prepare(ctx)
	if err != nil {
		return r.fail(err)
	}
	res, err := c.FullFlow(ctx, t, tea.FlowOptions{Select: tio.chooseTeaCandidate})
	if err != nil {
		return r.fail(err)
	}
	if printTeaFlow(tio.out, tio.errOut, res) {
		return teaExitPartial
	}
	return teaExitOK
}

// fail prints the one error line of a failed run and returns its exit code.
func (r *teaRun) fail(err error) teaExit {
	code := teaExitFor(err)
	fmt.Fprintln(r.tio.errOut, "Error: "+teaText(r.errorText(code, err)))
	return code
}

// teaExitFor is the one mapping from an error to an exit code; the first match wins.
func teaExitFor(err error) teaExit {
	var usage *teaUsageError
	var tokErr *tea.TokenError
	var authErr *teaAuthError
	var notResolved *tea.NotResolvedError
	var ambiguous *tea.AmbiguousError
	switch {
	case err == nil:
		return teaExitOK
	case errors.As(err, &usage),
		errors.Is(err, tei.ErrSyntax), errors.Is(err, tei.ErrLegacyForm),
		errors.Is(err, tea.ErrInvalidOption):
		return teaExitUsage
	case errors.Is(err, tea.ErrInsecureURL), errors.Is(err, tea.ErrRedirectLimit):
		return teaExitRefusedURL
	case errors.Is(err, tea.ErrUnauthorized), errors.Is(err, tea.ErrForbidden),
		errors.As(err, &tokErr), errors.As(err, &authErr):
		return teaExitAuth
	case errors.As(err, &notResolved), errors.Is(err, tea.ErrObjectUnknown):
		return teaExitNotResolved
	case errors.Is(err, errTeaAmbiguous), errors.As(err, &ambiguous):
		return teaExitAmbiguous
	default:
		return teaExitFailed
	}
}

// errorText is the text after "Error: " for a failed run with exit code code.
func (r *teaRun) errorText(code teaExit, err error) string {
	var usage *teaUsageError
	var authErr *teaAuthError
	var choice *teaChoiceError
	switch code {
	case teaExitUsage:
		if errors.As(err, &usage) {
			return usage.msg
		}
		return err.Error()
	case teaExitNotResolved:
		return "TEI not resolved: " + err.Error()
	case teaExitRefusedURL:
		s := "refused by the URL policy: " + err.Error()
		if errors.Is(err, tea.ErrInsecureURL) {
			s += "; over plain http only the origin given with --usehttp and --useport is allowed"
		}
		return s
	case teaExitAuth:
		if errors.As(err, &authErr) {
			return authErr.msg
		}
		status := http.StatusUnauthorized
		if errors.Is(err, tea.ErrForbidden) {
			status = http.StatusForbidden
		}
		var apiErr *tea.APIError
		if errors.As(err, &apiErr) {
			status = apiErr.Status
		}
		if r.cred.kind == teaCredentialNone {
			return fmt.Sprintf("the TEA server requires a credential (HTTP %d): %s; set %s with %s, or with %s and %s",
				status, err.Error(), teaEnvAuthRoot, teaEnvToken, teaEnvKeyID, teaEnvKey)
		}
		where := r.lastRefusal
		if where == "" {
			where = "an unknown URL"
		}
		return fmt.Sprintf("the TEA server refused the request (HTTP %d): %s; the credential is sent only under %s, the refused request went to %s",
			status, err.Error(), r.cred.root, where)
	case teaExitAmbiguous:
		if errors.As(err, &choice) {
			return choice.msg
		}
		return err.Error()
	default:
		return err.Error()
	}
}
