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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	rearm "github.com/relizaio/rearm-client-go"
	"github.com/spf13/cobra"
)

// Publishing a document version from a board hop.
//
//   rearm agent doc publish --session <uuid> --type REVIEW_FINDINGS --task <uuid> \
//       [--file findings/1a2b3c4d/round-2.md] [--index findings/1a2b3c4d/round-2.json] \
//       [--repo /path/to/documents-checkout] [--component <uuid>] [--lifecycle ASSEMBLED]
//
// Unlike usage reporting, this is NOT fire-and-forget: the agent asked for it, the hop cannot be
// signed off without it, and a silent failure would leave the agent to discover at sign-off that it
// has nothing to hand over. So this reports errors and exits non-zero.

var (
	docSession       string
	docType          string
	docTask          string
	docComponent     string
	docFile          string
	docIndexFile     string
	docIndexOnlyFlag bool
	docLifecycle     string
	docRepoPath      string
	docDryRun        bool
	docBoard         string
)

// taskScopedTypes need a task and carry a findings index; everything else is a document series
// belonging to a component.
var taskScopedTypes = map[string]bool{
	"REVIEW_FINDINGS": true,
	"TEST_REPORT":     true,
	// QUESTIONS is task-scoped like the other two -- it is in the server's INDEXED_TYPES and its
	// items are always about one task. Left out, every `doc publish --type QUESTIONS` was refused
	// here with "--component is required", a flag a task-scoped type has nothing to put in, so an
	// agent could not ask a question through the CLI at all.
	"QUESTIONS": true,
}

// findingHeading matches a markdown heading that opens with a finding id, e.g.
// "### F-3: Null dereference" or "## T-1 - flaky under load".
//
// The id shape is deliberately narrow: letters, then a dash, then digits, which is the `F-<n>`
// convention the role prompts describe. A looser rule -- any word before a colon -- turned
// ordinary prose headings into phantom ids: "## Context: what was reviewed" became finding
// "Context", absent from the index, and the publish was refused for a document that was perfectly
// correct. Being too permissive here blocks real work, while being too strict only means an agent
// that invents its own id scheme has to pass the heading it used.
var findingHeading = regexp.MustCompile(`(?m)^#{1,6}\s+([A-Za-z]{1,4}-\d+)\s*[:\-–]\s`)

func sha256File(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// crossCheckIds verifies that the index and the markdown describe the same findings.
//
// Both directions matter and they fail differently. An id in the index with no heading gives the
// next hop a finding it cannot read about; a heading with no index entry is a finding the router
// never sees, which is the same silent loss the carry-forward rule exists to prevent. Checked here
// rather than server-side because only the client has the markdown.
func crossCheckIds(indexRaw map[string]interface{}, markdown string) error {
	findings, _ := indexRaw["findings"].([]interface{})
	inIndex := map[string]bool{}
	for _, f := range findings {
		if m, ok := f.(map[string]interface{}); ok {
			if id, ok := m["id"].(string); ok && id != "" {
				inIndex[id] = true
			}
		}
	}
	inDoc := map[string]bool{}
	for _, m := range findingHeading.FindAllStringSubmatch(markdown, -1) {
		inDoc[m[1]] = true
	}

	var missingHeading, missingEntry []string
	for id := range inIndex {
		if !inDoc[id] {
			missingHeading = append(missingHeading, id)
		}
	}
	for id := range inDoc {
		if !inIndex[id] {
			missingEntry = append(missingEntry, id)
		}
	}
	sort.Strings(missingHeading)
	sort.Strings(missingEntry)

	var problems []string
	if len(missingHeading) > 0 {
		problems = append(problems, fmt.Sprintf("in the index with no heading in the markdown: %s",
			strings.Join(missingHeading, ", ")))
	}
	if len(missingEntry) > 0 {
		problems = append(problems, fmt.Sprintf("headed in the markdown but absent from the index: %s",
			strings.Join(missingEntry, ", ")))
	}
	if len(problems) > 0 {
		return fmt.Errorf("the index and the document disagree — %s", strings.Join(problems, "; "))
	}
	return nil
}

var agentDocCmd = &cobra.Command{
	Use:   "doc",
	Short: "Documents a board hop produces (findings, test reports, designs)",
}

var agentDocPublishCmd = &cobra.Command{
	Use:   "publish",
	Short: "Publish a document version from the board's documents repository",
	Long: `Publishes a document already committed in the board's documents repository.

The release pins the commit, so the files must be committed first — the digest is
taken from the working tree, and a release whose digest does not match its commit
points at bytes that were never there.

The documents repository is usually NOT the repository you are working in. It is
resolved from --repo, else the current directory when its origin matches the
board's documents repository, else the path remembered from an earlier --repo.

For REVIEW_FINDINGS and TEST_REPORT the index is read alongside the markdown and
checked against it: every id in one must appear in the other.

Publishing is idempotent on the task, the type, the commit and the digest, so a
re-run after a failure returns the release the first attempt created rather than
opening a new round.`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := runDocPublish(); err != nil {
			fmt.Fprintf(os.Stderr, "rearm: %v\n", err)
			os.Exit(1)
		}
	},
}

