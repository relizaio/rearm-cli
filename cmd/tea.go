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
var useHttp bool

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
  urn:tei:uuid:products.example.com:443:d4d9f54a-abcf-11ee-ac79-1a52914d44b
  urn:tei:purl:cyclonedx.org:443:pkg:pypi/cyclonedx-python-lib@8.4.0
  urn:tei:hash:localhost:3000:SHA256:fd44efd601f651c8865acf0dfeacb0df19a2b50ec69ead0262096fd2f67197b9`,
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
	Priority *float64 `json:"priority,omitempty"` // Pointer to distinguish between 0 and unset
}

// TEAWellKnownResponse represents the structure of the .well-known/tea response
type TEAWellKnownResponse struct {
	SchemaVersion int                    `json:"schemaVersion"`
	Endpoints     []TEAWellKnownEndpoint `json:"endpoints"`
}

// TEADiscoveryResponse represents the structure of the discovery API response (array of discovery info)
type TEADiscoveryResponse []TEADiscoveryInfo

// TEADiscoveryInfo represents a single discovery result
type TEADiscoveryInfo struct {
	ProductReleaseUuid string          `json:"productReleaseUuid"`
	Servers            []TEAServerInfo `json:"servers"`
}

// TEAServerInfo represents TEA server information
type TEAServerInfo struct {
	RootURL  string   `json:"rootUrl"`
	Versions []string `json:"versions"`
	Priority *float64 `json:"priority,omitempty"`
}

// TEA API response structures
type TEAProductRelease struct {
	UUID        string            `json:"uuid"`
	ProductName string            `json:"productName"`
	Version     string            `json:"version"`
	Components  []TEAComponentRef `json:"components"`
}

type TEAComponentRef struct {
	UUID    string  `json:"uuid"`
	Release *string `json:"release,omitempty"`
}

type TEAComponent struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
}

type TEAComponentRelease struct {
	Release          TEAReleaseInfo `json:"release"`
	LatestCollection *TEACollection `json:"latestCollection,omitempty"`
}

type TEAReleaseInfo struct {
	UUID          string `json:"uuid"`
	ComponentName string `json:"componentName"`
	Version       string `json:"version"`
}

type TEARelease struct {
	UUID string `json:"uuid"`
}

type TEACollection struct {
	Artifacts []TEAArtifact `json:"artifacts"`
}

type TEAArtifact struct {
	Type    string              `json:"type"`
	Formats []TEAArtifactFormat `json:"formats"`
}

type TEAArtifactFormat struct {
	Description  string `json:"description,omitempty"`
	MimeType     string `json:"mimeType"`
	URL          string `json:"url"`
	SignatureURL string `json:"signatureUrl,omitempty"`
}

// fullTeaFlowCmd represents the full_tea_flow command
var fullTeaFlowCmd = &cobra.Command{
	Use:   "full_tea_flow",
	Short: "Perform complete TEA discovery and data retrieval flow",
	Long: `Discovers a product release from TEI and retrieves complete product information including:
