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
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// teaCmd represents the tea command
var teaCmd = &cobra.Command{
	Use:   "tea",
	Short: "Transparency Exchange API commands",
	Long:  `Commands for interacting with Transparency Exchange API (TEA) endpoints.`,
}

var tei string

// discoveryCmd represents the discovery command
var discoveryCmd = &cobra.Command{
	Use:   "discovery",
	Short: "Discover product release UUID from TEI",
	Long: `Resolves a Transparency Exchange Identifier (TEI) to a product release UUID.
	
The command follows the TEA discovery flow:
1. Parse the TEI to extract the domain name
2. Resolve DNS records for the domain
3. Query the .well-known/tea endpoint
4. Call the discovery API endpoint with the TEI
5. Return the product release UUID

Example TEI formats:
  urn:tei:uuid:products.example.com:d4d9f54a-abcf-11ee-ac79-1a52914d44b
  urn:tei:purl:cyclonedx.org:pkg:pypi/cyclonedx-python-lib@8.4.0
  urn:tei:hash:cyclonedx.org:SHA256:fd44efd601f651c8865acf0dfeacb0df19a2b50ec69ead0262096fd2f67197b9`,
	Run: func(cmd *cobra.Command, args []string) {
		if debug == "true" {
			fmt.Println("Resolving TEI:", tei)
		}

		productReleaseUuid, err := resolveTEI(tei)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: Failed to retrieve product release UUID: %v\n", err)
			os.Exit(1)
		}

		fmt.Println(productReleaseUuid)
	},
}

// TEAWellKnownEndpoint represents the structure of the .well-known/tea endpoint response
type TEAWellKnownEndpoint struct {
	URL      string   `json:"url"`
	Versions []string `json:"versions"`
	Priority float64  `json:"priority"`
}

// TEAWellKnownResponse represents the structure of the .well-known/tea response
type TEAWellKnownResponse struct {
	SchemaVersion int                    `json:"schemaVersion"`
	Endpoints     []TEAWellKnownEndpoint `json:"endpoints"`
}

// TEADiscoveryResponse represents the structure of the discovery API response
type TEADiscoveryResponse struct {
	ProductReleaseUuid string   `json:"productReleaseUuid"`
	RootURL            string   `json:"rootUrl"`
	Versions           []string `json:"versions"`
}

func init() {
	discoveryCmd.Flags().StringVar(&tei, "tei", "", "Transparency Exchange Identifier (TEI) to resolve")
	discoveryCmd.MarkFlagRequired("tei")

	teaCmd.AddCommand(discoveryCmd)
}

// resolveTEI resolves a TEI to a product release UUID following the TEA discovery flow
func resolveTEI(tei string) (string, error) {
	// Parse TEI to extract domain name
	domainName, err := extractDomainFromTEI(tei)
	if err != nil {
		return "", fmt.Errorf("invalid TEI format: %w", err)
	}

	if debug == "true" {
		fmt.Println("Extracted domain:", domainName)
	}

	// Resolve DNS for the domain
	hosts, err := resolveDNS(domainName)
	if err != nil {
		return "", fmt.Errorf("DNS resolution failed: %w", err)
	}

	if debug == "true" {
		fmt.Println("Resolved hosts:", hosts)
	}

	// Query .well-known/tea endpoint
	wellKnownResp, err := queryWellKnown(domainName)
	if err != nil {
		return "", fmt.Errorf("failed to query .well-known/tea endpoint: %w", err)
	}

	if debug == "true" {
		fmt.Printf("Well-known response: %+v\n", wellKnownResp)
	}

	// Select the best endpoint
	endpoint := selectBestEndpoint(wellKnownResp.Endpoints)
	if endpoint == nil {
		return "", fmt.Errorf("no suitable endpoint found in .well-known/tea response")
	}

	if debug == "true" {
		fmt.Printf("Selected endpoint: %s (version: %s)\n", endpoint.URL, endpoint.Versions[0])
	}

	// Call discovery API
	discoveryResp, err := callDiscoveryAPI(endpoint, tei)
	if err != nil {
		return "", fmt.Errorf("discovery API call failed: %w", err)
	}

	return discoveryResp.ProductReleaseUuid, nil
}

