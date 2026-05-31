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
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/go-resty/resty/v2"
	"github.com/spf13/cobra"
)

var batchInfile string

var addReleasesCmd = &cobra.Command{
	Use:   "addreleases",
	Short: "Creates multiple releases on ReARM in a single batch",
	Long: `Creates several releases on ReARM in one all-or-nothing call.

The releases are read from a JSON file (--infile) containing an array of
release objects, each shaped exactly like the input accepted by the single
'addrelease' command (ReleaseInputProg). Artifacts reference local files via
their "filePath" field; the CLI reads each file and uploads it alongside the
batch — you do not embed file bytes in the JSON.

Unlike calling 'addrelease' once per component, the backend fires product
auto-integration only once per affected feature set for the whole batch
(deduped), rather than once per release. Use this when a CI run builds several
component releases at once and you want a single product auto-integrate.

Example batch.json (one element shown):
[
  {
    "component": "5a813e39-c453-444e-85cd-b618b7de6108",
    "branch": "main",
    "version": "1.4.0",
    "lifecycle": "ASSEMBLED",
    "sourceCodeEntry": {
      "commit": "9f1c2ab",
      "commitMessage": "fix: handle null tokens",
      "uri": "github.com/acme/widget",
      "type": "git",
      "artifacts": [
        { "displayIdentifier": "source-sbom", "type": "BOM", "bomFormat": "CYCLONEDX", "filePath": "./sboms/widget-source.cdx.json" }
      ]
    },
    "outboundDeliverables": [
      {
        "displayIdentifier": "registry.acme.com/widget:1.4.0",
        "type": "CONTAINER",
        "softwareMetadata": { "packageType": "CONTAINER", "digests": ["sha256:abc123"] },
        "artifacts": [
          { "displayIdentifier": "deliverable-sbom", "type": "BOM", "bomFormat": "CYCLONEDX", "filePath": "./sboms/widget-image.cdx.json" }
        ]
      }
    ],
    "artifacts": [
      { "displayIdentifier": "trivy-scan", "type": "SARIF", "filePath": "./scans/widget.sarif" }
    ]
  }
]`,
	Run: func(cmd *cobra.Command, args []string) {
		if debug == "true" {
			fmt.Println("Using ReARM at", rearmUri)
		}

		releases := readBatchReleasesFromFile(batchInfile)
		if len(releases) == 0 {
			fmt.Fprintln(os.Stderr, "Error: --infile must contain a non-empty JSON array of releases")
			os.Exit(1)
		}

		locationMap := make(map[string][]string)
		filesMap := make(map[string]interface{})
		filesCounter := 0

		for i := range releases {
			prefix := "variables.releaseInputsProg." + strconv.Itoa(i) + "."
			normalizeBatchReleaseLifecycle(releases[i])
			processBatchReleaseArtifacts(releases[i], prefix, &filesCounter, &locationMap, &filesMap)
		}

		if debug == "true" {
			jsonReleases, _ := json.Marshal(releases)
			fmt.Println(string(jsonReleases))
		}

		od := make(map[string]interface{})
		od["operationName"] = "addReleasesProgrammatic"
		od["variables"] = map[string]interface{}{"releaseInputsProg": releases}
		od["query"] = `mutation addReleasesProgrammatic($releaseInputsProg: [ReleaseInputProg!]!) {addReleasesProgrammatic(releases:$releaseInputsProg) {` + RELEASE_GQL_DATA + `}}`

		jsonOd, _ := json.Marshal(od)
		operations := map[string]string{"operations": string(jsonOd)}

		fileMapJson, _ := json.Marshal(locationMap)
		fileMapFd := map[string]string{"map": string(fileMapJson)}

		client := resty.New()
		applySessionToRestyClient(client)
		if len(apiKeyId) > 0 && len(apiKey) > 0 {
			auth := base64.StdEncoding.EncodeToString([]byte(apiKeyId + ":" + apiKey))
			client.SetHeader("Authorization", "Basic "+auth)
		}
		c := client.R()
		for key, value := range filesMap {
			if fileData, ok := value.(FileData); ok {
				c.SetFileReader(key, fileData.Filename, bytes.NewReader(fileData.Bytes))
			} else {
				fmt.Printf("Warning: Value for key '%s' is not FileData\n", key)
			}
		}

		resp, err := c.SetHeader("Content-Type", "multipart/form-data").
			SetHeader("User-Agent", "ReARM CLI").
			SetHeader("Accept-Encoding", "gzip, deflate").
			SetHeader("Apollo-Require-Preflight", "true").
			SetMultipartFormData(operations).
			SetMultipartFormData(fileMapFd).
			SetBasicAuth(apiKeyId, apiKey).
			Post(rearmUri + "/graphql")

		handleResponse(err, resp)
	},
}

