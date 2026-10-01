package cmd

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// Task RD4-12: an investigation. A role commissions another for a report through the board, and the report comes
// back pinned on the commissioning task.

// commissionOperation is the commission mutation, local to the CLI so the verb needs no rearm-client-go pin move.
const commissionOperation = `mutation AgentTaskCommissionProgrammatic ($input: AgentTaskCommissionInput!) {
	agentTaskCommissionProgrammatic(input: $input) {
	key number uuid org board title description status role orderIndex workLevel budgetMicros dependsOn
	kind investigation { commissionedBy { role roleUuid session task by { kind uuid name } } deliverable role roleUuid review reviewUuid deadline returnTo report completedAt }
	requiredInputs { kind specification scope minLifecycle resolution release }
	createdDate
} }`

var (
	commissionSession   string
	commissionBoard     string
	commissionRole      string
	commissionTitle     string
	commissionBrief     string
	commissionBriefFile string
	commissionFromTask  string
	commissionInputs    []string
	commissionBudget    string
	commissionDeadline  string
	commissionReview    string
	commissionReturnTo  string
	commissionLevel     int
	commissionGroup     string
	commissionTags      []string
)

var agentTaskCommissionCmd = &cobra.Command{
	Use:   "commission",
	Short: "Commission an investigation: ask a role for a report that comes back pinned on your task",
	Long: `Commissions an investigation: a new task of kind INVESTIGATION for a role that produces
BOARD_INVESTIGATION_REPORT, with the brief as its description and the --input releases pinned as what
it reads. It delivers a report, not code, and completes when that role passes with it (and the
--review role passes it, when one was asked for).

With --session, the session commissions from the task it holds (--from-task, the task's key or
uuid), in that task's role, and only a role that role's commissions name; the role's intake
queues it at once (AUTO) or sends it through the coordinator (COORDINATOR). The report comes back
pinned as an input on --from-task, and the task is offered back to your role. To wait for it,
return your hop with --reason BLOCKED_ON_DEPENDENCY.

Without --session the key commissions as a person with BOARD_WRITE does: any role that produces
the report, from a task or from none.

--budget is in dollars; left out, the commissioning role's default, capped by the board's budget.
--deadline is RFC 3339 (2026-10-02T12:00:00Z) or a span from now (48h, 90m, 3d); the board's
investigationOverdue staleness rule alerts past it.`,
	Run: func(cmd *cobra.Command, args []string) {
		v, err := commissionVariables(commissionOpts{
			session: commissionSession, board: commissionBoard, role: commissionRole, title: commissionTitle,
			brief: commissionBrief, briefFile: commissionBriefFile, fromTask: commissionFromTask,
			inputs: commissionInputs, budget: commissionBudget, deadline: commissionDeadline, review: commissionReview,
			returnTo: commissionReturnTo, level: commissionLevel, levelSet: cmd.Flags().Changed("work-level"),
			group: commissionGroup, tags: commissionTags,
		}, time.Now(), os.ReadFile)
		if err != nil {
			fail(err.Error())
		}
		runGqlCompact(commissionOperation, v, "agentTaskCommissionProgrammatic")
	},
}

type commissionOpts struct {
	session, board, role, title, brief, briefFile, fromTask string
	inputs                                                  []string
	budget, deadline, review, returnTo                      string
	level                                                   int
	levelSet                                                bool
	group                                                   string
	tags                                                    []string
}

// commissionVariables are the commission's variables from its flags, refused before sending when a flag cannot be
// read: the brief from --brief or --brief-file (not both), the budget in dollars, the deadline as RFC 3339 or a span.
func commissionVariables(o commissionOpts, now time.Time, readFile func(string) ([]byte, error)) (map[string]interface{}, error) {
	if strings.TrimSpace(o.board) == "" {
		return nil, errors.New("give --board: the board the investigation goes on")
	}
	if strings.TrimSpace(o.role) == "" {
		return nil, errors.New("give --role: the role to investigate, one that produces BOARD_INVESTIGATION_REPORT")
	}
	if strings.TrimSpace(o.title) == "" {
		return nil, errors.New("give --title: one line; the rest goes in the brief")
	}
	if strings.TrimSpace(o.session) != "" && strings.TrimSpace(o.fromTask) == "" {
		return nil, errors.New("give --from-task: a session commissions from the task it holds")
	}
	brief := o.brief
	if o.briefFile != "" {
		if o.brief != "" {
			return nil, errors.New("give the brief once: --brief or --brief-file")
		}
		b, err := readFile(o.briefFile)
		if err != nil {
			return nil, fmt.Errorf("could not read --brief-file: %v", err)
		}
		brief = string(b)
	}
	input := map[string]interface{}{"boardUuid": boardArg(o.board), "role": strings.TrimSpace(o.role),
		"title": strings.TrimSpace(o.title)}
	if s := strings.TrimSpace(o.session); s != "" {
		input["sessionUuid"] = s
	}
	if strings.TrimSpace(brief) != "" {
		input["brief"] = strings.TrimSpace(brief)
	}
	if f := strings.TrimSpace(o.fromTask); f != "" {
		input["fromTask"] = f
	}
	var inputs []string
	for _, in := range o.inputs {
		if t := strings.TrimSpace(in); t != "" {
			inputs = append(inputs, t)
		}
	}
	if len(inputs) > 0 {
		input["inputs"] = inputs
	}
	if strings.TrimSpace(o.budget) != "" {
		micros, err := parseBudgetUSD(o.budget)
		if err != nil {
			return nil, err
		}
		input["budgetMicros"] = micros
	}
	if strings.TrimSpace(o.deadline) != "" {
		due, err := parseDeadline(o.deadline, now)
		if err != nil {
			return nil, err
		}
		input["deadline"] = due
	}
	if r := strings.TrimSpace(o.review); r != "" {
		input["review"] = r
	}
	if r := strings.ToUpper(strings.TrimSpace(o.returnTo)); r != "" {
		if r != "TASK" && r != "NONE" {
			return nil, fmt.Errorf("--return-to is TASK or NONE, not %q", o.returnTo)
		}
		input["returnTo"] = r
	}
	if o.levelSet {
		input["workLevel"] = o.level
	}
	return map[string]interface{}{"input": withGroupAndTags(input, o.group, o.tags)}, nil
}

