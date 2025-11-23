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
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/machinebox/graphql"
	"github.com/spf13/cobra"
)

var (
	approvalMatchOperator string
	approvalEntries       []string
	approvalStates        []string
)

type ConditionOnReleaseInput struct {
	ApprovalEntry string `json:"approvalEntry"`
	ApprovalState string `json:"approvalState"`
}

type ConditionGroupOnReleaseInput struct {
	MatchOperator string                    `json:"matchOperator"`
	Conditions    []ConditionOnReleaseInput `json:"conditions"`
}

var getLatestReleaseCmd = &cobra.Command{
	Use:   "getlatestrelease",
	Short: "Obtains latest release for Component or Product",
	Long: `This CLI command would connect to ReARM and would obtain latest release for specified Component and Branch
			or specified Product and Feature Set.`,
	Run: func(cmd *cobra.Command, args []string) {
		getLatestReleaseFunc(debug, rearmUri, component, product, branch, tagKey, tagVal, apiKeyId, apiKey, lifecycle)
	},
}

func getLatestReleaseFunc(debug string, rearmUri string, component string, product string, branch string,
	tagKey string, tagVal string, apiKeyId string, apiKey string, lifecycle string) []byte {
	if debug == "true" {
		fmt.Println("Using ReARM at", rearmUri)
	}

	body := map[string]interface{}{}

	if len(component) > 0 {
		body["component"] = component
	}

	if len(product) > 0 {
		body["product"] = product
	}

	if len(tagKey) > 0 && len(tagVal) > 0 {
		body["tags"] = tagKey + "____" + tagVal
	}

	if len(branch) > 0 {
		body["branch"] = branch
	}

	if len(lifecycle) > 0 {
		body["lifecycle"] = strings.ToUpper(lifecycle)
	}

	// Add VCS-based component identification parameters
	if len(vcsUri) > 0 {
		body["vcsUri"] = vcsUri
		if len(repoPath) > 0 {
			body["repoPath"] = repoPath
		}
	}

	if len(approvalEntries) > 0 || len(approvalStates) > 0 {
		if len(approvalEntries) != len(approvalStates) {
			fmt.Println("Error: number of approvalentry and approvalstate arguments must be the same!")
			os.Exit(1)
		}
		var conditionGroup ConditionGroupOnReleaseInput
		conditionGroup.MatchOperator = approvalMatchOperator
		var conditions []ConditionOnReleaseInput
		for i := range approvalEntries {
			var condition ConditionOnReleaseInput
			condition.ApprovalEntry = approvalEntries[i]
			condition.ApprovalState = approvalStates[i]
			conditions = append(conditions, condition)
		}
		conditionGroup.Conditions = conditions
		body["conditions"] = conditionGroup

	}

	client := graphql.NewClient(rearmUri + "/graphql")
	req := graphql.NewRequest(`
		query ($GetLatestReleaseInput: GetLatestReleaseInput!) {
			getLatestReleaseProgrammatic(release:$GetLatestReleaseInput) {` + FULL_RELEASE_GQL_DATA + `}
		}`,
	)
	req.Var("GetLatestReleaseInput", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "ReARM CLI")
	req.Header.Set("Accept-Encoding", "gzip, deflate")

	if len(apiKeyId) > 0 && len(apiKey) > 0 {
		auth := base64.StdEncoding.EncodeToString([]byte(apiKeyId + ":" + apiKey))
		req.Header.Add("Authorization", "Basic "+auth)
	}

	session, _ := getSession()
	if session != nil {
		req.Header.Set("X-CSRF-Token", session.Token)
		req.Header.Set("Cookie", "JSESSIONID="+session.JSessionId)
	}

	var respData map[string]interface{}
	if err := client.Run(context.Background(), req, &respData); err != nil {
		printGqlError(err)
		os.Exit(1)
	}

	jsonResponse, _ := json.Marshal(respData["getLatestReleaseProgrammatic"])
	if string(jsonResponse) != "null" {
		fmt.Println(string(jsonResponse))
	}
	return jsonResponse
}

func init() {
	getLatestReleaseCmd.PersistentFlags().StringVar(&component, "component", "", "Component or Product UUID from ReARM for which to obtain latest release")
	getLatestReleaseCmd.PersistentFlags().StringVar(&product, "product", "", "Product UUID from ReARM to condition component release to this product (optional)")
	getLatestReleaseCmd.PersistentFlags().StringVarP(&branch, "branch", "b", "", "Name of branch or Feature Set from ReARM for which latest release is requested, if not supplied UI mapping is used (optional)")
	getLatestReleaseCmd.PersistentFlags().StringVar(&vcsUri, "vcsuri", "", "URI of VCS repository (optional)")
	getLatestReleaseCmd.PersistentFlags().StringVar(&repoPath, "repo-path", "", "Repository path for monorepo components (optional)")
	getLatestReleaseCmd.PersistentFlags().StringVar(&environment, "env", "", "Environment to obtain approvals details from (optional)")
	getLatestReleaseCmd.PersistentFlags().StringVar(&instance, "instance", "", "Instance ID for which to check release (optional)")
	getLatestReleaseCmd.PersistentFlags().StringVar(&namespace, "namespace", "", "Namespace within instance for which to check release, only matters if instance is supplied (optional)")
	getLatestReleaseCmd.PersistentFlags().StringVar(&tagKey, "tagkey", "", "Tag key to use for picking artifact (optional)")
	getLatestReleaseCmd.PersistentFlags().StringVar(&tagVal, "tagval", "", "Tag value to use for picking artifact (optional)")
	getLatestReleaseCmd.PersistentFlags().StringVar(&lifecycle, "lifecycle", "DRAFT", "Lifecycle of the release, default is 'DRAFT' (optional)")
	getLatestReleaseCmd.PersistentFlags().StringVar(&approvalMatchOperator, "operator", "AND", "Match operator for a list of approvals, 'AND' or 'OR' default is 'AND' (optional)")
	getLatestReleaseCmd.PersistentFlags().StringSliceVar(&approvalEntries, "approvalentry", []string{}, "Approval entry names or ids (optional, multiple allowed)")
	getLatestReleaseCmd.PersistentFlags().StringSliceVar(&approvalStates, "approvalstate", []string{}, "Approval states corresponding to approval entries, can be 'APPROVED', 'DISAPPROVED' or 'UNSET' (optional, multiple allowed, required if approval entries are present)")
	rootCmd.AddCommand(getLatestReleaseCmd)
}
