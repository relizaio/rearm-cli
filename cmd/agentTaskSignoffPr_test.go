package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"sync"
	"testing"

	rearm "github.com/relizaio/rearm-client-go"
)

// task signoff --pr (task RD5-5): each PR is linked before the sign-off is sent, in order; the first refused
// link stops the command before any sign-off; a url the task already links is not an error; the compact lines
// name the linked PRs and --json carries them.

// prBoard records every call in order and refuses the link of one url.
type prBoard struct {
	mu      sync.Mutex
	calls   []string
	linked  map[string]bool
	refuses string
}

func (b *prBoard) serve() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		b.mu.Lock()
		defer b.mu.Unlock()
		task := map[string]any{"uuid": "t-1", "key": "RD-1", "status": "QUEUED", "role": "tester"}
		data := map[string]any{}
		switch {
		case strings.Contains(req.Query, "agentTaskLinkPrProgrammatic("):
			u, _ := req.Variables["prUrl"].(string)
			b.calls = append(b.calls, "link "+u)
			if u == b.refuses {
				_ = json.NewEncoder(w).Encode(map[string]any{"errors": []any{map[string]any{"message": "Task RD-1 is an investigation: it links no PRs"}},
					"data": map[string]any{"agentTaskLinkPrProgrammatic": nil}})
				return
			}
			if b.linked == nil {
				b.linked = map[string]bool{}
			}
			b.linked[u] = true
			data["agentTaskLinkPrProgrammatic"] = task
		case strings.Contains(req.Query, "agentTaskSignOffProgrammatic("):
			b.calls = append(b.calls, "signoff")
			data["agentTaskSignOffProgrammatic"] = task
		default:
			b.calls = append(b.calls, "other")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
}

func (b *prBoard) seen() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.calls...)
}

func prWorld(t *testing.T, b *prBoard, prs ...string) {
	t.Helper()
	withStateDir(t)
	srv := b.serve()
	t.Cleanup(srv.Close)
	c, err := rearm.New(srv.URL, "id", "secret", rearm.WithoutTokenExchange())
	if err != nil {
		t.Fatal(err)
	}
	apiClient = c
	taskSessionUuid, taskOutcome, taskSignoffPrs = "s-1", "PASSED", prs
	t.Cleanup(func() {
		apiClient, compactJson, taskSessionUuid, taskOutcome, taskSignoffPrs = nil, false, "", "", nil
	})
}

const (
	prOne   = "https://github.com/acme/x/pull/1"
	prTwo   = "https://github.com/acme/y/pull/2"
	prThree = "https://github.com/acme/z/pull/3"
)

func TestSignOffLinksEachPrInOrderBeforeTheSignOff(t *testing.T) {
	b := &prBoard{}
	prWorld(t, b, prOne, prTwo)
	out := runCmd(t, agentTaskSignoffCmd, "t-1")
	if got, want := b.seen(), []string{"link " + prOne, "link " + prTwo, "signoff"}; !reflect.DeepEqual(got, want) {
		t.Errorf("calls %v, want %v", got, want)
	}
	if want := "RD-1 queued, role tester\nqueued for tester\nlinked PRs: " + prOne + ", " + prTwo; out != want {
		t.Errorf("printed\n%s\nwant\n%s", out, want)
	}
}

func TestSignOffWithoutPrLinksNothingAndPrintsAsBefore(t *testing.T) {
	b := &prBoard{}
	prWorld(t, b)
	out := runCmd(t, agentTaskSignoffCmd, "t-1")
	if got := b.seen(); !reflect.DeepEqual(got, []string{"signoff"}) {
		t.Errorf("calls %v", got)
	}
	if out != "RD-1 queued, role tester\nqueued for tester" {
		t.Errorf("printed %q", out)
	}
}

func TestSignOffPrAsJsonCarriesTheLinkedPrs(t *testing.T) {
	b := &prBoard{}
	prWorld(t, b, prOne, prTwo)
	compactJson = true
	var got map[string]any
	if err := json.Unmarshal([]byte(runCmd(t, agentTaskSignoffCmd, "t-1")), &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got["linkedPrs"], []any{prOne, prTwo}) || got["status"] != "QUEUED" || got["key"] != "RD-1" {
		t.Errorf("--json: %v", got)
	}
}

