package cmd

import (
	"fmt"
	"os"
	"strings"

	rearm "github.com/relizaio/rearm-client-go"
	"github.com/spf13/cobra"
)

// An answer signed off --no-change and the coordinator's verdict on it (task TPF-171, built in TPF-173): the seat's
// verb, the person's twin, and the lines task show prints for a task's answerConfirmations.

var (
	answerVerdict string
	answerNote    string
)

// answerVerdictOf is the verdict as the server's enum: either value in any case, upper case on the wire.
func answerVerdictOf(v string) (string, error) {
	switch u := strings.ToUpper(strings.TrimSpace(v)); u {
	case string(rearm.AgentTaskAnswerVerdictConfirmed), string(rearm.AgentTaskAnswerVerdictRefused):
		return u, nil
	case "":
		return "", fmt.Errorf(`required flag(s) "verdict" not set: --verdict CONFIRMED or REFUSED`)
	default:
		return "", fmt.Errorf("--verdict must be CONFIRMED or REFUSED, not %q", v)
	}
}

// confirmAnswerVars are the variables of a verdict: the session only for the seat (session ""), the note only when
// given.
func confirmAnswerVars(task, session, verdict, note string) (map[string]interface{}, error) {
	v, err := answerVerdictOf(verdict)
	if err != nil {
		return nil, err
	}
	vars := map[string]interface{}{"taskUuid": task, "verdict": v}
	if session != "" {
		vars["sessionUuid"] = session
	}
	if n := strings.TrimSpace(note); n != "" {
		vars["note"] = n
	}
	return vars, nil
}

// verdictArgs takes one task and refuses a missing or unknown --verdict as a usage error, before the session
// fallback, the key lookup or any request.
func verdictArgs(cmd *cobra.Command, args []string) error {
	if err := cobra.ExactArgs(1)(cmd, args); err != nil {
		return err
	}
	_, err := answerVerdictOf(answerVerdict)
	return err
}

var agentTaskConfirmAnswerCmd = &cobra.Command{
	Use:   "confirm-answer <task-uuid>",
	Short: "Coordinator: confirm or refuse an answer signed off --no-change",
	Long: `The ALERT names each answer round and the round before it (release, version, path); compare them, then
--verdict CONFIRMED (the earlier passes stand and the task goes on to the first role without a current pass, which
is the asker unless something else moved) or --verdict REFUSED (every role whose pass no longer stands reviews the
round first). Sending the task on with authorize instead confirms nothing: the round counts as a change, and you
cannot confirm an answer your own agent gave.`,
	Args: verdictArgs,
	Run: func(cmd *cobra.Command, args []string) {
		vars, err := confirmAnswerVars(args[0], taskSessionUuid, answerVerdict, answerNote)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		runGqlCompact(rearm.AgentTaskConfirmAnswerProgrammatic_Operation, vars, "agentTaskConfirmAnswerProgrammatic")
	},
}

var boardsConfirmAnswerCmd = &cobra.Command{
	Use:   "confirm-answer <task>",
	Short: "Confirm or refuse an answer signed off --no-change, as the coordinator does",
	Long: `A person's verdict on an answer signed off --no-change while the task waits on the coordinator:
CONFIRMED keeps the earlier passes (the task goes on to the first role without a current pass, the asker unless
something else moved); REFUSED has every role whose pass no longer stands review the round first. Needs write on
the board. CONFIRMED is refused with a personal key of the user whose key the answering agent runs on; REFUSED is
always accepted.`,
	Args: verdictArgs,
	Run: func(cmd *cobra.Command, args []string) {
		vars, err := confirmAnswerVars(args[0], "", answerVerdict, answerNote)
		if err != nil {
			fail(err.Error())
		}
		runGql(rearm.AgentTaskConfirmAnswer_Operation, vars, "agentTaskConfirmAnswer")
	},
}

// printAnswerConfirmations writes, after the condition skips, one line per answerConfirmations entry of what task
// show read to stderr, oldest first as the server sends them, keyed by task. Nothing for a task without entries.
func printAnswerConfirmations(read interface{}) {
	for _, t := range tasksOf(read) {
		key := orElse(str(t["key"]), str(t["uuid"]))
		for _, e := range asList(t["answerConfirmations"]) {
			fmt.Fprintln(os.Stderr, key+": "+answerConfirmationLine(key, e))
		}
	}
}

// answerConfirmationLine is "answer by <role> at <answeredAt>: <rounds>: <state clause>".
func answerConfirmationLine(key string, e map[string]interface{}) string {
	var clause string
	if state := str(e["state"]); state == "PENDING" {
		clause = "pending (rearm agent task confirm-answer " + key +
			" --session <seat-session-uuid> --verdict CONFIRMED|REFUSED)"
	} else {
		clause = settledLine(strings.ToLower(state), e)
	}
	return "answer by " + str(e["role"]) + " at " + str(e["answeredAt"]) + ": " + answerRoundsClause(e) + ": " + clause
}

// answerRoundsClause is one "<SPEC> round <n> vs round <m>" per specification, from the last entry of rounds for
// it, in the order the specifications first appear. A missing round number reads "release <first 8>" instead.
func answerRoundsClause(e map[string]interface{}) string {
	var order []string
	last := map[string]map[string]interface{}{}
	for _, r := range asList(e["rounds"]) {
		spec := str(r["specification"])
		if _, seen := last[spec]; !seen {
			order = append(order, spec)
		}
		last[spec] = r
	}
	parts := make([]string, 0, len(order))
	for _, spec := range order {
		r := last[spec]
		parts = append(parts, spec+" "+roundOrRelease(r["round"], r["release"])+" vs "+
			roundOrRelease(r["previousRound"], r["previous"]))
	}
	return strings.Join(parts, ", ")
}

// roundOrRelease is "round <n>", or "release <first 8 of the uuid>" when the number is null.
func roundOrRelease(round, release interface{}) string {
	if n, ok := round.(float64); ok {
		return fmt.Sprintf("round %d", int(n))
	}
	rel := str(release)
	if len(rel) > 8 {
		rel = rel[:8]
	}
	return "release " + rel
}

func init() {
	agentTaskCmd.AddCommand(agentTaskConfirmAnswerCmd)
	boardsCmd.AddCommand(boardsConfirmAnswerCmd)

	agentTaskConfirmAnswerCmd.PersistentFlags().StringVar(&taskSessionUuid, "session", "", "Coordinator seat session uuid (required)")
	_ = agentTaskConfirmAnswerCmd.MarkPersistentFlagRequired("session")
	for _, c := range []*cobra.Command{agentTaskConfirmAnswerCmd, boardsConfirmAnswerCmd} {
		c.Flags().StringVar(&answerVerdict, "verdict", "", "CONFIRMED (the answer changes nothing) or REFUSED (review it as a change) (required)")
		c.Flags().StringVar(&answerNote, "note", "", "Why, kept on the confirmation record")
		_ = c.MarkFlagRequired("verdict")
	}
}
