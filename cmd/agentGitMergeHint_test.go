package cmd

import (
	"strings"
	"testing"
)

// hintWords splits the printed command the way a POSIX shell would for what shellWord writes: words separated by
// spaces, a single-quoted word taken literally, and a backslash-escaped quote between quoted parts.
func hintWords(t *testing.T, line string) []string {
	t.Helper()
	var words []string
	var cur strings.Builder
	inWord, quoted := false, false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case quoted && c == '\'':
			quoted = false
		case quoted:
			cur.WriteByte(c)
		case c == '\'':
			quoted, inWord = true, true
		case c == '\\' && i+1 < len(line):
			i++
			cur.WriteByte(line[i])
			inWord = true
		case c == ' ':
			if inWord {
				words = append(words, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteByte(c)
			inWord = true
		}
	}
	if quoted {
		t.Fatalf("unterminated quote in %q", line)
	}
	if inWord {
		words = append(words, cur.String())
	}
	return words
}

// finishHint is the commit helper call the conflict message prints, as words after "rearm agent git commit".
func finishHint(t *testing.T, errOut string) []string {
	t.Helper()
	const prefix = "rearm agent git commit "
	for _, line := range strings.Split(errOut, "\n") {
		if l := strings.TrimSpace(line); strings.HasPrefix(l, prefix) {
			return hintWords(t, strings.TrimPrefix(l, prefix))
		}
	}
	t.Fatalf("no commit helper call in:\n%s", errOut)
	return nil
}

// Tester run 2, T-8: after a conflict in merge --no-co-author, the printed command carries --no-co-author, and
// following it as printed writes the two ReARM trailers alone, whether or not a co-author line is kept.
func TestGitMergeConflictHintKeepsNoCoAuthor(t *testing.T) {
	two := "ReARM-Agentic-Session: code-1\nReARM-Agent: " + gAgent
	for _, kept := range []bool{false, true} {
		name := "no co-author line kept"
		if kept {
			name = "a co-author line kept"
		}
		t.Run(name, func(t *testing.T) {
			w := newGitWorld(t)
			w.branches(true)
			if kept {
				gitCoAuthor, gitMessages = gCoAuthor, []string{"chore: keep the co-author line"}
				w.write("k.txt", "k\n")
				if code, out, errOut := w.commit("k.txt"); code != 0 {
					t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
				}
			}
			mainSha := gRun(t, w.repo, "", "rev-parse", "main")
			gitCoAuthor, gitMessages, gitNoCoAuthor = "", nil, true
			code, out, errOut := w.merge("main")
			want := "rearm agent git commit --session " + gSession + " --no-co-author -m 'Merge main (" + mainSha[:7] + ") into feature'"
			if code != 1 || !mergeInProgress() || !strings.Contains(errOut, want) {
				t.Fatalf("exit %d, want the hint %q\nstdout:\n%s\nstderr:\n%s", code, want, out, errOut)
			}

			// Resolve, stage, and run the printed command as printed: parse it with the verb's own flags.
			w.write("base.txt", "resolved\n")
			gRun(t, w.repo, "", "add", "base.txt")
			gitSession, gitMessages, gitCoAuthor, gitNoCoAuthor = "", nil, "", false
			if err := agentGitCommitCmd.ParseFlags(finishHint(t, errOut)); err != nil {
				t.Fatal(err)
			}
			code, out, errOut = w.commit(agentGitCommitCmd.Flags().Args()...)
			if code != 0 {
				t.Fatalf("following the hint: exit %d\n%s\n%s", code, out, errOut)
			}
			if got := w.logTrailers(); got != two {
				t.Fatalf("trailers:\n%s\nwant:\n%s", got, two)
			}
			if parents := strings.Fields(gRun(t, w.repo, "", "log", "-1", "--format=%P")); len(parents) != 2 {
				t.Fatalf("parents %v", parents)
			}
			if s := gRun(t, w.repo, "", "log", "-1", "--format=%s"); s != "Merge main ("+mainSha[:7]+") into feature" {
				t.Fatalf("subject %q", s)
			}
		})
	}
}

// Without --no-co-author the hint does not add it: the session, then the subject.
func TestGitMergeConflictHintWithoutTheFlag(t *testing.T) {
	w := newGitWorld(t)
	w.branches(true)
	gitCoAuthor = gCoAuthor
	if code, _, errOut := w.merge("main"); code != 1 || strings.Contains(errOut, "--no-co-author") {
		t.Fatalf("exit %d\n%s", code, errOut)
	} else if words := finishHint(t, errOut); len(words) != 4 || words[0] != "--session" || words[2] != "-m" {
		t.Fatalf("hint words %q", words)
	}
}
