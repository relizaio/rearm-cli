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
//   rearm agent doc publish --session <uuid> --type BOARD_REVIEW_ITEMS --task <uuid> \
//       [--file review-items/1a2b3c4d/round-2.md] [--index review-items/1a2b3c4d/round-2.json] \
//       [--repo /path/to/documents-checkout] [--component <uuid>]
//
// There is no lifecycle to choose: the server publishes a DRAFT and the board promotes it to
// ASSEMBLED when the hop that produced it signs off.
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
	docRepoPath      string
	docDryRun        bool
	docBoard         string
	docAdvisory      bool
	docCheck         bool
)

// taskScopedTypes need a task and carry a review item index; everything else is a document series
// belonging to a component.
var taskScopedTypes = map[string]bool{
	"BOARD_REVIEW_ITEMS": true,
	"BOARD_TEST_REPORT":  true,
	// BOARD_QUESTIONS is task-scoped like the other two -- it is in the server's INDEXED_TYPES and its
	// items are always about one task. Left out, every `doc publish --type BOARD_QUESTIONS` was refused
	// here with "--component is required", a flag a task-scoped type has nothing to put in, so an
	// agent could not ask a question through the CLI at all.
	"BOARD_QUESTIONS": true,
}

// reviewItemHeading matches a markdown heading that opens with a review item id, e.g.
// "### F-3: Null dereference" or "## T-1 - flaky under load".
//
// The id shape is deliberately narrow: letters, then a dash, then digits, which is the `F-<n>`
// convention the role prompts describe. A looser rule -- any word before a colon -- turned
// ordinary prose headings into phantom ids: "## Context: what was reviewed" became review item
// "Context", absent from the index, and the publish was refused for a document that was perfectly
// correct. Being too permissive here blocks real work, while being too strict only means an agent
// that invents its own id scheme has to pass the heading it used.
var reviewItemHeading = regexp.MustCompile(`(?m)^#{1,6}\s+([A-Za-z]{1,4}-\d+)\s*[:\-–]\s`)

