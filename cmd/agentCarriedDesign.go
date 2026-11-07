package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// The carried design on a split child (task TPF-163, parent TPF-161 D9, 6.2): a child the coordinator split with
// designFromParent builds from the parent's reviewed rounds, and its design-stage roles count as passed through
// the parent's sign-offs. task show and task brief print one line for the record; the brief lists the carried
// rounds under its documents and inlines them.

// carriedWhen is a sign-off instant as the line prints it, "2026-11-06 17:38 UTC"; a value that does not parse is
// printed as read.
func carriedWhen(v interface{}) string {
	s := str(v)
	if at, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return at.UTC().Format("2006-01-02 15:04 UTC")
	}
	return s
}

// carriedRoundLabel: "ARCHITECTURE round 1 (<path>)", the round and the path each left out when null.
func carriedRoundLabel(r map[string]interface{}) string {
	label := str(r["specification"])
	if r["round"] != nil {
		label += fmt.Sprintf(" round %d", intOf(r["round"]))
	}
	if p := str(r["path"]); p != "" {
		label += " (" + p + ")"
	}
	return label
}

// carriedRoleLabel: "<role> (<when>, by <person>)". A person is named only from reviewedBy, the human marker on a
// sign-off; a REJECTED pass a person decided over says so without a name; the signing session is never printed,
// so a blind read (session null) reads as an agent's pass, never as a person's. Nothing is printed for a null.
func carriedRoleLabel(r map[string]interface{}) string {
	var detail []string
	if when := carriedWhen(r["signedOffAt"]); when != "" {
		detail = append(detail, when)
	}
	if p, _ := r["reviewedBy"].(map[string]interface{}); p != nil {
		detail = append(detail, "by "+orElse(str(p["name"]), strings.ToLower(str(p["kind"]))))
	} else if r["decidedOver"] == true {
		detail = append(detail, "decided over by a person")
	}
	if len(detail) == 0 {
		return str(r["role"])
	}
	return str(r["role"]) + " (" + strings.Join(detail, ", ") + ")"
}

// carriedDesignLine is the line for a task's carried design: "design carried from TPF-12: ARCHITECTURE round 1
// (<path>), THREAT_MODEL round 1 (<path>); passed by architect (<when>), ...; standing". A role whose carried pass
// no longer stands adds "; <role> no longer carried". "" when the task carries none.
func carriedDesignLine(c map[string]interface{}) string {
	if c == nil {
		return ""
	}
	var rounds, roles, stale []string
	for _, r := range asList(c["rounds"]) {
		rounds = append(rounds, carriedRoundLabel(r))
	}
	for _, r := range asList(c["roles"]) {
		roles = append(roles, carriedRoleLabel(r))
		if r["live"] == false {
			stale = append(stale, str(r["role"])+" no longer carried")
		}
	}
	line := "design carried from " + orElse(str(c["parentKey"]), str(c["parentTask"]))
	if len(rounds) > 0 {
		line += ": " + strings.Join(rounds, ", ")
	}
	if len(roles) > 0 {
		line += "; passed by " + strings.Join(roles, ", ")
	}
	for _, s := range stale {
		line += "; " + s
	}
	if c["standing"] == true {
		return line + "; standing"
	}
	return line + "; not standing"
}

// compactCarriedLine is compact's short form: "carried from TPF-12 (2 rounds, standing|stale)".
func compactCarriedLine(t map[string]interface{}) string {
	c, _ := t["carriedDesign"].(map[string]interface{})
	if c == nil {
		return ""
	}
	n := len(asList(c["rounds"]))
	plural := "s"
	if n == 1 {
		plural = ""
	}
	state := "stale"
	if c["standing"] == true {
		state = "standing"
	}
	return fmt.Sprintf("carried from %s (%d round%s, %s)", orElse(str(c["parentKey"]), str(c["parentTask"])), n, plural, state)
}