// docIndexOnly reports whether this is an index-alone publish: an --index with no --file, and no
// path template to fall back on.
//
// Recognised by what was supplied rather than by a flag, so a caller cannot claim one shape and
// send the other -- the server decides the same way.
// sendDocPublish sends the publish and records what it produced.
//
// Shared by both shapes so the pending-output bookkeeping cannot drift between them: an
// index-only round is an output of the hop exactly as a markdown round is, and a sign-off that
// could not offer it would lose the questions it just asked.
func sendDocPublish(st *agentSessionState, input map[string]interface{}) error {
	if docDryRun {
		out, _ := json.MarshalIndent(input, "", "  ")
		fmt.Println(string(out))
		return nil
	}
	data, err := sendGraphQLRequest(rearm.AgentDocumentPublishProgrammatic_Operation,
		map[string]interface{}{"input": input})
	if err != nil {
		printGqlError(err)
		os.Exit(1)
	}
	release, _ := data["agentDocumentPublishProgrammatic"].(map[string]interface{})
	releaseUuid, _ := release["uuid"].(string)
	// Remembered so `task signoff` can send it without the agent copying a uuid by hand. Recorded
	// per task, because a session may work several tasks in its life and one hop's outputs must
	// never be offered as another's.
	if releaseUuid != "" && docTask != "" {
		rememberPendingOutput(st, docTask, releaseUuid)
	}
	emitJson(release)
	return nil
}

func docIndexOnly() bool {
	return docIndexOnlyFlag && docIndexFile != ""
}

// publishIndexOnly sends an index with no file, commit or repository.
//
// The index is read from the working directory rather than from the documents repository: it was
// never committed, because there is nothing to commit it alongside. Idempotency is the index
// itself, so a retry after a dropped response returns the round the first attempt created.
func publishIndexOnly(st *agentSessionState) error {
	raw, err := os.ReadFile(docIndexFile)
	if err != nil {
		return fmt.Errorf("could not read the index %s: %w", docIndexFile, err)
	}
	var idx map[string]interface{}
	if err := json.Unmarshal(raw, &idx); err != nil {
		return fmt.Errorf("the index %s is not valid JSON: %w", docIndexFile, err)
	}
	spec := strings.ToUpper(strings.ReplaceAll(docType, "-", "_"))
	if about, ok := idx["about"].(map[string]interface{}); !ok || about["specification"] == nil {
		if spec == "QUESTIONS" {
			return fmt.Errorf("a QUESTIONS index needs \"about\": {\"specification\": ...}, which is what" +
				" sends it to the role that produces that input")
		}
	}
	input := map[string]interface{}{
		"sessionUuid":   sessionUuidOf(st, docSession),
		"specification": spec,
		"index":         idx,
		"taskUuid":      docTask,
	}
	if docLifecycle != "" {
		input["lifecycle"] = strings.ToUpper(docLifecycle)
	}
	return sendDocPublish(st, input)
}

