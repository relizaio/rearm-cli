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
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/machinebox/graphql"
	"github.com/spf13/cobra"
)

var deliverableDigest string

func init() {
	deliverableGetSecrets.PersistentFlags().StringVar(&instance, "instance", "", "UUID of instance for which to generate (either this, or instanceuri must be provided)")
	deliverableGetSecrets.PersistentFlags().StringVar(&instanceURI, "instanceuri", "", "URI of instance for which to generate (either this, or instanceuri must be provided)")
	deliverableGetSecrets.PersistentFlags().StringVar(&deliverableDigest, "deldigest", "", "Digest or hash of the deliverable to resolve secrets for")
	deliverableGetSecrets.PersistentFlags().StringVar(&namespace, "namespace", "", "Namespace to use for secrets (optional, defaults to default namespace)")
	deliverableGetSecrets.MarkPersistentFlagRequired("deldigest")

	devopsCmd.AddCommand(deliverableGetSecrets)

	isInstHasSecretCertCmd.PersistentFlags().StringVar(&instance, "instance", "", "UUID of instance for which to check (optional)")
	isInstHasSecretCertCmd.PersistentFlags().StringVar(&instanceURI, "instanceuri", "", "URI of instance for which to check (optional)")
	devopsCmd.AddCommand(isInstHasSecretCertCmd)
}

var deliverableGetSecrets = &cobra.Command{
	Use:   "delsecrets",
	Short: "Get secrets to download specific deliverable",
	Long: `Command to get secrets for specific deliverable. Deliverable must belong to the organization.
			Secret names are returned`,
	Run: func(cmd *cobra.Command, args []string) {
		var respData DeliverableAuthResp

		if len(instance) <= 0 && len(instanceURI) <= 0 && !strings.HasPrefix(apiKeyId, "INSTANCE__") && !strings.HasPrefix(apiKeyId, "CLUSTER__") {
			fmt.Println("instance or instanceURI not specified!")
			os.Exit(1)
		}

		if len(namespace) <= 1 {
			namespace = "default"
		}

		client := graphql.NewClient(rearmUri + "/graphql")
		req := graphql.NewRequest(`
			query ($instanceUuid: ID, $instanceUri: String, $deliverableDigest: String!, $namespace: String) {
				deliverableDownloadSecrets(instanceUuid: $instanceUuid, instanceUri: $instanceUri, deliverableDigest: $deliverableDigest, namespace: $namespace) {
					login
					password
					type
				}
			}
		`)
		req.Var("instanceUuid", instance)
		req.Var("instanceUri", instanceURI)
		req.Var("deliverableDigest", deliverableDigest)
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
		if err := client.Run(context.Background(), req, &respData); err != nil {
			printGqlError(err)
			os.Exit(1)
		}

		respJson, err := json.Marshal(respData)
		if err != nil {
			panic(err)
		}

		fmt.Print(string(respJson))
	},
}

var isInstHasSecretCertCmd = &cobra.Command{
	Use:   "iscertinit",
	Short: "Use to check whether instance has sealed cert property configured",
	Long: `Bitnami Sealed Certificate property is used to encrypt secrets for instance.
	This command checks whether this property is configured for the particular instance.`,
	Run: func(cmd *cobra.Command, args []string) {
		var respData IsHasCertRHResp
		client := graphql.NewClient(rearmUri + "/graphql")
		req := graphql.NewRequest(`
			query ($instanceUuid: ID, $instanceUri: String) {
				isInstanceHasSealedSecretCert(instanceUuid: $instanceUuid, instanceUri: $instanceUri)
			}
		`)
		req.Var("instanceUuid", instance)
		req.Var("instanceUri", instanceURI)
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

		if err := client.Run(context.Background(), req, &respData); err != nil {
			printGqlError(err)
			os.Exit(1)
		}

		jsonResp, _ := json.Marshal(respData.Responsewrapper)
		fmt.Println(string(jsonResp))
	},
}

type DeliverableAuthResp struct {
	Responsewrapper DeliverableAuthRespMaps `json:"deliverableDownloadSecrets"`
}

type DeliverableAuthRespMaps struct {
	Login    string `json:"login"`
	Password string `json:"password"`
	Type     string `json:"type"`
}
