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
	"errors"
	"fmt"
	"os"

	rearm "github.com/relizaio/rearm-client-go"
	"github.com/spf13/cobra"
)

// The served prompt by version (task TPF-110, child 2 of TPF-97). The server answers each served prompt as
// {version, status, text}: SERVED with the text, or UNCHANGED without it when the caller says it holds that
// version (heldVersions). The CLI keeps no record of what any context holds: the context that printed a text
// is the only one that can pass its version back, with --prompt-version; --full-prompt sends nothing. The CLI
// only transports: it builds heldVersions from the flag, prints the text where it always printed it when the
// server says SERVED and a short form when it says UNCHANGED, and falls back to the legacy selection on a
// server that predates the field.

// Bounds of --prompt-version: the server's heldVersions bound (TPF-109), checked before any request so the
// server's refusal, which comes after an assign has been made, is never reached from this flag.
const (
	maxPromptVersions      = 50
	maxPromptVersionLength = 128
	promptPinLength        = 12
)

var errPromptVersionBound = errors.New("rearm: --prompt-version: at most 50 values of at most 128 characters, none empty")

// promptByVersionHelp is the paragraph task next and wait add to their help.
const promptByVersionHelp = `The served prompt is transferred only to a context that does not hold it.
The first offer printed carries it in full as rolePrompt, with its version in
promptByVersion.version (64 hex). Pass that version back with --prompt-version
on every later call (repeat the flag for several boards): when the prompt has
not changed the offer carries promptByVersion {version, status: UNCHANGED}
and no rolePrompt, and one stderr line says so; keep the prompt you hold.
status SERVED with rolePrompt means it changed (the operator edited it, or the
board's rules moved): re-read it and pass the new version from then on. Pass
only a version whose text is in your context: after a compaction or a restart
run without --prompt-version, or with --full-prompt, which prints it in full
whatever was passed. The CLI records nothing. On a server older than this
feature the prompt is printed every time.`

// promptFlags are one verb's --full-prompt and --prompt-version.
type promptFlags struct {
	full     bool
	versions []string
}

var (
	nextPromptFlags       promptFlags
	assignPromptFlags     promptFlags
	briefPromptFlags      promptFlags
	coordinatePromptFlags promptFlags
	waitPromptFlags       promptFlags
)

// promptByVersionUnsupported is set once a server answered FieldUndefined to a by-version operation: later
// calls in the process send the legacy operation at once.
var promptByVersionUnsupported bool

// assignSentHeldVersions tells the compact assign line whether this assign sent any held version, so a
// SERVED answer reads as "changed" only when the caller said it held one.
var assignSentHeldVersions bool

func addPromptFlags(cmd *cobra.Command, f *promptFlags) {
	cmd.Flags().StringArrayVar(&f.versions, "prompt-version", nil,
		"served-prompt version(s) this context holds, as printed in promptByVersion.version (64 hex) with the last "+
			"SERVED prompt; sent as heldVersions; repeat for several boards")
	cmd.Flags().BoolVar(&f.full, "full-prompt", false,
		"print the served prompt in full; send no heldVersions even when --prompt-version is given")
}

// checkPromptFlags refuses what the server would refuse, before any request, and warns about a value that
// looks like the 12-character prompt pin rather than the served version.
func checkPromptFlags(f promptFlags) error {
	if len(f.versions) > maxPromptVersions {
		return errPromptVersionBound
	}
	for _, v := range f.versions {
		if v == "" || len(v) > maxPromptVersionLength {
			return errPromptVersionBound
		}
	}
	for _, v := range f.versions {
		if len(v) == promptPinLength {
			fmt.Fprintf(os.Stderr, "rearm: --prompt-version %s looks like the prompt pin (12 characters), not the served "+
				"version (64 hex, promptByVersion.version); the server will serve the text\n", v)
		}
	}
	return nil
}