func runDocPublish() error {
	if docSession == "" {
		return fmt.Errorf("--session is required")
	}
	spec := strings.ToUpper(strings.ReplaceAll(docType, "-", "_"))
	if spec == "" {
		return fmt.Errorf("--type is required, e.g. REVIEW_FINDINGS")
	}
	if taskScopedTypes[spec] && docTask == "" {
		return fmt.Errorf("--task is required for %s, which is a per-task document", spec)
	}
	if !taskScopedTypes[spec] && docComponent == "" {
		return fmt.Errorf("--component is required for %s, which belongs to a document series", spec)
	}

	st := lookupAgentState(docSession)

	// An index with no markdown: the items ARE the document. Usual for QUESTIONS, where forcing a
	// file would mean committing an empty page to satisfy a check. Nothing below this touches the
	// documents repository -- there is no file to commit, no digest to take and no commit to pin.
	if docIndexOnly() {
		return publishIndexOnly(st)
	}

	board, documentsRepo, err := boardOfSession(st)
	if err != nil {
		return err
	}

	repoPath, err := resolveDocumentsRepo(st, documentsRepo)
	if err != nil {
		return err
	}
	head, err := readHead(repoPath)
	if err != nil {
		return fmt.Errorf("could not read HEAD of %s: %w", repoPath, err)
	}

	file := docFile
	if file == "" {
		file, err = resolveTemplatePath(board, spec)
		if err != nil {
			return err
		}
	}
	indexFile := docIndexFile
	if indexFile == "" && taskScopedTypes[spec] {
		// The index sits beside the markdown by convention, which is what the default path
		// templates lay out.
		indexFile = strings.TrimSuffix(file, filepath.Ext(file)) + ".json"
	}

	paths := []string{file}
	if indexFile != "" {
		paths = append(paths, indexFile)
	}
	if err := assertCommitted(repoPath, paths); err != nil {
		return err
	}

	digest, err := sha256File(filepath.Join(repoPath, file))
	if err != nil {
		return fmt.Errorf("could not read %s: %w", file, err)
	}

	input := map[string]interface{}{
		"sessionUuid":   sessionUuidOf(st, docSession),
		"specification": spec,
		"path":          file,
		"digest":        digest,
		"mediaType":     "text/markdown",
		"commit":        head.Commit,
		// The remote URL as this checkout reports it. The server compares it canonically with the
		// board's documents repository, so any of the forms a remote may be configured in works.
		"vcsUri": gitRemote(repoPath),
	}
	if docTask != "" {
		input["taskUuid"] = docTask
	}
	if docComponent != "" {
		input["component"] = docComponent
	}
	if head.Message != "" {
		input["commitMessage"] = head.Message
	}
	if head.Date != "" {
		input["commitDate"] = head.Date
	}
	if docLifecycle != "" {
		input["lifecycle"] = strings.ToUpper(docLifecycle)
	}

	if indexFile != "" {
		raw, err := os.ReadFile(filepath.Join(repoPath, indexFile))
		if err != nil {
			return fmt.Errorf("could not read the index %s: %w", indexFile, err)
		}
		var idx map[string]interface{}
		if err := json.Unmarshal(raw, &idx); err != nil {
			return fmt.Errorf("the index %s is not valid JSON: %w", indexFile, err)
		}
		markdown, err := os.ReadFile(filepath.Join(repoPath, file))
		if err != nil {
			return err
		}
		if err := crossCheckIds(idx, string(markdown)); err != nil {
			return err
		}
		indexDigest, err := sha256File(filepath.Join(repoPath, indexFile))
		if err != nil {
			return err
		}
		input["index"] = idx
		input["indexPath"] = indexFile
		input["indexDigest"] = indexDigest
	}

	if repoPath != "" {
		defer rememberDocumentsRepoPath(st, repoPath)
	}
	return sendDocPublish(st, input)
}

// resolveTemplatePath fills the board's path template for this type and the task's next round.
func resolveTemplatePath(board map[string]interface{}, spec string) (string, error) {
	tmpl := ""
	if paths, ok := board["documentPaths"].(map[string]interface{}); ok {
		if v, ok := paths[spec].(string); ok {
			tmpl = v
		}
	}
	if tmpl == "" {
		switch spec {
		case "REVIEW_FINDINGS":
			tmpl = "findings/{task}/round-{round}.md"
		case "TEST_REPORT":
			tmpl = "tests/{task}/run-{round}.md"
		default:
			return "", fmt.Errorf("this board has no path template for %s; pass --file", spec)
		}
	}
	round, err := nextRound(spec)
	if err != nil {
		return "", err
	}
	short := docTask
	if len(short) > 8 {
		short = short[:8]
	}
	p := strings.ReplaceAll(tmpl, "{task}", short)
	p = strings.ReplaceAll(p, "{round}", fmt.Sprintf("%d", round))
	p = strings.ReplaceAll(p, "{type}", strings.ToLower(spec))
	return p, nil
}

