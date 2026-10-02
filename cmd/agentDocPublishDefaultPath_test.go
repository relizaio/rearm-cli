package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
)

// doc publish without --file in the version case (task RD5-6): when this hop published the newest round of the
// type on the task, the default file is that round's path and the publish lands as a new version of it, saying
// so; otherwise the board's documentPath names the file, as before; --file always wins and says nothing extra.

const (
	dpNotes1 = "boards/x/impl/RD-1/notes-1.md"
	dpNotes2 = "boards/x/impl/RD-1/notes-2.md"
	dpArch1  = "boards/x/design/RD-1/architecture-1.md"
	dpQuest1 = "boards/x/questions/RD-1/round-1.md"
)

// dpBoard answers the reads and the publish a doc publish makes, and records the documentPath asks and the path
// each publish sent.
type dpBoard struct {
	mu        sync.Mutex
	repo      string
	docs      []any
	pathAsks  int
	taskReads int
	published []string
	// taskFails makes the task read fail: "error" answers a GraphQL error, "missing" answers no task.
	taskFails string
}

func (b *dpBoard) serve() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		b.mu.Lock()
		defer b.mu.Unlock()
		data := map[string]any{}
		switch q := req.Query; {
		case strings.Contains(q, "query AgentDocumentPath "):
			b.pathAsks++
			if spec := str(req.Variables["specification"]); spec != "DETAILED_DESIGN" {
				// Only ARCHITECTURE is published as another type here, always as its first round.
				if spec != "ARCHITECTURE" {
					panic("documentPath asked for " + spec)
				}
				data["agentBoardProgrammatic"] = map[string]any{"uuid": "b-1", "documentPath": dpArch1}
				break
			}
			// The server's answer: the next round, one more than the task's rounds of the type.
			n := 0
			for _, d := range b.docs {
				doc, _ := d.(map[string]any)["document"].(map[string]any)
				if doc["specification"] == "DETAILED_DESIGN" && doc["round"].(int) > n {
					n = doc["round"].(int)
				}
			}
			data["agentBoardProgrammatic"] = map[string]any{"uuid": "b-1",
				"documentPath": "boards/x/impl/RD-1/notes-" + string(rune('1'+n)) + ".md"}
		case strings.Contains(q, "query AgentTaskProgrammatic "):
			b.taskReads++
			if b.taskFails == "error" {
				_ = json.NewEncoder(w).Encode(map[string]any{"errors": []any{map[string]any{"message": "boom"}}})
				return
			}
			if b.taskFails == "missing" {
				break
			}
			data["agentTaskProgrammatic"] = map[string]any{"uuid": "t-1", "key": "RD-1", "board": "b-1",
				"status": "ASSIGNED", "role": "coder", "documents": b.docs}
		case strings.Contains(q, "query AgentBoardProgrammatic "):
			data["agentBoardProgrammatic"] = map[string]any{"uuid": "b-1", "documentsRoot": "boards/x/",
				"documentsRepo": map[string]any{"uri": b.repo},
				"documentPaths": map[string]any{"DETAILED_DESIGN": "impl/{key}/notes-{round}.md",
					"ARCHITECTURE": "design/{key}/architecture-{round}.md"}}
		case strings.Contains(q, "agentDocumentPublishProgrammatic("):
			in, _ := req.Variables["input"].(map[string]any)
			b.published = append(b.published, str(in["path"]))
			data["agentDocumentPublishProgrammatic"] = map[string]any{"uuid": "r-new", "version": "2",
				"lifecycle": "DRAFT", "document": map[string]any{"specification": "DETAILED_DESIGN", "round": 1}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
}

func dpDoc(uuid string, round int, path string, advisory bool) map[string]any {
	return map[string]any{"uuid": uuid, "version": "1", "lifecycle": "DRAFT",
		"document": map[string]any{"specification": "DETAILED_DESIGN", "round": round, "path": path, "task": "t-1",
			"advisory": advisory}}
}

// dpWorld is a pushed documents checkout holding both notes files and the first architecture round, a session whose current hop on t-1 published
// hopPublished, and a board whose task carries docs.
func dpWorld(t *testing.T, docs []any, hopPublished ...string) *dpBoard {
	t.Helper()
	withStateDir(t)
	st := &agentSessionState{SessionUuid: "s-1", ClientSessionId: "c-1",
		HopOutputs: map[string]*hopOutputs{"t-1": {AssignedAt: "2026-10-02T00:00:00Z", Outputs: hopPublished}}}
	if err := writeAgentState(st); err != nil {
		t.Fatal(err)
	}
	bare := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", "--bare", bare).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v %s", err, out)
	}
	dir := newRepo(t, bare)
	commitFile(t, dir, dpNotes1, "# notes 1\n")
	commitFile(t, dir, dpNotes2, "# notes 2\n")
	commitFile(t, dir, dpArch1, "# architecture 1\n")
	for _, args := range [][]string{
		{"config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*"},
		{"push", "-q", "origin", "HEAD:refs/heads/main"},
		{"fetch", "-q", "origin"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	b := &dpBoard{repo: bare, docs: docs}
	srv := b.serve()
	t.Cleanup(srv.Close)
	useFake(t, srv)
	docSession, docType, docTask, docRepoPath = "s-1", "DETAILED_DESIGN", "t-1", dir
	t.Cleanup(func() {
		docSession, docType, docTask, docFile, docRepoPath, compactJson, docDryRun = "", "", "", "", "", false, false
	})
	return b
}

// dpPublish runs the publish and returns what it printed on stdout and stderr.
func dpPublish(t *testing.T) (string, string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stderr
	os.Stderr = w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	out := stdoutOf(t, func() {
		if err := runDocPublish(); err != nil {
			t.Errorf("publish: %v", err)
		}
	})
	w.Close()
	os.Stderr = saved
	return out, <-done
}

func (b *dpBoard) only(t *testing.T, wantPath string, wantAsks int) {
	t.Helper()
	if len(b.published) != 1 || b.published[0] != wantPath {
		t.Errorf("published %v, want one publish at %s", b.published, wantPath)
	}
	if b.pathAsks != wantAsks {
		t.Errorf("documentPath asked %d times, want %d", b.pathAsks, wantAsks)
	}
}

// The version case: this hop published round 1, so the default is round 1's path, never the next round's, and
// the publish goes there as a new version.
func TestARepublishInTheSameHopDefaultsToTheHopsOwnPath(t *testing.T) {
	b := dpWorld(t, []any{dpDoc("r-n1", 1, dpNotes1, false)}, "r-n1")
	out, _ := dpPublish(t)
	b.only(t, dpNotes1, 0)
	if !strings.Contains(out, "published DETAILED_DESIGN v2 (draft); release r-new") {
		t.Errorf("the publish lands as a new version, got:\n%s", out)
	}
}

// The compact line names the path it took and why, before the publish line.
func TestTheVersionCaseSaysWhichPathAndWhy(t *testing.T) {
	dpWorld(t, []any{dpDoc("r-n1", 1, dpNotes1, false)}, "r-n1")
	out, _ := dpPublish(t)
	want := "republishing " + dpNotes1 + " as a new version of round 1, published in this hop\n" +
		"published DETAILED_DESIGN v2 (draft); release r-new\n"
	if out != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}
}

