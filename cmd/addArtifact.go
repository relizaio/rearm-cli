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
	"errors"
	"fmt"
	"os"

	rearm "github.com/relizaio/rearm-client-go"
	"github.com/spf13/cobra"
)

var (
	addArtifactReleaseArts     string
	addArtifactDeliverableArts string
	addArtifactSceArts         string
	addArtifactArtifacts       string // Simple mode flag
	addArtifactRelease         string
)

type DeliverableArtifactGroup struct {
	Deliverable string     `json:"deliverable"`
	Variant     string     `json:"variant,omitempty"`
	Artifacts   []Artifact `json:"artifacts"`
}

type SceArtifactGroup struct {
	Sce       string     `json:"sce"`
	Artifacts []Artifact `json:"artifacts"`
}

var addArtifactCmd = &cobra.Command{
	Use:   "addartifact",
	Short: "Add artifacts to an existing release, deliverable, or source code entry",
	Long: `Add artifacts to an existing release, deliverable, or source code entry.

Examples:
  # Add artifacts to release (simple mode)
  rearm-cli addartifact -i <api-key-id> -k <api-key-secret> -u http://localhost:8086 \
    --component "my-component" --version "1.0.0" \
    --artifacts '[{"filePath": "/path/to/sbom.json", "type": "BOM", "bomFormat": "CYCLONEDX", "storedIn": "REARM", "inventoryTypes": ["SOFTWARE"], "displayIdentifier": "my-sbom"}]'

  # Add a VEX document
  rearm-cli addartifact -i <api-key-id> -k <api-key-secret> -u http://localhost:8086 \
    --component "my-component" --version "1.0.0" \
    --artifacts '[{"filePath": "vex.cdx.json", "type": "VEX", "displayIdentifier": "vendor-vex", "importScope": "COMPONENT", "importMode": "STAGE"}]'

  # Add a SAFETY_CONTEXT document: a CycloneDX 2.x JSON snapshot of declared safety levels
  rearm-cli addartifact -i <api-key-id> -k <api-key-secret> -u http://localhost:8086 \
    --component "my-component" --version "1.0.0" \
    --artifacts '[{"filePath": "safety-context.cdx.json", "type": "SAFETY_CONTEXT", "displayIdentifier": "iec62304-classes", "importScope": "COMPONENT"}]'

Import fields (VEX and SAFETY_CONTEXT):
  importScope              ORG, RESOURCE_GROUP, COMPONENT, BRANCH or RELEASE (default COMPONENT)
  importMode               AUTO_ACCEPT, STAGE or REJECT (default AUTO_ACCEPT)
  userIssuerClassOverride  SELF, VENDOR or THIRD_PARTY
  vexScope and vexImportMode are deprecated aliases of importScope and importMode (the generic field wins).
  An artifact field this CLI does not know is refused.

Who can make an import take effect:
  Any key that may add artifacts to the release can upload the document.
  A statement or level takes effect automatically only when the uploading key itself holds Finding Analysis write at the import scope:
  on the release for RELEASE, on the component for COMPONENT, at the organization for BRANCH and ORG; RESOURCE_GROUP never takes effect automatically.
  Otherwise it is staged for review (reason NO_FINDING_ANALYSIS_WRITE in the summary); other reasons can stage it too, and the summary names them.
  Component keys and organization-wide read-write keys never hold Finding Analysis write: use a FREEFORM key granted it at the scope you import to.
  A personal (USER) key holds only what both the key and its owner hold now.
  A userIssuerClassOverride that raises trust (THIRD_PARTY, then VENDOR, then SELF) also needs that permission; without it the override is ignored and the summary says so.
  After rearm login, the key the session is bound to is judged.

Ordering:
  The release's SBOM must be indexed first.
  Upload the SBOM in its own addartifact (or addrelease) call, then the VEX or SAFETY_CONTEXT; an SBOM in the same call does not count.
  --wait-for-sbom 10m retries the upload until the SBOM is indexed (nothing is stored while the server refuses).

  rearm-cli addartifact --component "my-component" --version "1.0.0" \
    --artifacts '[{"filePath": "sbom.cdx.json", "type": "BOM", "bomFormat": "CYCLONEDX", "displayIdentifier": "my-sbom"}]'
  rearm-cli addartifact --component "my-component" --version "1.0.0" --wait-for-sbom 10m \
    --artifacts '[{"filePath": "vex.cdx.json", "type": "VEX", "displayIdentifier": "vendor-vex"}]'

Result and exit:
  The import summary of each VEX or SAFETY_CONTEXT artifact is printed on stderr; stdout keeps the JSON answer.
  The exit is 3 when an artifact imported nothing or its result is unknown (the artifacts are stored, so a re-run uploads them again).
  --allow-import-failure exits 0 and still prints the summary, once the upload completed.
  When the server refuses the question about which import result fields it reports, nothing is uploaded and the exit is 1, also with --allow-import-failure; a re-run is safe.
  --artifacts cannot be combined with the advanced flags.
  A VEX or SAFETY_CONTEXT artifact on a source code entry (` + "`--scearts`" + `) or nested under another artifact is refused unless --allow-import-failure is given, because its result cannot be checked (use --releasearts; the import runs against the release either way).
  addrelease, addreleases, addodeliverable and getversion never import such an artifact and refuse it unless --allow-import-failure is given.
  --allow-import-failure is read only from the command line: REARM_ALLOW_IMPORT_FAILURE and the config file do not set it.

  # Add artifacts to multiple targets (advanced mode)
  rearm-cli addartifact -i <api-key-id> -k <api-key-secret> -u http://localhost:8086 \
    --component "my-component" --version "1.0.0" \
    --releasearts '[{"filePath": "/path/to/release-sbom.json", ...}]' \
    --deliverablearts '[{"deliverable": "del-uuid", "artifacts": [{"filePath": "/path/to/del-sbom.json", ...}]}]' \
    --scearts '[{"sce": "sce-uuid", "artifacts": [{"filePath": "/path/to/sce-sbom.json", ...}]}]'
`,
	Run: func(cmd *cobra.Command, args []string) {
		if debug == "true" {
			fmt.Println("Using Reliza Hub at", rearmUri)
		}

		// Validate required flags
		if component == "" && addArtifactRelease == "" {
			fmt.Println("Error: either --component or --release must be specified")
			os.Exit(1)
		}

		if component != "" && version == "" && addArtifactRelease == "" {
			fmt.Println("Error: --version is required when using --component")
			os.Exit(1)
		}

		// Check if at least one artifact type is provided
		hasArtifacts := addArtifactArtifacts != "" || addArtifactReleaseArts != "" || addArtifactDeliverableArts != "" || addArtifactSceArts != ""
		if !hasArtifacts {
			fmt.Println("Error: at least one of --artifacts, --releasearts, --deliverablearts, or --scearts must be specified")
			os.Exit(1)
		}

		// --artifacts is the release part of simple mode; beside an advanced flag it is an input
		// error, never dropped
		if addArtifactArtifacts != "" && (addArtifactReleaseArts != "" || addArtifactDeliverableArts != "" || addArtifactSceArts != "") {
			fmt.Fprintln(os.Stderr, "Error: --artifacts cannot be combined with --releasearts, --deliverablearts or --scearts; put release artifacts in --releasearts")
			os.Exit(1)
		}
		releaseFlag, releaseJSON := "--releasearts", addArtifactReleaseArts
		if addArtifactArtifacts != "" {
			releaseFlag, releaseJSON = "--artifacts", addArtifactArtifacts
		}

		// every input check runs before any request (design SAFE-9 3.4 item 9)
		in, err := decodeAddArtifactInput(releaseFlag, releaseJSON, addArtifactDeliverableArts, addArtifactSceArts, allowImportFailure)
		if err != nil {
			var legacy *legacyInputError
			if errors.As(err, &legacy) {
				fmt.Println(legacy.line)
			} else {
				fmt.Fprintln(os.Stderr, "Error:", err)
			}
			os.Exit(1)
		}
		if waitForSbom < 0 {
			fmt.Fprintf(os.Stderr, "Error: --%s must not be negative, got %s\n", waitForSbomFlag, waitForSbom)
			os.Exit(1)
		}
		for _, n := range in.notices {
			fmt.Fprintln(os.Stderr, n)
		}
		if err := in.tagCheckedEntries(); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

		// Build GraphQL mutation variables
		variables := make(map[string]interface{})
		artifactInput := make(map[string]interface{})

		// Add target identification
		if addArtifactRelease != "" {
			artifactInput["release"] = addArtifactRelease
		}
		if component != "" {
			artifactInput["component"] = component
		}
		if version != "" {
			artifactInput["version"] = version
		}

		// Initialize file tracking
		filesCounter := 0
		locationMap := make(map[string][]string)
		filesMap := make(map[string]interface{})

		// Process release artifacts
		if in.hasRelease {
			indexPrefix := "variables.artifactInput.releaseArtifacts."
			processedArts := processArtifactsInput(&in.releaseArts, indexPrefix, &filesCounter, &locationMap, &filesMap)
			artifactInput["releaseArtifacts"] = processedArts
		}

		// Process deliverable artifacts
		if in.hasDeliverable {
			deliverableArtsList := in.deliverableGroups
			for i := range deliverableArtsList {
				indexPrefix := fmt.Sprintf("variables.artifactInput.deliverableArtifacts.%d.artifacts.", i)
				processedArts := processArtifactsInput(&deliverableArtsList[i].Artifacts, indexPrefix, &filesCounter, &locationMap, &filesMap)
				deliverableArtsList[i].Artifacts = *processedArts
			}
			artifactInput["deliverableArtifacts"] = deliverableArtsList
		}

		// Process SCE artifacts
		if in.hasSce {
			sceArtsList := in.sceGroups
			for i := range sceArtsList {
				indexPrefix := fmt.Sprintf("variables.artifactInput.sceArtifacts.%d.artifacts.", i)
				processedArts := processArtifactsInput(&sceArtsList[i].Artifacts, indexPrefix, &filesCounter, &locationMap, &filesMap)
				sceArtsList[i].Artifacts = *processedArts
			}
			artifactInput["sceArtifacts"] = sceArtsList
		}

		variables["artifactInput"] = artifactInput

		if len(in.importable) > 0 {
			// a VEX or SAFETY_CONTEXT artifact: read its import result and gate the exit
			if code := runAddArtifactImport(in, variables, locationMap, filesMap); code != 0 {
				os.Exit(code)
			}
			return
		}

		// Build GraphQL mutation
		mutation := rearm.AddArtifactProgrammatic_Operation

		// Execute GraphQL mutation
		body := map[string]interface{}{
			"query":     mutation,
			"variables": variables,
		}

		jsonBody, err := json.Marshal(body)
		if err != nil {
			fmt.Printf("Error marshaling request: %v\n", err)
			os.Exit(1)
		}

		if debug == "true" {
			fmt.Println("GraphQL Request:")
			fmt.Println(string(jsonBody))
		}

		printGraphQLMultipart(mutation, variables, locationMap, filesMap)
	},
}

