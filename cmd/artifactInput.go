/*
The MIT License (MIT)

Copyright (c) 2019 - 2026 Reliza Incorporated. https://reliza.io
*/

package cmd

import (
	"encoding/json"
	"fmt"
	"os"
)

type RawBomInput struct {
	RawBom  map[string]interface{} `json:"rawBom"`
	BomType string                 `json:"bomType"`
}

type TagInput struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type Artifact struct {
	DisplayIdentifier string     `json:"displayIdentifier"`
	Version           string     `json:"version"`
	DownloadLinks     []Link     `json:"downloadLinks"`
	InventoryTypes    []string   `json:"inventoryTypes"`
	BomFormat         string     `json:"bomFormat,omitempty"`
	Type              string     `json:"type"`
	StoredIn          string     `json:"storedIn"`
	Tags              []TagInput `json:"tags"`
	File              []byte     `json:"file"`
	FilePath          string     `json:"filePath,omitempty"`
	StripBom          string     `json:"stripBom,omitempty"`
	// VEX-only fields, applied when type is "VEX" — they control how an
	// inbound VEX document is imported. All optional; the backend applies
	// defaults (scope COMPONENT, mode AUTO_ACCEPT) when omitted.
	//   VexScope                - AnalysisScope: ORG/RESOURCE_GROUP/COMPONENT/BRANCH/RELEASE
	//   VexImportMode           - AUTO_ACCEPT/STAGE/REJECT
	//   UserIssuerClassOverride - SELF/VENDOR/THIRD_PARTY
	VexScope                string     `json:"vexScope,omitempty"`
	VexImportMode           string     `json:"vexImportMode,omitempty"`
	UserIssuerClassOverride string     `json:"userIssuerClassOverride,omitempty"`
	Artifacts               []Artifact `json:"artifacts,omitempty"`
}

type Link struct {
	Uri     string `json:"uri"`
	Content string `json:"content"`
}

// FileData holds file bytes and original filename for upload
type FileData struct {
	Bytes    []byte
	Filename string
}

// sanitizeFilename removes any characters that are not a-zA-Z0-9.-_
func sanitizeFilename(filename string) string {
	var result []byte
	for i := 0; i < len(filename); i++ {
		c := filename[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '-' || c == '_' {
			result = append(result, c)
		}
	}
	if len(result) == 0 {
		return "file"
	}
	return string(result)
}

func ReadBomJsonFromFile(filePath string) map[string]interface{} {
	// Make sure infile is a file and not a directory
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	} else if fileInfo.IsDir() {
		fmt.Println("Error: infile must be a path to a file, not a directory!")
		os.Exit(1)
	}
	fileContentByteSlice, _ := os.ReadFile(filePath)
	var bomJSON map[string]interface{}
	parseError := json.Unmarshal(fileContentByteSlice, &bomJSON)
	if parseError != nil {
		fmt.Println("Error unmarshalling json bom file")
		fmt.Println(parseError)
		os.Exit(1)
	}
	return bomJSON
}
