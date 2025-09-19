package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	cdx "github.com/CycloneDX/cyclonedx-go"
	"github.com/package-url/packageurl-go"
	"github.com/spdx/tools-golang/spdx/v2/v2_3"
	"github.com/spf13/cobra"
)

// Package type constants
const (
	PackageTypeNPM     = "npm"
	PackageTypeMaven   = "maven"
	PackageTypeNuGet   = "nuget"
	PackageTypePyPI    = "pypi"
	PackageTypeGem     = "gem"
	PackageTypeCargo   = "cargo"
	PackageTypeAPK     = "apk"
	PackageTypeGeneric = "generic"
)

// SPDX constants
const (
	SPDXRootPackageID  = "SPDXRef-RootPackage"
	SPDXRootPackageAlt = "RootPackage"
	SPDXFilePrefix     = "File--"
	SPDXNoAssertion    = "NOASSERTION"
	SPDXNone           = "NONE"
	PackageManagerRef  = "PACKAGE-MANAGER"
	PURLRefType        = "purl"
)

// CycloneDX constants
const (
	CycloneDXSchema   = "http://cyclonedx.org/schema/bom-1.6.schema.json"
	CycloneDXFormat   = "CycloneDX"
	DefaultBOMVersion = 1
	DefaultFileMode   = 0644
	DefaultDirMode    = 0755
	JSONIndent        = "  "
)

// Alpine package version pattern
const AlpineVersionPattern = "-r"

// Direct dependency relationship types for O(1) lookup
var directDependencyTypes = map[string]bool{
	"DEPENDS_ON":             true,
	"CONTAINS":               true,
	"BUILD_DEPENDENCY_OF":    true,
	"RUNTIME_DEPENDENCY_OF":  true,
	"DEV_DEPENDENCY_OF":      true,
	"OPTIONAL_DEPENDENCY_OF": true,
	"TEST_DEPENDENCY_OF":     true,
	"STATIC_LINK":            true,
	"DYNAMIC_LINK":           true,
}

// Package type mapping for external references
var externalRefTypeMapping = map[string]string{
	"npm":           PackageTypeNPM,
	"maven-central": PackageTypeMaven,
	"maven":         PackageTypeMaven,
	"nuget":         PackageTypeNuGet,
	"pypi":          PackageTypePyPI,
	"gem":           PackageTypeGem,
	"cargo":         PackageTypeCargo,
}

// Regex for optimized string cleaning
var cleanStringRegex = regexp.MustCompile(`[<>:"|\\?*\s\t\n\r]+`)

var (
	spdxInputFile  string
	spdxOutputFile string
	validateOutput bool
)

// convertSpdxCmd represents the convert-spdx command
var convertSpdxCmd = &cobra.Command{
	Use:   "convert-spdx",
	Short: "Convert SPDX JSON to CycloneDX JSON with normalized bom-refs",
	Long:  `Enterprise-grade SPDX to CycloneDX conversion with human-readable bom-refs, clean dependency relationships, and complete metadata preservation.`,
	Run: func(cmd *cobra.Command, args []string) {
		err := convertSpdxToCycloneDx(spdxInputFile, spdxOutputFile, validateOutput)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	bomUtils.AddCommand(convertSpdxCmd)
	convertSpdxCmd.Flags().StringVar(&spdxInputFile, "infile", "", "Input SPDX JSON file")
	convertSpdxCmd.Flags().StringVar(&spdxOutputFile, "outfile", "", "Output CycloneDX JSON file")
	convertSpdxCmd.Flags().BoolVar(&validateOutput, "validate", false, "Validate the generated CycloneDX BOM")
	convertSpdxCmd.MarkFlagRequired("infile")
	convertSpdxCmd.MarkFlagRequired("outfile")
}

func convertSpdxToCycloneDx(inputFile, outputFile string, validate bool) error {
	// Enhanced input validation
	if inputFile == "" {
		return fmt.Errorf("input file path cannot be empty")
	}
	if outputFile == "" {
		return fmt.Errorf("output file path cannot be empty")
	}

	// Validate input file exists and is readable
	if info, err := os.Stat(inputFile); os.IsNotExist(err) {
		return fmt.Errorf("input file does not exist: %s", inputFile)
	} else if err != nil {
		return fmt.Errorf("cannot access input file: %v", err)
	} else if info.IsDir() {
		return fmt.Errorf("input path is a directory, not a file: %s", inputFile)
	}

	// Validate output file path and create directory if needed
	if outputDir := filepath.Dir(outputFile); outputDir != "." {
		if err := os.MkdirAll(outputDir, DefaultDirMode); err != nil {
			return fmt.Errorf("cannot create output directory: %v", err)
		}
	}

	// Read and parse SPDX document
	spdxDocument, err := readSpdxDocument(inputFile)
	if err != nil {
		return fmt.Errorf("failed to read SPDX document: %w", err)
	}

	// Validate SPDX document structure
	if spdxDocument == nil {
		return fmt.Errorf("SPDX document is nil after parsing")
	}
	if len(spdxDocument.Packages) == 0 {
		return fmt.Errorf("SPDX document contains no packages")
	}

	// Convert to CycloneDX
	cycloneDxBom := convertSpdxDocumentToCycloneDx(spdxDocument)

	// Write CycloneDX BOM
	if err := writeCycloneDxBom(cycloneDxBom, outputFile); err != nil {
		return fmt.Errorf("failed to write CycloneDX BOM: %w", err)
	}

	// Validate if requested
	if validate {
		if err := validateBOM(outputFile); err != nil {
			return fmt.Errorf("validation failed: %w", err)
		}
		fmt.Printf("✓ CycloneDX BOM validation passed\n")
	}

	return nil
}

func readSpdxDocument(filePath string) (*v2_3.Document, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	if len(data) == 0 {
		return nil, fmt.Errorf("file is empty")
	}

	var document v2_3.Document
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("failed to parse SPDX JSON: %w", err)
	}

	// Basic validation of required SPDX fields
	if document.SPDXVersion == "" {
		return nil, fmt.Errorf("invalid SPDX document: missing SPDXVersion")
	}
	if document.DataLicense == "" {
		return nil, fmt.Errorf("invalid SPDX document: missing DataLicense")
	}

	return &document, nil
}

