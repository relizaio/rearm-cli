package cmd

import (
	"path/filepath"
	"strings"
	"testing"
)

// rearm agent git commit's signing flags (task RD5-2): --signing-format is ssh or gpg, anything else refused and
// nothing kept; a relative ssh --signing-key is kept as an absolute path, so a later commit from another
// directory still finds it.

// Tester run 1, T-4.
func TestGitCommitSigningFormatOtherThanSshOrGpgIsRefused(t *testing.T) {
	w := newGitWorld(t)
	gitCoAuthor, gitMessages = gCoAuthor, []string{"feat: x"}
	w.write("a.txt", "a\n")
	before := w.head()
	for _, f := range []string{"x509", "openpgp", "ssh2"} {
		gitSigningFormat, gitSigningKey = f, w.keyB
		code, _, errOut := w.commit("a.txt")
		if code != 1 || !strings.Contains(errOut, "--signing-format is ssh or gpg, got: "+f) {
			t.Fatalf("%q: exit %d\n%s", f, code, errOut)
		}
		if w.head() != before || w.staged() != "" {
			t.Fatalf("%q: a refused commit wrote to the repository", f)
		}
		if id, err := readAgentIdentity(gAgent); err != nil || id.SigningFormat != "" || id.SigningKey != "" || id.CoAuthor != "" {
			t.Fatalf("%q: kept %+v %v", f, id, err)
		}
	}
	// The case is folded: SSH is ssh.
	gitSigningFormat = "SSH"
	if code, out, errOut := w.commit("a.txt"); code != 0 || !strings.Contains(out, "-c gpg.format=ssh ") {
		t.Fatalf("SSH: exit %d\n%s\n%s", code, out, errOut)
	}
}

// Tester run 1, T-6.
func TestGitCommitRelativeSshKeyIsKeptAbsolute(t *testing.T) {
	w := newGitWorld(t)
	rel, err := filepath.Rel(w.repo, w.keyB)
	if err != nil || filepath.IsAbs(rel) {
		t.Fatalf("rel %q %v", rel, err)
	}
	gitCoAuthor, gitMessages = gCoAuthor, []string{"feat: relative key"}
	gitSigningKey, gitSigningFormat = rel, "ssh"
	w.write("a.txt", "a\n")
	code, out, errOut := w.commit("a.txt")
	if code != 0 || !strings.Contains(out, "-c user.signingkey="+w.keyB+" ") {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	if id, err := readAgentIdentity(gAgent); err != nil || id.SigningKey != w.keyB {
		t.Fatalf("kept %+v %v, want the absolute %s", id, err, w.keyB)
	}
	// From a subdirectory, with no flags, the kept key still resolves and signs.
	w.write("sub/b.txt", "b\n")
	t.Chdir(filepath.Join(w.repo, "sub"))
	gitSigningKey, gitSigningFormat, gitCoAuthor, gitMessages = "", "", "", []string{"feat: from sub"}
	if code, out, errOut := w.commit("b.txt"); code != 0 {
		t.Fatalf("from sub: exit %d\n%s\n%s", code, out, errOut)
	}
	if state, fp := w.signer(); state != "G" || fp != gFingerprint(t, w.keyB) {
		t.Fatalf("signature %s %s, want G by key b", state, fp)
	}
}
