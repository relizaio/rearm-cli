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
	"html"
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
		fmt.Fprintln(os.Stderr, "Error: ReARM URI is required (--uri, REARM_URI, or `rearm login`)")
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
	switch resolvedAuthMode() {
	case authSession:
		if !sessionOnFile() {
			fmt.Fprintln(os.Stderr, "Error: no browser-login session on file; run `rearm login`")
			os.Exit(1)
		}
		// a session from the credentials file renews under the file's lock, shared with every other rearm
		// process of this login (task S401-1); one from the environment keeps the old path
		sessionStoreInUse = sessionFromFile
		if sessionStoreInUse {
			opts = append(opts, rearm.WithSessionStore(credentialsStore{}))
		}
		c, err = rearm.NewWithSession(rearmUri, sessionRefreshToken, rearm.SessionTokens{
			AccessToken:       sessionAccessToken,
			AccessTokenExpiry: sessionAccessTokenExp,
			SessionExpiry:     sessionExpiresAt,
			SessionHardExpiry: sessionHardExpiry,
		}, persistSessionTokens, opts...)
	case authGitHubOIDC:
		// the identity token GitHub issues to the job, exchanged through an organization's trust rule: no secret
		c, err = rearm.NewWithAssertion(rearmUri, oidcOrg(), rearm.GitHubActionsAssertion(rearmUri, nil), opts...)
	default:
		// the access token outlives the process in the token cache file (task TPF-67, tokenCache.go)
		if s, ok := tokenCacheStore(); ok {
			opts = append(opts, rearm.WithTokenStore(s))
		}
		c, err = rearm.New(rearmUri, apiKeyId, apiKey, opts...)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
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
		// A verb that fell back to the current session clears it when the server says it is closed (task RD5-4).
		noticeClosedCurrentSession(err)
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
		// a 413 names the batch hint only for addreleases (task PFA-10)
		batch := query == rearm.AddReleasesProgrammatic_Operation
		if msg, ok := uploadLimitMessage(err, batch); ok {
			fmt.Println("Error:", msg)
		} else {
			fmt.Println("Error:", describeError(err))
		}
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

// uploadLimitMessage is the CLI's own message for an HTTP 413 (task PFA-10, PFA-3 design 3.3): the
// size of the request this CLI sent and the server's upload limit, named as a default that may
// differ, never as this server's value. It reports false for any other error. batch adds the hint
// for a call that carries several releases (addreleases).
func uploadLimitMessage(err error, batch bool) (string, bool) {
	var se *rearm.StatusError
	if !errors.As(err, &se) || se.StatusCode != http.StatusRequestEntityTooLarge {
		return "", false
	}
	request := "the request"
	if se.RequestBytes > 0 {
		// the server's 50MB and nginx's 50M are both multiples of 1048576 bytes
		request = fmt.Sprintf("the request of %.1f MB", float64(se.RequestBytes)/1048576)
	}
	msg := request + " exceeds the upload limit of this server (HTTP 413). The default limit is 50 MB, but it may be different on this server; it is set by the server's administrator (Helm value max_upload_size_in_MB)."
	msg += limitAnsweredBy(se.Body)
	if batch {
		msg += " For a batch, send fewer releases per call."
	}
	return msg, true
}

var (
	// a style or script block with its content, also one the 200-byte cut left unclosed
	styleBlock  = regexp.MustCompile(`(?is)<style\b.*?(?:</style\s*>|$)`)
	scriptBlock = regexp.MustCompile(`(?is)<script\b.*?(?:</script\s*>|$)`)
	htmlTag     = regexp.MustCompile(`<[^>]*>`)
	// a tag the 200-byte cut left without its closing >
	openTagTail = regexp.MustCompile(`<[^>]*$`)
	whitespace  = regexp.MustCompile(`\s+`)
)

// quotedAnswerRunes is how much of a proxy's page the message quotes, in characters.
const quotedAnswerRunes = 120

// limitAnsweredBy names who refused a 413 from the start of its page (StatusError.Body: the first 200
// bytes of an answer that was not JSON), so an administrator knows which limit to raise.
//   - ReARM's own nginx serves its error page, whose contact address names reliza.io once its HTML
//     entities are decoded.
//   - The ReARM backend answers its limit with JSON, but a request refused while a filter reads its
//     parts gets the servlet container's page (HTTP Status 413, Apache Tomcat), which ReARM's nginx
//     passes through. Both are the same Helm value.
//   - Any other page came from something in front of ReARM (a Traefik buffering middleware, a cloud
//     gateway, another ingress) with a limit of its own; its words are quoted without markup: style
//     and script blocks go with their content, and a tag the cut left unclosed goes too.
//
// An empty Body says nothing: a JSON answer leaves it empty, and a proxy may send no page at all.
func limitAnsweredBy(body string) string {
	if body == "" {
		return ""
	}
	text := styleBlock.ReplaceAllString(body, " ")
	text = scriptBlock.ReplaceAllString(text, " ")
	text = htmlTag.ReplaceAllString(text, " ")
	text = openTagTail.ReplaceAllString(text, " ")
	// the 200-byte cut can also split the last character
	text = strings.ToValidUTF8(html.UnescapeString(text), "")
	text = strings.TrimSpace(whitespace.ReplaceAllString(text, " "))
	if strings.Contains(text, "reliza.io") {
		return " It was answered by the ReARM web server (nginx), whose limit is that Helm value."
	}
	if strings.Contains(text, "HTTP Status 413") || strings.Contains(text, "Apache Tomcat") {
		return " It was answered by the ReARM backend, whose limit is that Helm value."
	}
	if text == "" {
		return ""
	}
	if runes := []rune(text); len(runes) > quotedAnswerRunes {
		text = strings.TrimSpace(string(runes[:quotedAnswerRunes])) + "..."
	}
	return fmt.Sprintf(" It was answered by something in front of ReARM, which has a limit of its own; its answer begins: %q.", text)
}

// describeError renders a client error for the terminal: GraphQL messages joined, a refused
// session refresh as a hint to log in again, a 413 as the upload limit message, anything else
// verbatim.
func describeError(err error) string {
	if msg, ok := uploadLimitMessage(err, false); ok {
		return msg
	}
	var ae *rearm.AssertionError
	if errors.As(err, &ae) {
		return fmt.Sprintf("identity token refused (%s: %s); check the trust rule in ReARM and, if several organizations trust this repository, pass --org", ae.Code, ae.Description)
	}
	var se *rearm.SessionError
	if errors.As(err, &se) {
		desc := se.Description
		if desc == "" {
			desc = se.Code
		}
		if strings.HasPrefix(desc, "session ended at ") {
			// the key's bound ended the session; the words already say what to do
			return desc
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