// parseDeadline reads --deadline: an RFC 3339 time, or a span from now in minutes, hours or days (90m, 48h, 3d),
// rendered as RFC 3339 in UTC.
func parseDeadline(v string, now time.Time) (string, error) {
	s := strings.TrimSpace(v)
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC().Format(time.RFC3339), nil
	}
	if strings.HasSuffix(s, "d") {
		if n, err := strconv.Atoi(strings.TrimSuffix(s, "d")); err == nil && n > 0 {
			return now.Add(time.Duration(n) * 24 * time.Hour).UTC().Format(time.RFC3339), nil
		}
	}
	if d, err := time.ParseDuration(s); err == nil && d > 0 {
		return now.Add(d).UTC().Format(time.RFC3339), nil
	}
	return "", fmt.Errorf("--deadline is an RFC 3339 time (2026-10-02T12:00:00Z) or a span from now (90m, 48h, 3d), not %q", v)
}

// investigationLine is how the compact output names an investigation: whose report, for whom, and where it goes.
func investigationLine(t map[string]interface{}) string {
	inv, _ := t["investigation"].(map[string]interface{})
	if str(t["kind"]) != "INVESTIGATION" || inv == nil {
		return ""
	}
	line := "investigation: a report by " + orElse(str(inv["role"]), "?")
	if r := str(inv["review"]); r != "" {
		line += ", reviewed by " + r
	}
	if d := str(inv["deadline"]); d != "" {
		line += ", due " + d
	}
	by, _ := inv["commissionedBy"].(map[string]interface{})
	if str(inv["returnTo"]) == "TASK" && by != nil && str(by["task"]) != "" {
		line += "; its report comes back pinned on task " + str(by["task"])
	} else {
		line += "; its report stays on it"
	}
	return line
}

func init() {
	f := agentTaskCommissionCmd.Flags()
	f.StringVar(&commissionSession, "session", "", "your board session, holding --from-task; left out, the key commissions as a person")
	f.StringVar(&commissionBoard, "board", "", "the board, by uuid or name — required")
	f.StringVar(&commissionRole, "role", "", "the role to investigate, one that produces BOARD_INVESTIGATION_REPORT — required")
	f.StringVar(&commissionTitle, "title", "", "one line, at most 120 characters — required")
	f.StringVar(&commissionBrief, "brief", "", "what to find out: the investigation's description")
	f.StringVar(&commissionBriefFile, "brief-file", "", "read the brief from this file")
	f.StringVar(&commissionFromTask, "from-task", "", "the task it is commissioned from, key or uuid; its report comes back pinned there")
	f.StringSliceVar(&commissionInputs, "input", nil, "a release the investigation reads, pinned; repeat or separate with commas")
	f.StringVar(&commissionBudget, "budget", "", "what it may spend, in dollars; left out, the role's default")
	f.StringVar(&commissionDeadline, "deadline", "", "when the report is due: RFC 3339, or a span from now (48h, 3d)")
	f.StringVar(&commissionReview, "review", "", "a role that reviews the report before it comes back")
	f.StringVar(&commissionReturnTo, "return-to", "", "TASK (the default with --from-task) or NONE")
	f.IntVar(&commissionLevel, "work-level", 0, "the investigation's work level, a rung of the board's ladder")
	f.StringVar(&commissionGroup, "group", "", "a task group by key")
	f.StringSliceVar(&commissionTags, "tag", nil, "a tag; repeat or separate with commas")
	agentTaskCmd.AddCommand(agentTaskCommissionCmd)
	acceptTaskKeys(agentTaskCommissionCmd, noTaskArgs, []*string{&commissionFromTask}, nil)
}

// investigationRead is what a brief adds to the task for an investigation (task RD4-12): its kind and block, the
// reports returned to it, and the releases pinned on it. Local to the CLI, like the commission, so the brief shows
// them without a rearm-client-go pin move; a server without them answers with an error, and the brief goes on.
const investigationRead = `query AgentTaskInvestigationProgrammatic ($taskUuid: ID!) { agentTaskProgrammatic(taskUuid: $taskUuid) {
	kind investigation { commissionedBy { role roleUuid session task } deliverable role review deadline returnTo report completedAt }
	reportsReturned { investigation investigationKey report session role at reoffered cancelled note }
	requiredInputs { kind specification release }
} }`

// mergeInvestigation adds the investigation fields to a task read, when the server has them.
func mergeInvestigation(task map[string]interface{}, taskUuid string) {
	data, err := sendGraphQLRequest(investigationRead, map[string]interface{}{"taskUuid": taskUuid})
	if err != nil {
		return
	}
	extra, _ := data["agentTaskProgrammatic"].(map[string]interface{})
	for _, k := range []string{"kind", "investigation", "reportsReturned", "requiredInputs"} {
		if v, ok := extra[k]; ok && v != nil {
			task[k] = v
		}
	}
}
