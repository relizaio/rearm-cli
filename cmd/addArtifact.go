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

	"github.com/go-resty/resty/v2"
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

		// Handle simple mode: --artifacts defaults to releaseArtifacts
		if addArtifactArtifacts != "" && addArtifactReleaseArts == "" && addArtifactDeliverableArts == "" && addArtifactSceArts == "" {
			addArtifactReleaseArts = addArtifactArtifacts
		}

		// Process release artifacts
		if addArtifactReleaseArts != "" {
			var releaseArtsList []Artifact
			if err := json.Unmarshal([]byte(addArtifactReleaseArts), &releaseArtsList); err != nil {
				fmt.Printf("Error parsing releasearts JSON: %v\n", err)
				os.Exit(1)
			}

			indexPrefix := "variables.artifactInput.releaseArtifacts."
			processedArts := processArtifactsInput(&releaseArtsList, indexPrefix, &filesCounter, &locationMap, &filesMap)
			artifactInput["releaseArtifacts"] = processedArts
		}

		// Process deliverable artifacts
		if addArtifactDeliverableArts != "" {
			var deliverableArtsList []DeliverableArtifactGroup
			if err := json.Unmarshal([]byte(addArtifactDeliverableArts), &deliverableArtsList); err != nil {
				fmt.Printf("Error parsing deliverablearts JSON: %v\n", err)
				os.Exit(1)
			}

			// Process each deliverable artifact group
			for i := range deliverableArtsList {
				indexPrefix := fmt.Sprintf("variables.artifactInput.deliverableArtifacts.%d.artifacts.", i)
				processedArts := processArtifactsInput(&deliverableArtsList[i].Artifacts, indexPrefix, &filesCounter, &locationMap, &filesMap)
				deliverableArtsList[i].Artifacts = *processedArts
			}

			artifactInput["deliverableArtifacts"] = deliverableArtsList
		}

		// Process SCE artifacts
		if addArtifactSceArts != "" {
			var sceArtsList []SceArtifactGroup
			if err := json.Unmarshal([]byte(addArtifactSceArts), &sceArtsList); err != nil {
				fmt.Printf("Error parsing scearts JSON: %v\n", err)
				os.Exit(1)
			}

			// Process each SCE artifact group
			for i := range sceArtsList {
				indexPrefix := fmt.Sprintf("variables.artifactInput.sceArtifacts.%d.artifacts.", i)
				processedArts := processArtifactsInput(&sceArtsList[i].Artifacts, indexPrefix, &filesCounter, &locationMap, &filesMap)
				sceArtsList[i].Artifacts = *processedArts
			}

			artifactInput["sceArtifacts"] = sceArtsList
		}

		variables["artifactInput"] = artifactInput

		// Build GraphQL mutation
		mutation := `
			mutation AddArtifactProgrammatic($artifactInput: AddArtifactInput) {
				addArtifactProgrammatic(artifactInput: $artifactInput) {
					uuid
					version
					lifecycle
					artifacts
				}
			}
		`

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

		// Build GraphQL operation for multipart upload
		od := make(map[string]interface{})
		od["operationName"] = "AddArtifactProgrammatic"
		od["variables"] = variables
		od["query"] = mutation

		jsonOd, _ := json.Marshal(od)
		operations := map[string]string{"operations": string(jsonOd)}

		// Build file map
		fileMapJson, _ := json.Marshal(locationMap)
		fileMapFd := map[string]string{"map": string(fileMapJson)}

		// Send request using resty
		client := resty.New()
		session, _ := getSession()
		if session != nil {
			client.SetHeader("X-CSRF-Token", session.Token)
			client.SetHeader("Cookie", "JSESSIONID="+session.JSessionId)
		}
		if len(apiKeyId) > 0 && len(apiKey) > 0 {
			auth := base64.StdEncoding.EncodeToString([]byte(apiKeyId + ":" + apiKey))
			client.SetHeader("Authorization", "Basic "+auth)
		}

		c := client.R()
		// Add files to multipart request
		for key, value := range filesMap {
			if fileData, ok := value.(FileData); ok {
				c.SetFileReader(key, fileData.Filename, bytes.NewReader(fileData.Bytes))
			}
		}

		resp, err := c.
			SetHeader("Content-Type", "multipart/form-data").
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
}
