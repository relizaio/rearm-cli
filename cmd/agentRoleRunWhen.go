package cmd

import (
	"fmt"
	"os"
	"strings"
)

// A role's runWhen condition (task TPF-156, backend TPF-155): routing queues an OPTIONAL role only on a task whose
// tags meet it, and records each role it passed over on the task (rolesSkippedByCondition). The CLI sends the
// condition, prints it beside roleconfig list, and prints the record in the brief, beside task show and task list,
// and in the compact line. It decides nothing: the server checks the keys and the roles, and resolves live.

// runWhenClearAlone is the one local refusal: one input field carries one value, so the server cannot see a clear
// given together with a list.
const runWhenClearAlone = "--run-when-clear clears the condition; give it alone, without --run-when-tagged-any or --run-when-tagged-none"

// runWhenHint is the run-it-now line, the verb per state, printed whole by task show after a task's passed-over lines.
const runWhenHint = "to run a passed-over role now, name it with the verb the task's state admits: task hold then task lifthold --role <role> while QUEUED; task authorize --role <role> while AWAITING_COORDINATOR; task reopen --role <role> --reason '<why>' once COMPLETED"

// keys is a run-when flag's list as sent: each entry trimmed, blanks dropped, never nil, so a list the caller left
// out marshals as [] (the server reads a missing side as no condition on it). Case is the server's to fold.
func keys(list []string) []string {
	out := []string{}
	for _, k := range list {
		if k = strings.TrimSpace(k); k != "" {
			out = append(out, k)
		}
	}
	return out
}

// strList is a JSON list of strings as read: each non-empty string element, nil for anything else.
func strList(v interface{}) []string {
	l, _ := v.([]interface{})
	var out []string
	for _, x := range l {
		if s := str(x); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// runWhenLine is a condition as roleconfig list prints it: "runs when: tagged a or b", "runs when: not tagged c",
// or "runs when: tagged a and not tagged c"; empty for no condition.
func runWhenLine(rw interface{}) string {
	m, _ := rw.(map[string]interface{})
	if m == nil {
		return ""
	}
	anyOf, noneOf := strList(m["taggedAny"]), strList(m["taggedNone"])
	var parts []string
	if len(anyOf) > 0 {
		parts = append(parts, "tagged "+strings.Join(anyOf, " or "))
	}
	if len(noneOf) > 0 {
		parts = append(parts, "not tagged "+strings.Join(noneOf, " or "))
	}
	if len(parts) == 0 {
		return ""
	}
	return "runs when: " + strings.Join(parts, " and ")
}

// printRunWhenLines writes, after roleconfig list's JSON, one line per role with a condition to stderr, keyed by
// role, so the JSON on stdout stays whole for a script reading it.
func printRunWhenLines(read interface{}) {
	var roles []interface{}
	switch x := read.(type) {
	case []interface{}:
		roles = x
	case map[string]interface{}:
		roles = []interface{}{x}
	}
	for _, e := range roles {
		rc, _ := e.(map[string]interface{})
		if rc == nil {
			continue
		}
		if line := runWhenLine(rc["runWhen"]); line != "" {
			fmt.Fprintln(os.Stderr, str(rc["name"])+": "+line)
		}
	}
}

// conditionSkipLine is one entry of the record: "passed over <role> at <at>: <reason>[; waives A, B][ (since ran)]".
// The marker reads the server's live as is; an entry without it is not marked.
func conditionSkipLine(e map[string]interface{}) string {
	line := "passed over " + str(e["role"]) + " at " + str(e["at"]) + ": " + str(e["reason"])
	if w := strList(e["waives"]); len(w) > 0 {
		line += "; waives " + strings.Join(w, ", ")
	}
	if live, ok := e["live"].(bool); ok && !live {
		line += " (since ran)"
	}
	return line
}

// liveSkipRoles is the role of each entry the server resolved live, in the order returned. Nothing is derived: an
// entry without live is not named.
func liveSkipRoles(t map[string]interface{}) []string {
	var out []string
	for _, e := range asList(t["rolesSkippedByCondition"]) {
		if live, _ := e["live"].(bool); live {
			out = append(out, str(e["role"]))
		}
	}
	return out
}

// tasksOf is a task read as a list: the array, or the single task.
func tasksOf(read interface{}) []map[string]interface{} {
	var all []interface{}
	switch x := read.(type) {
	case []interface{}:
		all = x
	case map[string]interface{}:
		all = []interface{}{x}
	}
	var out []map[string]interface{}
	for _, e := range all {
		if t, _ := e.(map[string]interface{}); t != nil {
			out = append(out, t)
		}
	}
	return out
}

// printConditionSkips writes, after the waiver lines, each entry of what task show read to stderr, keyed by task,
// then the run-it-now hint once when an entry is live. The hint goes to every caller: only the seat can run its
// verbs, and the line names verbs, no data.
func printConditionSkips(read interface{}) {
	for _, t := range tasksOf(read) {
		key := orElse(str(t["key"]), str(t["uuid"]))
		for _, e := range asList(t["rolesSkippedByCondition"]) {
			fmt.Fprintln(os.Stderr, key+": "+conditionSkipLine(e))
		}
		if len(liveSkipRoles(t)) > 0 {
			fmt.Fprintln(os.Stderr, key+": "+runWhenHint)
		}
	}
}

// printPassedOverSummary writes, after task list's JSON, one line per task with a live entry to stderr.
func printPassedOverSummary(read interface{}) {
	for _, t := range tasksOf(read) {
		if roles := liveSkipRoles(t); len(roles) > 0 {
			fmt.Fprintln(os.Stderr, orElse(str(t["key"]), str(t["uuid"]))+": passed over: "+strings.Join(roles, ", "))
		}
	}
}