func writeCycloneDxBom(bom *cdx.BOM, filePath string) error {
	if bom == nil {
		return fmt.Errorf("BOM cannot be nil")
	}

	// Use CycloneDX encoder for consistent output formatting
	buf := new(bytes.Buffer)
	if err := cdx.NewBOMEncoder(buf, cdx.BOMFileFormatJSON).Encode(bom); err != nil {
		return fmt.Errorf("failed to encode BOM: %w", err)
	}

	if filePath == "" || filePath == "-" {
		_, err := os.Stdout.Write(buf.Bytes())
		if err != nil {
			return fmt.Errorf("failed to write to stdout: %w", err)
		}
	} else {
		if err := os.WriteFile(filePath, buf.Bytes(), DefaultFileMode); err != nil {
			return fmt.Errorf("failed to write output file: %w", err)
		}
	}

	return nil
}

func convertSpdxDocumentToCycloneDx(spdxDocument *v2_3.Document) *cdx.BOM {
	bom := createBaseBom(spdxDocument)

	// Find root package and create bom-ref mapping
	rootPackage := findRootPackage(spdxDocument.Packages)
	bomRefMapping := createBomRefMapping(spdxDocument.Packages)

	// Set metadata with root component
	if rootPackage != nil {
		bom.Metadata.Component = createComponent(rootPackage, bomRefMapping)
	}

	// Convert packages to components (excluding root)
	components := convertPackagesToComponents(spdxDocument.Packages, rootPackage, bomRefMapping)
	bom.Components = &components

	// Convert relationships to dependencies
	dependencies := convertRelationshipsToDependencies(spdxDocument.Relationships, bomRefMapping)
	if len(dependencies) > 0 {
		bom.Dependencies = &dependencies
	}

	return bom
}

func createBaseBom(spdxDocument *v2_3.Document) *cdx.BOM {
	bom := &cdx.BOM{
		JSONSchema:   CycloneDXSchema,
		BOMFormat:    CycloneDXFormat,
		SpecVersion:  cdx.SpecVersion1_6,
		SerialNumber: "urn:uuid:" + generateUUID(),
		Version:      DefaultBOMVersion,
	}

	// Set metadata from SPDX creation info
	if spdxDocument.CreationInfo != nil {
		bom.Metadata = &cdx.Metadata{
			Timestamp: spdxDocument.CreationInfo.Created,
			Tools:     extractToolsChoice(spdxDocument.CreationInfo.Creators),
			Authors:   extractAuthors(spdxDocument.CreationInfo.Creators),
		}
	}

	return bom
}

// Helper function to convert SPDX identifier to string
func getSPDXID(pkg *v2_3.Package) string {
	return string(pkg.PackageSPDXIdentifier)
}

func findRootPackage(packages []*v2_3.Package) *v2_3.Package {
	for _, pkg := range packages {
		spdxId := getSPDXID(pkg)
		if spdxId == SPDXRootPackageID || spdxId == SPDXRootPackageAlt {
			return pkg
		}
	}
	return nil
}