// printCarriedDesign writes, after the waiver lines, the carried design line of each task show read to stderr,
// keyed by task; the JSON on stdout stays whole.
func printCarriedDesign(read interface{}) {
	for _, t := range tasksOf(read) {
		if c, _ := t["carriedDesign"].(map[string]interface{}); c != nil {
			fmt.Fprintln(os.Stderr, orElse(str(t["key"]), str(t["uuid"]))+": "+carriedDesignLine(c))
		}
	}
}

// briefCarriedRound is one carried round in the brief's documents section; the lifecycle comes from the
// assignment's bound inputs when the round is bound there, and is empty otherwise.
type briefCarriedRound struct {
	Release       string `json:"release"`
	Specification string `json:"specification"`
	Round         int    `json:"round,omitempty"`
	Lifecycle     string `json:"lifecycle,omitempty"`
	Path          string `json:"path,omitempty"`
}

// briefCarriedRounds is the task's carried rounds, in the record's order, and the parent's key; none when the
// task carries no design.
func briefCarriedRounds(t map[string]interface{}) (string, []briefCarriedRound) {
	c, _ := t["carriedDesign"].(map[string]interface{})
	if c == nil {
		return "", nil
	}
	bound := map[string]string{}
	if a, _ := t["assignment"].(map[string]interface{}); a != nil {
		for _, in := range asList(a["resolvedInputs"]) {
			bound[str(in["release"])] = str(in["lifecycle"])
		}
	}
	var out []briefCarriedRound
	for _, r := range asList(c["rounds"]) {
		out = append(out, briefCarriedRound{Release: str(r["release"]), Specification: str(r["specification"]),
			Round: intOf(r["round"]), Lifecycle: bound[str(r["release"])], Path: str(r["path"])})
	}
	return orElse(str(c["parentKey"]), str(c["parentTask"])), out
}

// boundReleases is the release each input type of the briefed role resolved to at its assignment; empty when the
// task is not assigned to that role.
func boundReleases(t map[string]interface{}, role string) map[string]string {
	out := map[string]string{}
	a, _ := t["assignment"].(map[string]interface{})
	if a == nil || !strings.EqualFold(str(a["role"]), role) {
		return out
	}
	for _, in := range asList(a["resolvedInputs"]) {
		if spec := str(in["specification"]); spec != "" {
			out[spec] = str(in["release"])
		}
	}
	return out
}

// carriedInputFor picks the carried round --inline prints for an input type: the one the role's input resolved
// to at its assignment, else, before an assignment binds anything, the carried round of that type when the task
// has no round of its own (the server resolves a carried child's input the same way). ok is false when the task's
// own round is the input.
func carriedInputFor(spec string, docs []briefDocument, carried []briefCarriedRound, bound map[string]string) (briefCarriedRound, bool) {
	if rel, ok := bound[spec]; ok {
		for _, c := range carried {
			if c.Release == rel {
				return c, true
			}
		}
		return briefCarriedRound{}, false
	}
	for _, d := range docs {
		if d.Specification == spec {
			return briefCarriedRound{}, false
		}
	}
	for _, c := range carried {
		if c.Specification == spec {
			return c, true
		}
	}
	return briefCarriedRound{}, false
}

// renderCarriedRounds is the documents section's sub-list: "Carried from TPF-12:" then one
// "- ARCHITECTURE round 1 (assembled) `<path>`" per round.
// The blank line before it separates it from the task's own documents, when there are any.
func renderCarriedRounds(sb *strings.Builder, parent string, carried []briefCarriedRound, afterOwn bool) {
	if len(carried) == 0 {
		return
	}
	if afterOwn {
		sb.WriteString("\n")
	}
	fmt.Fprintf(sb, "Carried from %s:\n", parent)
	for _, c := range carried {
		sb.WriteString("- " + c.Specification)
		if c.Round > 0 {
			fmt.Fprintf(sb, " round %d", c.Round)
		}
		if c.Lifecycle != "" {
			fmt.Fprintf(sb, " (%s)", strings.ToLower(strings.ReplaceAll(c.Lifecycle, "_", " ")))
		}
		if c.Path != "" {
			fmt.Fprintf(sb, " `%s`", c.Path)
		}
		sb.WriteString("\n")
	}
}
