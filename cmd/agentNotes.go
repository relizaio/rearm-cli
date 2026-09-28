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
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	rearm "github.com/relizaio/rearm-client-go"
	"github.com/spf13/cobra"
)

// Running notes per role (task RD3-9): notes/<role>.md under the board's documents root, a dated line at a
// time. A fresh context reads their tail in its brief. Notes are not documents: no publish, no round, no
// release -- a commit and a push, under the same rules as the documents repository (never force; a rejected
// push is merged and pushed again).

var (
	notesRole    string
	notesSession string
	notesLines   int
)

// notesBoard is the board the notes verbs name. A uuid for now; it becomes the name-or-uuid resolver of
// task RD3-3 (boardArg) once that is on this branch.
var notesBoard = func(arg string) string { return strings.TrimSpace(arg) }

// notesPushAttempts bounds the merge-and-push-again loop when another writer got there first.
const notesPushAttempts = 3

var agentNotesCmd = &cobra.Command{
	Use:   "notes",
	Short: "Running notes per role in the board's documents repository (append / tail)",
}

var agentNotesAppendCmd = &cobra.Command{
	Use:   "append <board> <line>",
	Short: "Append a dated line to notes/<role>.md: a commit with the session's trailers, pushed",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		out, err := appendNote(notesBoard(args[0]), notesRole, notesSession, args[1], time.Now().UTC())
		if err != nil {
			fail(err.Error())
		}
		fmt.Println(out)
	},
}

var agentNotesTailCmd = &cobra.Command{
	Use:   "tail <board>",
	Short: "Print the last lines of notes/<role>.md",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		lines, rel, err := tailNotes(notesBoard(args[0]), notesRole, notesSession, notesLines)
		if err != nil {
			fail(err.Error())
		}
		if len(lines) == 0 {
			fmt.Printf("no notes in %s yet\n", rel)
			return
		}
		fmt.Println(strings.Join(lines, "\n"))
	},
}

// gitTraced says whether git would print what it sends: its tracing prints the credential headers.
func gitTraced() bool {
	for _, v := range []string{"GIT_TRACE", "GIT_TRACE_PACKET", "GIT_TRACE_CURL", "GIT_CURL_VERBOSE"} {
		if val := strings.TrimSpace(os.Getenv(v)); val != "" && val != "0" && !strings.EqualFold(val, "false") {
			return true
		}
	}
	return false
}

// notesLocation is the board's documents checkout and the role's notes file in it, relative and absolute.
func notesLocation(board, role, session string) (repo, rel string, err error) {
	if strings.TrimSpace(role) == "" {
		return "", "", fmt.Errorf("--role is required: notes are kept per role")
	}
	data, err := sendGraphQLRequest(rearm.AgentBoardProgrammatic_Operation, map[string]interface{}{"boardUuid": board})
	if err != nil {
		return "", "", err
	}
	bd, _ := data["agentBoardProgrammatic"].(map[string]interface{})
	docsRepo, _ := bd["documentsRepo"].(map[string]interface{})
	uri := str(docsRepo["uri"])
	if uri == "" {
		return "", "", fmt.Errorf("the board has no documents repository")
	}
	repo, err = resolveDocumentsRepo(lookupAgentState(session), uri)
	if err != nil {
		return "", "", err
	}
	return repo, path.Join(str(bd["documentsRoot"]), "notes", strings.ToLower(strings.TrimSpace(role))+".md"), nil
}

// tailNotes is the last n lines of the role's notes, and where they are.
func tailNotes(board, role, session string, n int) ([]string, string, error) {
	repo, rel, err := notesLocation(board, role, session)
	if err != nil {
		return nil, "", err
	}
	if n <= 0 {
		n = briefNotesLines
	}
	return tailLines(filepath.Join(repo, filepath.FromSlash(rel)), n), rel, nil
}

