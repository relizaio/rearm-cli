/*
The MIT License (MIT)

Copyright (c) 2020 - 2022 Reliza Incorporated (Reliza (tm), https://reliza.io)

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

	"github.com/go-resty/resty/v2"
	"github.com/machinebox/graphql"
	"github.com/spf13/cobra"
)

var (
	rebomUri  string
	artDigest string
)

type BomInput struct {
	Meta string                 `json:"meta"`
	Bom  map[string]interface{} `json:"bom"`
	Tags map[string]interface{} `json:"tags"`
}

type RawBomInput struct {
	RawBom  map[string]interface{} `json:"rawBom"`
	BomType string                 `json:"bomType"`
}
type TagInput struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type Artifact struct {
	DisplayIdentifier string        `json:"displayIdentifier"`
	Version           string        `json:"version"`
	DownloadLinks     []Link        `json:"downloadLinks"`
	InventoryTypes    []string      `json:"inventoryTypes"`
	BomFormat         string        `json:"bomFormat"`
	Type              string        `json:"type"`
	Identities        []BomIdentity `json:"identities"`
	StoredIn          string        `json:"storedIn"`
	Digests           []string      `json:"digests"`
	Tags              []TagInput    `json:"tags"`
	File              []byte        `json:"file"`
	FilePath          string        `json:"filePath,omitempty"`
	StripBom          string        `json:"stripBom,omitempty"`
}

type BomIdentity struct {
	Identity    string `json:"identity"`
	IdenityType string `json:"idenityType"`
}
type Link struct {
	Uri     string `json:"uri"`
	Content string `json:"content"`
}

func init() {
	attachBomCmd.PersistentFlags().StringVar(&infile, "infile", "", "Input file with bom json")
	attachBomCmd.PersistentFlags().StringVar(&artDigest, "artdigest", "", "SHA 256 digest of the artifact")
	attachBomCmd.PersistentFlags().StringVar(&releaseId, "releaseid", "", "UUID of release")
	attachBomCmd.MarkPersistentFlagRequired("infile")
	attachBomCmd.MarkPersistentFlagRequired("artdigest")
	attachBomCmd.MarkPersistentFlagRequired("releaseid")

	putBomCmd.PersistentFlags().StringVar(&infile, "infile", "", "Input file with bom json")
	putBomCmd.PersistentFlags().StringVar(&rebomUri, "rebomuri", "http://localhost:4000", "Rebom URI")
	putBomCmd.MarkPersistentFlagRequired("infile")

	rootCmd.AddCommand(rebomCmd)
	rebomCmd.AddCommand(putBomCmd)
	rebomCmd.AddCommand(attachBomCmd)
}

var rebomCmd = &cobra.Command{
	Use:   "rebom",
	Short: "Set of commands to interact with rebom tool",
	Long:  `Set of commands to interact with rebom tool`,
	Run: func(cmd *cobra.Command, args []string) {
		addBomToRebomFunc()
	},
}

var putBomCmd = &cobra.Command{
	Use:   "put",
	Short: "Send bom file to rebom",
	Long:  `Send bom file to rebom`,
	Run: func(cmd *cobra.Command, args []string) {
		addBomToRebomFunc()
	},
}

var attachBomCmd = &cobra.Command{
	Use:   "attach",
	Short: "Attach bom file to an artifact on RH",
	Long:  `Attach bom file to an artifact on RH`,
	Run: func(cmd *cobra.Command, args []string) {
		attachToRebomFunc()
	},
}

func attachToRebomFunc() {
	// open infile
	// Make sure infile is a file and not a directory
	fileInfo, err := os.Stat(infile)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	} else if fileInfo.IsDir() {
		fmt.Println("Error: infile must be a path to a file, not a directory!")
		os.Exit(1)
	}

	if debug == "true" {
		fmt.Println("Using ReARM at", rearmUri)
	}

	body := map[string]string{}
	if len(releaseId) > 0 {
		body["release"] = releaseId
	}
	if len(artDigest) > 0 {
		body["digest"] = artDigest
	}

	client := resty.New()
	session, _ := getSession()
	if session != nil {
		client.SetHeader("X-CSRF-Token", session.Token)
		client.SetHeader("Cookie", "JSESSIONID="+session.JSessionId)
	}
	resp, err := client.R().
		SetHeader("Content-Type", "application/json").
		SetHeader("User-Agent", "Reliza Go Client").
		SetHeader("Accept-Encoding", "gzip, deflate").
		SetHeader("Accept-Encoding", "gzip, deflate").
		SetFile("file", infile).
		SetFormData(body).
		SetBasicAuth(apiKeyId, apiKey).
		Post(rearmUri + "/api/programmatic/v1/sbom/upload")

	printResponse(err, resp)
}

func ReadBomJsonFromFile(filePath string) map[string]interface{} {
	// open infile
	// Make sure infile is a file and not a directory
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	} else if fileInfo.IsDir() {
		fmt.Println("Error: infile must be a path to a file, not a directory!")
		os.Exit(1)
	}
	// Read infile if not directory:
	fileContentByteSlice, _ := os.ReadFile(filePath)
	// fileContent := string(fileContentByteSlice)

	// Parse file content into json
	var bomJSON map[string]interface{}
	parseError := json.Unmarshal(fileContentByteSlice, &bomJSON)
	if parseError != nil {
		fmt.Println("Error unmarshalling json bom file")
		fmt.Println(parseError)
		os.Exit(1)
	}
	return bomJSON
}

func addBomToRebomFunc() {
	var bomInput BomInput
	bomInput.Meta = "sent from rearm cli"
	bomInput.Bom = ReadBomJsonFromFile(infile)

	// fmt.Println(bomInput)

	req := graphql.NewRequest(`
		mutation addBom ($bomInput: BomInput) {
			addBom(bomInput: $bomInput) {
				uuid
				meta
			}
		}
	`)
	req.Var("bomInput", bomInput)
	fmt.Println("adding bom...")
	fmt.Println(sendRequestWithUri(req, "addBom", rebomUri+"/graphql"))
}
