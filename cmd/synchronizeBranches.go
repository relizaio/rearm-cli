/*
The MIT License (MIT)

Copyright (c) 2020 - 2025 Reliza Incorporated (Reliza (tm), https://reliza.io)

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
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/machinebox/graphql"
	"github.com/spf13/cobra"
)

var rawBranchesBase64 string

type SynchronizeBranchInput struct {
	Component    string   `json:"component"`
	LiveBranches []string `json:"liveBranches"`
}

var synchronizeBranchesCmd = &cobra.Command{
	Use:   "syncbranches",
	Short: "Synchronize list of live branches to ReARM",
	Long: `This CLI command sends a list of live branches to ReARM.
			Any branch not in the list will be archived on ReARM.`,
	Run: func(cmd *cobra.Command, args []string) {
		if debug == "true" {
			fmt.Println("Using ReARM instance at", rearmUri)
		}

		plainBranches, err := base64.StdEncoding.DecodeString(rawBranchesBase64)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		indBranches := strings.Split(string(plainBranches), "\n")

		var noEmptyBranches []string
		for i := range indBranches {
			if len(indBranches[i]) > 0 {
				noEmptyBranches = append(noEmptyBranches, indBranches[i])
			}
		}

		sbi := SynchronizeBranchInput{Component: component, LiveBranches: noEmptyBranches}

		if debug == "true" {
			jsonBody, _ := json.Marshal(sbi)
			fmt.Println("Request body = ", string(jsonBody))
		}

		req := graphql.NewRequest(`
			mutation synchronizeLiveBranches($synchronizeBranchInput: SynchronizeBranchInput!) {
				synchronizeLiveBranches(synchronizeBranchInput: $synchronizeBranchInput)
			}
		`)
		req.Var("synchronizeBranchInput", sbi)
		fmt.Println(sendRequest(req, "approveReleaseProgrammatic"))
	},
}

func init() {
	synchronizeBranchesCmd.PersistentFlags().StringVar(&component, "component", "", "UUID of component for which we are performing synchronization. Either this UUID must be provided or API key belonging to specific component.")
	synchronizeBranchesCmd.PersistentFlags().StringVar(&rawBranchesBase64, "livebranches", "", "Live branches of components in base64, use `git branch --format=\"%(refname)\" | base64 -w 0` or `git branch -r --format=\"%(refname)\" | base64 -w 0` to obtain")
	synchronizeBranchesCmd.MarkPersistentFlagRequired("livebranches")
	rootCmd.AddCommand(synchronizeBranchesCmd)
}
