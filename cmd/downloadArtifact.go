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
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	rearm "github.com/relizaio/rearm-client-go"
	"github.com/spf13/cobra"
)

var (
	dlArtifactUuid  string
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
	if debug == "true" {
		fmt.Println("Using ReARM at", rearmUri)
		fmt.Println("Downloading artifact", dlArtifactUuid, "raw:", rawDownload, "version:", artifactVersion)
	}
	body, contentDisposition, err := rearm.DownloadArtifact(context.Background(), rearmClient(), dlArtifactUuid, rawDownload, artifactVersion)
	if err != nil {
		fmt.Println("Error downloading artifact:", describeError(err))
		os.Exit(1)
	}
	defer body.Close()
	content, err := io.ReadAll(body)
	if err != nil {
		fmt.Println("Error downloading artifact:", err)
		os.Exit(1)
	}
	filename := outfile
	if filename == "" {
		for _, part := range strings.Split(contentDisposition, ";") {
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
		fmt.Println("Content-Disposition:", contentDisposition)
		fmt.Println("Writing to filename:", filename)
	}
	if err := os.MkdirAll(outDirectory, 0755); err != nil {
		fmt.Println("Error creating output directory:", err)
		os.Exit(1)
	}
	outPath := filepath.Join(outDirectory, filename)
	if err := os.WriteFile(outPath, content, 0644); err != nil {
		fmt.Println("Error writing file:", err)
		os.Exit(1)
	}
	fmt.Println(outPath)
}
