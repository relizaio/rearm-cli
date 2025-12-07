package cmd

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	cdx "github.com/CycloneDX/cyclonedx-go"
	"github.com/spdx/tools-golang/spdx/v2/common"
	"github.com/spdx/tools-golang/spdx/v2/v2_3"
)

// Helper functions for SPDX to CycloneDX conversion

func extractTools(creators []common.Creator) *[]cdx.Tool {
	var tools []cdx.Tool
	for _, creator := range creators {
		if creator.CreatorType == "Tool" {
			toolName := creator.Creator
			if toolName != "" {
				tools = append(tools, cdx.Tool{Name: toolName})
			}
		}
	}
	if len(tools) > 0 {
		return &tools
	}
	return nil
}

func extractToolsChoice(creators []common.Creator) *cdx.ToolsChoice {
	tools := extractTools(creators)
	if tools != nil {
		return &cdx.ToolsChoice{Tools: tools}
	}
	return nil
}

func extractAuthors(creators []common.Creator) *[]cdx.OrganizationalContact {
	var authors []cdx.OrganizationalContact
	for _, creator := range creators {
		if creator.CreatorType == "Person" || creator.CreatorType == "Organization" {
			authorName := creator.Creator
			if authorName != "" {
				authors = append(authors, cdx.OrganizationalContact{Name: authorName})
			}
		}
	}
	if len(authors) > 0 {
		return &authors
	}
	return nil
}

func determineComponentType(primaryPackagePurpose string) cdx.ComponentType {
	switch strings.ToUpper(primaryPackagePurpose) {
	case "APPLICATION":
		return cdx.ComponentTypeApplication
	case "FRAMEWORK":
		return cdx.ComponentTypeFramework
	case "LIBRARY":
		return cdx.ComponentTypeLibrary
	case "CONTAINER":
		return cdx.ComponentTypeContainer
	case "OPERATING-SYSTEM":
		return cdx.ComponentTypeOS
	case "DEVICE":
		return cdx.ComponentTypeDevice
	case "FIRMWARE":
		return cdx.ComponentTypeFirmware
	case "FILE":
		return cdx.ComponentTypeFile
	case "SOURCE":
		return cdx.ComponentTypeLibrary // Map SOURCE to library
	case "ARCHIVE":
		return cdx.ComponentTypeLibrary // Map ARCHIVE to library
	case "INSTALL":
		return cdx.ComponentTypeApplication // Map INSTALL to application
	default:
		return cdx.ComponentTypeLibrary
	}
}

// normalizeMalformedLicenseString fixes malformed comma-separated license strings
// from tools like Microsoft BOM tool that produce invalid SPDX expressions like:
// "AND, BSD-2-Clause, BSD-3-Clause, Beerware, Domain, ISC, Public"
// and converts them to proper SPDX expressions like:
// "BSD-2-Clause AND BSD-3-Clause AND Beerware AND ISC"
func normalizeMalformedLicenseString(licenseStr string) string {
	// Check if it's a comma-separated list (malformed format)
	if !strings.Contains(licenseStr, ",") {
		return licenseStr
	}

	// Split by comma and trim spaces
	parts := strings.Split(licenseStr, ",")
	var licenses []string
	var operator string

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Check if this is an operator (AND, OR)
		upperPart := strings.ToUpper(part)
		if upperPart == "AND" || upperPart == "OR" {
			// Store the operator if we haven't found one yet
			if operator == "" {
				operator = upperPart
			}
			continue
		}

		// Filter out invalid or generic license identifiers
		if part == "Domain" || part == "Public" {
			// These are not valid SPDX license identifiers
			// "Domain" likely means "Public Domain"
			continue
		}

		licenses = append(licenses, part)
	}

	// If no valid licenses found, return NOASSERTION instead of invalid original
	if len(licenses) == 0 {
		return "NOASSERTION"
	}

	// If only one license, return it
	if len(licenses) == 1 {
		return licenses[0]
	}

	// Default to AND if no operator was specified
	if operator == "" {
		operator = "AND"
	}

	// Join licenses with the operator
	return strings.Join(licenses, " "+operator+" ")
}