// extractDomainFromTEI extracts the domain name from a TEI
// TEI format: urn:tei:<type>:<domain-name>:<unique-identifier>
func extractDomainFromTEI(tei string) (string, error) {
	// Validate TEI format
	if !strings.HasPrefix(tei, "urn:tei:") {
		return "", fmt.Errorf("TEI must start with 'urn:tei:'")
	}

	// Split by colons
	parts := strings.Split(tei, ":")
	if len(parts) < 4 {
		return "", fmt.Errorf("TEI must have at least 4 parts separated by colons")
	}

	// parts[0] = "urn"
	// parts[1] = "tei"
	// parts[2] = type (e.g., "uuid", "purl", "hash")
	// parts[3] = domain-name

	domainName := parts[3]

	// Validate domain name format
	if domainName == "" {
		return "", fmt.Errorf("domain name cannot be empty")
	}

	// Basic domain validation
	domainRegex := regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?)*$`)
	if !domainRegex.MatchString(domainName) {
		return "", fmt.Errorf("invalid domain name format")
	}

	return domainName, nil
}

// resolveDNS resolves DNS records for a domain (A, AAAA, CNAME)
func resolveDNS(domainName string) ([]string, error) {
	// Use net.LookupHost which handles A, AAAA, and CNAME records
	hosts, err := net.LookupHost(domainName)
	if err != nil {
		return nil, fmt.Errorf("DNS lookup failed for %s: %w", domainName, err)
	}

	if len(hosts) == 0 {
		return nil, fmt.Errorf("no DNS records found for %s", domainName)
	}

	return hosts, nil
}

// queryWellKnown queries the .well-known/tea endpoint
func queryWellKnown(domainName string) (*TEAWellKnownResponse, error) {
	wellKnownURL := fmt.Sprintf("https://%s/.well-known/tea", domainName)

	if debug == "true" {
		fmt.Println("Querying well-known endpoint:", wellKnownURL)
	}

	// Create HTTP client with TLS verification
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
			},
		},
	}

	resp, err := client.Get(wellKnownURL)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP request returned status %d", resp.StatusCode)
	}

	var wellKnownResp TEAWellKnownResponse
	if err := json.NewDecoder(resp.Body).Decode(&wellKnownResp); err != nil {
		return nil, fmt.Errorf("failed to decode JSON response: %w", err)
	}

	if len(wellKnownResp.Endpoints) == 0 {
		return nil, fmt.Errorf("no endpoints found in .well-known/tea response")
	}

	return &wellKnownResp, nil
}

// selectBestEndpoint selects the best endpoint based on priority and version
func selectBestEndpoint(endpoints []TEAWellKnownEndpoint) *TEAWellKnownEndpoint {
	if len(endpoints) == 0 {
		return nil
	}

	// For simplicity, select the endpoint with the highest priority
	// In a production implementation, you would also consider version compatibility
	var bestEndpoint *TEAWellKnownEndpoint
	highestPriority := -1.0

	for i := range endpoints {
		endpoint := &endpoints[i]
		if len(endpoint.Versions) == 0 {
			continue
		}

		// If priority is not set, default to 1.0
		priority := endpoint.Priority
		if priority == 0 {
			priority = 1.0
		}

		if priority > highestPriority {
			highestPriority = priority
			bestEndpoint = endpoint
		}
	}

	return bestEndpoint
}

// callDiscoveryAPI calls the TEA discovery API endpoint
func callDiscoveryAPI(endpoint *TEAWellKnownEndpoint, tei string) (*TEADiscoveryResponse, error) {
	// Select the latest version from the endpoint
	if len(endpoint.Versions) == 0 {
		return nil, fmt.Errorf("no versions available for endpoint")
	}

	// Use the first version (in production, you'd select based on semver)
	version := endpoint.Versions[0]

	// URL-encode the TEI
	encodedTEI := url.QueryEscape(tei)

	// Construct the discovery API URL
	discoveryURL := fmt.Sprintf("%s/v%s/discovery?tei=%s", endpoint.URL, version, encodedTEI)

	if debug == "true" {
		fmt.Println("Calling discovery API:", discoveryURL)
	}

	// Create HTTP client
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
			},
		},
	}

	resp, err := client.Get(discoveryURL)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("TEI not found on the server")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP request returned status %d", resp.StatusCode)
	}

	var discoveryResp TEADiscoveryResponse
	if err := json.NewDecoder(resp.Body).Decode(&discoveryResp); err != nil {
		return nil, fmt.Errorf("failed to decode JSON response: %w", err)
	}

	if discoveryResp.ProductReleaseUuid == "" {
		return nil, fmt.Errorf("product release UUID not found in response")
	}

	return &discoveryResp, nil
}
