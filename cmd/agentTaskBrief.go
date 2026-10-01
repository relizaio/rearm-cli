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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	rearm "github.com/relizaio/rearm-client-go"
	"github.com/spf13/cobra"
)

// rearm agent task brief (task RD3-9): everything a fresh context needs for one task, in one call. A
// context that starts for a single task has none of what a long-lived session learned -- the prompt it
// works under, the task, its documents, where the documents repository is, the rules -- and used to spend
// five or six calls, each printing a whole task or board, to find it.

var (
	briefSession string
	briefRole    string
	briefInline  bool
	briefJson    bool
)

// servedPromptMissing is what the brief says when the server serves only the role's own prompt.
const servedPromptMissing = "This server does not serve the composed prompt: the board's routing rules are not shown; task next prints them"

// briefNotesLines is how much of notes/<role>.md the brief prints: the running notes' tail.
const briefNotesLines = 20

// briefRules are the rules a fresh context is told every time; they do not change per board.
var briefRules = []string{
	"Credentials are in the environment. Never print them, never put them in a file, a commit or a note.",
	"Never run the CLI, git or a wait with shell tracing (set -x): it prints the credentials.",
	"Push the documents repository before you publish: a release pins a commit the server must be able to read.",
	"Never force-push the documents repository, and never rewrite a commit you pushed there.",
}

type briefDocument struct {
	Release       string `json:"release"`
	Specification string `json:"specification"`
	Round         int    `json:"round"`
	Version       string `json:"version"`
	Lifecycle     string `json:"lifecycle"`
	Advisory      bool   `json:"advisory"`
	Path          string `json:"path,omitempty"`
	Verdict       string `json:"verdict,omitempty"`
	Counts        string `json:"counts,omitempty"`
	// The version of the same round that replaced this one (task RD4-7); empty for the newest.
	ReplacedBy string `json:"replacedBy,omitempty"`
}

type briefDependency struct {
	Uuid   string `json:"uuid"`
	Key    string `json:"key,omitempty"`
	Title  string `json:"title,omitempty"`
	Status string `json:"status,omitempty"`
}

type briefInlined struct {
	Specification string `json:"specification"`
	Round         int    `json:"round"`
	Path          string `json:"path"`
	Content       string `json:"content,omitempty"`
	Missing       string `json:"missing,omitempty"`
}

type briefRepository struct {
	Uri       string          `json:"uri"`
	LocalPath string          `json:"localPath,omitempty"`
	Clone     string          `json:"clone,omitempty"`
	Root      string          `json:"root,omitempty"`
	Templates json.RawMessage `json:"templates,omitempty"`
}

type taskBrief struct {
	Key           string            `json:"key"`
	TaskUuid      string            `json:"taskUuid"`
	Role          string            `json:"role"`
	PromptVersion string            `json:"promptVersion,omitempty"`
	ServedPrompt  string            `json:"servedPrompt"`
	Orientation   string            `json:"orientation"`
	Task          map[string]any    `json:"task"`
	Dependencies  []briefDependency `json:"dependencies"`
	Documents     []briefDocument   `json:"documents"`
	Inline        []briefInlined    `json:"inline,omitempty"`
	Repository    briefRepository   `json:"repository"`
	Rules         []string          `json:"rules"`
	NotesPath     string            `json:"notesPath,omitempty"`
	Notes         []string          `json:"notes"`
	// The orientation's core and the sections this task's actions need (task RD3-10); empty against a
	// server that does not serve the split, which leaves the URL above.
	OrientationCore     string         `json:"orientationCore,omitempty"`
	OrientationSections []briefSection `json:"orientationSections,omitempty"`
}

type briefSection struct {
	Key     string `json:"key"`
	Content string `json:"content"`
}

// briefSectionKeys are the orientation sections a task's actions need (task RD3-10): taking a task and
// publishing always; asking when the role reads documents it may have to ask about; commit trailers when
// the role pushes code; waiting only on the session's first brief, since the wait loop is the session's.
func briefSectionKeys(asks, pushesCode, firstBrief bool) []string {
	keys := []string{"taking-a-task", "publishing"}
	if asks {
		keys = append(keys, "asking")
	}
	if pushesCode {
		keys = append(keys, "commit-trailers")
	}
	if firstBrief {
		keys = append(keys, "waiting")
	}
	return keys
}