// isPublicDomainVariant checks if the license string is a Public-Domain variant
func isPublicDomainVariant(licenseStr string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(licenseStr, "-", ""))
	return normalized == "publicdomain" || normalized == "public domain"
}

// Helper function to check if a license string is a simple SPDX license ID
func isSimpleLicenseId(license string) bool {
	// First check if it's a complex expression
	if strings.Contains(license, " AND ") ||
		strings.Contains(license, " OR ") ||
		strings.Contains(license, " WITH ") ||
		strings.Contains(license, "(") ||
		strings.Contains(license, ")") {
		return false
	}

	// Check if it's in our valid SPDX license list
	if validSpdxLicenses[license] {
		return true
	}

	// For LicenseRef- IDs, these are also considered simple IDs in SPDX but
	// CycloneDX handles them via ID field if they are valid SPDX IDs or Name field if custom
	// However, if we want to be strict about "spdx-known license ids" only going into ID field:
	// If it starts with LicenseRef-, it's a custom license defined in the SPDX document itself.
	// CycloneDX supports this via the ID field but it's not a "known SPDX license ID".
	// But usually LicenseRef- is used in ID field.
	// The user request says: "if particular encountered license during conversion is not in that license list, in the resulting cyclonedx file use "name" field for this license instead of "id" - so the "id" field is reserved for only spdx-known license ids"

	// So if it's not in the list, it's NOT a simple SPDX ID for our purpose of putting it in the ID field.
	return false
}

// validateLicenseExpressionIds checks if all license IDs in an expression are valid SPDX IDs
func validateLicenseExpressionIds(expression string) bool {
	// Simple tokenization: replace operators and parens with spaces
	// Note: SPDX allows "WITH" for exceptions. Exceptions are also in our validSpdxLicenses map.
	replacer := strings.NewReplacer(
		"(", " ",
		")", " ",
		" AND ", " ",
		" OR ", " ",
		" WITH ", " ",
	)
	cleaned := replacer.Replace(expression)

	parts := strings.Fields(cleaned)
	for _, part := range parts {
		// Verify each part is a known SPDX license ID or exception ID
		if !validSpdxLicenses[part] {
			return false
		}
	}
	return true
}

func parseLicenseExpression(licenseStr string) cdx.Licenses {
	if licenseStr == "" || licenseStr == "NOASSERTION" {
		return nil
	}

	// Normalize malformed comma-separated license strings (e.g., from Microsoft BOM tool)
	licenseStr = normalizeMalformedLicenseString(licenseStr)

	// Handle Public-Domain variants with name property instead of ID
	if isPublicDomainVariant(licenseStr) {
		return cdx.Licenses{{
			License: &cdx.License{
				Name:            "Public Domain",
				Acknowledgement: cdx.LicenseAcknowledgementDeclared,
			},
		}}
	}

	// If it looks like a simple license ID (no operators)
	if isSimpleLicenseId(licenseStr) {
		// It's already validated by isSimpleLicenseId to be in our map
		return cdx.Licenses{{
			License: &cdx.License{
				ID:              licenseStr,
				Acknowledgement: cdx.LicenseAcknowledgementDeclared,
			},
		}}
	}

	// If it contains boolean operators, it's a complex expression
	if strings.Contains(licenseStr, " AND ") ||
		strings.Contains(licenseStr, " OR ") ||
		strings.Contains(licenseStr, " WITH ") ||
		strings.Contains(licenseStr, "(") ||
		strings.Contains(licenseStr, ")") {

		// Validate that all IDs in the expression are known SPDX IDs
		if validateLicenseExpressionIds(licenseStr) {
			return cdx.Licenses{{Expression: licenseStr}}
		}
		// If validation fails (contains unknown IDs), fall through to use Name field
	}

	// Otherwise (unknown license ID, simple string not in our list, or invalid expression), use the Name field
	return cdx.Licenses{{
		License: &cdx.License{
			Name:            licenseStr,
			Acknowledgement: cdx.LicenseAcknowledgementDeclared,
		},
	}}
}

