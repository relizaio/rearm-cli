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
	"encoding/json"
	"strings"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// listFeatureSetsCmd wraps the FREEFORM-only listInstanceProductFeatureSets
// query: for every product mapped on the instance plan, report the product
// (uuid+name), the feature set currently deployed on it, and every active
// feature set the caller could switch to. Requires DEVOPS_READ on the
// instance (or its parent cluster, via the server-side cluster-aware
// fallback).
var listFeatureSetsCmd = &cobra.Command{
	Use:   "listfeaturesets",
	Short: "List feature sets per product on an instance plan",
	Long: `Fetch the per-product feature-set inventory of an instance's plan.
Returns the product name + uuid, the feature set currently deployed on it,
and the full list of feature sets the FREEFORM API key could switch the
deployment to. Requires a FREEFORM API key with DEVOPS_READ permission on
the instance (or its parent cluster).

Identify the instance by UUID via --instance OR by URI via --instanceuri
(URI is resolved against the FREEFORM key's org).

The --namespace flag is required for STANDALONE_INSTANCE and CLUSTER
instances (deployments are scoped per-namespace). For CLUSTER_INSTANCE
the server pins the namespace to the instance's own namespace and any
value passed is ignored.`,
	Run: func(cmd *cobra.Command, args []string) {
		if debug == "true" {
			fmt.Println("Using ReARM at", rearmUri)
		}
		if instance == "" && instanceURI == "" {
			fmt.Fprintln(os.Stderr, "either --instance or --instanceuri must be supplied")
			os.Exit(1)
		}
		// Release-aware selection (backend 26.09+): per product the plan's
		// integrate type, the release it aims at, the release actually
		// deployed, and the deployable releases of every available feature
		// set -- what switchfeatureset --release accepts. Falls back to the
		// legacy names-only selection when the backend does not know the
		// fields yet, so the command keeps working against older servers.
		query := `
			query ($instanceUuid: ID, $instanceUri: String, $namespace: String) {
				listInstanceProductFeatureSets(instanceUuid: $instanceUuid, instanceUri: $instanceUri, namespace: $namespace) {
					namespace
					product { uuid name }
					currentFeatureSet { uuid name }
					integrateType
					targetRelease { uuid version lifecycle createdDate approvedForInstanceEnvironment }
					deployedRelease { uuid version lifecycle createdDate approvedForInstanceEnvironment }
					availableFeatureSets {
						uuid
						name
						releases { uuid version lifecycle createdDate approvedForInstanceEnvironment }
					}
				}
			}
		`
		legacyQuery := `
			query ($instanceUuid: ID, $instanceUri: String, $namespace: String) {
				listInstanceProductFeatureSets(instanceUuid: $instanceUuid, instanceUri: $instanceUri, namespace: $namespace) {
					namespace
					product { uuid name }
					currentFeatureSet { uuid name }
					availableFeatureSets { uuid name }
				}
			}
		`
		variables := map[string]interface{}{}
		if instance != "" {
			variables["instanceUuid"] = instance
		}
		if instanceURI != "" {
			variables["instanceUri"] = instanceURI
		}
		if namespace != "" {
			variables["namespace"] = namespace
		}
		data, err := sendGraphQLRequest(query, variables, rearmUri+graphqlPath())
		if err != nil && isFieldUndefinedError(err) {
			if debug == "true" {
				fmt.Println("Backend predates release fields on listInstanceProductFeatureSets, using legacy selection")
			}
			data, err = sendGraphQLRequest(legacyQuery, variables, rearmUri+graphqlPath())
		}
		if err != nil {
			printGqlError(err)
			os.Exit(1)
		}
		jsonResponse, _ := json.Marshal(data["listInstanceProductFeatureSets"])
		fmt.Println(string(jsonResponse))
	},
}

