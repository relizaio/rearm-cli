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

package tests

import (
	"bytes"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// The environment variables of bomutils enrich, enrichsupplier and enrichlicense:
// REARM_BEARAPIKEY is sent as the X-API-Key header, REARM_BEARURI is the BEAR URI.
// Both are read through the root command's flag binding, so these tests run the
// CLI as built, in a child process, against a listener of the test. The child's
// environment is built here: no REARM_ variable of the test process and a new,
// empty HOME. The test process itself never sets a variable.
//
// There is no default BEAR URI: a run without a usable URI from --bearUri,
// REARM_BEARURI or the configuration file is refused before the BOM is read,
// with exit code 2 and one fixed line on stderr.

const (
	bearKeyFromVariable = "not-a-real-key-from-the-variable"
	bearKeyFromFlag     = "not-a-real-key-from-the-flag"
	// The public demo instance that was the default of --bearUri until the URI
	// became required. Used in negative checks only.
	bearFormerDefaultName = "beardemo"
	bearFormerDefaultUri  = "https://" + bearFormerDefaultName + ".rearmhq.com"
	bearOkBody            = `{"data":{"enrichBatch":[]}}`
)

var bearEnrichCommands = []string{"enrich", "enrichsupplier", "enrichlicense"}

// One component with a purl and no supplier, licenses or copyright: each of the
// three commands sends exactly one request for it.
const bearOneComponentBom = `{"bomFormat":"CycloneDX","specVersion":"1.6","version":1,` +
	`"components":[{"type":"library","name":"left-pad","version":"1.3.0","purl":"pkg:npm/left-pad@1.3.0"}]}`

// No components: with a URI every command prints it and returns before any request.
const bearNoComponentsBom = `{"bomFormat":"CycloneDX","specVersion":"1.6","version":1}`

type bearRequest struct {
	method  string
	path    string
	apiKeys []string
}

// bearListener records every request and answers with a fixed status and body.
type bearListener struct {
	url      string
	mu       sync.Mutex
	requests []bearRequest
}

func newBearListener(t *testing.T, status int, body string) *bearListener {
	t.Helper()
	l := &bearListener{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		l.mu.Lock()
		l.requests = append(l.requests, bearRequest{method: r.Method, path: r.URL.Path, apiKeys: r.Header.Values("X-API-Key")})
		l.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	l.url = srv.URL
	return l
}

func (l *bearListener) recorded() []bearRequest {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]bearRequest(nil), l.requests...)
}

// oneRequestWithKeys checks that the listener got exactly one POST /graphql and
// that its X-API-Key values are want (none at all when want is empty).
func (l *bearListener) oneRequestWithKeys(t *testing.T, want ...string) {
	t.Helper()
	got := l.recorded()
	if len(got) != 1 {
		t.Fatalf("%d requests, want 1", len(got))
	}
	r := got[0]
	if r.method != http.MethodPost || r.path != "/graphql" {
		t.Errorf("request %s %s, want POST /graphql", r.method, r.path)
	}
	if len(r.apiKeys) != len(want) {
		t.Fatalf("X-API-Key values %q, want %q", r.apiKeys, want)
	}
	for i := range want {
		if r.apiKeys[i] != want[i] {
			t.Errorf("X-API-Key values %q, want %q", r.apiKeys, want)
		}
	}
}

type bearRun struct {
	code   int
	stdout string
	stderr string
}

// bearCli builds the CLI once for the test and runs it with the environment of
// the case.
type bearCli struct {
	bin string
}

func buildBearCli(t *testing.T) *bearCli {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH")
	}
	bin := filepath.Join(t.TempDir(), "rearm")
	build := exec.Command("go", "build", "-o", bin, "..")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return &bearCli{bin: bin}
}