func convertChecksums(checksums []common.Checksum) []cdx.Hash {
	var hashes []cdx.Hash
	for _, checksum := range checksums {
		var alg cdx.HashAlgorithm
		switch strings.ToUpper(string(checksum.Algorithm)) {
		case "SHA1":
			alg = cdx.HashAlgoSHA1
		case "SHA256":
			alg = cdx.HashAlgoSHA256
		case "SHA512":
			alg = cdx.HashAlgoSHA512
		case "MD5":
			alg = cdx.HashAlgoMD5
		default:
			continue // Skip unknown algorithms
		}

		hashes = append(hashes, cdx.Hash{
			Algorithm: alg,
			Value:     checksum.Value,
		})
	}
	return hashes
}

func buildProperties(pkg *v2_3.Package) []cdx.Property {
	var properties []cdx.Property

	// Core SPDX properties
	addProperty(&properties, "spdx:package:"+string(pkg.PackageSPDXIdentifier), pkg.PackageName)
	addProperty(&properties, "spdx:spdxid", string(pkg.PackageSPDXIdentifier))
	addProperty(&properties, "spdx:license-concluded", pkg.PackageLicenseConcluded)
	addProperty(&properties, "spdx:download-location", pkg.PackageDownloadLocation)

	// Enhanced properties from official library
	addPropertyIfNotEmpty(&properties, "spdx:license-declared", pkg.PackageLicenseDeclared)
	addPropertyIfNotEmpty(&properties, "spdx:homepage", pkg.PackageHomePage)
	addPropertyIfNotEmpty(&properties, "spdx:source-info", pkg.PackageSourceInfo)
	addPropertyIfNotEmpty(&properties, "spdx:primary-package-purpose", pkg.PrimaryPackagePurpose)
	addPropertyIfNotEmpty(&properties, "spdx:built-date", pkg.BuiltDate)
	addPropertyIfNotEmpty(&properties, "spdx:valid-until-date", pkg.ValidUntilDate)
	addPropertyIfNotEmpty(&properties, "spdx:release-date", pkg.ReleaseDate)
	addPropertyIfNotEmpty(&properties, "spdx:package-summary", pkg.PackageSummary)
	addPropertyIfNotEmpty(&properties, "spdx:package-description", pkg.PackageDescription)
	addPropertyIfNotEmpty(&properties, "spdx:package-file-name", pkg.PackageFileName)
	addPropertyIfNotEmpty(&properties, "spdx:license-comments", pkg.PackageLicenseComments)
	addPropertyIfNotEmpty(&properties, "spdx:package-comment", pkg.PackageComment)

	// Package verification code
	if pkg.PackageVerificationCode != nil {
		addProperty(&properties, "spdx:package-verification-code", pkg.PackageVerificationCode.Value)
	}

	// Attribution texts
	for i, attribution := range pkg.PackageAttributionTexts {
		addProperty(&properties, fmt.Sprintf("spdx:attribution-text-%d", i+1), attribution)
	}

	return properties
}

// Helper functions to reduce code duplication
func addProperty(properties *[]cdx.Property, name, value string) {
	*properties = append(*properties, cdx.Property{Name: name, Value: value})
}

func addPropertyIfNotEmpty(properties *[]cdx.Property, name, value string) {
	if value != "" && value != "NOASSERTION" {
		addProperty(properties, name, value)
	}
}

// Helper functions for supplier and originator names
func extractSupplierName(supplier *common.Supplier) string {
	if supplier == nil {
		return ""
	}
	return supplier.Supplier
}

func extractOriginatorName(originator *common.Originator) string {
	if originator == nil {
		return ""
	}
	return originator.Originator
}

// Extract author information from supplier or originator
func extractAuthor(pkg *v2_3.Package) string {
	// Try originator first (original author)
	if pkg.PackageOriginator != nil {
		author := parsePersonOrOrganization(pkg.PackageOriginator.Originator)
		if author != "" {
			return author
		}
	}

	// Fallback to supplier - handle SPDX library structure
	if pkg.PackageSupplier != nil && pkg.PackageSupplier.Supplier != "" && pkg.PackageSupplier.Supplier != "NOASSERTION" {
		// The SPDX library separates type and name, so we need to extract just the name
		return extractNameFromSupplierString(pkg.PackageSupplier.Supplier)
	}

	return ""
}