// isFieldUndefinedError reports whether a GraphQL error is the server's
// validation failure for a field the schema does not define -- the signal
// that the backend is older than the selection we asked for.
func isFieldUndefinedError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "fieldundefined") || strings.Contains(msg, "is undefined") ||
		strings.Contains(msg, "unknown field") || strings.Contains(msg, "validation error")
}

var switchFeatureSetCmd = &cobra.Command{
	Use:   "switchfeatureset",
	Short: "Switch a deployed product on an instance plan to a different feature set",
	Long: `Switch the feature set deployed for a particular product on an
instance plan. The new feature set must be a branch on the same product.
Requires a FREEFORM API key with DEVOPS_WRITE permission on the instance
(or its parent cluster).

By default the plan entry keeps its integrate type and follows the newest
release of the new feature set approved for the instance environment.
Pass --release <uuid or version> to pin a specific release (the entry
becomes TARGET), or --follow to return a pinned entry to FOLLOW. Valid
--release values are listed by listfeaturesets under
availableFeatureSets[].releases[].

Identify the instance by UUID via --instance OR by URI via --instanceuri
(URI is resolved against the FREEFORM key's org).

The --namespace flag is required for STANDALONE_INSTANCE and CLUSTER
instances — the (product, namespace) pair uniquely identifies one
deployment on the plan. For CLUSTER_INSTANCE the server pins the
namespace to the instance's own namespace and any value passed is
ignored.`,
	Run: func(cmd *cobra.Command, args []string) {
		if debug == "true" {
			fmt.Println("Using ReARM at", rearmUri)
		}
		if instance == "" && instanceURI == "" {
			fmt.Fprintln(os.Stderr, "either --instance or --instanceuri must be supplied")
			os.Exit(1)
		}
		if switchRelease != "" && switchFollow {
			fmt.Fprintln(os.Stderr, "--release and --follow are mutually exclusive")
			os.Exit(1)
		}
		query := `
			mutation ($instanceUuid: ID, $instanceUri: String, $productUuid: ID!, $featureSetUuid: ID!, $namespace: String, $release: String, $follow: Boolean) {
				switchInstanceProductFeatureSet(
					instanceUuid: $instanceUuid,
					instanceUri: $instanceUri,
					productUuid: $productUuid,
					featureSetUuid: $featureSetUuid,
					namespace: $namespace,
					release: $release,
					follow: $follow
				) { uuid name }
			}
		`
		variables := map[string]interface{}{
			"productUuid":    productId,
			"featureSetUuid": featureSetId,
		}
		if switchRelease != "" {
			variables["release"] = switchRelease
		}
		if switchFollow {
			variables["follow"] = true
		}
		if instance != "" {
			variables["instanceUuid"] = instance
		}
		if instanceURI != "" {
			variables["instanceUri"] = instanceURI
		}
		if namespace != "" {
			variables["namespace"] = namespace
		}
		fmt.Println(sendRequest(query, variables, "switchInstanceProductFeatureSet"))
	},
}

var productId string
var featureSetId string
var switchRelease string
var switchFollow bool

