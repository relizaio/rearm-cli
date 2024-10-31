/*
The MIT License (MIT)

Copyright (c) 2020 - 2024 Reliza Incorporated (Reliza (tm), https://reliza.io)

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
	"encoding/json"
	"fmt"

	"github.com/machinebox/graphql"
	"github.com/spf13/cobra"
)

var (
	approvalEntry string
	approvalRole  string
	approvalState string
)

type Approval struct {
	ApprovalEntry  string `json:"approvalEntry"`
	ApprovalRoleId string `json:"approvalRoleId"`
	State          string `json:"state"`
}

var approveReleaseCmd = &cobra.Command{
	Use:   "approverelease",
	Short: "Programmatic approval of releases using valid API key",
	Long: `This CLI command would connect to ReARM and submit approval for a release using valid API key.
			The API key used must be valid and also must be authorized
			to perform requested approval.`,
	Run: func(cmd *cobra.Command, args []string) {
		if debug == "true" {
			fmt.Println("Using ReARM instance at", rearmUri)
		}

		body := map[string]interface{}{}
		approval := Approval{ApprovalEntry: approvalEntry, ApprovalRoleId: approvalRole, State: approvalState}
		approvals := make([]Approval, 1)
		approvals[0] = approval
		body["approvals"] = approvals
		if len(releaseId) > 0 {
			body["release"] = releaseId
		}
		if len(releaseVersion) > 0 {
			body["version"] = releaseVersion
		}
		if len(component) > 0 {
			body["component"] = component
		}

		if debug == "true" {
			jsonBody, _ := json.Marshal(body)
			fmt.Println("Request body = ", string(jsonBody))
		}

		req := graphql.NewRequest(`
			mutation approveReleaseProgrammatic($releaseApprovals: ReleaseApprovalProgrammaticInput!) {
				approveReleaseProgrammatic(releaseApprovals:$releaseApprovals) {` + RELEASE_GQL_DATA + `}
			}
		`)
		req.Var("releaseApprovals", body)
		fmt.Println(sendRequest(req, "approveReleaseProgrammatic"))
	},
}

func init() {
	approveReleaseCmd.PersistentFlags().StringVar(&releaseId, "releaseid", "", "UUID of release to be approved (either releaseid or releaseversion and component must be set)")
	approveReleaseCmd.PersistentFlags().StringVar(&releaseVersion, "releaseversion", "", "Version of release to be approved (either releaseid or releaseversion and component must be set)")
	approveReleaseCmd.PersistentFlags().StringVar(&component, "component", "", "UUID of component or product which release should be approved (either releaseid or releaseversion and component must be set)")
	approveReleaseCmd.PersistentFlags().StringVar(&approvalEntry, "approvalentry", "", "UUID of approval to approve")
	approveReleaseCmd.PersistentFlags().StringVar(&approvalRole, "approvalrole", "", "Approval role with which to approve")
	approveReleaseCmd.PersistentFlags().StringVar(&approvalState, "approvalstate", "", "Approval state, possible values: APPROVED, DISAPPROVED")
	approveReleaseCmd.MarkPersistentFlagRequired("approvalentry")
	approveReleaseCmd.MarkPersistentFlagRequired("approvalrole")
	approveReleaseCmd.MarkPersistentFlagRequired("approvalstate")
	rootCmd.AddCommand(approveReleaseCmd)
}
