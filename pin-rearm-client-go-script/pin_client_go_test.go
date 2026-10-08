package pinscript

// Tests for `make pin-client-go` (task RD4-8). They run the repository's
// Makefile and pin-rearm-client-go-script/pin-client-go.sh in a throwaway consumer module against a
// throwaway rearm-client-go repository: git's url.insteadOf sends
// https://github.com/relizaio/rearm-client-go to a local repository, so go get
// resolves real pseudo-versions without the network. They need git and make,
// and skip without them (the golang:alpine build stage has neither).

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const clientGo = "github.com/relizaio/rearm-client-go"

type pinFixture struct {
	t        *testing.T
	env      []string
	upstream string // the fake rearm-client-go repository
	consumer string // the module whose go.mod is pinned
	commits  map[string]string
}

func run(t *testing.T, dir string, env []string, name string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (f *pinFixture) must(dir string, name string, args ...string) string {
	f.t.Helper()
	out, err := run(f.t, dir, f.env, name, args...)
	if err != nil {
		f.t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
	return out
}

func (f *pinFixture) write(path, content string) {
	f.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *pinFixture) read(name string) string {
	f.t.Helper()
	b, err := os.ReadFile(filepath.Join(f.consumer, name))
	if err != nil {
		f.t.Fatal(err)
	}
	return string(b)
}

// commitUpstream commits one change to the fake client-go at a fixed date, so
// its pseudo-version is the same on every run.
func (f *pinFixture) commitUpstream(label, date string) string {
	f.t.Helper()
	f.write(filepath.Join(f.upstream, "client.go"), "package client\n\nconst Label = \""+label+"\"\n")
	f.must(f.upstream, "git", "add", "-A")
	cmd := exec.Command("git", "commit", "-q", "-m", label)
	cmd.Dir = f.upstream
	cmd.Env = append(append([]string{}, f.env...), "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
	if out, err := cmd.CombinedOutput(); err != nil {
		f.t.Fatalf("commit %s: %v\n%s", label, err, out)
	}
	sha := strings.TrimSpace(f.must(f.upstream, "git", "rev-parse", "HEAD"))
	f.commits[label] = sha
	return sha
}

func newPinFixture(t *testing.T) *pinFixture {
	for _, tool := range []string{"git", "make", "go"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not installed", tool)
		}
	}
	root := t.TempDir()
	f := &pinFixture{t: t, upstream: filepath.Join(root, "client-go"), consumer: filepath.Join(root, "consumer"), commits: map[string]string{}}

	gitconfig := filepath.Join(root, "gitconfig")
	f.write(gitconfig, "[user]\n\tname = pin test\n\temail = pin@example.com\n"+
		"[init]\n\tdefaultBranch = main\n"+
		"[protocol \"file\"]\n\tallow = always\n"+
		"[commit]\n\tgpgsign = false\n"+
		"[url \"file://"+f.upstream+"\"]\n\tinsteadOf = https://"+clientGo+"\n")
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(k, "GO") || strings.HasPrefix(k, "GIT_") || k == "HOME" {
			continue
		}
		f.env = append(f.env, kv)
	}
	f.env = append(f.env,
		"HOME="+root,
		"GIT_CONFIG_GLOBAL="+gitconfig,
		"GIT_CONFIG_NOSYSTEM=1",
		"GOMODCACHE="+filepath.Join(root, "modcache"),
		"GOCACHE="+filepath.Join(root, "gocache"),
		"GOFLAGS=-modcacherw",
		"GOPROXY=off",
		"GOPRIVATE="+clientGo, // the check fetches through the proxy unless told otherwise
		"GOTOOLCHAIN=local",
	)

	f.must(root, "git", "init", "-q", f.upstream)
	f.write(filepath.Join(f.upstream, "go.mod"), "module "+clientGo+"\n\ngo 1.22\n")
	f.commitUpstream("one", "2026-01-02T03:04:05Z")
	f.must(f.upstream, "git", "tag", "v0.1.0")
	f.commitUpstream("two", "2026-02-03T04:05:06Z")
	f.must(f.upstream, "git", "checkout", "-q", "-b", "feature")
	f.commitUpstream("three", "2026-03-04T05:06:07Z")
	f.must(f.upstream, "git", "checkout", "-q", "main")

	// The consumer carries this repository's Makefile and script, as a consumer repository does.
	f.must(root, "git", "init", "-q", f.consumer)
	makefile, err := os.ReadFile("../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	script, err := os.ReadFile("pin-client-go.sh")
	if err != nil {
		t.Fatal(err)
	}
	f.write(filepath.Join(f.consumer, "Makefile"), string(makefile))
	f.write(filepath.Join(f.consumer, "pin-rearm-client-go-script", "pin-client-go.sh"), string(script))
	f.write(filepath.Join(f.consumer, "go.mod"), "module example.com/consumer\n\ngo 1.22\n")
	f.write(filepath.Join(f.consumer, "go.sum"), "")
	f.write(filepath.Join(f.consumer, "main.go"), "package main\n\nimport client \""+clientGo+"\"\n\nfunc main() { println(client.Label) }\n")
	f.must(f.consumer, "git", "add", "-A")
	f.must(f.consumer, "git", "commit", "-q", "-m", "consumer")
	return f
}

// pin runs the target and returns its output; wantErr says whether it must fail.
func (f *pinFixture) pin(wantErr bool, args ...string) string {
	f.t.Helper()
	out, err := run(f.t, f.consumer, f.env, "make", append([]string{"-s"}, args...)...)
	if wantErr && err == nil {
		f.t.Fatalf("make %s should fail\n%s", strings.Join(args, " "), out)
	}
	if !wantErr && err != nil {
		f.t.Fatalf("make %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

func (f *pinFixture) required() string {
	f.t.Helper()
	m := regexp.MustCompile(`(?m)^\s*(?:require\s+)?` + regexp.QuoteMeta(clientGo) + `\s+(\S+)`).FindStringSubmatch(f.read("go.mod"))
	if m == nil {
		f.t.Fatalf("go.mod requires no %s:\n%s", clientGo, f.read("go.mod"))
	}
	return m[1]
}

func (f *pinFixture) commitConsumer(msg string) {
	f.t.Helper()
	f.must(f.consumer, "git", "add", "-A")
	f.must(f.consumer, "git", "commit", "-q", "-m", msg)
}

func TestPinClientGoPinsTheGivenCommitAndIsIdempotent(t *testing.T) {
	f := newPinFixture(t)

	out := f.pin(false, "pin-client-go", "REF="+f.commits["two"])
	want := "v0.1.1-0.20260203040506-" + f.commits["two"][:12]
	if got := f.required(); got != want {
		t.Fatalf("pinned %s, want %s", got, want)
	}
	if !strings.Contains(out, "rearm-client-go: none -> "+want) {
		t.Fatalf("the output should name the old and the new version:\n%s", out)
	}
	if !strings.Contains(f.read("go.sum"), clientGo+" "+want+" h1:") {
		t.Fatalf("go.sum should carry the new version:\n%s", f.read("go.sum"))
	}
	f.commitConsumer("pin two")
	mod, sum := f.read("go.mod"), f.read("go.sum")

	// The same REF again changes nothing, byte for byte.
	out = f.pin(false, "pin-client-go", "REF="+f.commits["two"])
	if f.read("go.mod") != mod || f.read("go.sum") != sum {
		t.Fatalf("a second run with the same REF changed the module files")
	}
	if !strings.Contains(out, want+" (unchanged)") {
		t.Fatalf("a second run should say the pin is unchanged:\n%s", out)
	}
	if st := f.must(f.consumer, "git", "status", "--porcelain"); st != "" {
		t.Fatalf("a second run left changes:\n%s", st)
	}

	// A short commit resolves to the same pin.
	f.pin(false, "pin-client-go", "REF="+f.commits["two"][:9])
	if f.read("go.mod") != mod || f.read("go.sum") != sum {
		t.Fatalf("a short commit should pin the same version")
	}
}

func TestPinClientGoResolvesABranchOnTheRemote(t *testing.T) {
	f := newPinFixture(t)
	f.pin(false, "pin-client-go", "REF="+f.commits["one"])
	if got := f.required(); got != "v0.1.0" {
		t.Fatalf("the tagged commit should pin v0.1.0, got %s", got)
	}
	f.commitConsumer("pin one")

	out := f.pin(false, "pin-client-go", "REF=feature")
	want := "v0.1.1-0.20260304050607-" + f.commits["three"][:12]
	if got := f.required(); got != want {
		t.Fatalf("REF=feature pinned %s, want the branch head %s", got, want)
	}
	if !strings.Contains(out, "rearm-client-go: v0.1.0 -> "+want) {
		t.Fatalf("the output should name the old and the new version:\n%s", out)
	}
	f.pin(true, "pin-client-go", "REF=no-such-branch")
	if got := f.required(); got != want {
		t.Fatalf("an unknown REF changed the pin to %s", got)
	}
}

func TestPinClientGoNeedsARef(t *testing.T) {
	f := newPinFixture(t)
	if out := f.pin(true, "pin-client-go"); !strings.Contains(out, "usage: make pin-client-go REF=") {
		t.Fatalf("without REF the target should print its usage:\n%s", out)
	}
}

func TestPinClientGoRefusesADirtyModuleFile(t *testing.T) {
	f := newPinFixture(t)
	f.pin(false, "pin-client-go", "REF="+f.commits["one"])
	f.commitConsumer("pin one")

	edited := strings.Replace(f.read("go.mod"), "v0.1.0", "v0.1.1-0.20260203040506-"+f.commits["two"][:12], 1)
	f.write(filepath.Join(f.consumer, "go.mod"), edited)
	out := f.pin(true, "pin-client-go", "REF="+f.commits["two"])
	if !strings.Contains(out, "unstaged changes") {
		t.Fatalf("the refusal should say why:\n%s", out)
	}
	if f.read("go.mod") != edited {
		t.Fatalf("a refused run must leave go.mod as it was")
	}
	f.pin(true, "pin-client-go-check")
}

// A conflict on the pin: the base and the branch each moved it. The target
// refuses the conflicted files; taking either side and re-running settles it.
func TestPinClientGoResolvesAConflictByReRunning(t *testing.T) {
	f := newPinFixture(t)
	f.pin(false, "pin-client-go", "REF="+f.commits["one"])
	f.commitConsumer("pin one")
	f.must(f.consumer, "git", "checkout", "-q", "-b", "task")
	f.pin(false, "pin-client-go", "REF=feature")
	f.commitConsumer("pin feature")
	f.must(f.consumer, "git", "checkout", "-q", "main")
	f.pin(false, "pin-client-go", "REF="+f.commits["two"])
	f.commitConsumer("pin two")
	f.must(f.consumer, "git", "checkout", "-q", "task")

	if out, err := run(t, f.consumer, f.env, "git", "merge", "-q", "main"); err == nil {
		t.Fatalf("the merge should conflict on the pin:\n%s", out)
	}
	out := f.pin(true, "pin-client-go", "REF=feature")
	if !strings.Contains(out, "unresolved conflict") {
		t.Fatalf("the refusal should name the conflict:\n%s", out)
	}

	f.must(f.consumer, "git", "checkout", "--theirs", "go.mod", "go.sum")
	f.must(f.consumer, "git", "add", "go.mod", "go.sum")
	f.pin(false, "pin-client-go", "REF=feature")
	want := "v0.1.1-0.20260304050607-" + f.commits["three"][:12]
	if got := f.required(); got != want {
		t.Fatalf("after the re-run the pin is %s, want %s", got, want)
	}
	f.must(f.consumer, "git", "add", "go.mod", "go.sum")
	f.pin(false, "pin-client-go-check")
	if strings.Contains(f.read("go.sum"), f.commits["two"][:12]) {
		t.Fatalf("go.sum keeps the other side's lines:\n%s", f.read("go.sum"))
	}
}

func TestPinClientGoCheckCatchesAHandEditedPin(t *testing.T) {
	f := newPinFixture(t)
	f.pin(false, "pin-client-go", "REF="+f.commits["two"])
	f.commitConsumer("pin two")
	if out := f.pin(false, "pin-client-go-check"); !strings.Contains(out, "is what make pin-client-go writes") {
		t.Fatalf("the check should pass on the target's own pin:\n%s", out)
	}
	if st := f.must(f.consumer, "git", "status", "--porcelain"); st != "" {
		t.Fatalf("the check left changes:\n%s", st)
	}

	// A pseudo-version typed by hand with the wrong time.
	good := f.read("go.mod")
	f.write(filepath.Join(f.consumer, "go.mod"), strings.Replace(good, "20260203040506", "20260203040507", 1))
	f.commitConsumer("hand-edited pin")
	f.pin(true, "pin-client-go-check")

	// The right go.mod, but go.sum lost the module's line.
	f.write(filepath.Join(f.consumer, "go.mod"), good)
	var kept []string
	for _, line := range strings.SplitAfter(f.read("go.sum"), "\n") {
		if !strings.Contains(line, "/go.mod h1:") || !strings.HasPrefix(line, clientGo+" ") {
			kept = append(kept, line)
		}
	}
	f.write(filepath.Join(f.consumer, "go.sum"), strings.Join(kept, ""))
	f.commitConsumer("go.sum out of step")
	sum := f.read("go.sum")
	out := f.pin(true, "pin-client-go-check")
	if !strings.Contains(out, "is not what make pin-client-go writes") {
		t.Fatalf("the check should say what is wrong:\n%s", out)
	}
	if f.read("go.sum") != sum {
		t.Fatalf("a failed check must leave go.sum as it was")
	}
}

// A docker build context carries no .git: the check still runs there, and
// the pin itself refuses.
func TestPinClientGoCheckRunsWithoutAGitWorkTree(t *testing.T) {
	f := newPinFixture(t)
	f.pin(false, "pin-client-go", "REF="+f.commits["two"])
	f.commitConsumer("pin two")
	if err := os.RemoveAll(filepath.Join(f.consumer, ".git")); err != nil {
		t.Fatal(err)
	}
	if out := f.pin(false, "pin-client-go-check"); !strings.Contains(out, "is what make pin-client-go writes") {
		t.Fatalf("the check should pass outside a git work tree:\n%s", out)
	}
	if out := f.pin(true, "pin-client-go", "REF="+f.commits["one"]); !strings.Contains(out, "not inside a git work tree") {
		t.Fatalf("the pin should refuse outside a git work tree:\n%s", out)
	}
	f.write(filepath.Join(f.consumer, "go.mod"), strings.Replace(f.read("go.mod"), "20260203040506", "20260203040507", 1))
	f.pin(true, "pin-client-go-check")
}
