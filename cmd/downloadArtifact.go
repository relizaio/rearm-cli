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
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-resty/resty/v2"
	"github.com/spf13/cobra"
)

var (
	dlArtifactUuid    string
	artifactVersion int
	rawDownload     bool
)

func init() {
	downloadArtifactCmd.PersistentFlags().StringVar(&dlArtifactUuid, "artifactuuid", "", "UUID of the artifact to download (required)")
	downloadArtifactCmd.MarkPersistentFlagRequired("artifactuuid")
	downloadArtifactCmd.PersistentFlags().StringVar(&outDirectory, "outdirectory", "", "Directory to write the downloaded file into (required)")
	downloadArtifactCmd.MarkPersistentFlagRequired("outdirectory")
	downloadArtifactCmd.PersistentFlags().StringVar(&outfile, "outfile", "", "Override filename for the downloaded file (optional, default taken from Content-Disposition header)")
	downloadArtifactCmd.PersistentFlags().BoolVar(&rawDownload, "raw", false, "Download raw artifact instead of processed BOM (optional, default false)")
	downloadArtifactCmd.PersistentFlags().IntVar(&artifactVersion, "version", 0, "Artifact version to download (optional)")
	rootCmd.AddCommand(downloadArtifactCmd)
}

var downloadArtifactCmd = &cobra.Command{
	Use:   "downloadartifact",
	Short: "Download an artifact from ReARM",
	Long: `Download an artifact from ReARM to a local directory.

By default downloads the processed BOM artifact. Use --raw to download the
raw (unprocessed) artifact instead.

The output filename is taken from the Content-Disposition header returned by
the server. Use --outfile to override it with a custom filename.

Examples:
  rearm-cli downloadartifact -i $APIKEY_ID -k $APIKEY_SECRET -u $REARM_URI \
    --artifactuuid <artifact-uuid> --outdirectory ./downloads

  # Download raw artifact with custom filename:
  rearm-cli downloadartifact -i $APIKEY_ID -k $APIKEY_SECRET -u $REARM_URI \
    --artifactuuid <artifact-uuid> --outdirectory ./downloads \
    --raw --outfile my-sbom.json

  # Download a specific version:
  rearm-cli downloadartifact -i $APIKEY_ID -k $APIKEY_SECRET -u $REARM_URI \
    --artifactuuid <artifact-uuid> --outdirectory ./downloads --version 2`,
	Run: func(cmd *cobra.Command, args []string) {
		downloadArtifactFunc()
	},
}

func downloadArtifactFunc() {
	endpoint := "/download"
	if rawDownload {
		endpoint = "/rawdownload"
	}
	url := rearmUri + "/api/programmatic/v1/artifact/" + dlArtifactUuid + endpoint

	if debug == "true" {
		fmt.Println("Using ReARM at", rearmUri)
		fmt.Println("Downloading artifact", dlArtifactUuid, "from", url)
	}

	client := resty.New()
	applySessionToRestyClient(client)
	req := client.R().
		SetHeader("User-Agent", "ReARM CLI").
		SetHeader("Accept-Encoding", "identity"). // disable compression so Body() is raw bytes
		SetBasicAuth(apiKeyId, apiKey)

	if artifactVersion > 0 {
		req = req.SetQueryParam("version", strconv.Itoa(artifactVersion))
	}

	resp, err := req.Get(url)
	if err != nil {
		fmt.Println("Error downloading artifact:", err)
		os.Exit(1)
	}

	if resp.StatusCode() != 200 {
		fmt.Println("Error: server returned status", resp.Status())
		if debug == "true" {
			fmt.Println("Response body:", resp.String())
		}
		os.Exit(1)
	}

	// Determine output filename
	filename := outfile
	if filename == "" {
		cd := resp.Header().Get("Content-Disposition")
		for _, part := range strings.Split(cd, ";") {
			part = strings.TrimSpace(part)
			if strings.HasPrefix(part, "filename=") {
				filename = strings.Trim(strings.TrimPrefix(part, "filename="), `"`)
				break
			}
		}
	}
	if filename == "" {
		filename = dlArtifactUuid + ".bin"
	}

	if debug == "true" {
		fmt.Println("Content-Disposition:", resp.Header().Get("Content-Disposition"))
		fmt.Println("Writing to filename:", filename)
	}

	// Ensure output directory exists
	if err := os.MkdirAll(outDirectory, 0755); err != nil {
		fmt.Println("Error creating output directory:", err)
		os.Exit(1)
	}

	outPath := filepath.Join(outDirectory, filename)
	if err := os.WriteFile(outPath, resp.Body(), 0644); err != nil {
		fmt.Println("Error writing file:", err)
		os.Exit(1)
	}

	fmt.Println(outPath)
}
