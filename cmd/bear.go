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

	cdx "github.com/CycloneDX/cyclonedx-go"
	"github.com/spf13/cobra"
)

var (
	bearUri      string
	bearApiKey   string
	skipPatterns []string
)

const bearBatchSize = 20

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
	Short: "Enrich Supplier and License on BEAR",
	Long:  `For each component, enrich supplier (if missing) and license (if missing or contains LicenseRef-scancode) using BEAR`,
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
	Type     string              `json:"type,omitempty"`
	Name     string              `json:"name,omitempty"`
	Purl     string              `json:"purl,omitempty"`
	Supplier *BearSupplier       `json:"supplier,omitempty"`
	Licenses []BearLicenseChoice `json:"licenses,omitempty"`
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
	enrichedComponents := bearEnrichBatch(purlsToEnrich)

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
	enrichedComponents := bearEnrichBatch(purlsToEnrich)

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

	// Collect components that need enrichment (supplier OR license)
	var purlsToEnrich []string
	purlToIndices := make(map[string][]int)
	needsSupplier := make(map[string]bool)
	needsLicense := make(map[string]bool)

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

		if compNeedsSupplier || compNeedsLicense {
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
		}
	}

	if len(purlsToEnrich) == 0 {
		fmt.Println("No components need enrichment")
		writeOutput(bom)
		return
	}

	fmt.Printf("Found %d components needing enrichment\n", len(purlsToEnrich))

	// Process in batches
	enrichedComponents := bearEnrichBatch(purlsToEnrich)

	// Update components with enriched data
	suppliersEnriched := 0
	licensesEnriched := 0
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
			}
		}
	}

	if debug == "true" {
		fmt.Printf("Enrichment statistics: %d suppliers enriched, %d licenses enriched\n", suppliersEnriched, licensesEnriched)
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
	lower := strings.ToLower(val)
	return lower == "noassertion" || lower == "unresolved" || lower == "undetected" || lower == "other"
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

func bearEnrichBatch(purls []string) []BearComponent {
	var allResults []BearComponent

	// Process in batches of bearBatchSize
	for i := 0; i < len(purls); i += bearBatchSize {
		end := i + bearBatchSize
		if end > len(purls) {
			end = len(purls)
		}
		batch := purls[i:end]

		fmt.Printf("Processing batch %d-%d of %d\n", i+1, end, len(purls))

		results := bearEnrichBatchRequest(batch)
		allResults = append(allResults, results...)
	}

	return allResults
}

func bearEnrichBatchRequest(purls []string) []BearComponent {
	// Build the GraphQL query with purls array
	purlsJson, _ := json.Marshal(purls)
	query := fmt.Sprintf(`{"query":"mutation { enrichBatch(purls: %s) { type name purl supplier { name address { country region locality postOfficeBoxNumber postalCode streetAddress } url contact { name email phone } } licenses { license { id name url } expression } } }"}`,
		strings.ReplaceAll(string(purlsJson), `"`, `\"`))

	req, err := http.NewRequest("POST", bearUri+"/graphql", bytes.NewBufferString(query))
	if err != nil {
		fmt.Printf("Error creating request: %v\n", err)
		return nil
	}

	req.Header.Set("Content-Type", "application/json")
	if bearApiKey != "" {
		req.Header.Set("X-API-Key", bearApiKey)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Error making request: %v\n", err)
		return nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("Error reading response: %v\n", err)
		return nil
	}

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Error response from BEAR: %s\n", string(body))
		return nil
	}

	var response BearEnrichBatchResponse
	if err := json.Unmarshal(body, &response); err != nil {
		fmt.Printf("Error parsing response: %v\n", err)
		return nil
	}

	return response.Data.EnrichBatch
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

func convertBearLicensesToCdx(licenses []BearLicenseChoice) *cdx.Licenses {
	if len(licenses) == 0 {
		return nil
	}

	var cdxLicenses cdx.Licenses
	for _, lic := range licenses {
		if lic.Expression != "" {
			cdxLicenses = append(cdxLicenses, cdx.LicenseChoice{
				Expression: lic.Expression,
			})
		} else if lic.License != nil {
			cdxLicense := &cdx.License{}
			if lic.License.ID != "" {
				cdxLicense.ID = lic.License.ID
			}
			if lic.License.Name != "" {
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
