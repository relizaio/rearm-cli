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
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"

	"github.com/machinebox/graphql"
)

// CustomGraphQLClient wraps the machinebox/graphql client and adds file upload support
type CustomGraphQLClient struct {
	client *graphql.Client
}

type FileUpload struct {
	Name     string
	Filename string
	Filepath string
}

// NewCustomGraphQLClient creates a new instance of our custom GraphQL client
func NewCustomGraphQLClient(url string) (*CustomGraphQLClient, error) {
	gqlClient := graphql.NewClient(url)
	return &CustomGraphQLClient{client: gqlClient}, nil
}

type Operation struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables"`
}

// // Run executes a regular GraphQL query or mutation
// func (c *CustomGraphQLClient) Run(req interface{}) (*map[string]interface{}, error) {
// 	return c.client.Run(req)
// }

func (c *CustomGraphQLClient) UploadFile(mutationName, uri string, variables map[string]interface{}, files []FileUpload) (*map[string]interface{}, error) {
	// Prepare the operations JSON
	query := fmt.Sprintf(`
		mutation %s($%s: Upload!) {
			%s(file: $%s) {
				# Your mutation fields here
			}
		}
	`, mutationName, files[0].Name, mutationName, files[0].Name)

	operation := Operation{
		Query:     query,
		Variables: variables,
	}
	operationsJSON, err := json.Marshal(map[string][]Operation{"": {operation}})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal operations: %v", err)
	}

	// Prepare the multipart form
	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)

	// Add operations to the form
	part, err := writer.CreateFormFile("operations", "operations.json")
	if err != nil {
		return nil, fmt.Errorf("failed to create form part for operations: %v", err)
	}
	_, err = part.Write(operationsJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to write operations: %v", err)
	}

	// Add map to the form
	mapData := make(map[string]string)
	for _, file := range files {
		mapData[file.Name] = fmt.Sprintf("variables.%s", file.Name)
	}
	mapJSON, err := json.Marshal(mapData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal map: %v", err)
	}
	mapPart, err := writer.CreateFormFile("map", "map.json")
	if err != nil {
		return nil, fmt.Errorf("failed to create form part for map: %v", err)
	}
	_, err = mapPart.Write(mapJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to write map: %v", err)
	}

	// Add files to the form
	for _, file := range files {
		filePart, err := writer.CreateFormFile("file", file.Filename)
		if err != nil {
			return nil, fmt.Errorf("failed to create form part for file: %v", err)
		}

		f, err := os.Open(file.Filepath)
		if err != nil {
			return nil, fmt.Errorf("failed to open file: %v", err)
		}
		defer f.Close()

		_, err = io.Copy(filePart, f)
		if err != nil {
			return nil, fmt.Errorf("failed to copy file contents: %v", err)
		}
	}

	err = writer.Close()
	if err != nil {
		return nil, fmt.Errorf("failed to close multipart writer: %v", err)
	}

	// Prepare the HTTP request
	req, err := http.NewRequest("POST", uri, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %v", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	// Execute the request
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute HTTP request: %v", err)
	}
	defer resp.Body.Close()

	// Parse the response
	var gqlResponse map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&gqlResponse)
	if err != nil {
		return nil, fmt.Errorf("failed to decode GraphQL response: %v", err)
	}

	return &gqlResponse, nil
}