// Parse "Person: Name (email)" or "Organization: Name" format
func parsePersonOrOrganization(input string) string {
	if input == "" || input == "NOASSERTION" {
		return ""
	}

	// Handle "Person: Name (email)" format
	if strings.HasPrefix(input, "Person: ") {
		name := strings.TrimPrefix(input, "Person: ")
		// Remove email part if present
		if idx := strings.Index(name, " ("); idx != -1 {
			name = name[:idx]
		}
		return strings.TrimSpace(name)
	}

	// Handle "Organization: Name" format
	if strings.HasPrefix(input, "Organization: ") {
		name := strings.TrimPrefix(input, "Organization: ")
		// Remove email part if present
		if idx := strings.Index(name, " ("); idx != -1 {
			name = name[:idx]
		}
		return strings.TrimSpace(name)
	}

	return ""
}

// Extract name from supplier string (handles SPDX library format)
func extractNameFromSupplierString(supplier string) string {
	if supplier == "" || supplier == "NOASSERTION" {
		return ""
	}

	// Remove email part if present
	name := supplier
	if idx := strings.Index(name, " <"); idx != -1 {
		name = name[:idx]
	}

	return strings.TrimSpace(name)
}

// Extract external references from SPDX package
func extractExternalReferences(pkg *v2_3.Package) []cdx.ExternalReference {
	var extRefs []cdx.ExternalReference

	// Add download location if available
	if pkg.PackageDownloadLocation != "" &&
		pkg.PackageDownloadLocation != "NOASSERTION" &&
		pkg.PackageDownloadLocation != "NONE" {
		extRefs = append(extRefs, cdx.ExternalReference{
			Type: cdx.ERTypeDistribution,
			URL:  pkg.PackageDownloadLocation,
		})
	}

	// Add homepage if available
	if pkg.PackageHomePage != "" &&
		pkg.PackageHomePage != "NOASSERTION" &&
		pkg.PackageHomePage != "NONE" {
		extRefs = append(extRefs, cdx.ExternalReference{
			Type: cdx.ERTypeWebsite,
			URL:  pkg.PackageHomePage,
		})
	}

	return extRefs
}

// Extract group from PURL namespace for scoped packages
func extractGroupFromPURL(purlString string) string {
	if purlString == "" {
		return ""
	}

	// Parse the PURL to extract namespace
	if strings.HasPrefix(purlString, "pkg:") {
		// Find the namespace part in PURL format: pkg:type/namespace/name@version
		parts := strings.Split(purlString, "/")
		if len(parts) >= 3 {
			// For npm scoped packages like pkg:npm/@types/node@18.0.0
			// The namespace is the second part after pkg:npm/
			namespace := parts[1]
			// Remove @ prefix if present (for npm scoped packages)
			if strings.HasPrefix(namespace, "%40") {
				// URL decoded @ symbol
				return strings.TrimPrefix(namespace, "%40")
			} else if strings.HasPrefix(namespace, "@") {
				return strings.TrimPrefix(namespace, "@")
			}
			return namespace
		}
	}

	return ""
}

// UUID generation
func generateUUID() string {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		return fmt.Sprintf("spdx-%d", len(b))
	}
	b[6] = (b[6] & 0x0f) | 0x40 // Version 4
	b[8] = (b[8] & 0x3f) | 0x80 // Variant bits
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// BOM validation
func validateBOM(filename string) error {
	// Simple validation - check if file exists and is valid JSON
	data, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("failed to read BOM file: %w", err)
	}

	var bom cdx.BOM
	if err := json.Unmarshal(data, &bom); err != nil {
		return fmt.Errorf("invalid CycloneDX JSON: %w", err)
	}

	return nil
}

// Utility functions for dependencies
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func removeDuplicates(slice []string) []string {
	keys := make(map[string]bool)
	var result []string

	for _, item := range slice {
		if !keys[item] {
			keys[item] = true
			result = append(result, item)
		}
	}

	return result
}