func init() {
	f := agentDocPublishCmd.Flags()
	f.StringVar(&docSession, "session", "", "session publishing the document")
	f.StringVar(&docType, "type", "", "specification type, e.g. REVIEW_FINDINGS")
	f.StringVar(&docTask, "task", "", "task this round belongs to (task-scoped types)")
	f.StringVar(&docComponent, "component", "", "document series (component-scoped types)")
	f.StringVar(&docFile, "file", "", "repo-relative path; resolved from the board's template when omitted")
	f.StringVar(&docIndexFile, "index", "", "repo-relative path of the JSON index; defaults beside the file")
	f.BoolVar(&docIndexOnlyFlag, "index-only", false,
		"publish the index alone, with no file: the items ARE the document, which is the usual"+
			" shape for QUESTIONS. --index is then a path in the current directory, not in the"+
			" documents repository, and nothing is committed")
	f.StringVar(&docLifecycle, "lifecycle", "", "release lifecycle; DRAFT when omitted")
	f.StringVar(&docRepoPath, "repo", "", "path to the documents repository checkout")
	f.StringVar(&docBoard, "board", "", "board this document belongs to; needed for component-scoped types when the session holds no seat")
	f.BoolVar(&docDryRun, "dry-run", false, "print what would be sent and exit")

	agentDocCmd.AddCommand(agentDocPublishCmd)
}

// boardOfSession resolves the board this publish belongs to, and its documents repository.
//
// Read from the server rather than from local state: the documents repository and its path
// templates are board configuration an operator changes, and a stale local copy would send an agent
// to write in the wrong place.
func boardOfSession(st *agentSessionState) (map[string]interface{}, string, error) {
	boardUuid := docBoard
	if boardUuid == "" && docTask != "" {
		data, err := sendGraphQLRequest(rearm.AgentTaskProgrammatic_Operation,
			map[string]interface{}{"taskUuid": docTask})
		if err != nil {
			return nil, "", fmt.Errorf("could not read task %s: %w", docTask, err)
		}
		task, _ := data["agentTaskProgrammatic"].(map[string]interface{})
		if task == nil {
			return nil, "", fmt.Errorf("task %s not found", docTask)
		}
		cachedTask = task
		boardUuid, _ = task["board"].(string)
	} else if boardUuid == "" && st != nil {
		boardUuid = st.Board
	}
	if boardUuid == "" {
		return nil, "", fmt.Errorf("cannot tell which board this document belongs to; pass --board " +
			"(or --task for a per-task document)")
	}

	data, err := sendGraphQLRequest(rearm.AgentBoardProgrammatic_Operation,
		map[string]interface{}{"boardUuid": boardUuid})
	if err != nil {
		return nil, "", fmt.Errorf("could not read board %s: %w", boardUuid, err)
	}
	board, _ := data["agentBoardProgrammatic"].(map[string]interface{})
	if board == nil {
		return nil, "", fmt.Errorf("board %s not found", boardUuid)
	}
	// An object now, not a string: the board names a repository ROW and the uri is the row's.
	repoObj, _ := board["documentsRepo"].(map[string]interface{})
	repo := ""
	if repoObj != nil {
		repo, _ = repoObj["uri"].(string)
	}
	if repo == "" {
		return nil, "", fmt.Errorf("board %s has no documents repository configured; "+
			"an operator must set one before documents can be published", boardUuid)
	}
	return board, repo, nil
}

// cachedTask is the task read during board resolution, reused for the round count so publishing
// does not read the same task twice.
var cachedTask map[string]interface{}

// nextRound counts this task's existing rounds of a type and returns the next.
//
// Mirrors the server's rule, and only ever feeds the PATH template: the server computes the round
// it stores under the task lock, which is the authoritative one. They agree because the agent
// holding the assignment is the only thing publishing for this task.
func nextRound(spec string) (int, error) {
	if cachedTask == nil {
		return 0, fmt.Errorf("no task read; pass --task")
	}
	docs, _ := cachedTask["documents"].([]interface{})
	n := 0
	for _, d := range docs {
		rel, ok := d.(map[string]interface{})
		if !ok {
			continue
		}
		ref, ok := rel["document"].(map[string]interface{})
		if !ok {
			continue
		}
		if s, _ := ref["specification"].(string); s == spec {
			if t, _ := ref["task"].(string); t == docTask {
				n++
			}
		}
	}
	return n + 1, nil
}
