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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	cdx "github.com/CycloneDX/cyclonedx-go"
	purl "github.com/package-url/packageurl-go"
	"github.com/spf13/cobra"
)

var (
	bearUri      string
	bearApiKey   string
	skipPatterns []string
)

const bearBatchSize = 5

var enrichSupplierCmd = &cobra.Command{
	Use:   "enrichsupplier",
	Short: "Enrich Supplier on BEAR",
	Long:  `For each component, enrich its supplier where not present using BEAR`,
	Run: func(cmd *cobra.Command, args []string) {
		enrichSupplierFunc()
	},
}

var enrichLicenseCmd = &cobra.Command{
	Use:   "enrichlicense",
	Short: "Enrich License on BEAR",
	Long:  `For each component, enrich its license where missing or contains LicenseRef-scancode using BEAR`,
	Run: func(cmd *cobra.Command, args []string) {
		enrichLicenseFunc()
	},
}

var enrichCmd = &cobra.Command{
	Use:   "enrich",
	Short: "Enrich Supplier, License, and Copyright on BEAR",
	Long:  `For each component, enrich supplier (if missing), license (if missing or contains LicenseRef-scancode), and copyright (if missing) using BEAR`,
	Run: func(cmd *cobra.Command, args []string) {
		enrichFunc()
	},
}

func init() {
	enrichSupplierCmd.PersistentFlags().StringVar(&bearUri, "bearUri", "https://beardemo.rearmhq.com", "BEAR URI to use")
	enrichSupplierCmd.PersistentFlags().StringVar(&bearApiKey, "bearApiKey", "", "BEAR API Key")
	bomUtils.AddCommand(enrichSupplierCmd)

	enrichLicenseCmd.PersistentFlags().StringVar(&bearUri, "bearUri", "https://beardemo.rearmhq.com", "BEAR URI to use")
	enrichLicenseCmd.PersistentFlags().StringVar(&bearApiKey, "bearApiKey", "", "BEAR API Key")
	bomUtils.AddCommand(enrichLicenseCmd)

	enrichCmd.PersistentFlags().StringVar(&bearUri, "bearUri", "https://beardemo.rearmhq.com", "BEAR URI to use")
	enrichCmd.PersistentFlags().StringVar(&bearApiKey, "bearApiKey", "", "BEAR API Key")
	enrichCmd.PersistentFlags().StringArrayVar(&skipPatterns, "skipPattern", []string{}, "Skip components whose purl contains this pattern (can be specified multiple times)")
	bomUtils.AddCommand(enrichCmd)
}

type BearGraphQLRequest struct {
	Query string `json:"query"`
}

type BearLicense struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
	URL  string `json:"url,omitempty"`
}

type BearLicenseChoice struct {
	License    *BearLicense `json:"license,omitempty"`
	Expression string       `json:"expression,omitempty"`
}

type BearAddress struct {
	Country             string `json:"country,omitempty"`
	Region              string `json:"region,omitempty"`
	Locality            string `json:"locality,omitempty"`
	PostOfficeBoxNumber string `json:"postOfficeBoxNumber,omitempty"`
	PostalCode          string `json:"postalCode,omitempty"`
	StreetAddress       string `json:"streetAddress,omitempty"`
}

type BearContact struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email,omitempty"`
	Phone string `json:"phone,omitempty"`
}

type BearSupplier struct {
	Name    string        `json:"name,omitempty"`
	Address *BearAddress  `json:"address,omitempty"`
	URL     []string      `json:"url,omitempty"`
	Contact []BearContact `json:"contact,omitempty"`
}

type BearComponent struct {
	Type      string              `json:"type,omitempty"`
	Name      string              `json:"name,omitempty"`
	Purl      string              `json:"purl,omitempty"`
	Supplier  *BearSupplier       `json:"supplier,omitempty"`
	Licenses  []BearLicenseChoice `json:"licenses,omitempty"`
	Copyright string              `json:"copyright,omitempty"`
}