func init() {
	listFeatureSetsCmd.PersistentFlags().StringVar(&instance, "instance", "", "UUID of the instance whose plan to inspect (either this or --instanceuri must be supplied)")
	listFeatureSetsCmd.PersistentFlags().StringVar(&instanceURI, "instanceuri", "", "URI of the instance whose plan to inspect; resolved against the FREEFORM key's org (either this or --instance must be supplied)")
	listFeatureSetsCmd.PersistentFlags().StringVar(&namespace, "namespace", "", "Namespace whose deployments to inspect (required for STANDALONE_INSTANCE / CLUSTER; ignored for CLUSTER_INSTANCE)")

	switchFeatureSetCmd.PersistentFlags().StringVar(&instance, "instance", "", "UUID of the instance whose plan to mutate (either this or --instanceuri must be supplied)")
	switchFeatureSetCmd.PersistentFlags().StringVar(&instanceURI, "instanceuri", "", "URI of the instance whose plan to mutate; resolved against the FREEFORM key's org (either this or --instance must be supplied)")
	switchFeatureSetCmd.PersistentFlags().StringVar(&productId, "product", "", "UUID of the product (component) whose deployment to switch (required)")
	switchFeatureSetCmd.PersistentFlags().StringVar(&featureSetId, "featureset", "", "UUID of the feature set (branch) to switch the deployment to (required)")
	switchFeatureSetCmd.PersistentFlags().StringVar(&namespace, "namespace", "", "Namespace of the deployment to switch (required for STANDALONE_INSTANCE / CLUSTER; ignored for CLUSTER_INSTANCE)")
	switchFeatureSetCmd.PersistentFlags().StringVar(&switchRelease, "release", "", "Pin the deployment to this release of the new feature set: uuid or exact version, one of availableFeatureSets[].releases[] from listfeaturesets (optional; the plan entry becomes TARGET). Requires backend 26.09+")
	switchFeatureSetCmd.PersistentFlags().BoolVar(&switchFollow, "follow", false, "Return the deployment to FOLLOW: newest release of the feature set approved for the instance environment (optional, mutually exclusive with --release). Requires backend 26.09+")
	switchFeatureSetCmd.MarkPersistentFlagRequired("product")
	switchFeatureSetCmd.MarkPersistentFlagRequired("featureset")

	versionFeatureSetCmd.PersistentFlags().StringVar(&versionFsProduct, "product", "", "UUID of the PRODUCT component to version (required)")
	versionFeatureSetCmd.PersistentFlags().StringVar(&overridesJson, "overrides", "", "JSON array of dependency-branch overrides (required); each entry: {\"componentUuid\":\"...\",\"branch\":\"...\"} or {\"vcsUri\":\"...\",\"repoPath\":\"...\",\"branch\":\"...\"}")
	versionFeatureSetCmd.MarkPersistentFlagRequired("product")
	versionFeatureSetCmd.MarkPersistentFlagRequired("overrides")

	devopsCmd.AddCommand(listFeatureSetsCmd)
	devopsCmd.AddCommand(switchFeatureSetCmd)
	devopsCmd.AddCommand(versionFeatureSetCmd)
}

var versionFsProduct string
var overridesJson string

var versionFeatureSetCmd = &cobra.Command{
	Use:   "versionfeatureset",
	Short: "Create a new feature set on a PRODUCT with selected dependency-branch overrides",
	Long: `Spin up a new feature set on a PRODUCT, copying the BASE feature
set's dependency configuration and re-pointing the listed
dependencies to the supplied branches. The new feature set is named
after the first override branch, gets autoIntegrate=ENABLED, and an
auto-integrate run is triggered before the call returns.

Each override can identify its component either by UUID
(componentUuid) or by (vcsUri, repoPath). The branch field is the
NAME of the branch on the resolved component; the override branch
must already exist (no auto-creation).

Requires a FREEFORM API key with the VERSION_FEATURESET permission
function on the product.`,
	Run: func(cmd *cobra.Command, args []string) {
		if debug == "true" {
			fmt.Println("Using ReARM at", rearmUri)
		}
		var overrides []map[string]interface{}
		if err := json.Unmarshal([]byte(overridesJson), &overrides); err != nil {
			fmt.Fprintln(os.Stderr, "Failed to parse --overrides JSON:", err)
			os.Exit(1)
		}
		query := `
			mutation ($productUuid: ID!, $overrides: [VersionFeatureSetOverride!]!) {
				versionFeatureSet(productUuid: $productUuid, overrides: $overrides) {
					uuid name component autoIntegrate
				}
			}
		`
		variables := map[string]interface{}{
			"productUuid": versionFsProduct,
			"overrides":   overrides,
		}
		fmt.Println(sendRequest(query, variables, "versionFeatureSet"))
	},
}
