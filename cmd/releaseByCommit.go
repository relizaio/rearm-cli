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
	"fmt"
	"os"

	rearm "github.com/relizaio/rearm-client-go"
	"github.com/spf13/cobra"
)

// rearm releasebycommit (task TPF-72 / TPF-99): the OBOM of the newest release of a component or
// product built from exactly the given head commit, the by-commit twin of releasebyversion.

// releaseByCommitExit ends the process with the command's exit code; a test seam.
var releaseByCommitExit = os.Exit

func newReleaseByCommitCmd() *cobra.Command {
	var commitFlag, componentFlag string
	cmd := &cobra.Command{
		Use:   "releasebycommit",
		Args:  cobra.NoArgs,
		Short: "Outputs the OBOM (CycloneDX 1.6) of the newest release of a component or product built from a head commit",
		Long: `Looks up the releases whose head commit is exactly the given commit (whole sha, as recorded by
addrelease --commit; no prefix, tag or branch) on the component or product and prints the OBOM of the
newest by creation date: the release plus every dependency release unwound, with deliverables, as
CycloneDX 1.6 JSON. Prints {} when no release of the component has that head commit. Lifecycle and
approvals are not consulted. Needs a ReARM server that serves getReleaseByCommitProgrammatic.`,
		Run: func(cmd *cobra.Command, args []string) {
			if debug == "true" {
				fmt.Fprintln(cmd.ErrOrStderr(), "Using ReARM at", rearmUri)
			}
			query := rearm.GetReleaseByCommitProgrammatic_Operation
			// componentId takes the uuid or, with an org-scoped key, the unique name; ReARM resolves it.
			variables := map[string]interface{}{"commit": commitFlag, "componentId": componentFlag}
			data, err := sendGraphQLRequest(query, variables)
			if err != nil {
				fmt.Fprintln(cmd.OutOrStdout(), "Error:", describeError(err))
				releaseByCommitExit(1)
				return
			}
			if result, ok := data["getReleaseByCommitProgrammatic"].(string); ok {
				fmt.Fprintln(cmd.OutOrStdout(), result)
			}
		},
	}
	cmd.Flags().StringVar(&commitFlag, "commit", "", "Head commit of the release whose OBOM to output, whole sha as recorded (required)")
	cmd.Flags().StringVar(&componentFlag, "component", "", "Component or Product UUID, or its name when it is unique in the org (required)")
	_ = cmd.MarkFlagRequired("commit")
	_ = cmd.MarkFlagRequired("component")
	return cmd
}

func init() {
	rootCmd.AddCommand(newReleaseByCommitCmd())
}