type BearEnrichBatchResponse struct {
	Data struct {
		EnrichBatch []BearComponent `json:"enrichBatch"`
	} `json:"data"`
}

func enrichSupplierFunc() {
	fmt.Println("Using BEAR at", bearUri)

	bom := readBom()
	components := bom.Components
	if components == nil {
		fmt.Println("No components found in BOM")
		return
	}

	// Collect components that need supplier enrichment
	var purlsToEnrich []string
	purlToIndices := make(map[string][]int)

	for ind, comp := range *components {
		if comp.PackageURL != "" && needsSupplierEnrichment(&comp) {
			purlsToEnrich = append(purlsToEnrich, comp.PackageURL)
			purlToIndices[comp.PackageURL] = append(purlToIndices[comp.PackageURL], ind)
		}
	}

	if len(purlsToEnrich) == 0 {
		fmt.Println("No components need supplier enrichment")
		writeOutput(bom)
		return
	}

	fmt.Printf("Found %d components needing supplier enrichment\n", len(purlsToEnrich))

	// Process in batches
	enrichedComponents, err := bearEnrichBatch(purlsToEnrich)
	if err != nil {
		fmt.Printf("Error during enrichment: %v\n", err)
		os.Exit(1)
	}

	// Update components with enriched supplier data
	suppliersEnriched := 0
	for _, enriched := range enrichedComponents {
		if indices, ok := purlToIndices[enriched.Purl]; ok {
			supplier := convertBearSupplierToCdx(enriched.Supplier)
			if supplier != nil {
				for _, idx := range indices {
					(*components)[idx].Supplier = supplier
					suppliersEnriched++
				}
			}
		}
	}

	if debug == "true" {
		fmt.Printf("Enrichment statistics: %d suppliers enriched\n", suppliersEnriched)
	}

	bom.Components = components
	outErr := writeOutput(bom)
	if outErr != nil {
		fmt.Println(outErr)
		os.Exit(1)
	}
}

func enrichLicenseFunc() {
	fmt.Println("Using BEAR at", bearUri)

	bom := readBom()
	components := bom.Components
	if components == nil {
		fmt.Println("No components found in BOM")
		return
	}

	// Collect components that need license enrichment
	var purlsToEnrich []string
	purlToIndices := make(map[string][]int)

	for ind, comp := range *components {
		if comp.PackageURL != "" && needsLicenseEnrichment(&comp) {
			purlsToEnrich = append(purlsToEnrich, comp.PackageURL)
			purlToIndices[comp.PackageURL] = append(purlToIndices[comp.PackageURL], ind)
		}
	}

	if len(purlsToEnrich) == 0 {
		fmt.Println("No components need license enrichment")
		writeOutput(bom)
		return
	}

	fmt.Printf("Found %d components needing license enrichment\n", len(purlsToEnrich))

	// Process in batches
	enrichedComponents, err := bearEnrichBatch(purlsToEnrich)
	if err != nil {
		fmt.Printf("Error during enrichment: %v\n", err)
		os.Exit(1)
	}

	// Update components with enriched license data
	licensesEnriched := 0
	for _, enriched := range enrichedComponents {
		if indices, ok := purlToIndices[enriched.Purl]; ok {
			licenses := convertBearLicensesToCdx(enriched.Licenses)
			if licenses != nil && len(*licenses) > 0 {
				for _, idx := range indices {
					(*components)[idx].Licenses = licenses
					licensesEnriched++
				}
			}
		}
	}

	if debug == "true" {
		fmt.Printf("Enrichment statistics: %d licenses enriched\n", licensesEnriched)
	}

	bom.Components = components
	outErr := writeOutput(bom)
	if outErr != nil {
		fmt.Println(outErr)
		os.Exit(1)
	}
}