// readBatchReleasesFromFile reads the --infile JSON array of release objects.
func readBatchReleasesFromFile(filePath string) []map[string]interface{} {
	if filePath == "" {
		fmt.Fprintln(os.Stderr, "Error: --infile is required")
		os.Exit(1)
	}
	raw, err := os.ReadFile(filePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error reading batch file: ", err)
		os.Exit(1)
	}
	var releases []map[string]interface{}
	if err := json.Unmarshal(raw, &releases); err != nil {
		fmt.Fprintln(os.Stderr, "Error parsing batch file (expected a JSON array of release objects): ", err)
		os.Exit(1)
	}
	return releases
}

// normalizeBatchReleaseLifecycle upper-cases the lifecycle so callers can write
// it in any case; the server matches the enum case-sensitively.
func normalizeBatchReleaseLifecycle(release map[string]interface{}) {
	if lc, ok := release["lifecycle"].(string); ok && lc != "" {
		release["lifecycle"] = strings.ToUpper(lc)
	}
}

// processBatchReleaseArtifacts walks the artifact-bearing locations of a single
// release object and rewrites each artifact's local "filePath" into an uploaded
// multipart file part (recording the JSON path in locationMap). The prefix is
// the path to this release inside the GraphQL variables tree, e.g.
// "variables.releaseInputsProg.0.".
func processBatchReleaseArtifacts(release map[string]interface{}, prefix string, filesCounter *int,
	locationMap *map[string][]string, filesMap *map[string]interface{}) {
	// release-level artifacts
	processArtifactsAtKey(release, "artifacts", prefix+"artifacts.", filesCounter, locationMap, filesMap)

	// sourceCodeEntry.artifacts
	if sce, ok := release["sourceCodeEntry"].(map[string]interface{}); ok {
		processArtifactsAtKey(sce, "artifacts", prefix+"sourceCodeEntry.artifacts.", filesCounter, locationMap, filesMap)
	}

	// commits[k].artifacts
	if commits, ok := release["commits"].([]interface{}); ok {
		for k := range commits {
			if commit, ok := commits[k].(map[string]interface{}); ok {
				processArtifactsAtKey(commit, "artifacts", prefix+"commits."+strconv.Itoa(k)+".artifacts.", filesCounter, locationMap, filesMap)
			}
		}
	}

	// outboundDeliverables[k].artifacts
	if odels, ok := release["outboundDeliverables"].([]interface{}); ok {
		for k := range odels {
			if odel, ok := odels[k].(map[string]interface{}); ok {
				processArtifactsAtKey(odel, "artifacts", prefix+"outboundDeliverables."+strconv.Itoa(k)+".artifacts.", filesCounter, locationMap, filesMap)
			}
		}
	}
}

// processArtifactsAtKey converts container[key] (a JSON artifact array) into a
// typed []Artifact, runs it through the shared processArtifactsInput (which
// reads each filePath, uploads it, and clears the path), then writes the
// processed slice back. No-op when the key is absent or empty.
func processArtifactsAtKey(container map[string]interface{}, key, indexPrefix string, filesCounter *int,
	locationMap *map[string][]string, filesMap *map[string]interface{}) {
	raw, ok := container[key]
	if !ok || raw == nil {
		return
	}
	bts, err := json.Marshal(raw)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error reading artifacts: ", err)
		os.Exit(1)
	}
	var arts []Artifact
	if err := json.Unmarshal(bts, &arts); err != nil {
		fmt.Fprintln(os.Stderr, "Error parsing artifacts: ", err)
		os.Exit(1)
	}
	if len(arts) == 0 {
		return
	}
	processed := processArtifactsInput(&arts, indexPrefix, filesCounter, locationMap, filesMap)
	container[key] = *processed
}

func init() {
	addReleasesCmd.PersistentFlags().StringVar(&batchInfile, "infile", "", "Path to a JSON file with an array of release objects (ReleaseInputProg shape). Artifacts reference local files via their filePath field.")
	addReleasesCmd.MarkPersistentFlagRequired("infile")
	addReleasesCmd.PersistentFlags().StringVar(&stripBom, "stripbom", "true", "(Optional) Set --stripbom false to disable striping bom for digest matching. Applied to every artifact in the batch.")
	rootCmd.AddCommand(addReleasesCmd)
}