// processArtifactFiles processes artifact list and handles file uploads
func processArtifactFiles(artifacts *[]map[string]interface{}, filesCounter *int, locationMap *map[string][]string, filesMap *map[string]interface{}) []map[string]interface{} {
	processedArtifacts := make([]map[string]interface{}, 0)

	for _, art := range *artifacts {
		if filePath, ok := art["filePath"].(string); ok && filePath != "" {
			// Read file content
			fileContent, err := os.ReadFile(filePath)
			if err != nil {
				fmt.Printf("Error reading file %s: %v\n", filePath, err)
				os.Exit(1)
			}

			// Create file upload entry
			fileKey := fmt.Sprintf("%d", *filesCounter)
			(*filesMap)[fileKey] = fileContent
			(*locationMap)[fileKey] = []string{"variables", "artifactInput", "artifacts", fmt.Sprintf("%d", len(processedArtifacts)), "file"}
			*filesCounter++

			// Remove filePath and add file reference
			delete(art, "filePath")
			art["file"] = nil // Will be replaced by multipart upload
		}

		processedArtifacts = append(processedArtifacts, art)
	}

	return processedArtifacts
}

func init() {
	rootCmd.AddCommand(addArtifactCmd)

	// Required flags
	addArtifactCmd.Flags().StringVarP(&component, "component", "c", "", "Component UUID or name")
	addArtifactCmd.Flags().StringVarP(&version, "version", "v", "", "Release version")
	addArtifactCmd.Flags().StringVarP(&addArtifactRelease, "release", "r", "", "Release UUID")

	// Artifact flags
	addArtifactCmd.Flags().StringVar(&addArtifactArtifacts, "artifacts", "", "Artifacts JSON (simple mode, defaults to release artifacts)")
	addArtifactCmd.Flags().StringVar(&addArtifactReleaseArts, "releasearts", "", "Release artifacts JSON array")
	addArtifactCmd.Flags().StringVar(&addArtifactDeliverableArts, "deliverablearts", "", "Deliverable artifacts JSON array")
	addArtifactCmd.Flags().StringVar(&addArtifactSceArts, "scearts", "", "SCE artifacts JSON array")
	addArtifactCmd.Flags().BoolVar(&allowImportFailure, allowImportFailureFlag, false, allowImportFailureAddArtifactHelp)
	addArtifactCmd.Flags().DurationVar(&waitForSbom, waitForSbomFlag, 0, waitForSbomHelp)
}
