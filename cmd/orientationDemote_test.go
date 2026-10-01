package cmd

import (
	"regexp"
	"strings"
	"testing"
)

// The brief nests the orientation one heading level down, unnumbered (RD3-10 architecture round 2).

const numberedCore = "---\nrearm_cli_min: 26.09.1\n---\n\n# ReARM agent orientation\n\nIntro.\n\n## Where to read what\n\n" +
	"| when you need to | read |\n|---|---|\n| wait for work | `waiting` |\n\n## 1. Prerequisites\n\n### 1.1 Environment variables\n\n" +
	"```bash\n# a shell comment, not a heading\nexport REARM_URL=...\n```\n\n## 2. Session lifecycle\n\n### 2.3 Initializing\n\n" +
	"#### Install the usage hooks — right after `init`\n\n### 2.8 Heartbeat and close\n"

const numberedSection = "<!-- orientation section: waiting · core 2026-09-28 -->\n### 2.5c Waiting for work (task boards)\n\nText.\n\n" +
	"### 2.5d Working a task in a fresh context (task boards)\n\n## 10. Deployment operations (ReARM Pro only)\n"

func TestTheOrientationIsDemotedAndUnnumbered(t *testing.T) {
	got := demoteHeadings(numberedCore)
	for _, want := range []string{"### Where to read what", "### Prerequisites", "#### Environment variables", "### Session lifecycle",
		"#### Initializing", "##### Install the usage hooks — right after `init`", "#### Heartbeat and close",
		"# a shell comment, not a heading", "| wait for work | `waiting` |", "Intro."} {
		if !strings.Contains(got, want+"\n") {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	for _, gone := range []string{"rearm_cli_min", "# ReARM agent orientation", "## 1. Prerequisites", "### 1.1 "} {
		if strings.Contains(got, gone) {
			t.Errorf("%q should be gone from\n%s", gone, got)
		}
	}
	sec := demoteHeadings(numberedSection)
	for _, want := range []string{"<!-- orientation section: waiting · core 2026-09-28 -->", "#### Waiting for work (task boards)",
		"#### Working a task in a fresh context (task boards)", "### Deployment operations (ReARM Pro only)"} {
		if !strings.Contains(sec, want) {
			t.Errorf("missing %q in\n%s", want, sec)
		}
	}
}

func TestTheBriefsPartNumbersAreItsOwn(t *testing.T) {
	b := &taskBrief{Key: "RD-1", Role: "coder", ServedPrompt: "the prompt", Task: map[string]any{"key": "RD-1", "title": "t"},
		Rules: []string{"a rule"}, Notes: []string{}, Orientation: "https://rearm.example/api/agents/orientation.md",
		OrientationCore: numberedCore, OrientationSections: []briefSection{{Key: "waiting", Content: numberedSection}}}
	out := renderTaskBrief(b)
	numbered := regexp.MustCompile(`(?m)^#{1,6} (\d+(\.\d+)*[a-z]?)\.? `)
	seen := map[string]int{}
	for _, m := range numbered.FindAllStringSubmatch(out, -1) {
		seen[m[1]]++
	}
	for n, c := range seen {
		if c > 1 {
			t.Errorf("heading number %s appears %d times", n, c)
		}
	}
	for _, n := range []string{"1", "2", "3", "4", "5", "6", "7"} {
		if seen[n] != 1 {
			t.Errorf("the brief's part %s should be its only heading numbered %s, got %d", n, n, seen[n])
		}
	}
	if len(seen) != 7 {
		t.Errorf("only the brief's own seven parts are numbered, got %v", seen)
	}
	part := out[strings.Index(out, "## 7. Orientation"):]
	if !strings.Contains(part, "\n### Prerequisites\n") || strings.Contains(part, "\n## ") {
		t.Errorf("the orientation sits one level under part 7, got\n%s", part)
	}
}