// appendNote writes a dated line to the role's notes, commits it alone with the session's trailers, and
// pushes; a rejected push is merged and pushed again, never forced. Signing follows the repository's own
// configuration, as every commit there does.
func appendNote(board, role, session, line string, now time.Time) (string, error) {
	if traced(os.Getenv("SHELLOPTS")) || gitTraced() {
		return "", fmt.Errorf("refusing to write notes under shell or git tracing: it would print the credentials")
	}
	text := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(line, "\r", " "), "\n", " "))
	if text == "" {
		return "", fmt.Errorf("the note is empty")
	}
	if strings.TrimSpace(session) == "" {
		return "", fmt.Errorf("--session is required: the commit carries the session's trailers")
	}
	sd, err := sendGraphQLRequest(rearm.SessionProgrammatic_Operation, map[string]interface{}{"sessionUuid": session})
	if err != nil {
		return "", err
	}
	s, _ := sd["sessionProgrammatic"].(map[string]interface{})
	clientSessionId, agent := str(s["clientSessionId"]), str(s["agent"])
	if clientSessionId == "" || agent == "" {
		return "", fmt.Errorf("session %s has no client session id or agent to attribute the note to", session)
	}
	repo, rel, err := notesLocation(board, role, session)
	if err != nil {
		return "", err
	}
	abs := filepath.Join(repo, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", err
	}
	f, err := os.OpenFile(abs, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}
	_, werr := fmt.Fprintf(f, "- %s: %s\n", now.UTC().Format("2006-01-02 15:04 UTC"), text)
	cerr := f.Close()
	if werr != nil || cerr != nil {
		return "", fmt.Errorf("could not write %s: %v %v", rel, werr, cerr)
	}
	subject := text
	if len(subject) > 60 {
		subject = subject[:57] + "..."
	}
	msg := fmt.Sprintf("notes(%s): %s\n\nReARM-Agentic-Session: %s\nReARM-Agent: %s\n", strings.ToLower(role), subject,
		clientSessionId, agent)
	if _, err := git(repo, "add", "--", rel); err != nil {
		return "", err
	}
	if _, err := git(repo, "commit", "-q", "-m", msg, "--", rel); err != nil {
		return "", fmt.Errorf("could not commit %s: %w", rel, err)
	}
	branch, err := git(repo, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	for attempt := 1; ; attempt++ {
		if _, err = git(repo, "push", "-q", "origin", "HEAD:"+branch); err == nil {
			break
		}
		if attempt == notesPushAttempts {
			return "", fmt.Errorf("the note is committed but the push was refused %d times: %w", attempt, err)
		}
		// Someone pushed first: take theirs and ours together, never force.
		if _, ferr := git(repo, "fetch", "-q", "origin", branch); ferr != nil {
			return "", ferr
		}
		if _, merr := git(repo, "merge", "-q", "--no-edit", "origin/"+branch); merr != nil {
			_, _ = git(repo, "merge", "--abort")
			return "", fmt.Errorf("the note is committed, but merging origin/%s failed; resolve it by hand and push: %w", branch, merr)
		}
	}
	head, _ := git(repo, "rev-parse", "--short", "HEAD")
	return fmt.Sprintf("appended to %s (%s), pushed", rel, head), nil
}

func init() {
	for _, c := range []*cobra.Command{agentNotesAppendCmd, agentNotesTailCmd} {
		c.Flags().StringVar(&notesRole, "role", "", "the role whose notes these are")
		c.Flags().StringVar(&notesSession, "session", "", "your board session: its trailers on the commit, its remembered checkout")
		c.Flags().StringVar(&docRepoPath, "repo", "", "the documents repository checkout, when it is not the current directory or remembered")
	}
	agentNotesTailCmd.Flags().IntVar(&notesLines, "lines", briefNotesLines, "how many lines")
	agentNotesCmd.AddCommand(agentNotesAppendCmd, agentNotesTailCmd)
	agentCmd.AddCommand(agentNotesCmd)
}