func enrichFunc() {
	fmt.Println("Using BEAR at", bearUri)

	bom := readBom()
	components := bom.Components
	if components == nil {
		fmt.Println("No components found in BOM")
		return
	}

	// Extract metadata component purl and add its name to skipPatterns
	addMetadataComponentToSkipPatterns(bom)

	// Collect components that need enrichment (supplier OR license OR copyright)
	var purlsToEnrich []string
	purlToIndices := make(map[string][]int)
	needsSupplier := make(map[string]bool)
	needsLicense := make(map[string]bool)
	needsCopyright := make(map[string]bool)

	for ind, comp := range *components {
		if comp.PackageURL == "" {
			continue
		}

		// Skip components matching any skip pattern
		if shouldSkipPurl(comp.PackageURL) {
			continue
		}

		compNeedsSupplier := needsSupplierEnrichment(&comp)
		compNeedsLicense := needsLicenseEnrichment(&comp)
		compNeedsCopyright := needsCopyrightEnrichment(&comp)

		if compNeedsSupplier || compNeedsLicense || compNeedsCopyright {
			if _, exists := purlToIndices[comp.PackageURL]; !exists {
				purlsToEnrich = append(purlsToEnrich, comp.PackageURL)
			}
			purlToIndices[comp.PackageURL] = append(purlToIndices[comp.PackageURL], ind)
			if compNeedsSupplier {
				needsSupplier[comp.PackageURL] = true
			}
			if compNeedsLicense {
				needsLicense[comp.PackageURL] = true
			}
			if compNeedsCopyright {
				needsCopyright[comp.PackageURL] = true
			}
		}
	}

	if len(purlsToEnrich) == 0 {
		fmt.Println("No components need enrichment")
		writeOutput(bom)
		return
	}

	fmt.Printf("Found %d components needing enrichment\n", len(purlsToEnrich))

	// Process in batches
	enrichedComponents, err := bearEnrichBatch(purlsToEnrich)
	if err != nil {
		fmt.Printf("Error during enrichment: %v\n", err)
		os.Exit(1)
	}

	// Update components with enriched data
	suppliersEnriched := 0
	licensesEnriched := 0
	copyrightsEnriched := 0
	for _, enriched := range enrichedComponents {
		if indices, ok := purlToIndices[enriched.Purl]; ok {
			for _, idx := range indices {
				// Only update supplier if it was missing
				if needsSupplier[enriched.Purl] {
					supplier := convertBearSupplierToCdx(enriched.Supplier)
					if supplier != nil {
						(*components)[idx].Supplier = supplier
						suppliersEnriched++
					}
				}
				// Only update license if it was missing or contained LicenseRef-scancode
				if needsLicense[enriched.Purl] {
					licenses := convertBearLicensesToCdx(enriched.Licenses)
					if licenses != nil && len(*licenses) > 0 {
						(*components)[idx].Licenses = licenses
						licensesEnriched++
					}
				}
				// Only update copyright if it was missing
				if needsCopyright[enriched.Purl] {
					if enriched.Copyright != "" {
						(*components)[idx].Copyright = enriched.Copyright
						copyrightsEnriched++
					}
				}
			}
		}
	}

	if debug == "true" {
		fmt.Printf("Enrichment statistics: %d suppliers enriched, %d licenses enriched, %d copyrights enriched\n", suppliersEnriched, licensesEnriched, copyrightsEnriched)
	}

	bom.Components = components
	outErr := writeOutput(bom)
	if outErr != nil {
		fmt.Println(outErr)
		os.Exit(1)
	}
}

// shouldSkipPurl checks if a purl matches any of the skip patterns (case-insensitive)
func shouldSkipPurl(purl string) bool {
	lowerPurl := strings.ToLower(purl)
	for _, pattern := range skipPatterns {
		if strings.Contains(lowerPurl, strings.ToLower(pattern)) {
			return true
		}
	}
	return false
}

