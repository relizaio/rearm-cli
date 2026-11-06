package cmd

import (
	"fmt"
	"os"
	"strings"
)

// designCheckpointLine is the brief's line for a task's design checkpoint record (task SPLIT-7): its state, never
// "waits", since a PENDING record outlives the wait at the coordinator. "" when the task never reached one.
func designCheckpointLine(c map[string]interface{}) string {
	if c == nil {
		return ""
	}
	head := "design checkpoint before " + str(c["beforeRole"]) + ": "
	switch str(c["state"]) {
	case "PENDING":
		line := head + "pending since " + str(c["reachedAt"])
		if after := str(c["afterRole"]); after != "" {
			line += ", reached after " + after
		}
		design, _ := c["design"].([]interface{})
		n := len(design)
		plural := "s"
		if n == 1 {
			plural = ""
		}
		return fmt.Sprintf("%s (%d design round%s)", line, n, plural)
	case "SENT_ON":
		if role, text, ok := waiverNote(str(c["note"])); ok {
			// A pass the architect waived with --no-split (task TPF-153): the server wrote the note
			// "waived by <role>: <text>", or "waived by <role>" alone for a blind reader, whose settledBy is null too.
			line := "design checkpoint waived by " + role
			if text != "" {
				line += fmt.Sprintf(": %q", text)
			}
			return line + " (before " + str(c["beforeRole"]) + ", " + str(c["settledAt"]) + ")"
		}
		return head + settledLine("sent on", c)
	case "SPLIT":
		return head + settledLine("split", c)
	}
	return head + strings.ToLower(str(c["state"]))
}

// settledLine: "sent on by <who> <when>: <note>", the note and its colon left out when null.
func settledLine(verb string, c map[string]interface{}) string {
	parts := []string{verb}
	if a, _ := c["settledBy"].(map[string]interface{}); a != nil {
		parts = append(parts, "by", orElse(str(a["name"]), strings.ToLower(str(a["kind"]))))
	}
	if at := str(c["settledAt"]); at != "" {
		parts = append(parts, at)
	}
	line := strings.Join(parts, " ")
	if note := str(c["note"]); note != "" {
		line += ": " + note
	}
	return line
}

// waivedNotePrefix starts the note the server writes on a checkpoint the architect waived (task TPF-152).
const waivedNotePrefix = "waived by "

// waiverNote splits a waived checkpoint's note "waived by <role>: <text>" on its first ": ". A note
// "waived by <role>" with no ": " (a blind reader's copy) gives an empty text; ok is false for any other note.
func waiverNote(note string) (role, text string, ok bool) {
	if !strings.HasPrefix(note, waivedNotePrefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(note, waivedNotePrefix)
	if i := strings.Index(rest, ": "); i >= 0 {
		return rest[:i], rest[i+2:], true
	}
	return rest, "", true
}

// architectureRounds maps each of the task's ARCHITECTURE release uuids to its round, to label a waiver's
// designRound. Read for the label only: whether the waiver stands is the server's standing, never derived here.
func architectureRounds(t map[string]interface{}) map[string]any {
	out := map[string]any{}
	for _, d := range asList(t["documents"]) {
		doc, _ := d["document"].(map[string]interface{})
		if doc == nil || str(doc["specification"]) != "ARCHITECTURE" || str(d["uuid"]) == "" {
			continue
		}
		out[str(d["uuid"])] = doc["round"]
	}
	return out
}

// waiverHead is "waiver: --no-split by <role> on ARCHITECTURE round <n> at <at>", or "on release <first 8>" for a
// round the read did not return. It never prints bySession, byAgent or the waiver's note: a blind reader's copy
// has them null, and the note is the checkpoint line's.
func waiverHead(w map[string]interface{}, rounds map[string]any) string {
	round := str(w["designRound"])
	label := "release " + round
	if len(round) > 8 {
		label = "release " + round[:8]
	}
	if n, ok := rounds[round]; ok && n != nil {
		label = fmt.Sprintf("ARCHITECTURE round %d", intOf(n))
	}
	return "waiver: --no-split by " + str(w["role"]) + " on " + label + " at " + str(w["at"])
}

// waiverLine is the brief's waiver line: the head, then one clause, ignored where the board's switch is off,
// else not standing when the server says the waiver no longer stands.
func waiverLine(w map[string]interface{}, rounds map[string]any, switchOff bool) string {
	line := waiverHead(w, rounds)
	if switchOff {
		return line + " (ignored: this board's architectCheckpointWaiver is off)"
	}
	if w["standing"] == false {
		return line + " (not standing: a newer design round, an advisory round or a reopen since)"
	}
	return line
}

// printWaiverLines writes, beside the base-moved lines, the waived checkpoint line and the waiver line of what
// task show read to stderr, keyed by task (task TPF-153). task show reads no board, so a waiver that does not
// stand says only "(not standing)"; the brief says why.
func printWaiverLines(read interface{}) {
	var tasks []interface{}
	switch x := read.(type) {
	case []interface{}:
		tasks = x
	case map[string]interface{}:
		tasks = []interface{}{x}
	}
	for _, e := range tasks {
		t, _ := e.(map[string]interface{})
		if t == nil {
			continue
		}
		key := orElse(str(t["key"]), str(t["uuid"]))
		if c, _ := t["designCheckpoint"].(map[string]interface{}); c != nil && str(c["state"]) == "SENT_ON" {
			if _, _, ok := waiverNote(str(c["note"])); ok {
				fmt.Fprintln(os.Stderr, key+": "+designCheckpointLine(c))
			}
		}
		if w, _ := t["designCheckpointWaiver"].(map[string]interface{}); w != nil {
			line := waiverHead(w, architectureRounds(t))
			if w["standing"] == false {
				line += " (not standing)"
			}
			fmt.Fprintln(os.Stderr, key+": "+line)
		}
	}
}

// The server's refusals of --no-split (TPF-152 4.2) that task signoff can tell from the command line alone,
// copied word for word so the local and the server's refusal read the same.
const (
	noSplitNeedsPassed = "--no-split says a passed design fits one coder pass; a rejection hands nothing over, so sign off REJECTED without it"
	noSplitNeedsNote   = "--no-split needs a note: say in one line why the design fits one coder pass (--note '...')"
)