// With --json the line goes to stderr, so stdout stays the release JSON a script parses.
func TestTheVersionLineKeepsJsonOnStdout(t *testing.T) {
	dpWorld(t, []any{dpDoc("r-n1", 1, dpNotes1, false)}, "r-n1")
	compactJson = true
	out, errOut := dpPublish(t)
	var rel map[string]any
	if err := json.Unmarshal([]byte(out), &rel); err != nil || rel["uuid"] != "r-new" {
		t.Errorf("stdout is the release JSON, got %q (%v)", out, err)
	}
	if !strings.Contains(errOut, "republishing "+dpNotes1+" as a new version of round 1") {
		t.Errorf("the line goes to stderr, got %q", errOut)
	}
}

// With --dry-run the line goes to stderr too, so stdout stays the input JSON a script parses, naming the hop's
// path; nothing is published (T-1 of RD5-6 run 1).
func TestTheVersionLineKeepsDryRunStdoutToTheInput(t *testing.T) {
	b := dpWorld(t, []any{dpDoc("r-n1", 1, dpNotes1, false)}, "r-n1")
	docDryRun = true
	out, errOut := dpPublish(t)
	var in map[string]any
	if err := json.Unmarshal([]byte(out), &in); err != nil || in["path"] != dpNotes1 {
		t.Errorf("stdout is the input JSON with path %s, got %q (%v)", dpNotes1, out, err)
	}
	if !strings.Contains(errOut, "republishing "+dpNotes1+" as a new version of round 1") {
		t.Errorf("the line goes to stderr, got %q", errOut)
	}
	if len(b.published) != 0 || b.pathAsks != 0 {
		t.Errorf("a dry run publishes nothing and asks no path, got %v and %d asks", b.published, b.pathAsks)
	}
}