// isUnresolvedValue checks if a string value indicates unresolved/missing data (case-insensitive)
func isUnresolvedValue(val string) bool {
	if val == "" {
		return true
	}
	lower := strings.ToLower(val)
	return strings.Contains(lower, "noassertion") || strings.Contains(lower, "unresolved") || strings.Contains(lower, "undetected") || strings.Contains(lower, "other")
}

// needsSupplierEnrichment checks if a component needs supplier enrichment
func needsSupplierEnrichment(comp *cdx.Component) bool {
	if comp.Supplier == nil {
		return true
	}
	if isUnresolvedValue(comp.Supplier.Name) {
		return true
	}
	return false
}

func needsLicenseEnrichment(comp *cdx.Component) bool {
	// Missing license
	if comp.Licenses == nil || len(*comp.Licenses) == 0 {
		return true
	}

	// Check if any license contains LicenseRef-scancode or unresolved values
	for _, lic := range *comp.Licenses {
		if lic.Expression != "" {
			if strings.Contains(lic.Expression, "LicenseRef-scancode") || isUnresolvedValue(lic.Expression) {
				return true
			}
		}
		if lic.License != nil {
			if strings.Contains(lic.License.ID, "LicenseRef-scancode") || isUnresolvedValue(lic.License.ID) {
				return true
			}
			if strings.Contains(lic.License.Name, "LicenseRef-scancode") || isUnresolvedValue(lic.License.Name) {
				return true
			}
		}
	}

	return false
}

// needsCopyrightEnrichment checks if a component needs copyright enrichment
func needsCopyrightEnrichment(comp *cdx.Component) bool {
	if comp.Copyright == "" {
		return true
	}
	if isUnresolvedValue(comp.Copyright) {
		return true
	}
	return false
}

func bearEnrichBatch(purls []string) ([]BearComponent, error) {
	var allResults []BearComponent

	// Process in batches of bearBatchSize
	for i := 0; i < len(purls); i += bearBatchSize {
		end := i + bearBatchSize
		if end > len(purls) {
			end = len(purls)
		}
		batch := purls[i:end]

		fmt.Printf("Processing batch %d-%d of %d\n", i+1, end, len(purls))

		results, err := bearEnrichBatchRequest(batch)
		if err != nil {
			return nil, fmt.Errorf("failed to enrich batch %d-%d: %w", i+1, end, err)
		}
		allResults = append(allResults, results...)
	}

	return allResults, nil
}

func bearEnrichBatchRequest(purls []string) ([]BearComponent, error) {
	// Build the GraphQL query with purls array
	purlsJson, _ := json.Marshal(purls)
	query := fmt.Sprintf(`{"query":"mutation { enrichBatch(purls: %s) { type name purl supplier { name address { country region locality postOfficeBoxNumber postalCode streetAddress } url contact { name email phone } } licenses { license { id name url } expression } copyright } }"}`,
		strings.ReplaceAll(string(purlsJson), `"`, `\"`))

	req, err := http.NewRequest("POST", bearUri+"/graphql", bytes.NewBufferString(query))
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if bearApiKey != "" {
		req.Header.Set("X-API-Key", bearApiKey)
	}

	// Add timeout to prevent hanging
	client := &http.Client{
		Timeout: 300 * time.Second,
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error making request to BEAR: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("BEAR API returned status %d: %s", resp.StatusCode, string(body))
	}

	var response BearEnrichBatchResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("error parsing response: %w", err)
	}

	return response.Data.EnrichBatch, nil
}

// addMetadataComponentToSkipPatterns extracts the metadata->component->purl if present,
// parses it to get the name, and adds .*<name>.* to skipPatterns
func addMetadataComponentToSkipPatterns(bom *cdx.BOM) {
	// Check if metadata exists
	if bom.Metadata == nil {
		return
	}

	// Check if component exists
	if bom.Metadata.Component == nil {
		return
	}

	// Check if purl exists
	if bom.Metadata.Component.PackageURL == "" {
		return
	}

	// Try to parse the purl
	parsedPurl, err := purl.FromString(bom.Metadata.Component.PackageURL)
	if err != nil {
		// Silently fail - just don't add skip pattern
		return
	}

	// Extract name from parsed purl
	if parsedPurl.Name == "" {
		return
	}

	// Add .*<name>.* pattern to skipPatterns
	pattern := ".*" + parsedPurl.Name + ".*"
	skipPatterns = append(skipPatterns, pattern)

	if debug == "true" {
		fmt.Printf("Auto-added skip pattern from metadata component: %s\n", pattern)
	}
}

