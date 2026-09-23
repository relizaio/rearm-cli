package cmd

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

// clearToolEnv makes each case start outside any agent tool, whatever the machine running the
// tests happens to be -- these tests are themselves often run by Claude Code.
func clearToolEnv(t *testing.T) {
	t.Helper()
	t.Setenv("CLAUDECODE", "")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("CLAUDE_SESSION_ID", "")
}

func TestResolveProviderSession(t *testing.T) {
	const local = "1cc6d359-2923-5bac-ae54-bc1d21fafe7f"
	const remote = "session_01Ai6MkzwLWUET7yyXAm4z9p"
	cases := []struct {
		name    string
		env     map[string]string
		opts    providerSessionOpts
		want    map[string]interface{}
		wantErr bool
	}{
		{name: "outside any tool, nothing to send, not an error"},
		{name: "outside any tool, strict fails", opts: providerSessionOpts{require: true}, wantErr: true},
		{name: "claude code env is picked up",
			env:  map[string]string{"CLAUDE_CODE_SESSION_ID": local},
			want: map[string]interface{}{"provider": "claude-code", "id": local}},
		{name: "strict is satisfied by the env",
			env:  map[string]string{"CLAUDE_CODE_SESSION_ID": local},
			opts: providerSessionOpts{require: true},
			want: map[string]interface{}{"provider": "claude-code", "id": local}},
		{name: "the agent adds the remote id",
			env:  map[string]string{"CLAUDE_CODE_SESSION_ID": local},
			opts: providerSessionOpts{remoteId: remote},
			want: map[string]interface{}{"provider": "claude-code", "id": local, "remoteId": remote}},
		{name: "opt-out sends nothing even under claude code",
			env:  map[string]string{"CLAUDE_CODE_SESSION_ID": local},
			opts: providerSessionOpts{optOut: true}},
		{name: "opt-out and strict contradict", opts: providerSessionOpts{optOut: true, require: true}, wantErr: true},
		{name: "explicit flags win over the env",
			env:  map[string]string{"CLAUDE_CODE_SESSION_ID": local},
			opts: providerSessionOpts{provider: "cursor", id: "abc"},
			want: map[string]interface{}{"provider": "cursor", "id": "abc"}},
		{name: "an id outside any tool must name its tool", opts: providerSessionOpts{id: "abc"}, wantErr: true},
		{name: "an id under claude code defaults the tool",
			env:  map[string]string{"CLAUDECODE": "1"},
			opts: providerSessionOpts{id: local},
			want: map[string]interface{}{"provider": "claude-code", "id": local}},
		{name: "a remote id alone is refused", opts: providerSessionOpts{remoteId: remote}, wantErr: true},
		{name: "a provider named with no id is refused", opts: providerSessionOpts{provider: "cursor"}, wantErr: true},
		{name: "another tool never borrows claude code's id",
			env:     map[string]string{"CLAUDE_CODE_SESSION_ID": local},
			opts:    providerSessionOpts{provider: "cursor"},
			wantErr: true},
		{name: "another tool never borrows the legacy claude flag",
			opts:    providerSessionOpts{provider: "cursor", legacyClaudeId: local},
			wantErr: true},
		{name: "naming claude code explicitly still uses its env",
			env:  map[string]string{"CLAUDE_CODE_SESSION_ID": local},
			opts: providerSessionOpts{provider: "Claude-Code"},
			want: map[string]interface{}{"provider": "claude-code", "id": local}},
		{name: "the legacy claude flag still works",
			opts: providerSessionOpts{legacyClaudeId: local},
			want: map[string]interface{}{"provider": "claude-code", "id": local}},
		{name: "the older env spelling still works",
			env:  map[string]string{"CLAUDE_SESSION_ID": local},
			want: map[string]interface{}{"provider": "claude-code", "id": local}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clearToolEnv(t)
			for k, v := range c.env {
				t.Setenv(k, v)
			}
			got, err := resolveProviderSession(c.opts)
			if c.wantErr {
				if err == nil {
					t.Fatalf("want an error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if c.want == nil {
				if got != nil {
					t.Fatalf("want nothing sent, got %v", got)
				}
				return
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestStrictFailureNamesTheWayOut(t *testing.T) {
	clearToolEnv(t)
	_, err := resolveProviderSession(providerSessionOpts{require: true})
	if !errors.Is(err, errNoProviderSession) {
		t.Fatalf("want errNoProviderSession, got %v", err)
	}
}

func TestSessionDeviceInput(t *testing.T) {
	d := sessionDeviceInput(false)
	for _, k := range []string{"os", "timeZone", "client"} {
		if v, _ := d[k].(string); v == "" {
			t.Fatalf("want %s reported, got %v", k, d)
		}
	}
	if !strings.HasPrefix(d["client"].(string), "rearm-cli ") {
		t.Fatalf("client should name the CLI, got %q", d["client"])
	}
	if h, err := os.Hostname(); err == nil && h != "" && d["hostname"] != h {
		t.Fatalf("want hostname %q, got %v", h, d["hostname"])
	}
	if sessionDeviceInput(true) != nil {
		t.Fatal("--no-device-info must send nothing")
	}
}