func TestAnAlreadyLinkedPrIsNotAnError(t *testing.T) {
	b := &prBoard{linked: map[string]bool{prOne: true}}
	prWorld(t, b, prOne)
	out := runCmd(t, agentTaskSignoffCmd, "t-1")
	if got := b.seen(); !reflect.DeepEqual(got, []string{"link " + prOne, "signoff"}) {
		t.Errorf("calls %v", got)
	}
	if !strings.HasSuffix(out, "linked PRs: "+prOne) {
		t.Errorf("printed %q", out)
	}
}

func TestARefusedLinkStopsTheLinksAfterIt(t *testing.T) {
	b := &prBoard{refuses: prTwo}
	prWorld(t, b)
	linked, err := linkSignOffPrs("t-1", []string{prOne, prTwo, prThree})
	if err == nil {
		t.Fatal("a refused link returned no error")
	}
	if !reflect.DeepEqual(linked, []string{prOne}) {
		t.Errorf("linked %v", linked)
	}
	if got := b.seen(); !reflect.DeepEqual(got, []string{"link " + prOne, "link " + prTwo}) {
		t.Errorf("calls %v: nothing is tried after the refusal", got)
	}
	for _, want := range []string{"linking " + prTwo + " was refused", "the sign-off was not sent", "linked before it: " + prOne,
		"it links no PRs"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal %q lacks %q", err, want)
		}
	}
}

// The whole command: the refusal reaches stderr, the command exits 1, and no sign-off is sent.
func TestARefusedLinkStopsTheSignOff(t *testing.T) {
	if url := os.Getenv("REARM_TEST_SIGNOFF_PR_CHILD"); url != "" {
		c, err := rearm.New(url, "id", "secret", rearm.WithoutTokenExchange())
		if err != nil {
			os.Exit(3)
		}
		apiClient = c
		taskSessionUuid, taskOutcome, taskSignoffPrs = "s-1", "PASSED", []string{prOne, prTwo, prThree}
		agentTaskSignoffCmd.Run(agentTaskSignoffCmd, []string{"t-1"})
		os.Exit(0)
	}
	b := &prBoard{refuses: prTwo}
	srv := b.serve()
	defer srv.Close()
	child := exec.Command(os.Args[0], "-test.run=^TestARefusedLinkStopsTheSignOff$")
	child.Env = append(os.Environ(), "REARM_TEST_SIGNOFF_PR_CHILD="+srv.URL, "XDG_STATE_HOME="+t.TempDir())
	var stdout, stderr strings.Builder
	child.Stdout, child.Stderr = &stdout, &stderr
	err := child.Run()
	if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 1 {
		t.Fatalf("a refused link should exit 1, got %v\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
	}
	if got := b.seen(); !reflect.DeepEqual(got, []string{"link " + prOne, "link " + prTwo}) {
		t.Errorf("calls %v: the sign-off and the third link must not be sent", got)
	}
	if !strings.Contains(stderr.String(), "linking "+prTwo+" was refused, so the sign-off was not sent") {
		t.Errorf("the refusal is not on stderr: %q", stderr.String())
	}
	if strings.Contains(stdout.String(), "queued") {
		t.Errorf("stdout shows a sign-off: %q", stdout.String())
	}
}

func TestPrIsRepeatableAndKeepsEachUrlWhole(t *testing.T) {
	f := agentTaskSignoffCmd.PersistentFlags().Lookup("pr")
	if f == nil {
		t.Fatal("task signoff has no --pr")
	}
	t.Cleanup(func() { taskSignoffPrs = nil; f.Changed = false })
	odd := "https://example.com/pr?a=1,b=2"
	for _, u := range []string{prOne, odd} {
		if err := f.Value.Set(u); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(taskSignoffPrs, []string{prOne, odd}) {
		t.Errorf("--pr twice gave %v", taskSignoffPrs)
	}
}