- Product name and version
- Component releases with their versions
- Artifacts and their formats for each component`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := executeFullTeaFlow(tei); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	discoveryCmd.Flags().StringVar(&tei, "tei", "", "Transparency Exchange Identifier (TEI) to resolve")
	discoveryCmd.MarkFlagRequired("tei")
	discoveryCmd.Flags().BoolVar(&useHttp, "usehttp", false, "Use HTTP instead of HTTPS (default: false)")

	fullTeaFlowCmd.Flags().StringVar(&tei, "tei", "", "Transparency Exchange Identifier (TEI) to resolve")
	fullTeaFlowCmd.MarkFlagRequired("tei")
	fullTeaFlowCmd.Flags().BoolVar(&useHttp, "usehttp", false, "Use HTTP instead of HTTPS (default: false)")

	teaCmd.AddCommand(discoveryCmd)
	teaCmd.AddCommand(fullTeaFlowCmd)
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

	// Handle the array response
	if len(*discoveryResp) == 1 {
		// Single element - extract and return the productReleaseUuid
		return (*discoveryResp)[0].ProductReleaseUuid, nil
	}

	// Multiple elements - for now, return an error (will be handled in future)
	return "", fmt.Errorf("multiple discovery results found (%d results) - this case is not yet implemented", len(*discoveryResp))
}

// extractDomainFromTEI extracts the domain name with port from a TEI
// TEI format: urn:tei:<type>:<domain-name>:<domain-port>:<unique-identifier>
// Supported types: uuid, purl, hash, swid
func extractDomainFromTEI(tei string) (string, error) {
	// Validate TEI format
	if !strings.HasPrefix(tei, "urn:tei:") {
		return "", fmt.Errorf("TEI must start with 'urn:tei:'")
	}

	// Remove the "urn:tei:" prefix
	remainder := strings.TrimPrefix(tei, "urn:tei:")

	// Split by colons
	parts := strings.Split(remainder, ":")
	if len(parts) < 4 {
		return "", fmt.Errorf("TEI must have format urn:tei:<type>:<domain-name>:<domain-port>:<unique-identifier>")
	}

	// parts[0] = type (uuid, purl, hash, swid)
	// parts[1] = domain-name
	// parts[2] = domain-port
	// parts[3+] = unique-identifier (which may contain colons)

	teiType := parts[0]
	domainName := parts[1]
	domainPort := parts[2]

	// Validate TEI type
	validTypes := map[string]bool{"uuid": true, "purl": true, "hash": true, "swid": true, "asin": true, "gs1": true}
	if !validTypes[teiType] {
		return "", fmt.Errorf("invalid TEI type: %s (must be uuid, purl, hash, swid, asin, or gs1)", teiType)
	}

	// Validate port number (1-5 digits)
	portRegex := regexp.MustCompile(`^[0-9]{1,5}$`)
	if !portRegex.MatchString(domainPort) {
		return "", fmt.Errorf("invalid port number: %s", domainPort)
	}

	// Validate domain name format
	if domainName == "" {
		return "", fmt.Errorf("domain name cannot be empty")
	}

	// Basic domain validation
	// Matches: domain.com, localhost, etc.
	domainRegex := regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?)*$`)
	if !domainRegex.MatchString(domainName) {
		return "", fmt.Errorf("invalid domain name format")
	}

	// Combine domain and port
	domainWithPort := domainName + ":" + domainPort

	return domainWithPort, nil
}

// resolveDNS resolves DNS records for a domain (A, AAAA, CNAME)
func resolveDNS(domainName string) ([]string, error) {
	// Extract just the hostname part (without port) for DNS lookup
	hostname := domainName
	if strings.Contains(domainName, ":") {
		parts := strings.Split(domainName, ":")
		hostname = parts[0]
	}

	// Use net.LookupHost which handles A, AAAA, and CNAME records
	hosts, err := net.LookupHost(hostname)
	if err != nil {
		return nil, fmt.Errorf("DNS lookup failed for %s: %w", hostname, err)
	}

	if len(hosts) == 0 {
		return nil, fmt.Errorf("no DNS records found for %s", hostname)
	}

	return hosts, nil
}

// createHTTPClient creates an HTTP client with appropriate TLS configuration
func createHTTPClient() *http.Client {
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Only configure TLS if using HTTPS
	if !useHttp {
		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
			},
		}
	}

	return client
}