func convertBearSupplierToCdx(supplier *BearSupplier) *cdx.OrganizationalEntity {
	if supplier == nil || supplier.Name == "" {
		return nil
	}

	entity := &cdx.OrganizationalEntity{
		Name: supplier.Name,
	}

	if len(supplier.URL) > 0 {
		entity.URL = &supplier.URL
	}

	if supplier.Address != nil {
		entity.Address = &cdx.PostalAddress{
			Country:             supplier.Address.Country,
			Region:              supplier.Address.Region,
			Locality:            supplier.Address.Locality,
			PostOfficeBoxNumber: supplier.Address.PostOfficeBoxNumber,
			PostalCode:          supplier.Address.PostalCode,
			StreetAddress:       supplier.Address.StreetAddress,
		}
	}

	if len(supplier.Contact) > 0 {
		var contacts []cdx.OrganizationalContact
		for _, c := range supplier.Contact {
			contacts = append(contacts, cdx.OrganizationalContact{
				Name:  c.Name,
				Email: c.Email,
				Phone: c.Phone,
			})
		}
		entity.Contact = &contacts
	}

	return entity
}

// isInvalidBearLicense checks if a license value from BEAR should not override existing license
func isInvalidBearLicense(val string) bool {
	upper := strings.ToUpper(val)
	if upper == "UNLICENSED" {
		return true
	}
	if strings.HasPrefix(upper, "SEE LICENSE IN") {
		return true
	}
	return false
}

func convertBearLicensesToCdx(licenses []BearLicenseChoice) *cdx.Licenses {
	if len(licenses) == 0 {
		return nil
	}

	var cdxLicenses cdx.Licenses
	for _, lic := range licenses {
		// Skip invalid license values that should not override existing licenses
		if lic.Expression != "" && isInvalidBearLicense(lic.Expression) {
			continue
		}
		if lic.License != nil && lic.License.ID != "" && isInvalidBearLicense(lic.License.ID) {
			continue
		}
		if lic.License != nil && lic.License.Name != "" && isInvalidBearLicense(lic.License.Name) {
			continue
		}
		if lic.Expression != "" {
			// Validate expression - only use if all IDs are valid SPDX IDs
			if validateLicenseExpressionIds(lic.Expression) {
				cdxLicenses = append(cdxLicenses, cdx.LicenseChoice{
					Expression: lic.Expression,
				})
			} else {
				// Invalid expression, use as name instead
				cdxLicenses = append(cdxLicenses, cdx.LicenseChoice{
					License: &cdx.License{
						Name: lic.Expression,
					},
				})
			}
		} else if lic.License != nil {
			cdxLicense := &cdx.License{}
			// Only put valid SPDX license IDs in the ID field
			if lic.License.ID != "" && validSpdxLicenses[lic.License.ID] {
				cdxLicense.ID = lic.License.ID
			} else if lic.License.ID != "" {
				// Not a valid SPDX ID, use as name instead
				cdxLicense.Name = lic.License.ID
			}
			// If name is provided and we haven't set it from invalid ID
			if lic.License.Name != "" && cdxLicense.Name == "" {
				cdxLicense.Name = lic.License.Name
			}
			if lic.License.URL != "" {
				cdxLicense.URL = lic.License.URL
			}
			cdxLicenses = append(cdxLicenses, cdx.LicenseChoice{
				License: cdxLicense,
			})
		}
	}

	if len(cdxLicenses) == 0 {
		return nil
	}

	return &cdxLicenses
}
