/*
The MIT License (MIT)

Copyright (c) 2019 - 2026 Reliza Incorporated. https://reliza.io
*/

package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	rearm "github.com/relizaio/rearm-client-go"
)

// Every ReARM call goes through rearm-client-go. The CLI carries no GraphQL text of its own: the
// operations are the client's generated *_Operation constants, sent through its raw passthrough so
// the JSON printed here is exactly what the server produced (typed responses would normalise
// nulls and field order, and scripts parse this output).
//
// Two credential modes, decided once per invocation: an API key (--apikeyid/--apikey, the
// environment, or the credentials file) or a browser-login session (refresh token on file, see
// browserlogin.go). The client handles the token exchange, the endpoint fallback for older
// servers, and session refresh; refreshed tokens come back through persistSessionTokens.

var apiClient *rearm.Client

func rearmClient() *rearm.Client {
	if apiClient != nil {
		return apiClient
	}
	if strings.TrimSpace(rearmUri) == "" {
		fmt.Println("Error: ReARM URI is required (--uri, REARM_URI, or `rearm login`)")
		os.Exit(1)
	}
	opts := []rearm.Option{
		rearm.WithUserAgent("ReARM CLI"),
		rearm.WithHTTPClient(&http.Client{Timeout: 10 * time.Minute}),
	}
	var (
		c   *rearm.Client
		err error
	)
	if inSessionMode() {
		c, err = rearm.NewWithSession(rearmUri, sessionRefreshToken, rearm.SessionTokens{
			AccessToken:       sessionAccessToken,
			AccessTokenExpiry: sessionAccessTokenExp,
			SessionExpiry:     sessionExpiresAt,
		}, persistSessionTokens, opts...)
	} else {
		c, err = rearm.New(rearmUri, apiKeyId, apiKey, opts...)
	}
	if err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
	apiClient = c
	return c
}

var (
	opNameRe    = regexp.MustCompile(`^\s*(?:query|mutation)\s+([A-Za-z_][A-Za-z0-9_]*)`)
	rootFieldRe = regexp.MustCompile(`\{\s*([A-Za-z_][A-Za-z0-9_]*)`)
)

// opNameOf is the operation name declared by a generated *_Operation document.
func opNameOf(query string) string {
	m := opNameRe.FindStringSubmatch(query)
	if m == nil {
		return ""
	}
	return m[1]
}

// rootFieldOf is the first selected root field: the key under `data` that holds the result.
func rootFieldOf(query string) string {
	m := rootFieldRe.FindStringSubmatch(query)
	if m == nil {
		return ""
	}
	return m[1]
}

func decodeData(raw json.RawMessage) map[string]interface{} {
	var data map[string]interface{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &data)
	}
	if data == nil {
		data = map[string]interface{}{}
	}
	return data
}

// sendGraphQLRequest runs one operation and returns the server's `data` member as a map.
func sendGraphQLRequest(query string, variables map[string]interface{}) (map[string]interface{}, error) {
	raw, err := rearm.Raw(context.Background(), rearmClient(), opNameOf(query), query, variables)
	if err != nil {
		return nil, err
	}
	return decodeData(raw), nil
}

// sendRequest runs one operation and returns the marshalled result of the named root field, or
// prints the error and exits.
func sendRequest(query string, variables map[string]interface{}, endpoint string) string {
	data, err := sendGraphQLRequest(query, variables)
	if err != nil {
		printGqlError(err)
		os.Exit(1)
	}
	jsonResponse, _ := json.Marshal(data[endpoint])
	return string(jsonResponse)
}

// fileParts turns the CLI's numbered file map (built by processArtifactsInput and its kin) into
// the client's ordered file parts.
func fileParts(locationMap map[string][]string, filesMap map[string]interface{}) []rearm.FilePart {
	keys := make([]string, 0, len(filesMap))
	for k := range filesMap {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, _ := strconv.Atoi(keys[i])
		b, _ := strconv.Atoi(keys[j])
		return a < b
	})
	parts := make([]rearm.FilePart, 0, len(keys))
	for _, k := range keys {
		fd, ok := filesMap[k].(FileData)
		if !ok {
			fmt.Printf("Warning: Value for key '%s' is not FileData\n", k)
			continue
		}
		paths := locationMap[k]
		if len(paths) == 0 {
			fmt.Fprintf(os.Stderr, "Error: no variable location for file part %s\n", k)
			os.Exit(1)
		}
		parts = append(parts, rearm.FilePart{Filename: fd.Filename, Content: bytes.NewReader(fd.Bytes), VariablePath: paths[0]})
	}
	return parts
}

// uploadMultipart sends an operation whose variables carry files and returns the raw `data`
// member; GraphQL errors are printed the way the multipart commands always reported them.
func uploadMultipart(query string, variables map[string]interface{}, files []rearm.FilePart) json.RawMessage {
	raw, err := rearm.UploadMultipart(context.Background(), rearmClient(), opNameOf(query), query, variables, files)
	if err != nil {
		var gqlErrs rearm.GraphQLErrors
		if errors.As(err, &gqlErrs) {
			if len(raw) > 0 {
				fmt.Println(`{"data":` + string(raw) + `}`)
			}
			fmt.Println("GraphQL returned errors:")
			for _, e := range gqlErrs {
				fmt.Printf("- %s\n", e.Message)
			}
			os.Exit(1)
		}
		fmt.Println("Error:", describeError(err))
		os.Exit(1)
	}
	return raw
}

// printGraphQLMultipart uploads and prints the response envelope, as addrelease, addreleases,
// addartifact, addodeliverable and `agent session add-artifact` always have.
func printGraphQLMultipart(query string, variables map[string]interface{}, locationMap map[string][]string, filesMap map[string]interface{}) {
	raw := uploadMultipart(query, variables, fileParts(locationMap, filesMap))
	if debug == "true" {
		fmt.Println("Response Body:")
	}
	fmt.Println(`{"data":` + string(raw) + `}`)
}

// sendGraphQLMultipart uploads and returns the marshalled result of the root field, the same
// shape as sendRequest, so callers that pipe getversion output through jq see no difference.
func sendGraphQLMultipart(query string, variables map[string]interface{}, locationMap map[string][]string, filesMap map[string]interface{}) string {
	raw := uploadMultipart(query, variables, fileParts(locationMap, filesMap))
	out, _ := json.Marshal(decodeData(raw)[rootFieldOf(query)])
	return string(out)
}

// describeError renders a client error for the terminal: GraphQL messages joined, a refused
// session refresh as a hint to log in again, anything else verbatim.
func describeError(err error) string {
	var se *rearm.SessionError
	if errors.As(err, &se) {
		desc := se.Description
		if desc == "" {
			desc = se.Code
		}
		return fmt.Sprintf("session refresh failed (%s): run `rearm login` again", desc)
	}
	var gqlErrs rearm.GraphQLErrors
	if errors.As(err, &gqlErrs) {
		messages := make([]string, 0, len(gqlErrs))
		for _, e := range gqlErrs {
			if e.Message != "" {
				messages = append(messages, e.Message)
			}
		}
		if len(messages) > 0 {
			return strings.Join(messages, "; ")
		}
	}
	return err.Error()
}

func printGqlError(err error) {
	fmt.Println("Error:", describeError(err))
}
