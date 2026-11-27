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

const (
	bearKeyFromVariable = "not-a-real-key-from-the-variable"
	bearKeyFromFlag     = "not-a-real-key-from-the-flag"
	bearDefaultUri      = "https://beardemo.rearmhq.com"
	bearOkBody          = `{"data":{"enrichBatch":[]}}`
)

var bearEnrichCommands = []string{"enrich", "enrichsupplier", "enrichlicense"}

// One component with a purl and no supplier, licenses or copyright: each of the
// three commands sends exactly one request for it.
const bearOneComponentBom = `{"bomFormat":"CycloneDX","specVersion":"1.6","version":1,` +
	`"components":[{"type":"library","name":"left-pad","version":"1.3.0","purl":"pkg:npm/left-pad@1.3.0"}]}`

// No components: every command prints the URI and returns before any request.
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

	// T-1025: REARM_BEARURI sets the URI, the flag wins over it, an empty one leaves
	// the default. The BOM has no components, so no request is sent in any outcome.
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
			{"empty variable", []string{"REARM_BEARURI="}, nil, bearDefaultUri},
		}
		for _, command := range bearEnrichCommands {
			for _, tc := range cases {
				t.Run(command+" "+tc.name, func(t *testing.T) {
					r := cli.run(t, tc.env, bearEnrichArgs(t, command, bearNoComponentsBom, tc.extra...)...)
					expectExit(t, r, 0)
					first, _, _ := strings.Cut(r.stdout, "\n")
					if first != "Using BEAR at "+tc.want {
						t.Errorf("first line %q, want %q", first, "Using BEAR at "+tc.want)
					}
				})
			}
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

	// T-1027: the help of --bearApiKey names the variable, the help of --bearUri is
	// unchanged, and a key in the environment is not shown.
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
				if !strings.Contains(uriLine, `(default "`+bearDefaultUri+`")`) {
					t.Errorf("--bearUri line %q, want its default %q", uriLine, bearDefaultUri)
				}
			})
		}
	})
}
