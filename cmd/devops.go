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
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

var sealedCert string

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

	setInstSecretCertCmd.PersistentFlags().StringVar(&sealedCert, "cert", "", "Sealed certificate used by the instance (required)")

	devopsCmd.AddCommand(exportInstCmd)
	devopsCmd.AddCommand(setInstSecretCertCmd)
	rootCmd.AddCommand(devopsCmd)
}

var setInstSecretCertCmd = &cobra.Command{
	Use:   "setsecretcert",
	Short: "Use to to set sealed cert property on the instance",
	Long: `Bitnami Sealed Certificate property is used to encrypt secrets for instance.
	This command sets this certificate for the particular instance.
	Only supports instance own API Key.`,
	Run: func(cmd *cobra.Command, args []string) {
		query := `
			mutation ($sealedCert: String!) {
				setInstanceSealedSecretCert(sealedCert: $sealedCert)
			}
		`
		variables := map[string]interface{}{"sealedCert": sealedCert}

		data, err := sendGraphQLRequest(query, variables, rearmUri+"/graphql")
		if err != nil {
			printGqlError(err)
			os.Exit(1)
		}

		type SetCertRHResp struct {
			Responsewrapper bool `json:"setInstanceSealedSecretCert"`
		}
		var respData SetCertRHResp
		if val, ok := data["setInstanceSealedSecretCert"].(bool); ok {
			respData.Responsewrapper = val
		}

		jsonResp, _ := json.Marshal(respData.Responsewrapper)
		fmt.Println(string(jsonResp))
	},
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

	query := `
		query ($instanceUuid: ID, $instanceUri: String, $revision: Int!, $namespace: String) {
			getInstanceRevisionCycloneDxExportProg(instanceUuid: $instanceUuid, instanceUri: $instanceUri, revision: $revision, namespace: $namespace)
		}
	`
	intRevision, _ := strconv.Atoi(revision)
	variables := map[string]interface{}{
		"instanceUuid": instance,
		"instanceUri":  instanceURI,
		"revision":     intRevision,
		"namespace":    namespace,
	}

	data, err := sendGraphQLRequest(query, variables, rearmUri+"/graphql")
	if err != nil {
		printGqlError(err)
		os.Exit(1)
	}

	if result, ok := data["getInstanceRevisionCycloneDxExportProg"].(string); ok {
		return []byte(result)
	}
	return []byte("")
}