func sha256File(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// crossCheckIds verifies that the index and the markdown describe the same review items.
//
// Both directions matter and they fail differently. An id in the index with no heading gives the
// next hop a review item it cannot read about; a heading with no index entry is a review item the router
// never sees, which is the same silent loss the carry-forward rule exists to prevent. Checked here
// rather than server-side because only the client has the markdown.
func crossCheckIds(indexRaw map[string]interface{}, markdown string) error {
	reviewItems, _ := indexRaw["reviewItems"].([]interface{})
	inIndex := map[string]bool{}
	for _, f := range reviewItems {
		if m, ok := f.(map[string]interface{}); ok {
			if id, ok := m["id"].(string); ok && id != "" {
				inIndex[id] = true
			}
		}
	}
	inDoc := map[string]bool{}
	for _, m := range reviewItemHeading.FindAllStringSubmatch(markdown, -1) {
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
	Short: "Documents a board hop produces (review items, test reports, designs)",
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

Without --file the path is the board's for the type's next round, except when this hop
already published the newest round of the type on the task: the publish then defaults to
that round's path and lands as a new version of it, and says so. --file always wins.

For BOARD_REVIEW_ITEMS and BOARD_TEST_REPORT the index is read alongside the markdown and
checked against it: every id in one must appear in the other.

Publishing is idempotent on the task, the type, the commit and the digest, so a
re-run after a failure returns the release the first attempt created rather than
opening a new round.

--advisory puts a round on a task another role holds, for example an architect's
amendment answering a review item while the coder works the task. Only a prose type a
role you have held on the board produces, on an active task; never an index type.

--json prints one JSON value on stdout and nothing on stderr on success: the release, plus
checks {verdict, counts, blocking, lines} (null for a document without elements) and, when
the publish said anything beside it, notices. With --check it prints {check: true, checks},
with --dry-run {dryRun: true, input}, each with notices when there are any.
A refusal or local error still goes to stderr with exit 1 and nothing on stdout.`,
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
//
// repoPath is the documents checkout to remember for the session ("" for an index-only publish). It is
// remembered once the publish succeeded (or the dry run got this far), before anything is printed, so a
// warning that it could not be kept goes into the --json object rather than after it (task RD5-8, round 2).
func sendDocPublish(st *agentSessionState, input map[string]interface{}, repoPath string) error {
	if docDryRun {
		rememberDocumentsRepoPath(st, repoPath)
		// --json: a dry run is a success too, so one object, {dryRun: true, input, notices}, and nothing on
		// stderr (ARCHITECTURE round 2). Without --json the input alone, indented, as before.
		if compactJson {
			emitJson(withNotices(map[string]interface{}{"dryRun": true, "input": input}))
			return nil
		}
		out, _ := json.MarshalIndent(input, "", "  ")
		fmt.Println(string(out))
		return nil
	}
	data, err := sendGraphQLRequest(rearm.AgentDocumentPublishProgrammatic_Operation,
		map[string]interface{}{"input": input})
	if err != nil {
		printRefusal(err)
		os.Exit(1)
	}
	release, _ := data["agentDocumentPublishProgrammatic"].(map[string]interface{})
	releaseUuid, _ := release["uuid"].(string)
	// Remembered so `task signoff` can send it without the agent copying a uuid by hand. Recorded
	// per task, because a session may work several tasks in its life and one hop's outputs must
	// never be offered as another's. An advisory round is no hop's output, so it is not remembered.
	if releaseUuid != "" && docTask != "" && remembersAsOutput() {
		rememberPendingOutput(st, docTask, releaseUuid)
	}
	rememberDocumentsRepoPath(st, repoPath)
	// The board checked the elements as it took the document (elements.md §7): say what it found
	// now, while the author can still fix it, rather than at the sign-off it would refuse.
	_, withElements := input["elements"]
	withChecks := withElements && releaseUuid != "" && input["taskUuid"] != nil
	// --json: one object, the release with the report folded in as checks, and nothing on stderr (task RD5-8).
	// A report that could not be read is said in checks too: the publish itself succeeded.
	if compactJson {
		var checks interface{}
		if withChecks {
			if rel, err := readElementCheckReport(releaseUuid); err != nil {
				checks = map[string]interface{}{"verdict": nil, "error": err.Error()}
			} else {
				checks = releaseChecks(rel)
			}
		}
		emitJson(publishObject(release, checks))
		return nil
	}
	// Compact by default (task RD3-9): what was published and its release, then the report on stderr.
	fmt.Println(compactDocument(release, strings.ToUpper(strings.ReplaceAll(docType, "-", "_")), docAdvisory))
	if withChecks {
		printElementCheckReport(readElementCheckReport(releaseUuid))
	}
	return nil
}

// applyAdvisory marks the publish advisory when --advisory is set (task e97fde56): a round on a task
// another session holds. The server decides whether it may be one.
func applyAdvisory(input map[string]interface{}) {
	if docAdvisory {
		input["advisory"] = true
	}
}

// remembersAsOutput is whether the release becomes an output this session's sign-off offers. An
// advisory round is nobody's hop output, and the server would refuse it as one.
func remembersAsOutput() bool {
	return !docAdvisory
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
		if spec == "BOARD_QUESTIONS" {
			return fmt.Errorf("a BOARD_QUESTIONS index needs \"about\": {\"specification\": ...}, which is what" +
				" sends it to the role that produces that input")
		}
	}
	return sendDocPublish(st, indexOnlyInput(sessionUuidOf(st, docSession), spec, idx), "")
}

// indexOnlyInput is an index-alone publish as sent, with --advisory applied like any other publish
// (task e97fde56, T-1): an index type is never advisory, and the server says so only if it is told
// the publish asked to be.
func indexOnlyInput(session, spec string, idx map[string]interface{}) map[string]interface{} {
	input := map[string]interface{}{
		"sessionUuid":   session,
		"specification": spec,
		"index":         idx,
		"taskUuid":      docTask,
	}
	applyAdvisory(input)
	return input
}

func runDocPublish() error {
	publishNotices = nil
	if docSession == "" {
		return fmt.Errorf("--session is required")
	}
	spec := strings.ToUpper(strings.ReplaceAll(docType, "-", "_"))
	if spec == "" {
		return fmt.Errorf("--type is required, e.g. BOARD_REVIEW_ITEMS")
	}
	if spec == "BOARD_ELEMENT_CHECK_REPORT" {
		return fmt.Errorf("the board cuts BOARD_ELEMENT_CHECK_REPORT rounds when a document with elements is published;" +
			" run `rearm agent doc element-check` to re-run the element checks")
	}
	if docCheck && (docIndexOnly() || docTask == "") {
		return fmt.Errorf("--check previews the element checks of a task's document with a file: give --task," +
			" and not --index-only")
	}

	if taskScopedTypes[spec] && docTask == "" {
		return fmt.Errorf("--task is required for %s, which is a per-task document", spec)
	}
	// Only when there is no board context to resolve it from. A component-scoped document
	// published against a task, or against a board, hangs off that board's target: the server
	// finds the series or creates it. Demanding the uuid regardless meant an architect could not
	// publish the design it had just been asked about without first going to look the series up.
	if !taskScopedTypes[spec] && docComponent == "" && docTask == "" && docBoard == "" {
		return fmt.Errorf("--component is required for %s outside a board: it belongs to a document"+
			" series, and with no --task or --board there is nothing to resolve the series from", spec)
	}

	st := lookupAgentState(docSession)

	// An index with no markdown: the items ARE the document. Usual for BOARD_QUESTIONS, where forcing a
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

	file, err := documentFile(board, spec)
	if err != nil {
		return err
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
	if docCheck {
		return runPublishCheck(st, repoPath, file, spec, board)
	}
	if err := assertPushed(repoPath); err != nil {
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
	applyAdvisory(input)
	// The element index, parsed from the committed file (gaps §2.A): what the server checks and
	// keeps on the release. A document without ids sends nothing, as before.
	source, err := os.ReadFile(filepath.Join(repoPath, file))
	if err != nil {
		return fmt.Errorf("could not read %s: %w", file, err)
	}
	extra, ix, err := elementsInput(spec, source, board)
	if err != nil {
		return err
	}
	for k, v := range extra {
		input[k] = v
	}
	if extra != nil {
		sayPublishNote(os.Stderr, "elements: "+summarise(ix))
	}
	if head.Message != "" {
		input["commitMessage"] = head.Message
	}
	if head.Date != "" {
		input["commitDate"] = head.Date
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

	return sendDocPublish(st, input, repoPath)
}

// queryDocumentPath asks the server where a new document of this type goes on the board: its
// template after overrides and scope defaults, with {key}, {round}, {type} and {component} filled
// the way publish itself counts rounds, and the board's documents root (boards/<board>/ on a shared
// repository) in front. A variable so tests can stand in for the server.
var queryDocumentPath = defaultQueryDocumentPath

func defaultQueryDocumentPath(boardUuid, spec, task, component string) (string, error) {
	vars := map[string]interface{}{"boardUuid": boardUuid, "specification": spec}
	if task != "" {
		vars["task"] = task
	}
	if component != "" {
		vars["component"] = component
	}
	data, err := sendGraphQLRequest(rearm.AgentDocumentPath_Operation, vars)
	return pathFromResponse(data, err, spec)
}

// pathFromResponse reads the server's answer. Its refusal -- a task-scoped type with no --task, a
// component-scoped one with no --component -- is passed on with the one way round it.
func pathFromResponse(data map[string]interface{}, err error, spec string) (string, error) {
	if err != nil {
		return "", fmt.Errorf("the board could not give a path for %s: %w; pass --file", spec, err)
	}
	b, _ := data["agentBoardProgrammatic"].(map[string]interface{})
	p, _ := b["documentPath"].(string)
	if p == "" {
		return "", fmt.Errorf("the board gave no path for %s; pass --file", spec)
	}
	return p, nil
}

// documentFile is --file when given; else, when this hop published the newest round of the type on the task,
// that round's path, a republish there being a new version of it (task RD5-6); else the path the board gives
// for this type.
func documentFile(board map[string]interface{}, spec string) (string, error) {
	if docFile != "" {
		return docFile, nil
	}
	if n, ok, err := hopVersionRound(board, spec); err != nil {
		return "", err
	} else if ok {
		sayPublishNote(publishNoteOut(), versionPathLine(n))
		return n.Path, nil
	}
	boardUuid, _ := board["uuid"].(string)
	return queryDocumentPath(boardUuid, spec, docTask, docComponent)
}

func init() {
	f := agentDocPublishCmd.Flags()
	f.StringVar(&docSession, "session", "", "session publishing the document")
	f.StringVar(&docType, "type", "", "specification type, e.g. BOARD_REVIEW_ITEMS")
	f.StringVar(&docTask, "task", "", "task this round belongs to (task-scoped types)")
	f.StringVar(&docComponent, "component", "", "document series (component-scoped types)")
	f.StringVar(&docFile, "file", "", "repo-relative path; when omitted, the round this hop published of the type (a new version of it), else asked of the board (its template, placeholders filled)")
	f.StringVar(&docIndexFile, "index", "", "repo-relative path of the JSON index; defaults beside the file")
	f.BoolVar(&docIndexOnlyFlag, "index-only", false,
		"publish the index alone, with no file: the items ARE the document, which is the usual"+
			" shape for BOARD_QUESTIONS. --index is then a path in the current directory, not in the"+
			" documents repository, and nothing is committed")
	f.StringVar(&docRepoPath, "repo", "", "path to the documents repository checkout")
	f.StringVar(&docBoard, "board", "", "board this document belongs to; needed for component-scoped types when the session holds no seat")
	f.BoolVar(&docDryRun, "dry-run", false, "print what would be sent and exit")
	f.BoolVar(&docCheck, "check", false,
		"build the element index from the committed file and print the element checks the board would run on it,"+
			" in the task's current scope, without publishing; exits 1 on a failure the board blocks on")
	f.BoolVar(&docAdvisory, "advisory", false,
		"publish on a task another session holds, as an advisory round: a prose type a role you have"+
			" held on this board produces. Assembled at once, announced on the board, and never an output"+
			" of your own sign-off; ignored when you hold the task")

	agentDocCmd.AddCommand(agentDocPublishCmd)
}

// boardOfSession resolves the board this publish belongs to, and its documents repository.
//
// Read from the server rather than from local state: the documents repository and its path
// templates are board configuration an operator changes, and a stale local copy would send an agent
// to write in the wrong place.
func boardOfSession(st *agentSessionState) (map[string]interface{}, string, error) {
	boardUuid := docBoard
	if boardUuid != "" {
		boardUuid = boardArg(boardUuid)
	}
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