// requirePromptFlags is checkPromptFlags for a verb: the refusal on stderr and exit 1, nothing sent.
func requirePromptFlags(f promptFlags) {
	if err := checkPromptFlags(f); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// heldVersionsToSend is what goes in heldVersions, and whether the key is sent at all: nothing with
// --full-prompt or without --prompt-version, else the values deduplicated in order.
func heldVersionsToSend(f promptFlags) ([]string, bool) {
	if f.full || len(f.versions) == 0 {
		return nil, false
	}
	seen := map[string]bool{}
	var out []string
	for _, v := range f.versions {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out, true
}

// shapePromptByVersion takes the nested {version, status, text} out of obj[field]: on SERVED the text goes
// to obj[textKey], where the verb always printed it, and leaves the nested object, so it is never printed
// twice; {version, status} stay. Empty when the field is absent or null (the legacy selection).
func shapePromptByVersion(obj map[string]interface{}, field, textKey string) (version, status string, text *string) {
	nested, ok := obj[field].(map[string]interface{})
	if !ok || nested == nil {
		return "", "", nil
	}
	version, status = str(nested["version"]), str(nested["status"])
	if s, ok := nested["text"].(string); ok && status == "SERVED" {
		obj[textKey] = s
		text = &s
	}
	delete(nested, "text")
	return version, status, text
}

// unchangedHint is the one stderr line of an UNCHANGED answer: the remedy for a context that does not hold
// the text it named (a compaction kept the version and lost the text).
func unchangedHint(version string) {
	fmt.Fprintf(os.Stderr, "rearm: prompt unchanged (served version %s): keep the text you hold; a context that does "+
		"not hold it re-runs without --prompt-version or with --full-prompt\n", version)
}

// unchangedNotAsked is the fail-safe: an UNCHANGED for a version this command did not send is printed as
// received (never a fabricated text) with one stderr line. It cannot happen with the server's contract.
func unchangedNotAsked(version, status string, held []string, sent bool) bool {
	if status != "UNCHANGED" {
		return false
	}
	if sent {
		for _, v := range held {
			if v == version {
				return false
			}
		}
	}
	fmt.Fprintln(os.Stderr, "rearm: the server reports the prompt unchanged against a version this command did not send; --full-prompt prints it")
	return true
}

// reportPromptStatus prints, for the JSON verbs, the fail-safe line or the unchanged hint.
func reportPromptStatus(version, status string, held []string, sent bool) {
	if unchangedNotAsked(version, status, held, sent) {
		return
	}
	if status == "UNCHANGED" {
		unchangedHint(version)
	}
}

// sendByVersion sends the by-version operation, or the legacy one on a server that predates the field:
// once a FieldUndefined answer was seen, at once, without heldVersions. Any other error is returned as is,
// so the bound refusal is never retried.
func sendByVersion(op, legacyOp string, variables map[string]interface{}) (map[string]interface{}, bool, error) {
	if !promptByVersionUnsupported {
		data, err := sendGraphQLRequest(op, variables)
		if err == nil || !isFieldUndefinedError(err) {
			return data, false, err
		}
		promptByVersionUnsupported = true
		if debug == "true" {
			fmt.Fprintln(os.Stderr, "server predates promptByVersion, using the legacy selection")
		}
	}
	delete(variables, "heldVersions")
	data, err := sendGraphQLRequest(legacyOp, variables)
	return data, true, err
}

// nextOffer is task next's and wait's poll: the offer by version, shaped, with the stderr line of an
// UNCHANGED answer.
func nextOffer(variables map[string]interface{}, f promptFlags) (interface{}, error) {
	held, send := heldVersionsToSend(f)
	if send {
		variables["heldVersions"] = held
	}
	data, legacy, err := sendByVersion(rearm.AgentTaskNextByPromptVersion_Operation, rearm.AgentTaskNextProgrammatic_Operation, variables)
	if err != nil {
		return nil, err
	}
	offered := data["agentTaskNextProgrammatic"]
	if m, ok := offered.(map[string]interface{}); ok && !legacy {
		version, status, _ := shapePromptByVersion(m, "promptByVersion", "rolePrompt")
		reportPromptStatus(version, status, held, send)
	}
	return offered, nil
}