// A first publish of the type in the hop asks the board, even when the hop published another type already. The
// other type's round has a path, so taking it as the version case would publish there.
func TestAFirstPublishOfATypeAsksTheBoard(t *testing.T) {
	for name, hop := range map[string][]string{"nothing published": nil, "another type published": {"r-q1"}} {
		t.Run(name, func(t *testing.T) {
			b := dpWorld(t, []any{map[string]any{"uuid": "r-q1", "version": "1", "lifecycle": "DRAFT",
				"document": map[string]any{"specification": "BOARD_QUESTIONS", "round": 1, "task": "t-1",
					"path": dpQuest1}}}, hop...)
			out, _ := dpPublish(t)
			b.only(t, dpNotes1, 1)
			if strings.Contains(out, "republishing") {
				t.Errorf("a new round says nothing extra, got:\n%s", out)
			}
		})
	}
}

// The other direction (T-2 of RD5-6 run 1): the hop published DETAILED_DESIGN round 1, and the publish is of
// another type. The requested type has no round this hop published, so the board names the file; the note's path
// is never taken for it.
func TestARoundOfAnotherTypeIsNotTheVersionCase(t *testing.T) {
	b := dpWorld(t, []any{dpDoc("r-n1", 1, dpNotes1, false)}, "r-n1")
	docType = "ARCHITECTURE"
	out, errOut := dpPublish(t)
	b.only(t, dpArch1, 1)
	if strings.Contains(out+errOut, "republishing") {
		t.Errorf("a first round of the type says nothing extra, got:\n%s%s", out, errOut)
	}
}

// A task read that fails, or finds no task, stops the publish with the way round it, rather than falling back to
// the board's next round, which would quietly name the wrong file in the version case.
func TestAFailedTaskReadAsksForAFile(t *testing.T) {
	for _, mode := range []string{"error", "missing"} {
		t.Run(mode, func(t *testing.T) {
			b := dpWorld(t, []any{dpDoc("r-n1", 1, dpNotes1, false)}, "r-n1")
			b.taskFails = mode
			_, ok, err := hopVersionRound(map[string]any{"uuid": "b-1"}, "DETAILED_DESIGN")
			if ok || err == nil || !strings.HasSuffix(err.Error(), "pass --file") {
				t.Errorf("want an error naming --file, got %v %v", ok, err)
			}
		})
	}
}

// With nothing published in the hop, the task is not read for its rounds: there is no version case to find.
func TestNothingPublishedInTheHopReadsNoRounds(t *testing.T) {
	b := dpWorld(t, []any{dpDoc("r-n1", 1, dpNotes1, false)})
	if _, ok, err := hopVersionRound(map[string]any{"uuid": "b-1"}, "DETAILED_DESIGN"); ok || err != nil {
		t.Errorf("no version case, got %v %v", ok, err)
	}
	if b.taskReads != 0 {
		t.Errorf("the task was read %d times", b.taskReads)
	}
}

// A later hop: round 1 came from an earlier hop of the session (the assignment started a new record), so the
// publish is round 2 and the board names it.
func TestALaterHopAsksTheBoard(t *testing.T) {
	b := dpWorld(t, []any{dpDoc("r-n1", 1, dpNotes1, false)}, "r-q2")
	out, _ := dpPublish(t)
	b.only(t, dpNotes2, 1)
	if strings.Contains(out, "republishing") {
		t.Errorf("a new round says nothing extra, got:\n%s", out)
	}
}

// The hop published round 1, but round 2 of the type is newer: a republish of round 1 is not the version case.
func TestAnOlderRoundThisHopPublishedIsNotTheVersionCase(t *testing.T) {
	dpWorld(t, []any{dpDoc("r-n2", 2, dpNotes2, false), dpDoc("r-n1", 1, dpNotes1, false)}, "r-n1")
	if _, ok, err := hopVersionRound(map[string]any{"uuid": "b-1"}, "DETAILED_DESIGN"); ok || err != nil {
		t.Errorf("round 2 is newer, so no version case, got %v %v", ok, err)
	}
}

// An advisory round of the type by this session does not count.
func TestAnAdvisoryRoundIsNotTheVersionCase(t *testing.T) {
	dpWorld(t, []any{dpDoc("r-n1", 1, dpNotes1, true)}, "r-n1")
	if _, ok, err := hopVersionRound(map[string]any{"uuid": "b-1"}, "DETAILED_DESIGN"); ok || err != nil {
		t.Errorf("an advisory round is no version case, got %v %v", ok, err)
	}
}

// --file wins over the version case and over the board, and prints nothing extra.
func TestAFileWinsOverTheVersionCase(t *testing.T) {
	b := dpWorld(t, []any{dpDoc("r-n1", 1, dpNotes1, false)}, "r-n1")
	docFile = dpNotes2
	out, _ := dpPublish(t)
	b.only(t, dpNotes2, 0)
	if b.taskReads != 1 {
		// The one read is boardOfSession's, for the board; --file needs no rounds.
		t.Errorf("the task was read %d times, want 1", b.taskReads)
	}
	if strings.Contains(out, "republishing") {
		t.Errorf("--file says nothing extra, got:\n%s", out)
	}
}
