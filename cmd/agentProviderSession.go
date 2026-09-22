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
	"strings"

	rearm "github.com/relizaio/rearm-client-go"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// The agent tool's own session id -- Claude Code's, and whatever the next tool's is -- reported
// to ReARM so a session can be traced back to the conversation that ran it.
//
// The CLI detects only what a tool documents: for Claude Code, $CLAUDE_CODE_SESSION_ID. A hosted
// surface's id (Claude Code's bridge session_...) is not exported, and reading it out of the
// tool's private state files would tie the CLI to a format nobody promised to keep. The agent
// knows where it runs, so it passes that one itself with --provider-remote-session-id.

const claudeCodeProvider = "claude-code"

var (
	providerName            string
	providerSessionId       string
	providerRemoteSessionId string
	noProviderSession       bool
	requireProviderSession  bool
)

var errNoProviderSession = errors.New("no provider session id found: pass --provider-session-id " +
	"(and --provider), or run under a tool that exports one ($CLAUDE_CODE_SESSION_ID for Claude Code)")

// addProviderSessionFlags registers the same five flags on every command that reports one, so
// init and update-meta cannot drift apart.
func addProviderSessionFlags(f *pflag.FlagSet) {
	f.StringVar(&providerName, "provider", "", "Agent tool the provider session belongs to (e.g. claude-code); "+
		"defaults to claude-code when running under Claude Code")
	f.StringVar(&providerSessionId, "provider-session-id", "", "The agent tool's own session id; "+
		"defaults to $CLAUDE_CODE_SESSION_ID")
	f.StringVar(&providerRemoteSessionId, "provider-remote-session-id", "", "The id a hosted surface of the "+
		"tool knows the session by (Claude Code's bridge session_...), when there is one")
	f.BoolVar(&noProviderSession, "no-provider-session", false, "Do not report the agent tool's session id")
	f.BoolVar(&requireProviderSession, "require-provider-session", false, "Fail when no provider session id "+
		"can be found, rather than proceeding without one")
}

// providerSessionOpts is the flag state, taken as a value so the resolution can be tested
// without package globals.
type providerSessionOpts struct {
	provider, id, remoteId string
	legacyClaudeId         string // --claude-session-id, which predates the generic flags
	optOut, require        bool
}

func currentProviderSessionOpts() providerSessionOpts {
	return providerSessionOpts{
		provider:       providerName,
		id:             providerSessionId,
		remoteId:       providerRemoteSessionId,
		legacyClaudeId: claudeSessionId,
		optOut:         noProviderSession,
		require:        requireProviderSession,
	}
}

// runningUnderClaudeCode is what lets the provider default: an id with no tool named is only
// attributable when the environment says which tool this is.
func runningUnderClaudeCode() bool {
	return os.Getenv("CLAUDECODE") == "1" || firstNonEmptyEnv("CLAUDE_CODE_SESSION_ID", "CLAUDE_SESSION_ID") != ""
}

// resolveProviderSession decides what to report. It returns nil with no error when there is
// nothing to report and nothing required it -- the ordinary case outside a supported tool. It
// errors when the flags contradict each other, when a remote id arrives without the local id it
// belongs to, when an id names no tool, or when --require-provider-session finds nothing.
func resolveProviderSession(o providerSessionOpts) (map[string]interface{}, error) {
	if o.optOut {
		if o.require {
			return nil, errors.New("--no-provider-session and --require-provider-session contradict each other")
		}
		return nil, nil
	}
	provider := strings.ToLower(strings.TrimSpace(o.provider))
	id := strings.TrimSpace(o.id)
	remote := strings.TrimSpace(o.remoteId)
	// Claude Code's fallbacks only speak for Claude Code. Under Claude Code, --provider cursor
	// with no id would otherwise send Claude's id labelled as Cursor's.
	claudeOrUnnamed := provider == "" || provider == claudeCodeProvider
	if id == "" && claudeOrUnnamed {
		id = strings.TrimSpace(o.legacyClaudeId)
	}
	if id == "" && claudeOrUnnamed {
		// The older CLAUDE_SESSION_ID spelling is kept for the same reason recordInitState keeps
		// it: an agent that already exports it should not silently lose the mapping.
		id = firstNonEmptyEnv("CLAUDE_CODE_SESSION_ID", "CLAUDE_SESSION_ID")
	}
	if id != "" && provider == "" && o.id == "" {
		// Found by a Claude Code fallback, so it is Claude Code's.
		provider = claudeCodeProvider
	}
	if id == "" {
		if provider != "" {
			// The mirror of an id with no tool: a tool named with nothing to attribute to it is
			// a mistake, not an absence, so it is not left to --require-provider-session.
			return nil, fmt.Errorf("--provider %s needs --provider-session-id: there is no id to "+
				"report for it", provider)
		}
		if remote != "" {
			return nil, errors.New("--provider-remote-session-id needs the local id it belongs to: " +
				"pass --provider-session-id as well")
		}
		if o.require {
			return nil, errNoProviderSession
		}
		return nil, nil
	}
	if provider == "" {
		if !runningUnderClaudeCode() {
			return nil, errors.New("--provider-session-id needs --provider to say which tool it belongs to")
		}
		provider = claudeCodeProvider
	}
	ps := map[string]interface{}{"provider": provider, "id": id}
	if remote != "" {
		ps["remoteId"] = remote
	}
	return ps, nil
}

var updateMetaTitle string

var agentSessionUpdateMetaCmd = &cobra.Command{
	Use:   "update-meta <session-uuid>",
	Short: "Update a session's title, or record the conversation now working it",
	Long: `Updates an open session's metadata via sessionUpdateMetaProgrammatic.

Reports the agent tool's session id the same way 'session init' does, so a
conversation that resumes work on an existing session records itself against
it. Provider sessions append: the session keeps the id of the conversation
that opened it as well.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		ps, err := resolveProviderSession(currentProviderSessionOpts())
		if err != nil {
			fmt.Fprintln(os.Stderr, "rearm:", err)
			os.Exit(1)
		}
		input := map[string]interface{}{"uuid": args[0]}
		if cmd.Flags().Changed("title") {
			input["title"] = updateMetaTitle
		}
		if ps != nil {
			input["providerSession"] = ps
		}
		if len(input) == 1 {
			fmt.Fprintln(os.Stderr, "rearm: nothing to update: pass --title, or run where a provider session id is available")
			os.Exit(1)
		}
		data, err := sendGraphQLRequest(rearm.SessionUpdateMetaProgrammatic_Operation,
			map[string]interface{}{"updateMeta": input})
		if err != nil {
			printGqlError(err)
			os.Exit(1)
		}
		emitJson(data["sessionUpdateMetaProgrammatic"])
	},
}

func init() {
	addProviderSessionFlags(agentSessionInitCmd.PersistentFlags())
	addProviderSessionFlags(agentSessionUpdateMetaCmd.PersistentFlags())
	agentSessionUpdateMetaCmd.PersistentFlags().StringVar(&updateMetaTitle, "title", "", "New session title")
	agentSessionCmd.AddCommand(agentSessionUpdateMetaCmd)
}