func createBomRefMapping(packages []*v2_3.Package) map[string]string {
	mapping := make(map[string]string)

	for _, pkg := range packages {
		spdxId := getSPDXID(pkg)
		bomRef := generateBomReference(pkg)
		mapping[spdxId] = bomRef
	}

	return mapping
}

func generateBomReference(pkg *v2_3.Package) string {
	// Try to get PURL and normalize it
	if purl := extractPackageUrl(pkg); purl != "" {
		return normalizePurlToBomRef(purl)
	}

	// Fallback to name@version format
	if pkg.PackageVersion != "" {
		return cleanString(fmt.Sprintf("%s@%s", pkg.PackageName, pkg.PackageVersion))
	}

	return cleanString(pkg.PackageName)
}

func extractPackageUrl(pkg *v2_3.Package) string {
	// First check external references
	for _, ref := range pkg.PackageExternalReferences {
		if ref.Category == PackageManagerRef && ref.RefType == PURLRefType {
			return ref.Locator
		}
	}

	// Generate PURL if not found
	return generatePackageUrl(pkg)
}

func generatePackageUrl(pkg *v2_3.Package) string {
	if pkg == nil || pkg.PackageName == "" {
		return ""
	}

	packageType := detectPackageType(pkg)
	namespace := extractNamespace(pkg, packageType)

	purl := packageurl.PackageURL{
		Type:      packageType,
		Namespace: namespace,
		Name:      pkg.PackageName,
		Version:   pkg.PackageVersion,
	}

	return purl.ToString()
}

func detectPackageType(pkg *v2_3.Package) string {
	// Check external references for type hints using mapping
	for _, ref := range pkg.PackageExternalReferences {
		if packageType, exists := externalRefTypeMapping[ref.RefType]; exists {
			return packageType
		}
	}

	// Detect Alpine packages by version pattern
	if pkg.PackageVersion != "" && strings.Contains(pkg.PackageVersion, AlpineVersionPattern) {
		if len(strings.Split(pkg.PackageVersion, AlpineVersionPattern)) == 2 {
			return PackageTypeAPK
		}
	}

	return PackageTypeGeneric
}

func extractNamespace(pkg *v2_3.Package, packageType string) string {
	if packageType == PackageTypeNPM && strings.HasPrefix(pkg.PackageName, "@") {
		parts := strings.Split(pkg.PackageName, "/")
		if len(parts) > 1 {
			return strings.TrimPrefix(parts[0], "@")
		}
	}
	return ""
}

func normalizePurlToBomRef(purlString string) string {
	purl, err := packageurl.FromString(purlString)
	if err != nil {
		return cleanString(purlString)
	}

	var parts []string

	if purl.Type != "" {
		parts = append(parts, purl.Type)
	}

	if purl.Namespace != "" {
		parts = append(parts, purl.Namespace)
	}

	nameWithVersion := purl.Name
	if purl.Version != "" {
		nameWithVersion = fmt.Sprintf("%s@%s", purl.Name, purl.Version)
	}
	parts = append(parts, nameWithVersion)

	return cleanString(strings.Join(parts, "/"))
}

func cleanString(input string) string {
	// Use regex for optimized string cleaning
	result := cleanStringRegex.ReplaceAllString(input, "-")

	// Remove consecutive dashes
	for strings.Contains(result, "--") {
		result = strings.ReplaceAll(result, "--", "-")
	}

	return strings.Trim(result, "-")
}

func convertPackagesToComponents(packages []*v2_3.Package, rootPackage *v2_3.Package, bomRefMapping map[string]string) []cdx.Component {
	// Pre-allocate slice with known capacity (-1 for root package)
	capacity := len(packages)
	if rootPackage != nil {
		capacity--
	}
	components := make([]cdx.Component, 0, capacity)

	for _, pkg := range packages {
		// Skip root package (it's in metadata)
		if rootPackage != nil && pkg == rootPackage {
			continue
		}

		component := createComponent(pkg, bomRefMapping)
		components = append(components, *component)
	}

	return components
}

