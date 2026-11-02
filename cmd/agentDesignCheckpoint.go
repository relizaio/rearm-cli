package cmd

import (
	"fmt"
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
