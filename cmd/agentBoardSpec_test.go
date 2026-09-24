package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func gitInit(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "t"},
		{"config", "commit.gpgsign", "false"},
		{"remote", "add", "origin", "https://github.com/acme/boards.git"},
	} {
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
}

func TestSpecSourceReadsRepoCommitAndPath(t *testing.T) {
	t.Setenv("GITHUB_REPOSITORY", "")
	t.Setenv("GITHUB_SHA", "")
	dir := t.TempDir()
	gitInit(t, dir)
	spec := filepath.Join(dir, "boards", "platform.yaml")
	if err := os.MkdirAll(filepath.Dir(spec), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(spec, []byte("kind: BOARD\nname: p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "."}, {"commit", "-q", "-m", "board"}} {
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	src := specSource(spec)
	if src == nil || src.Path == nil || *src.Path != "boards/platform.yaml" {
		t.Fatalf("path: %+v", src)
	}
	if src.Repo == nil || *src.Repo != "https://github.com/acme/boards.git" {
		t.Errorf("repo: %v", src.Repo)
	}
	if src.Commit == nil || len(*src.Commit) != 40 {
		t.Errorf("commit: %v", src.Commit)
	}

	// Edited since the commit: the commit is not what is being applied, so none is recorded.
	if err := os.WriteFile(spec, []byte("kind: BOARD\nname: p\ndescription: edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if dirty := specSource(spec); dirty == nil || dirty.Commit != nil {
		t.Errorf("a dirty file records no commit: %+v", dirty)
	}
}

func TestSpecSourceOutsideACheckoutIsNone(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "b.yaml")
	if err := os.WriteFile(spec, []byte("kind: BOARD\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if src := specSource(spec); src != nil {
		t.Errorf("no checkout, no source: %+v", src)
	}
}