func createComponent(pkg *v2_3.Package, bomRefMapping map[string]string) *cdx.Component {
	spdxId := getSPDXID(pkg)
	bomRef := bomRefMapping[spdxId]

	component := &cdx.Component{
		Type:    determineComponentType(pkg.PrimaryPackagePurpose),
		BOMRef:  bomRef,
		Name:    pkg.PackageName,
		Version: pkg.PackageVersion,
	}

	// Add optional fields
	if licenses := extractLicenses(pkg); len(licenses) > 0 {
		licenseChoices := cdx.Licenses(licenses)
		component.Licenses = &licenseChoices
	}

	if supplier := extractSupplierInfo(pkg); supplier != nil {
		component.Supplier = supplier
	}

	if purl := extractPackageUrl(pkg); purl != "" {
		component.PackageURL = purl
		// Extract group from PURL namespace for scoped packages
		if group := extractGroupFromPURL(purl); group != "" {
			component.Group = group
		}
	}

	if author := extractAuthor(pkg); author != "" {
		component.Author = author
	}

	if extRefs := extractExternalReferences(pkg); len(extRefs) > 0 {
		component.ExternalReferences = &extRefs
	}

	if properties := buildProperties(pkg); len(properties) > 0 {
		component.Properties = &properties
	}

	if hashes := convertChecksums(pkg.PackageChecksums); len(hashes) > 0 {
		component.Hashes = &hashes
	}

	return component
}

// Helper function to validate license strings
func isValidLicense(license string) bool {
	return license != "" && license != SPDXNoAssertion && license != SPDXNone
}

func extractLicenses(pkg *v2_3.Package) []cdx.LicenseChoice {
	// Prioritize declared license over concluded (declared is usually more accurate)
	if isValidLicense(pkg.PackageLicenseDeclared) {
		return parseLicenseExpression(pkg.PackageLicenseDeclared)
	}
	if isValidLicense(pkg.PackageLicenseConcluded) {
		return parseLicenseExpression(pkg.PackageLicenseConcluded)
	}

	return nil
}

func extractSupplierInfo(pkg *v2_3.Package) *cdx.OrganizationalEntity {
	if pkg.PackageSupplier != nil {
		return &cdx.OrganizationalEntity{
			Name: extractSupplierName(pkg.PackageSupplier),
		}
	}

	if pkg.PackageOriginator != nil {
		return &cdx.OrganizationalEntity{
			Name: extractOriginatorName(pkg.PackageOriginator),
		}
	}

	return nil
}

func convertRelationshipsToDependencies(relationships []*v2_3.Relationship, bomRefMapping map[string]string) []cdx.Dependency {
	dependencyMap := make(map[string][]string)

	for _, relationship := range relationships {
		if !isDirectDependencyRelationship(relationship.Relationship) {
			continue
		}

		fromSpdxId, toSpdxId := extractRelationshipIds(relationship)
		if fromSpdxId == "" || toSpdxId == "" {
			continue
		}

		// Filter out file references - only include package-to-package dependencies
		if isFileReference(fromSpdxId) || isFileReference(toSpdxId) {
			continue
		}

		// Convert SPDX IDs to bom-refs with fallback
		fromBomRef := getBomRef(fromSpdxId, bomRefMapping)
		toBomRef := getBomRef(toSpdxId, bomRefMapping)

		dependencyMap[fromBomRef] = append(dependencyMap[fromBomRef], toBomRef)
	}

	return buildDirectDependencyList(dependencyMap)
}

// Helper function to get BOM reference with fallback
func getBomRef(spdxId string, bomRefMapping map[string]string) string {
	if bomRef := bomRefMapping[spdxId]; bomRef != "" {
		return bomRef
	}
	return spdxId
}

func isDirectDependencyRelationship(relationshipType string) bool {
	// Use O(1) map lookup instead of O(n) slice iteration
	return directDependencyTypes[relationshipType]
}

func extractRelationshipIds(relationship *v2_3.Relationship) (string, string) {
	return string(relationship.RefA.ElementRefID), string(relationship.RefB.ElementRefID)
}

// isFileReference checks if an SPDX ID represents a file rather than a package
func isFileReference(spdxId string) bool {
	return strings.HasPrefix(spdxId, SPDXFilePrefix) || strings.Contains(spdxId, SPDXFilePrefix)
}

func buildDirectDependencyList(dependencyMap map[string][]string) []cdx.Dependency {
	var dependencies []cdx.Dependency

	for sourceRef, targetRefs := range dependencyMap {
		uniqueTargets := removeDuplicateStrings(targetRefs)
		if len(uniqueTargets) > 0 {
			dependency := cdx.Dependency{
				Ref:          sourceRef,
				Dependencies: &uniqueTargets,
			}
			dependencies = append(dependencies, dependency)
		}
	}

	return dependencies
}

func removeDuplicateStrings(slice []string) []string {
	if len(slice) == 0 {
		return slice
	}

	seen := make(map[string]struct{}, len(slice))
	result := make([]string, 0, len(slice))

	for _, item := range slice {
		if _, exists := seen[item]; !exists {
			seen[item] = struct{}{}
			result = append(result, item)
		}
	}

	return result
}