// run starts the CLI with an environment built for the case: the test process's
// environment without any REARM_ name and without HOME, then HOME set to a new
// empty directory, then caseEnv.
func (c *bearCli) run(t *testing.T, caseEnv []string, args ...string) bearRun {
	t.Helper()
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(strings.ToUpper(kv), "REARM_") || strings.HasPrefix(kv, "HOME=") {
			continue
		}
		env = append(env, kv)
	}
	env = append(env, "HOME="+t.TempDir())
	env = append(env, caseEnv...)
	cmd := exec.Command(c.bin, args...)
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return bearRun{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

// The two refusal lines, exactly as the CLI prints them.
const (
	bearUriMissingLine = "Error: no BEAR URI given: pass --bearUri or set REARM_BEARURI (there is no default)"
	bearUriInvalidLine = "Error: the BEAR URI is not valid: --bearUri or REARM_BEARURI must be an absolute http or https URI with a host"
)

// bearEnrichArgsOut is bearEnrichArgs that also returns the output file of the
// run, so a case can check whether it was written.
func bearEnrichArgsOut(t *testing.T, command string, bom string, extra ...string) ([]string, string) {
	t.Helper()
	args := bearEnrichArgs(t, command, bom, extra...)
	return args, args[5]
}

// bearMissingInputArgs is the argument list of one enrich command whose -f names
// a file that does not exist, and the output file of the run.
func bearMissingInputArgs(t *testing.T, command string, extra ...string) ([]string, string) {
	t.Helper()
	dir := t.TempDir()
	out := filepath.Join(dir, "output-bom.json")
	args := []string{"bomutils", command, "-f", filepath.Join(dir, "no-such-bom.json"), "-o", out}
	return append(args, extra...), out
}

// expectRefused checks a refused run: exit code 2, stderr exactly the line and a
// line feed, stdout empty, the output file not written, and the former default
// instance named nowhere.
func expectRefused(t *testing.T, r bearRun, line string, outFile string) {
	t.Helper()
	expectExit(t, r, 2)
	if r.stderr != line+"\n" {
		t.Errorf("stderr %q, want %q", r.stderr, line+"\n")
	}
	if r.stdout != "" {
		t.Errorf("stdout %q, want it empty", r.stdout)
	}
	if outFile != "" {
		if _, err := os.Stat(outFile); !os.IsNotExist(err) {
			t.Errorf("output file %s exists or cannot be checked (%v), want none", outFile, err)
		}
	}
	if strings.Contains(r.stdout+r.stderr, bearFormerDefaultName) {
		t.Errorf("the former default instance is in the output")
	}
}

// expectNoRequest checks that the listener recorded nothing at all, a proxied
// CONNECT (empty path) included.
func expectNoRequest(t *testing.T, l *bearListener) {
	t.Helper()
	if got := l.recorded(); len(got) != 0 {
		t.Errorf("%d requests recorded, want none: %+v", len(got), got)
	}
}

// expectFirstLine checks the first line of stdout.
func expectFirstLine(t *testing.T, r bearRun, want string) {
	t.Helper()
	first, _, _ := strings.Cut(r.stdout, "\n")
	if first != want {
		t.Errorf("first line %q, want %q", first, want)
	}
}

// bearEnrichArgs is the argument list of one enrich command that reads a BOM from a
// temp file and writes to another one, followed by extra.
func bearEnrichArgs(t *testing.T, command string, bom string, extra ...string) []string {
	t.Helper()
	dir := t.TempDir()
	in := filepath.Join(dir, "input-bom.json")
	if err := os.WriteFile(in, []byte(bom), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"bomutils", command, "-f", in, "-o", filepath.Join(dir, "output-bom.json")}
	return append(args, extra...)
}

// closedLoopbackUri is the URI of a loopback port nothing listens on.
func closedLoopbackUri(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return "http://" + addr
}

func expectExit(t *testing.T, r bearRun, want int) {
	t.Helper()
	if r.code != want {
		t.Fatalf("exit %d, want %d\nstdout: %s\nstderr: %s", r.code, want, r.stdout, r.stderr)
	}
}

func expectNoKeyPrinted(t *testing.T, r bearRun) {
	t.Helper()
	for _, key := range []string{bearKeyFromVariable, bearKeyFromFlag} {
		if strings.Contains(r.stdout, key) || strings.Contains(r.stderr, key) {
			t.Errorf("the key %q is in the output", key)
		}
	}
}

func TestBearEnrichEnvironment(t *testing.T) {
	cli := buildBearCli(t)

	// T-1020: the variable alone sets the key.
	t.Run("T-1020 REARM_BEARAPIKEY alone sends the key", func(t *testing.T) {
		for _, command := range bearEnrichCommands {
			t.Run(command, func(t *testing.T) {
				l := newBearListener(t, http.StatusOK, bearOkBody)
				r := cli.run(t, []string{"REARM_BEARAPIKEY=" + bearKeyFromVariable},
					bearEnrichArgs(t, command, bearOneComponentBom, "--bearUri", l.url)...)
				expectExit(t, r, 0)
				l.oneRequestWithKeys(t, bearKeyFromVariable)
			})
		}
	})

	// T-1021: the flag wins over the variable.
	t.Run("T-1021 the flag wins over REARM_BEARAPIKEY", func(t *testing.T) {
		for _, command := range bearEnrichCommands {
			t.Run(command, func(t *testing.T) {
				l := newBearListener(t, http.StatusOK, bearOkBody)
				r := cli.run(t, []string{"REARM_BEARAPIKEY=" + bearKeyFromVariable},
					bearEnrichArgs(t, command, bearOneComponentBom, "--bearUri", l.url, "--bearApiKey", bearKeyFromFlag)...)
				expectExit(t, r, 0)
				l.oneRequestWithKeys(t, bearKeyFromFlag)
			})
		}
	})

	// T-1022: neither variable nor flag sends no X-API-Key header at all.
	t.Run("T-1022 no variable and no flag sends no header", func(t *testing.T) {
		for _, command := range bearEnrichCommands {
			t.Run(command, func(t *testing.T) {
				l := newBearListener(t, http.StatusOK, bearOkBody)
				r := cli.run(t, nil, bearEnrichArgs(t, command, bearOneComponentBom, "--bearUri", l.url)...)
				expectExit(t, r, 0)
				l.oneRequestWithKeys(t)
			})
		}
	})

	// T-1023: an empty variable counts as not set.
	t.Run("T-1023 an empty REARM_BEARAPIKEY sends no header", func(t *testing.T) {
		for _, command := range bearEnrichCommands {
			t.Run(command, func(t *testing.T) {
				l := newBearListener(t, http.StatusOK, bearOkBody)
				r := cli.run(t, []string{"REARM_BEARAPIKEY="},
					bearEnrichArgs(t, command, bearOneComponentBom, "--bearUri", l.url)...)
				expectExit(t, r, 0)
				l.oneRequestWithKeys(t)
			})
		}
	})

	// T-1024: an empty flag wins over the variable.
	t.Run("T-1024 an empty flag wins over REARM_BEARAPIKEY", func(t *testing.T) {
		l := newBearListener(t, http.StatusOK, bearOkBody)
		r := cli.run(t, []string{"REARM_BEARAPIKEY=" + bearKeyFromVariable},
			bearEnrichArgs(t, "enrich", bearOneComponentBom, "--bearUri", l.url, "--bearApiKey=")...)
		expectExit(t, r, 0)
		l.oneRequestWithKeys(t)
	})

	// T-1025: REARM_BEARURI sets the URI and the flag wins over it. An empty variable
	// is no URI: the run is refused. The BOM has no components, so no request is
	// sent in any outcome.
	t.Run("T-1025 REARM_BEARURI", func(t *testing.T) {
		const fromVariable = "http://127.0.0.1:9/from-variable"
		const fromFlag = "http://127.0.0.1:9/from-flag"
		cases := []struct {
			name  string
			env   []string
			extra []string
			want  string
		}{
			{"variable alone", []string{"REARM_BEARURI=" + fromVariable}, nil, fromVariable},
			{"flag wins", []string{"REARM_BEARURI=" + fromVariable}, []string{"--bearUri", fromFlag}, fromFlag},
		}
		for _, command := range bearEnrichCommands {
			for _, tc := range cases {
				t.Run(command+" "+tc.name, func(t *testing.T) {
					r := cli.run(t, tc.env, bearEnrichArgs(t, command, bearNoComponentsBom, tc.extra...)...)
					expectExit(t, r, 0)
					expectFirstLine(t, r, "Using BEAR at "+tc.want)
				})
			}
			t.Run(command+" empty variable", func(t *testing.T) {
				args, out := bearEnrichArgsOut(t, command, bearNoComponentsBom)
				r := cli.run(t, []string{"REARM_BEARURI="}, args...)
				expectRefused(t, r, bearUriMissingLine, out)
				if strings.Contains(r.stdout+r.stderr, bearFormerDefaultUri) {
					t.Errorf("the former default %q is in the output", bearFormerDefaultUri)
				}
			})
		}
	})

	// T-1026: with debug on, the key is in no output, on success, on a 401 and on a
	// closed port. The listener cases also check that the key was really sent, so
	// its absence from the output is not for want of a key.
	t.Run("T-1026 the key is never printed", func(t *testing.T) {
		withVariable := []string{"REARM_BEARAPIKEY=" + bearKeyFromVariable}

		t.Run("success", func(t *testing.T) {
			l := newBearListener(t, http.StatusOK, bearOkBody)
			r := cli.run(t, withVariable, bearEnrichArgs(t, "enrich", bearOneComponentBom, "-d", "true", "--bearUri", l.url)...)
			expectExit(t, r, 0)
			l.oneRequestWithKeys(t, bearKeyFromVariable)
			expectNoKeyPrinted(t, r)
		})

		t.Run("401", func(t *testing.T) {
			l := newBearListener(t, http.StatusUnauthorized, `{"errors":[{"message":"unauthorized"}]}`)
			r := cli.run(t, withVariable, bearEnrichArgs(t, "enrich", bearOneComponentBom, "-d", "true", "--bearUri", l.url)...)
			expectExit(t, r, 1)
			l.oneRequestWithKeys(t, bearKeyFromVariable)
			expectNoKeyPrinted(t, r)
			if !strings.Contains(r.stdout, "Error during enrichment:") || !strings.Contains(r.stdout, "BEAR API returned status 401") {
				t.Errorf("stdout %q, want the enrichment error with status 401", r.stdout)
			}
		})

		t.Run("closed port", func(t *testing.T) {
			r := cli.run(t, withVariable, bearEnrichArgs(t, "enrich", bearOneComponentBom, "-d", "true", "--bearUri", closedLoopbackUri(t))...)
			expectExit(t, r, 1)
			expectNoKeyPrinted(t, r)
			if !strings.Contains(r.stdout, "Error during enrichment:") || !strings.Contains(r.stdout, "error making request to BEAR") {
				t.Errorf("stdout %q, want the enrichment error of a failed request", r.stdout)
			}
		})

		t.Run("success with the flag", func(t *testing.T) {
			l := newBearListener(t, http.StatusOK, bearOkBody)
			r := cli.run(t, withVariable, bearEnrichArgs(t, "enrich", bearOneComponentBom, "-d", "true", "--bearUri", l.url, "--bearApiKey", bearKeyFromFlag)...)
			expectExit(t, r, 0)
			l.oneRequestWithKeys(t, bearKeyFromFlag)
			expectNoKeyPrinted(t, r)
		})
	})

	// T-1027: help is not refused; the help of --bearApiKey names the variable, the
	// help of --bearUri says it is required, names REARM_BEARURI and shows no
	// default, and a key in the environment is not shown.
	t.Run("T-1027 help names REARM_BEARAPIKEY", func(t *testing.T) {
		for _, command := range bearEnrichCommands {
			t.Run(command, func(t *testing.T) {
				r := cli.run(t, []string{"REARM_BEARAPIKEY=" + bearKeyFromVariable}, "bomutils", command, "--help")
				expectExit(t, r, 0)
				expectNoKeyPrinted(t, r)
				keyLine, uriLine := "", ""
				for _, line := range strings.Split(r.stdout, "\n") {
					if strings.Contains(line, "--bearApiKey") {
						keyLine = line
					}
					if strings.Contains(line, "--bearUri") {
						uriLine = line
					}
				}
				if !strings.Contains(keyLine, "REARM_BEARAPIKEY") {
					t.Errorf("--bearApiKey line %q, want it to name REARM_BEARAPIKEY", keyLine)
				}
				if !strings.Contains(uriLine, "required") || !strings.Contains(uriLine, "REARM_BEARURI") {
					t.Errorf("--bearUri line %q, want it to say required and name REARM_BEARURI", uriLine)
				}
				if strings.Contains(uriLine, "(default") {
					t.Errorf("--bearUri line %q shows a default", uriLine)
				}
				if strings.Contains(r.stdout+r.stderr, bearFormerDefaultName) {
					t.Errorf("the help names the former default instance")
				}
			})
		}
	})

	// T-1300: no flag, no variable, empty HOME: refused with the no-URI line for a
	// BOM to enrich, for a BOM without components (the URI is required at all
	// times) and for a missing input file (the check comes before the BOM is read).
	t.Run("T-1300 no URI is refused", func(t *testing.T) {
		for _, command := range bearEnrichCommands {
			t.Run(command+" one component", func(t *testing.T) {
				args, out := bearEnrichArgsOut(t, command, bearOneComponentBom)
				expectRefused(t, cli.run(t, nil, args...), bearUriMissingLine, out)
			})
			t.Run(command+" no components", func(t *testing.T) {
				args, out := bearEnrichArgsOut(t, command, bearNoComponentsBom)
				expectRefused(t, cli.run(t, nil, args...), bearUriMissingLine, out)
			})
			t.Run(command+" missing input file", func(t *testing.T) {
				args, out := bearMissingInputArgs(t, command)
				expectRefused(t, cli.run(t, nil, args...), bearUriMissingLine, out)
			})
		}
	})

	// T-1301: a key and no URI is refused and nothing is dialled. The proxy
	// variables name the listener, so a default that came back would be recorded
	// there and would not reach a public host; --resilientDns=false selects the
	// client that honours them.
	t.Run("T-1301 a key and no URI sends nothing", func(t *testing.T) {
		proxyEnv := func(l *bearListener) []string {
			return []string{"HTTPS_PROXY=" + l.url, "HTTP_PROXY=" + l.url, "https_proxy=" + l.url, "http_proxy=" + l.url, "NO_PROXY=", "no_proxy="}
		}
		for _, command := range bearEnrichCommands {
			t.Run(command+" key in the variable", func(t *testing.T) {
				l := newBearListener(t, http.StatusOK, bearOkBody)
				args, out := bearEnrichArgsOut(t, command, bearOneComponentBom, "--resilientDns=false")
				r := cli.run(t, append(proxyEnv(l), "REARM_BEARAPIKEY="+bearKeyFromVariable), args...)
				expectNoRequest(t, l)
				expectNoKeyPrinted(t, r)
				expectRefused(t, r, bearUriMissingLine, out)
			})
		}
		t.Run("enrich key in the flag", func(t *testing.T) {
			l := newBearListener(t, http.StatusOK, bearOkBody)
			args, out := bearEnrichArgsOut(t, "enrich", bearOneComponentBom, "--resilientDns=false", "--bearApiKey", bearKeyFromFlag)
			r := cli.run(t, proxyEnv(l), args...)
			expectNoRequest(t, l)
			expectNoKeyPrinted(t, r)
			expectRefused(t, r, bearUriMissingLine, out)
		})
	})

	// T-1302: an empty --bearUri is given, wins over REARM_BEARURI and is no URI.
	t.Run("T-1302 an empty flag is refused", func(t *testing.T) {
		for _, command := range bearEnrichCommands {
			t.Run(command+" --bearUri ''", func(t *testing.T) {
				l := newBearListener(t, http.StatusOK, bearOkBody)
				args, out := bearEnrichArgsOut(t, command, bearOneComponentBom, "--bearUri", "")
				expectRefused(t, cli.run(t, []string{"REARM_BEARURI=" + l.url}, args...), bearUriMissingLine, out)
				expectNoRequest(t, l)
			})
		}
		t.Run("enrich --bearUri=", func(t *testing.T) {
			l := newBearListener(t, http.StatusOK, bearOkBody)
			args, out := bearEnrichArgsOut(t, "enrich", bearOneComponentBom, "--bearUri=")
			expectRefused(t, cli.run(t, []string{"REARM_BEARURI=" + l.url}, args...), bearUriMissingLine, out)
			expectNoRequest(t, l)
		})
	})

	// T-1303: a value that is not an absolute http or https URI with a host is
	// refused with its own line, which holds no part of the value.
	t.Run("T-1303 an unusable URI is refused", func(t *testing.T) {
		const first = "127.0.0.1:9"
		values := []string{
			first,
			"bear.internal",
			"none",
			" ",
			"//127.0.0.1:9",
			"ftp://127.0.0.1:9",
			"file:///tmp/x",
			"http://",
			"http:///graphql",
			"ftp://USERW:PASSW@127.0.0.1:9",
		}
		expectNoValue := func(t *testing.T, r bearRun) {
			t.Helper()
			for _, part := range []string{"USERW", "PASSW", "127.0.0.1"} {
				if strings.Contains(r.stderr, part) {
					t.Errorf("stderr %q holds %q of the value", r.stderr, part)
				}
			}
		}
		for i, value := range values {
			t.Run(fmt.Sprintf("enrich flag %d %q", i, value), func(t *testing.T) {
				args, out := bearEnrichArgsOut(t, "enrich", bearOneComponentBom, "--bearUri", value)
				r := cli.run(t, nil, args...)
				expectRefused(t, r, bearUriInvalidLine, out)
				expectNoValue(t, r)
			})
		}
		for _, command := range bearEnrichCommands[1:] {
			t.Run(command+" flag", func(t *testing.T) {
				args, out := bearEnrichArgsOut(t, command, bearOneComponentBom, "--bearUri", first)
				r := cli.run(t, nil, args...)
				expectRefused(t, r, bearUriInvalidLine, out)
				expectNoValue(t, r)
			})
		}
		for _, command := range bearEnrichCommands {
			t.Run(command+" variable", func(t *testing.T) {
				args, out := bearEnrichArgsOut(t, command, bearOneComponentBom)
				r := cli.run(t, []string{"REARM_BEARURI=" + first}, args...)
				expectRefused(t, r, bearUriInvalidLine, out)
				expectNoValue(t, r)
			})
		}
		t.Run("enrich flag missing input file", func(t *testing.T) {
			args, out := bearMissingInputArgs(t, "enrich", "--bearUri", first)
			r := cli.run(t, nil, args...)
			expectRefused(t, r, bearUriInvalidLine, out)
			expectNoValue(t, r)
		})
	})

	// T-1304: every form of URI that works today still works and is used as given.
	t.Run("T-1304 usable URIs still work", func(t *testing.T) {
		expectSent := func(t *testing.T, r bearRun, l *bearListener, value, path, out string) {
			t.Helper()
			expectExit(t, r, 0)
			got := l.recorded()
			if len(got) != 1 {
				t.Fatalf("%d requests, want 1", len(got))
			}
			if got[0].method != http.MethodPost || got[0].path != path {
				t.Errorf("request %s %s, want POST %s", got[0].method, got[0].path, path)
			}
			expectFirstLine(t, r, "Using BEAR at "+value)
			if _, err := os.Stat(out); err != nil {
				t.Errorf("output file: %v", err)
			}
		}
		forms := []struct {
			name string
			uri  func(l *bearListener) string
			path string
		}{
			{"as it is", func(l *bearListener) string { return l.url }, "/graphql"},
			{"trailing slash", func(l *bearListener) string { return l.url + "/" }, "//graphql"},
			{"path base", func(l *bearListener) string { return l.url + "/base" }, "/base/graphql"},
			{"upper-case scheme", func(l *bearListener) string { return "HTTP://" + strings.TrimPrefix(l.url, "http://") }, "/graphql"},
			{"userinfo", func(l *bearListener) string { return "http://USERW:PASSW@" + strings.TrimPrefix(l.url, "http://") }, "/graphql"},
		}
		for _, form := range forms {
			t.Run("enrich flag "+form.name, func(t *testing.T) {
				l := newBearListener(t, http.StatusOK, bearOkBody)
				value := form.uri(l)
				args, out := bearEnrichArgsOut(t, "enrich", bearOneComponentBom, "--bearUri", value)
				expectSent(t, cli.run(t, nil, args...), l, value, form.path, out)
			})
		}
		for _, command := range bearEnrichCommands {
			t.Run(command+" variable alone", func(t *testing.T) {
				l := newBearListener(t, http.StatusOK, bearOkBody)
				args, out := bearEnrichArgsOut(t, command, bearOneComponentBom)
				expectSent(t, cli.run(t, []string{"REARM_BEARURI=" + l.url}, args...), l, l.url, "/graphql", out)
			})
		}
		t.Run("enrich https closed port", func(t *testing.T) {
			value := "https://" + strings.TrimPrefix(closedLoopbackUri(t), "http://")
			r := cli.run(t, nil, bearEnrichArgs(t, "enrich", bearOneComponentBom, "--bearUri", value)...)
			expectExit(t, r, 1)
			if !strings.Contains(r.stdout, "Error during enrichment") {
				t.Errorf("stdout %q, want the enrichment error, not a refusal", r.stdout)
			}
			if r.stderr == bearUriMissingLine+"\n" || r.stderr == bearUriInvalidLine+"\n" {
				t.Errorf("stderr %q is a refusal", r.stderr)
			}
		})
	})

	// T-1305: a BEARURI line of the configuration file counts as given, and the
	// variable wins over it. The later HOME entry of the case wins over the one run
	// sets (os/exec keeps the last value of a name).
	t.Run("T-1305 the configuration file counts as given", func(t *testing.T) {
		fromFile := newBearListener(t, http.StatusOK, bearOkBody)
		home := t.TempDir()
		if err := os.WriteFile(filepath.Join(home, ".rearm.env"), []byte("BEARURI="+fromFile.url+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Run("file alone", func(t *testing.T) {
			r := cli.run(t, []string{"HOME=" + home}, bearEnrichArgs(t, "enrich", bearOneComponentBom)...)
			expectExit(t, r, 0)
			fromFile.oneRequestWithKeys(t)
			expectFirstLine(t, r, "Using BEAR at "+fromFile.url)
		})
		t.Run("the variable wins over the file", func(t *testing.T) {
			before := len(fromFile.recorded())
			fromVariable := newBearListener(t, http.StatusOK, bearOkBody)
			r := cli.run(t, []string{"HOME=" + home, "REARM_BEARURI=" + fromVariable.url}, bearEnrichArgs(t, "enrich", bearOneComponentBom)...)
			expectExit(t, r, 0)
			fromVariable.oneRequestWithKeys(t)
			if after := len(fromFile.recorded()); after != before {
				t.Errorf("the file's listener got %d more requests, want none", after-before)
			}
			expectFirstLine(t, r, "Using BEAR at "+fromVariable.url)
		})
	})
}
