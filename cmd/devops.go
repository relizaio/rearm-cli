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
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/machinebox/graphql"
	"github.com/spf13/cobra"
)

var devopsCmd = &cobra.Command{
	Use:   "devops",
	Short: "DevOps commands",
	Long:  `Set of commands for DevOps operations.`,
}

var exportInstCmd = &cobra.Command{
	Use:   "exportinst",
	Short: "Outputs the Cyclone DX spec of your instance",
	Long:  `Outputs the Cyclone DX spec of your instance`,
	Run: func(cmd *cobra.Command, args []string) {
		cycloneBytes := getInstanceRevisionCycloneDxExportV1(apiKeyId, apiKey, instance, revision, instanceURI, namespace)
		fmt.Println(string(cycloneBytes))
	},
}

func init() {
	exportInstCmd.PersistentFlags().StringVar(&instance, "instance", "", "UUID of instance for which export from (optional)")
	exportInstCmd.PersistentFlags().StringVar(&instanceURI, "instanceuri", "", "URI of instance for which to export from (optional)")
	exportInstCmd.PersistentFlags().StringVar(&revision, "revision", "", "Revision of instance for which to export from (optional, default is -1)")
	exportInstCmd.PersistentFlags().StringVar(&namespace, "namespace", "", "Use to define specific namespace for instance export (optional)")

	devopsCmd.AddCommand(exportInstCmd)
	rootCmd.AddCommand(devopsCmd)
}

func getInstanceRevisionCycloneDxExportV1(apiKeyId string, apiKey string, instance string, revision string, instanceURI string, namespace string) []byte {
	if len(instance) <= 0 && len(instanceURI) <= 0 && !strings.HasPrefix(apiKeyId, "INSTANCE__") && !strings.HasPrefix(apiKeyId, "CLUSTER__") {
		fmt.Println("instance or instanceURI not specified!")
		os.Exit(1)
	}

	if len(revision) < 1 {
		revision = "-1"
	}

	if len(namespace) <= 0 {
		namespace = ""
	}

	client := graphql.NewClient(rearmUri + "/graphql")
	req := graphql.NewRequest(`
		query ($instanceUuid: ID, $instanceUri: String, $revision: Int!, $namespace: String) {
			getInstanceRevisionCycloneDxExportProg(instanceUuid: $instanceUuid, instanceUri: $instanceUri, revision: $revision, namespace: $namespace)
		}
	`)
	req.Var("instanceUuid", instance)
	req.Var("instanceUri", instanceURI)
	intRevision, _ := strconv.Atoi(revision)
	req.Var("revision", intRevision)
	req.Var("namespace", namespace)
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

	var respData map[string]string
	if err := client.Run(context.Background(), req, &respData); err != nil {
		printGqlError(err)
		os.Exit(1)
	}
	return []byte(respData["getInstanceRevisionCycloneDxExportProg"])
}