// queryWellKnown queries the .well-known/tea endpoint
func queryWellKnown(domainName string) (*TEAWellKnownResponse, error) {
	protocol := "https"
	if useHttp {
		protocol = "http"
	}
	wellKnownURL := fmt.Sprintf("%s://%s/.well-known/tea", protocol, domainName)

	if debug == "true" {
		fmt.Println("Querying well-known endpoint:", wellKnownURL)
	}

	// Create HTTP client
	client := createHTTPClient()

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
		priority := 1.0
		if endpoint.Priority != nil {
			priority = *endpoint.Priority
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
	client := createHTTPClient()

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

	if len(discoveryResp) == 0 {
		return nil, fmt.Errorf("no discovery results found in response")
	}

	return &discoveryResp, nil
}

// executeFullTeaFlow performs the complete TEA discovery and data retrieval flow
func executeFullTeaFlow(tei string) error {
	// Step 1: Perform discovery
	if debug == "true" {
		fmt.Println("Step 1: Performing TEI discovery...")
	}

	productReleaseUuid, err := resolveTEI(tei)
	if err != nil {
		return fmt.Errorf("discovery failed: %w", err)
	}

	if debug == "true" {
		fmt.Printf("Discovered product release UUID: %s\n", productReleaseUuid)
	}

	// Extract domain and endpoint info for subsequent API calls
	domainName, err := extractDomainFromTEI(tei)
	if err != nil {
		return fmt.Errorf("failed to extract domain: %w", err)
	}

	wellKnownResp, err := queryWellKnown(domainName)
	if err != nil {
		return fmt.Errorf("failed to query well-known endpoint: %w", err)
	}

	endpoint := selectBestEndpoint(wellKnownResp.Endpoints)
	if endpoint == nil {
		return fmt.Errorf("no suitable endpoint found")
	}

	version := endpoint.Versions[0]
	baseURL := fmt.Sprintf("%s/v%s", endpoint.URL, version)

	// Step 2: Get product release details
	if debug == "true" {
		fmt.Println("\nStep 2: Fetching product release details...")
	}

	productRelease, err := getProductRelease(baseURL, productReleaseUuid)
	if err != nil {
		return fmt.Errorf("failed to get product release: %w", err)
	}

	// Print product information
	fmt.Printf("\n=== Product Information ===\n")
	fmt.Printf("Product Name: %s\n", productRelease.ProductName)
	fmt.Printf("Version: %s\n", productRelease.Version)
	fmt.Printf("\n")

	// Step 3: Process each component
	for i, compRef := range productRelease.Components {
		if debug == "true" {
			fmt.Printf("\nStep 3.%d: Processing component %s...\n", i+1, compRef.UUID)
		}

		var releaseUUID string
		var componentName string

		if compRef.Release != nil {
			// Component has a pinned release
			releaseUUID = *compRef.Release
		} else {
			// Component does not have a pinned release, get the latest
			component, err := getComponent(baseURL, compRef.UUID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: Failed to get component %s: %v\n", compRef.UUID, err)
				continue
			}
			componentName = component.Name

			releases, err := getComponentReleases(baseURL, compRef.UUID)
			if err != nil || len(releases) == 0 {
				fmt.Fprintf(os.Stderr, "Warning: No releases found for component %s\n", componentName)
				continue
			}

			releaseUUID = releases[0].UUID
			fmt.Printf("Note: Component '%s' does not have a pinned release. Selecting latest available release.\n", componentName)
		}

		// Get component release details
		componentRelease, err := getComponentRelease(baseURL, releaseUUID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Failed to get component release %s: %v\n", releaseUUID, err)
			continue
		}

		// Print component information
		fmt.Printf("\n--- Component: %s ---\n", componentRelease.Release.ComponentName)
		fmt.Printf("Version: %s\n", componentRelease.Release.Version)

		// Process artifacts in latest collection
		if componentRelease.LatestCollection != nil {
			for _, artifact := range componentRelease.LatestCollection.Artifacts {
				fmt.Printf("\n  Artifact Type: %s\n", artifact.Type)
				for _, format := range artifact.Formats {
					fmt.Printf("    - Description: %s\n", format.Description)
					fmt.Printf("      Media Type: %s\n", format.MimeType)
					fmt.Printf("      URL: %s\n", format.URL)
					if format.SignatureURL != "" {
						fmt.Printf("      Signature URL: %s\n", format.SignatureURL)
					}
				}
			}
		} else {
			fmt.Printf("  No collection available for this component release.\n")
		}
	}

	return nil
}

// getProductRelease retrieves product release details from TEA API
func getProductRelease(baseURL, uuid string) (*TEAProductRelease, error) {
	url := fmt.Sprintf("%s/productRelease/%s", baseURL, uuid)
	var productRelease TEAProductRelease
	if err := makeTeaAPICall(url, &productRelease); err != nil {
		return nil, err
	}
	return &productRelease, nil
}

// getComponent retrieves component details from TEA API
func getComponent(baseURL, uuid string) (*TEAComponent, error) {
	url := fmt.Sprintf("%s/component/%s", baseURL, uuid)
	var component TEAComponent
	if err := makeTeaAPICall(url, &component); err != nil {
		return nil, err
	}
	return &component, nil
}

// getComponentReleases retrieves component releases from TEA API
func getComponentReleases(baseURL, componentUUID string) ([]TEARelease, error) {
	url := fmt.Sprintf("%s/component/%s/releases", baseURL, componentUUID)
	var releases []TEARelease
	if err := makeTeaAPICall(url, &releases); err != nil {
		return nil, err
	}
	return releases, nil
}

// getComponentRelease retrieves component release details from TEA API
func getComponentRelease(baseURL, uuid string) (*TEAComponentRelease, error) {
	url := fmt.Sprintf("%s/componentRelease/%s", baseURL, uuid)
	var componentRelease TEAComponentRelease
	if err := makeTeaAPICall(url, &componentRelease); err != nil {
		return nil, err
	}
	return &componentRelease, nil
}

// makeTeaAPICall makes a generic HTTP GET call to TEA API and decodes JSON response
func makeTeaAPICall(url string, result interface{}) error {
	if debug == "true" {
		fmt.Printf("API Call: %s\n", url)
	}

	client := createHTTPClient()

	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP request returned status %d", resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
		return fmt.Errorf("failed to decode JSON response: %w", err)
	}

	return nil
}
