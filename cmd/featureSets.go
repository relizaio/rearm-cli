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

The --namespace flag is required for STANDALONE_INSTANCE and CLUSTER
instances (deployments are scoped per-namespace). For CLUSTER_INSTANCE
the server pins the namespace to the instance's own namespace and any
value passed is ignored.`,
	Run: func(cmd *cobra.Command, args []string) {
		if debug == "true" {
			fmt.Println("Using ReARM at", rearmUri)
		}
		query := `
			query ($instanceUuid: ID!, $namespace: String) {
				listInstanceProductFeatureSets(instanceUuid: $instanceUuid, namespace: $namespace) {
					namespace
					product { uuid name }
					currentFeatureSet { uuid name }
					availableFeatureSets { uuid name }
				}
			}
		`
		variables := map[string]interface{}{"instanceUuid": instance}
		if namespace != "" {
			variables["namespace"] = namespace
		}
		fmt.Println(sendRequest(query, variables, "listInstanceProductFeatureSets"))
	},
}

var switchFeatureSetCmd = &cobra.Command{
	Use:   "switchfeatureset",
	Short: "Switch a deployed product on an instance plan to a different feature set",
	Long: `Switch the feature set deployed for a particular product on an
instance plan. The new feature set must be a branch on the same product.
Requires a FREEFORM API key with DEVOPS_WRITE permission on the instance
(or its parent cluster).

The --namespace flag is required for STANDALONE_INSTANCE and CLUSTER
instances — the (product, namespace) pair uniquely identifies one
deployment on the plan. For CLUSTER_INSTANCE the server pins the
namespace to the instance's own namespace and any value passed is
ignored.`,
	Run: func(cmd *cobra.Command, args []string) {
		if debug == "true" {
			fmt.Println("Using ReARM at", rearmUri)
		}
		query := `
			mutation ($instanceUuid: ID!, $productUuid: ID!, $featureSetUuid: ID!, $namespace: String) {
				switchInstanceProductFeatureSet(
					instanceUuid: $instanceUuid,
					productUuid: $productUuid,
					featureSetUuid: $featureSetUuid,
					namespace: $namespace
				) { uuid name }
			}
		`
		variables := map[string]interface{}{
			"instanceUuid":   instance,
			"productUuid":    productId,
			"featureSetUuid": featureSetId,
		}
		if namespace != "" {
			variables["namespace"] = namespace
		}
		fmt.Println(sendRequest(query, variables, "switchInstanceProductFeatureSet"))
	},
}

var productId string
var featureSetId string

func init() {
	listFeatureSetsCmd.PersistentFlags().StringVar(&instance, "instance", "", "UUID of the instance whose plan to inspect (required)")
	listFeatureSetsCmd.PersistentFlags().StringVar(&namespace, "namespace", "", "Namespace whose deployments to inspect (required for STANDALONE_INSTANCE / CLUSTER; ignored for CLUSTER_INSTANCE)")
	listFeatureSetsCmd.MarkPersistentFlagRequired("instance")

	switchFeatureSetCmd.PersistentFlags().StringVar(&instance, "instance", "", "UUID of the instance whose plan to mutate (required)")
	switchFeatureSetCmd.PersistentFlags().StringVar(&productId, "product", "", "UUID of the product (component) whose deployment to switch (required)")
	switchFeatureSetCmd.PersistentFlags().StringVar(&featureSetId, "featureset", "", "UUID of the feature set (branch) to switch the deployment to (required)")
	switchFeatureSetCmd.PersistentFlags().StringVar(&namespace, "namespace", "", "Namespace of the deployment to switch (required for STANDALONE_INSTANCE / CLUSTER; ignored for CLUSTER_INSTANCE)")
	switchFeatureSetCmd.MarkPersistentFlagRequired("instance")
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