// briefOrientation fetches the core and the sections; any failure leaves the brief with the URL alone.
func briefOrientation(b *taskBrief, keys []string) {
	core, err := fetchOrientation("core")
	if err != nil {
		return
	}
	var secs []briefSection
	for _, k := range keys {
		text, err := fetchOrientation(k)
		if err != nil {
			return
		}
		secs = append(secs, briefSection{Key: k, Content: text})
	}
	b.OrientationCore, b.OrientationSections = core, secs
}

var agentTaskBriefCmd = &cobra.Command{
	Use:   "brief <task>",
	Short: "Everything a fresh context needs for one task: prompt, task, documents, repository, rules, notes",
	Long: `Prints, in one call and in this order: the prompt the task's role is served (or --role's), the task,
its documents newest round first per type, the documents repository (its uri, your checkout, the
board's root and path templates), the rules, and the tail of notes/<role>.md. Markdown by default;
--json for the same as structure. --inline prints the newest round of each of the role's input types
in full, from your checkout. The documents it prints are recorded as read for --session, so the
ordinary flow -- brief, work, publish, sign off -- needs no task show.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if strings.TrimSpace(briefSession) == "" {
			fail("--session is required: the brief records what it showed for that session")
		}
		b, read, err := buildTaskBrief(args[0], briefSession, briefRole, briefInline)
		if err != nil {
			fail(err.Error())
		}
		// What it printed is recorded as read by the session (RD2-34): the acknowledgement its sign-off sends.
		rememberSeenFromRead(briefSession, read)
		rememberOrientationShown(briefSession, b.OrientationSections)
		if briefJson {
			out, _ := json.MarshalIndent(b, "", "  ")
			fmt.Println(string(out))
			return
		}
		fmt.Print(renderTaskBrief(b))
	},
}

// buildTaskBrief reads what the brief prints: the task, its dependencies, the board and the role's served
// prompt, then the session's checkout for the notes and --inline. It returns the task read too, for the
// seen-inputs record.
func buildTaskBrief(taskUuid, session, role string, inline bool) (*taskBrief, interface{}, error) {
	data, err := sendGraphQLRequest(rearm.AgentTaskProgrammatic_Operation, map[string]interface{}{"taskUuid": taskUuid})
	if err != nil {
		return nil, nil, err
	}
	task, _ := data["agentTaskProgrammatic"].(map[string]interface{})
	if task == nil {
		return nil, nil, fmt.Errorf("no task %s", taskUuid)
	}
	if strings.TrimSpace(role) == "" {
		role = str(task["role"])
	}
	board := str(task["board"])
	b := &taskBrief{Key: str(task["key"]), TaskUuid: str(task["uuid"]), Role: role, Rules: briefRules, Notes: []string{},
		Orientation: strings.TrimRight(rearmUri, "/") + "/api/agents/orientation.md"}

	roles, err := sendGraphQLRequest(rearm.AgentRoleBriefProgrammatic_Operation, map[string]interface{}{"boardUuid": board})
	composed := true
	if err != nil {
		// A server from before the served-prompt read: the role's own prompt, and say what is missing.
		fallback, ferr := sendGraphQLRequest(rearm.AgentTaskRoleConfigsProgrammatic_Operation, map[string]interface{}{"boardUuid": board})
		if ferr != nil {
			return nil, nil, err
		}
		roles, composed = fallback, false
	}
	var inputs []string
	found, pushesCode := false, false
	for _, r := range asList(roles["agentTaskRoleConfigsProgrammatic"]) {
		if !strings.EqualFold(str(r["name"]), role) {
			continue
		}
		found = true
		b.Role = str(r["name"])
		b.ServedPrompt = str(r["servedPrompt"])
		b.PromptVersion = str(r["promptVersion"])
		if !composed {
			b.ServedPrompt = str(r["prompt"]) + "\n\n(" + servedPromptMissing + ")"
		}
		for _, in := range asList(r["requiredInputs"]) {
			if str(in["kind"]) == "DOCUMENT" && str(in["specification"]) != "" {
				inputs = append(inputs, str(in["specification"]))
			}
		}
		if caps, ok := r["requiredCapabilities"].([]interface{}); ok {
			for _, c := range caps {
				if str(c) == "CODE_PUSH" {
					pushesCode = true
				}
			}
		}
	}
	if !found {
		return nil, nil, fmt.Errorf("the board has no role %q", role)
	}

	mergeInvestigation(task, taskUuid)
	b.Task = briefTaskFields(task)
	b.Dependencies = briefDependencies(task)
	b.Documents = briefDocuments(task)

	bd, err := sendGraphQLRequest(rearm.AgentBoardProgrammatic_Operation, map[string]interface{}{"boardUuid": board})
	if err != nil {
		return nil, nil, err
	}
	brd, _ := bd["agentBoardProgrammatic"].(map[string]interface{})
	repo, _ := brd["documentsRepo"].(map[string]interface{})
	b.Repository = briefRepository{Uri: str(repo["uri"]), Root: str(brd["documentsRoot"])}
	if t, err := json.Marshal(brd["documentPaths"]); err == nil && string(t) != "null" {
		b.Repository.Templates = t
	}
	if st := lookupAgentState(session); st != nil && st.DocumentsRepoPath != "" {
		b.Repository.LocalPath = st.DocumentsRepoPath
	} else if b.Repository.Uri != "" {
		b.Repository.Clone = "git clone https://" + strings.TrimPrefix(strings.TrimPrefix(b.Repository.Uri, "https://"), "http://")
	}

	if b.Repository.LocalPath != "" {
		b.NotesPath = filepath.Join(b.Repository.LocalPath, b.Repository.Root, "notes", strings.ToLower(b.Role)+".md")
		b.Notes = tailLines(b.NotesPath, briefNotesLines)
	}
	if inline {
		b.Inline = briefInlineInputs(b.Documents, inputs, b.Repository.LocalPath)
	}
	briefOrientation(b, briefSectionKeys(len(inputs) > 0, pushesCode, !orientationShown(session, "waiting")))
	return b, task, nil
}

func str(v interface{}) string {
	s, _ := v.(string)
	return s
}

func asList(v interface{}) []map[string]interface{} {
	var out []map[string]interface{}
	for _, e := range func() []interface{} { l, _ := v.([]interface{}); return l }() {
		if m, ok := e.(map[string]interface{}); ok {
			out = append(out, m)
		}
	}
	return out
}

func intOf(v interface{}) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	}
	return 0
}

// briefTaskFields is the task as the brief shows it: who it is, where it stands, what holds it.
// returnedReportLine is one report an investigation brought back (task RD4-12): the pinned report, or a
// cancelled investigation that returned none, with the cancel's note (design round 2 §2), so the asker sees the
// answer is not coming.
func returnedReportLine(r map[string]interface{}) string {
	inv := orElse(str(r["investigationKey"]), str(r["investigation"]))
	if c, _ := r["cancelled"].(bool); c {
		line := inv + ": cancelled, no report; this task no longer waits on it"
		if n := str(r["note"]); n != "" {
			line += " (" + n + ")"
		}
		return line
	}
	return inv + ": report " + str(r["report"]) + " pinned"
}

func briefTaskFields(t map[string]interface{}) map[string]any {
	out := map[string]any{}
	for _, k := range []string{"key", "title", "description", "status", "role", "effectiveWorkLevel", "budgetMicros", "spentMicros"} {
		if v, ok := t[k]; ok && v != nil {
			out[k] = v
		}
	}
	if g, _ := t["group"].(map[string]interface{}); g != nil {
		out["group"] = g["key"]
	}
	var tags []string
	for _, tg := range asList(t["tags"]) {
		tags = append(tags, str(tg["key"]))
	}
	if len(tags) > 0 {
		out["tags"] = tags
	}
	if h, _ := t["hold"].(map[string]interface{}); h != nil {
		out["hold"] = map[string]any{"kind": h["kind"], "level": h["level"], "reason": h["reason"]}
	}
	if inv := investigationLine(t); inv != "" {
		out["investigation"] = inv
	}
	var pins []string
	for _, in := range asList(t["requiredInputs"]) {
		if r := str(in["release"]); r != "" {
			pins = append(pins, orElse(str(in["specification"]), str(in["kind"]))+" "+r)
		}
	}
	if len(pins) > 0 {
		out["pinnedInputs"] = pins
	}
	var back []string
	for _, r := range asList(t["reportsReturned"]) {
		back = append(back, returnedReportLine(r))
	}
	if len(back) > 0 {
		out["reportsReturned"] = back
	}
	var qs []string
	for _, q := range asList(t["openQuestions"]) {
		qs = append(qs, str(q["id"])+": "+str(q["title"]))
	}
	if len(qs) > 0 {
		out["openQuestions"] = qs
	}
	return out
}

// briefDependencies names each task this one depends on, with where it stands.
func briefDependencies(t map[string]interface{}) []briefDependency {
	var ids []interface{}
	ids, _ = t["dependsOn"].([]interface{})
	out := []briefDependency{}
	if len(ids) == 0 {
		return out
	}
	data, err := sendGraphQLRequest(rearm.AgentTasksByUuidProgrammatic_Operation, map[string]interface{}{"taskUuids": ids})
	byUuid := map[string]map[string]interface{}{}
	if err == nil {
		for _, d := range asList(data["agentTasksByUuidProgrammatic"]) {
			byUuid[str(d["uuid"])] = d
		}
	}
	for _, id := range ids {
		d := byUuid[str(id)]
		out = append(out, briefDependency{Uuid: str(id), Key: str(d["key"]), Title: str(d["title"]), Status: str(d["status"])})
	}
	return out
}

// briefDocuments is the task's documents, grouped by type in the order the types first appear, newest round
// first within a type. Two releases of one type and round are versions of that round (task RD4-7): the
// server lists the newest first, and each older one is marked as replaced by it.
func briefDocuments(t map[string]interface{}) []briefDocument {
	var docs []briefDocument
	order := map[string]int{}
	newest := map[string]string{}
	for _, r := range asList(t["documents"]) {
		d, _ := r["document"].(map[string]interface{})
		spec := str(d["specification"])
		if _, seen := order[spec]; !seen {
			order[spec] = len(order)
		}
		bd := briefDocument{Release: str(r["uuid"]), Specification: spec, Round: intOf(d["round"]), Version: str(r["version"]),
			Lifecycle: str(r["lifecycle"]), Path: str(d["path"])}
		bd.Advisory, _ = d["advisory"].(bool)
		if key := fmt.Sprintf("%s/%d", spec, bd.Round); bd.Round > 0 {
			if v, seen := newest[key]; seen {
				bd.ReplacedBy = v
			} else {
				newest[key] = bd.Version
			}
		}
		if f, _ := d["reviewItems"].(map[string]interface{}); f != nil {
			bd.Verdict = str(f["verdict"])
			if c, _ := f["counts"].(map[string]interface{}); c != nil {
				bd.Counts = fmt.Sprintf("%d passed, %d failed, %d skipped", intOf(c["passed"]), intOf(c["failed"]), intOf(c["skipped"]))
			}
		}
		docs = append(docs, bd)
	}
	sort.SliceStable(docs, func(i, j int) bool {
		if docs[i].Specification != docs[j].Specification {
			return order[docs[i].Specification] < order[docs[j].Specification]
		}
		return docs[i].Round > docs[j].Round
	})
	if docs == nil {
		docs = []briefDocument{}
	}
	return docs
}

// briefInlineInputs is the newest round of each input type the role reads, from the session's checkout.
func briefInlineInputs(docs []briefDocument, inputs []string, local string) []briefInlined {
	var out []briefInlined
	for _, spec := range inputs {
		for _, d := range docs {
			if d.Specification != spec || d.ReplacedBy != "" {
				continue
			}
			in := briefInlined{Specification: spec, Round: d.Round, Path: d.Path}
			switch {
			case d.Path == "":
				in.Missing = "the round has no file"
			case local == "":
				in.Missing = "no local checkout of the documents repository for this session"
			default:
				b, err := os.ReadFile(filepath.Join(local, d.Path))
				if err != nil {
					in.Missing = "not in the checkout: " + err.Error()
				} else {
					in.Content = string(b)
				}
			}
			out = append(out, in)
			break
		}
	}
	return out
}

// tailLines is the last n lines of a file, or none when it does not exist.
func tailLines(path string, n int) []string {
	b, err := os.ReadFile(path)
	if err != nil {
		return []string{}
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return []string{}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

func renderTaskBrief(b *taskBrief) string {
	var sb strings.Builder
	inlineBytes := 0
	for _, in := range b.Inline {
		inlineBytes += len(in.Content)
	}
	fmt.Fprintf(&sb, "Brief for %s as %s: prompt %d bytes, %d documents", b.Key, b.Role, len(b.ServedPrompt), len(b.Documents))
	if b.Inline != nil {
		fmt.Fprintf(&sb, ", inline %d bytes", inlineBytes)
	}
	if b.OrientationCore != "" {
		n := len(b.OrientationCore)
		for _, s := range b.OrientationSections {
			n += len(s.Content)
		}
		fmt.Fprintf(&sb, ", orientation %d bytes", n)
	}
	sb.WriteString("\n\n")

	fmt.Fprintf(&sb, "## 1. Your prompt (%s", b.Role)
	if b.PromptVersion != "" {
		fmt.Fprintf(&sb, ", version %s", b.PromptVersion)
	}
	sb.WriteString(")\n\n")
	sb.WriteString(strings.TrimRight(b.ServedPrompt, "\n"))
	fmt.Fprintf(&sb, "\n\nOrientation: %s\n\n", b.Orientation)

	fmt.Fprintf(&sb, "## 2. The task\n\n")
	t := b.Task
	fmt.Fprintf(&sb, "- **%s** %s\n", str(t["key"]), str(t["title"]))
	fmt.Fprintf(&sb, "- status %s, role %s", strings.ToLower(strings.ReplaceAll(str(t["status"]), "_", " ")), str(t["role"]))
	if lv, ok := t["effectiveWorkLevel"]; ok {
		fmt.Fprintf(&sb, ", work level %d", intOf(lv))
	}
	if g, ok := t["group"]; ok && g != nil {
		fmt.Fprintf(&sb, ", group %v", g)
	}
	if tags, ok := t["tags"].([]string); ok {
		fmt.Fprintf(&sb, ", tags %s", strings.Join(tags, ", "))
	}
	sb.WriteString("\n")
	if h, ok := t["hold"].(map[string]any); ok {
		fmt.Fprintf(&sb, "- held (%v, %v): %v\n", h["kind"], h["level"], h["reason"])
	}
	if inv, ok := t["investigation"].(string); ok {
		fmt.Fprintf(&sb, "- %s. Deliver a BOARD_INVESTIGATION_REPORT and no code: the brief is the description, the inputs are the pinned releases\n", inv)
	}
	if pins, ok := t["pinnedInputs"].([]string); ok {
		fmt.Fprintf(&sb, "- pinned inputs: %s\n", strings.Join(pins, ", "))
	}
	if back, ok := t["reportsReturned"].([]string); ok {
		fmt.Fprintf(&sb, "- reports returned: %s\n", strings.Join(back, "; "))
	}
	if bm, ok := t["budgetMicros"]; ok {
		fmt.Fprintf(&sb, "- budget %s, spent %s\n", usd(bm), usd(t["spentMicros"]))
	} else if sp, ok := t["spentMicros"]; ok {
		fmt.Fprintf(&sb, "- spent %s\n", usd(sp))
	}
	for _, d := range b.Dependencies {
		fmt.Fprintf(&sb, "- depends on %s %s (%s)\n", orElse(d.Key, d.Uuid), d.Title, strings.ToLower(strings.ReplaceAll(d.Status, "_", " ")))
	}
	if qs, ok := t["openQuestions"].([]string); ok {
		for _, q := range qs {
			fmt.Fprintf(&sb, "- open question %s\n", q)
		}
	}
	if desc := str(t["description"]); desc != "" {
		fmt.Fprintf(&sb, "\n%s\n", strings.TrimRight(desc, "\n"))
	}

	sb.WriteString("\n## 3. Its documents\n\n")
	if len(b.Documents) == 0 {
		sb.WriteString("None yet.\n")
	}
	for _, d := range b.Documents {
		fmt.Fprintf(&sb, "- %s round %d v%s, %s", d.Specification, d.Round, d.Version, strings.ToLower(strings.ReplaceAll(d.Lifecycle, "_", " ")))
		if d.Advisory {
			sb.WriteString(", advisory")
		}
		if d.ReplacedBy != "" {
			fmt.Fprintf(&sb, ", replaced by v%s", d.ReplacedBy)
		}
		if d.Verdict != "" {
			fmt.Fprintf(&sb, ", %s", strings.ToLower(d.Verdict))
		}
		if d.Counts != "" {
			fmt.Fprintf(&sb, " (%s)", d.Counts)
		}
		if d.Path != "" {
			fmt.Fprintf(&sb, ": `%s`", d.Path)
		}
		sb.WriteString("\n")
	}
	for _, in := range b.Inline {
		fmt.Fprintf(&sb, "\n### %s round %d (`%s`)\n\n", in.Specification, in.Round, in.Path)
		if in.Missing != "" {
			fmt.Fprintf(&sb, "Not inlined: %s.\n", in.Missing)
		} else {
			sb.WriteString(strings.TrimRight(in.Content, "\n") + "\n")
		}
	}

	sb.WriteString("\n## 4. The repository\n\n")
	fmt.Fprintf(&sb, "- documents repository: %s\n", orElse(b.Repository.Uri, "none on the board"))
	if b.Repository.LocalPath != "" {
		fmt.Fprintf(&sb, "- your checkout: %s\n", b.Repository.LocalPath)
	} else if b.Repository.Clone != "" {
		fmt.Fprintf(&sb, "- no checkout recorded for this session; clone it: `%s` (then pass --repo to doc publish once)\n", b.Repository.Clone)
	}
	if b.Repository.Root != "" {
		fmt.Fprintf(&sb, "- the board's root: `%s`\n", b.Repository.Root)
	}
	if len(b.Repository.Templates) > 0 {
		fmt.Fprintf(&sb, "- path templates: `%s`\n", string(b.Repository.Templates))
	}

	sb.WriteString("\n## 5. The rules\n\n")
	for _, r := range b.Rules {
		fmt.Fprintf(&sb, "- %s\n", r)
	}

	fmt.Fprintf(&sb, "\n## 6. Notes (notes/%s.md)\n\n", strings.ToLower(b.Role))
	if len(b.Notes) == 0 {
		sb.WriteString("None yet.\n")
	}
	for _, n := range b.Notes {
		sb.WriteString(n + "\n")
	}
	if b.OrientationCore != "" {
		keys := make([]string, 0, len(b.OrientationSections))
		for _, sec := range b.OrientationSections {
			keys = append(keys, sec.Key)
		}
		fmt.Fprintf(&sb, "\n## 7. Orientation: the core, then %s\n\n", strings.Join(keys, ", "))
		sb.WriteString(strings.TrimRight(demoteHeadings(b.OrientationCore), "\n"))
		sb.WriteString("\n")
		for _, sec := range b.OrientationSections {
			sb.WriteString("\n")
			sb.WriteString(strings.TrimRight(demoteHeadings(sec.Content), "\n"))
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func usd(v interface{}) string {
	if v == nil {
		return "none"
	}
	return fmt.Sprintf("$%.2f", float64(intOf(v))/1_000_000)
}

func orElse(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

func init() {
	agentTaskBriefCmd.Flags().StringVar(&briefSession, "session", "", "your board session; what the brief shows is recorded as read for it")
	agentTaskBriefCmd.Flags().StringVar(&briefRole, "role", "", "brief as this role instead of the one the task waits on")
	agentTaskBriefCmd.Flags().BoolVar(&briefInline, "inline", false, "print the newest round of each of the role's input types in full, from your checkout")
	agentTaskBriefCmd.Flags().BoolVar(&briefJson, "json", false, "print the brief as JSON")
	agentTaskCmd.AddCommand(agentTaskBriefCmd)
	acceptTaskKeys(agentTaskBriefCmd, firstTaskArg, nil, nil)
}
