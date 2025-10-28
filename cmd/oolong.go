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
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/goccy/go-yaml"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// oolongCmd represents the oolong command
var oolongCmd = &cobra.Command{
	Use:   "oolong",
	Short: "Oolong TEA server content management commands",
	Long:  `Commands for managing Oolong TEA Server content including products, components, releases, and artifacts.`,
}

var contentDir string
var productName string
var productUuid string
var componentNameFlag string
var componentUuid string

// Component release flags
var releaseComponent string
var componentReleaseVersion string
var componentReleaseUuid string
var releaseCreatedDate string
var releaseReleaseDate string
var releasePrerelease bool
var releaseTeis []string
var releasePurls []string

// Artifact flags
var artifactUuid string
var artifactName string
var artifactType string
var artifactMediaType string
var artifactUrl string
var artifactSignatureUrl string
var artifactDescription string
var artifactHashes []string

// Product release flags
var productReleaseProduct string
var productReleaseVersion string
var productReleaseUuid string
var productReleaseCreatedDate string
var productReleaseReleaseDate string
var productReleasePrerelease bool
var productReleaseTeis []string
var productReleasePurls []string
var productReleaseComponents []string
var productReleaseComponentReleases []string

// Add artifact to releases flags
var addArtifactToReleasesArtifactUuid string
var addArtifactToReleasesComponents []string
var addArtifactToReleasesComponentReleases []string
var addArtifactToReleasesProducts []string
var addArtifactToReleasesProductReleases []string

// Product represents the structure of product.yaml
type Product struct {
	UUID        string   `yaml:"uuid"`
	Name        string   `yaml:"name"`
	Identifiers []string `yaml:"identifiers"`
}

// Component represents the structure of component.yaml
type Component struct {
	UUID        string   `yaml:"uuid"`
	Name        string   `yaml:"name"`
	Identifiers []string `yaml:"identifiers"`
}

// OolongIdentifier represents an identifier in release.yaml
type OolongIdentifier struct {
	IdType  string `yaml:"idType"`
	IdValue string `yaml:"idValue"`
}

// ComponentRelease represents the structure of component release.yaml
type ComponentRelease struct {
	UUID          string             `yaml:"uuid"`
	Version       string             `yaml:"version"`
	CreatedDate   string             `yaml:"createdDate"`
	ReleaseDate   string             `yaml:"releaseDate"`
	PreRelease    bool               `yaml:"preRelease"`
	Identifiers   []OolongIdentifier `yaml:"identifiers"`
	Distributions []string           `yaml:"distributions"`
}

// ProductReleaseComponent represents a component reference in product release.yaml
type ProductReleaseComponent struct {
	UUID    string `yaml:"uuid"`
	Release string `yaml:"release"`
}

// ProductRelease represents the structure of product release.yaml
type ProductRelease struct {
	UUID        string                     `yaml:"uuid"`
	Version     string                     `yaml:"version"`
	CreatedDate string                     `yaml:"createdDate"`
	ReleaseDate string                     `yaml:"releaseDate"`
	PreRelease  bool                       `yaml:"preRelease"`
	Identifiers []OolongIdentifier         `yaml:"identifiers"`
	Components  []ProductReleaseComponent  `yaml:"components"`
}

// UpdateReason represents the update reason in collection.yaml
type UpdateReason struct {
	Type    string `yaml:"type"`
	Comment string `yaml:"comment"`
}

// Collection represents the structure of collection.yaml
type Collection struct {
	Version      int          `yaml:"version"`
	Date         string       `yaml:"date"`
	UpdateReason UpdateReason `yaml:"updateReason"`
	Artifacts    []string     `yaml:"artifacts"`
}

// Checksum represents a checksum in artifact format
type Checksum struct {
	AlgType  string `yaml:"algType"`
	AlgValue string `yaml:"algValue"`
}

// ArtifactFormat represents a format entry in artifact.yaml
type ArtifactFormat struct {
	MimeType     string     `yaml:"mimeType"`
	Description  string     `yaml:"description"`
	Url          string     `yaml:"url"`
	SignatureUrl string     `yaml:"signatureUrl"`
	Checksums    []Checksum `yaml:"checksums"`
}

// OolongArtifact represents the structure of artifact.yaml
type OolongArtifact struct {
	Name              string           `yaml:"name"`
	Type              string           `yaml:"type"`
	Version           int              `yaml:"version"`
	DistributionTypes []string         `yaml:"distributionTypes"`
	Formats           []ArtifactFormat `yaml:"formats"`
}

// add_productCmd represents the add_product command
var add_productCmd = &cobra.Command{
	Use:   "add_product",
	Short: "Add or update a product in the content directory",
	Long: `Creates or updates a product.yaml file in the content directory.
The product directory name will be the lowercase snake_case version of the product name.`,
	Run: func(cmd *cobra.Command, args []string) {
		if productName == "" {
			fmt.Fprintf(os.Stderr, "Error: product name is required\n")
			os.Exit(1)
		}

		// Generate UUID if not provided
		var prodUuid string
		if productUuid != "" {
			prodUuid = productUuid
		} else {
			prodUuid = uuid.New().String()
		}

		// Convert name to lowercase snake_case
		dirName := toSnakeCase(productName)

		// Create directory path
		productDir := filepath.Join(contentDir, "products", dirName)

		// Check if directory already exists
		if _, err := os.Stat(productDir); err == nil {
			fmt.Fprintf(os.Stderr, "Error: product with name '%s' already exists at %s\n", productName, productDir)
			os.Exit(1)
		}

		if err := os.MkdirAll(productDir, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to create directory %s: %v\n", productDir, err)
			os.Exit(1)
		}

		// Create product structure
		product := Product{
			UUID:        prodUuid,
			Name:        productName,
			Identifiers: []string{},
		}

		// Write YAML file
		yamlPath := filepath.Join(productDir, "product.yaml")
		if err := writeYAML(yamlPath, product); err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to write product.yaml: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Successfully created/updated product: %s\n", productName)
		fmt.Printf("  Directory: %s\n", productDir)
		fmt.Printf("  UUID: %s\n", prodUuid)
	},
}

// add_componentCmd represents the add_component command
var add_componentCmd = &cobra.Command{
	Use:   "add_component",
	Short: "Add or update a component in the content directory",
	Long: `Creates or updates a component.yaml file in the content directory.
The component directory name will be the lowercase snake_case version of the component name.`,
	Run: func(cmd *cobra.Command, args []string) {
		if componentNameFlag == "" {
			fmt.Fprintf(os.Stderr, "Error: component name is required\n")
			os.Exit(1)
		}

		// Generate UUID if not provided
		var compUuid string
		if componentUuid != "" {
			compUuid = componentUuid
		} else {
			compUuid = uuid.New().String()
		}

		// Convert name to lowercase snake_case
		dirName := toSnakeCase(componentNameFlag)

		// Create directory path
		componentDir := filepath.Join(contentDir, "components", dirName)

		// Check if directory already exists
		if _, err := os.Stat(componentDir); err == nil {
			fmt.Fprintf(os.Stderr, "Error: component with name '%s' already exists at %s\n", componentNameFlag, componentDir)
			os.Exit(1)
		}

		if err := os.MkdirAll(componentDir, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to create directory %s: %v\n", componentDir, err)
			os.Exit(1)
		}

		// Create component structure
		component := Component{
			UUID:        compUuid,
			Name:        componentNameFlag,
			Identifiers: []string{},
		}

		// Write YAML file
		yamlPath := filepath.Join(componentDir, "component.yaml")
		if err := writeYAML(yamlPath, component); err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to write component.yaml: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Successfully created/updated component: %s\n", componentNameFlag)
		fmt.Printf("  Directory: %s\n", componentDir)
		fmt.Printf("  UUID: %s\n", compUuid)
	},
}

// add_component_releaseCmd represents the add_component_release command
var add_component_releaseCmd = &cobra.Command{
	Use:   "add_component_release",
	Short: "Add a component release to the content directory",
	Long: `Creates a component release with release.yaml and an initial collection.
The component can be specified by name or UUID.`,
	Run: func(cmd *cobra.Command, args []string) {
		if releaseComponent == "" {
			fmt.Fprintf(os.Stderr, "Error: component is required\n")
			os.Exit(1)
		}
		if componentReleaseVersion == "" {
			fmt.Fprintf(os.Stderr, "Error: version is required\n")
			os.Exit(1)
		}

		// Find component by name or UUID
		componentDir, componentData, err := findComponent(contentDir, releaseComponent)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		// Create release directory
		releaseDir := filepath.Join(componentDir, "releases", componentReleaseVersion)

		// Check if release already exists
		if _, err := os.Stat(releaseDir); err == nil {
			fmt.Fprintf(os.Stderr, "Error: release version '%s' already exists for component '%s'\n", componentReleaseVersion, componentData.Name)
			os.Exit(1)
		}

		if err := os.MkdirAll(releaseDir, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to create release directory %s: %v\n", releaseDir, err)
			os.Exit(1)
		}

		// Generate UUID if not provided
		var relUuid string
		if componentReleaseUuid != "" {
			relUuid = componentReleaseUuid
		} else {
			relUuid = uuid.New().String()
		}

		// Set timestamps if not provided
		currentTime := time.Now().UTC().Format("2006-01-02T15:04:05Z")
		createdDate := releaseCreatedDate
		if createdDate == "" {
			createdDate = currentTime
		}
		relDate := releaseReleaseDate
		if relDate == "" {
			relDate = currentTime
		}

		// Build identifiers list
		identifiers := []OolongIdentifier{}
		for _, tei := range releaseTeis {
			identifiers = append(identifiers, OolongIdentifier{
				IdType:  "TEI",
				IdValue: tei,
			})
		}
		for _, purl := range releasePurls {
			identifiers = append(identifiers, OolongIdentifier{
				IdType:  "PURL",
				IdValue: purl,
			})
		}

		// Create component release structure
		release := ComponentRelease{
			UUID:          relUuid,
			Version:       componentReleaseVersion,
			CreatedDate:   createdDate,
			ReleaseDate:   relDate,
			PreRelease:    releasePrerelease,
			Identifiers:   identifiers,
			Distributions: []string{},
		}

		// Write release.yaml
		releaseYamlPath := filepath.Join(releaseDir, "release.yaml")
		if err := writeYAML(releaseYamlPath, release); err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to write release.yaml: %v\n", err)
			os.Exit(1)
		}

		// Create collections directory
		collectionsDir := filepath.Join(releaseDir, "collections")
		if err := os.MkdirAll(collectionsDir, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to create collections directory: %v\n", err)
			os.Exit(1)
		}

		// Create initial collection
		collection := Collection{
			Version: 1,
			Date:    currentTime,
			UpdateReason: UpdateReason{
				Type:    "INITIAL_RELEASE",
				Comment: "",
			},
			Artifacts: []string{},
		}

		collectionYamlPath := filepath.Join(collectionsDir, "1.yaml")
		if err := writeYAML(collectionYamlPath, collection); err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to write collection: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Successfully created component release: %s\n", componentReleaseVersion)
		fmt.Printf("  Component: %s\n", componentData.Name)
		fmt.Printf("  Directory: %s\n", releaseDir)
		fmt.Printf("  UUID: %s\n", relUuid)
		fmt.Printf("  Created initial collection: collections/1.yaml\n")
	},
}

// add_artifactCmd represents the add_artifact command
var add_artifactCmd = &cobra.Command{
	Use:   "add_artifact",
	Short: "Add an artifact to the content directory",
	Long: `Creates an artifact YAML file in the artifacts directory.
The artifact file is named with its UUID.`,
	Run: func(cmd *cobra.Command, args []string) {
		if artifactName == "" {
			fmt.Fprintf(os.Stderr, "Error: artifact name is required\n")
			os.Exit(1)
		}
		if artifactType == "" {
			fmt.Fprintf(os.Stderr, "Error: artifact type is required\n")
			os.Exit(1)
		}
		if artifactMediaType == "" {
			fmt.Fprintf(os.Stderr, "Error: media type is required\n")
			os.Exit(1)
		}
		if artifactUrl == "" {
			fmt.Fprintf(os.Stderr, "Error: url is required\n")
			os.Exit(1)
		}

		// Validate artifact type
		validTypes := map[string]bool{
			"ATTESTATION":   true,
			"BOM":           true,
			"BUILD_META":    true,
			"CERTIFICATION": true,
			"FORMULATION":   true,
			"LICENSE":       true,
			"RELEASE_NOTES": true,
			"SECURITY_TXT":  true,
			"THREAT_MODEL":  true,
			"VULNERABILITIES": true,
			"OTHER":         true,
		}
		if !validTypes[artifactType] {
			fmt.Fprintf(os.Stderr, "Error: invalid artifact type '%s'. Must be one of: ATTESTATION, BOM, BUILD_META, CERTIFICATION, FORMULATION, LICENSE, RELEASE_NOTES, SECURITY_TXT, THREAT_MODEL, VULNERABILITIES, OTHER\n", artifactType)
			os.Exit(1)
		}

		// Generate UUID if not provided
		var artUuid string
		if artifactUuid != "" {
			artUuid = artifactUuid
		} else {
			artUuid = uuid.New().String()
		}

		// Create artifacts directory if it doesn't exist
		artifactsDir := filepath.Join(contentDir, "artifacts")
		if err := os.MkdirAll(artifactsDir, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to create artifacts directory: %v\n", err)
			os.Exit(1)
		}

		// Check if artifact file already exists
		artifactPath := filepath.Join(artifactsDir, artUuid+".yaml")
		if _, err := os.Stat(artifactPath); err == nil {
			fmt.Fprintf(os.Stderr, "Error: artifact with UUID '%s' already exists at %s\n", artUuid, artifactPath)
			os.Exit(1)
		}

		// Parse hashes
		checksums := []Checksum{}
		for _, hash := range artifactHashes {
			parts := strings.SplitN(hash, "=", 2)
			if len(parts) != 2 {
				fmt.Fprintf(os.Stderr, "Error: invalid hash format '%s'. Expected format: algorithm=value (e.g., sha256=abcd)\n", hash)
				os.Exit(1)
			}
			// Normalize algorithm name
			algType := strings.ToUpper(parts[0])
			switch algType {
			case "MD5":
				algType = "MD5"
			case "SHA1", "SHA-1":
				algType = "SHA-1"
			case "SHA256", "SHA-256":
				algType = "SHA-256"
			case "SHA384", "SHA-384":
				algType = "SHA-384"
			case "SHA512", "SHA-512":
				algType = "SHA-512"
			case "SHA3-256", "SHA3256":
				algType = "SHA3-256"
			case "SHA3-384", "SHA3384":
				algType = "SHA3-384"
			case "SHA3-512", "SHA3512":
				algType = "SHA3-512"
			case "BLAKE2B-256", "BLAKE2B256":
				algType = "BLAKE2b-256"
			case "BLAKE2B-384", "BLAKE2B384":
				algType = "BLAKE2b-384"
			case "BLAKE2B-512", "BLAKE2B512":
				algType = "BLAKE2b-512"
			case "BLAKE3":
				algType = "BLAKE3"
			default:
				fmt.Fprintf(os.Stderr, "Error: invalid hash algorithm '%s'. Must be one of: MD5, SHA-1, SHA-256, SHA-384, SHA-512, SHA3-256, SHA3-384, SHA3-512, BLAKE2b-256, BLAKE2b-384, BLAKE2b-512, BLAKE3\n", parts[0])
				os.Exit(1)
			}
			checksums = append(checksums, Checksum{
				AlgType:  algType,
				AlgValue: parts[1],
			})
		}

		// Create artifact format
		format := ArtifactFormat{
			MimeType:     artifactMediaType,
			Description:  artifactDescription,
			Url:          artifactUrl,
			SignatureUrl: artifactSignatureUrl,
			Checksums:    checksums,
		}

		// Create artifact structure
		artifact := OolongArtifact{
			Name:              artifactName,
			Type:              artifactType,
			Version:           1,
			DistributionTypes: []string{},
			Formats:           []ArtifactFormat{format},
		}

		// Write YAML file
		if err := writeYAML(artifactPath, artifact); err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to write artifact.yaml: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Successfully created artifact: %s\n", artifactName)
		fmt.Printf("  Type: %s\n", artifactType)
		fmt.Printf("  File: %s\n", artifactPath)
		fmt.Printf("  UUID: %s\n", artUuid)
	},
}

// add_product_releaseCmd represents the add_product_release command
var add_product_releaseCmd = &cobra.Command{
	Use:   "add_product_release",
	Short: "Add a product release to the content directory",
	Long: `Creates a product release with release.yaml and an initial collection.
The product can be specified by name or UUID.
Optionally, components can be linked to the release by providing paired component and component_release flags.`,
	Run: func(cmd *cobra.Command, args []string) {
		if productReleaseProduct == "" {
			fmt.Fprintf(os.Stderr, "Error: product is required\n")
			os.Exit(1)
		}
		if productReleaseVersion == "" {
			fmt.Fprintf(os.Stderr, "Error: version is required\n")
			os.Exit(1)
		}

		// Validate component and component_release flags match
		if len(productReleaseComponents) != len(productReleaseComponentReleases) {
			fmt.Fprintf(os.Stderr, "Error: number of --component flags (%d) must match number of --component_release flags (%d)\n", len(productReleaseComponents), len(productReleaseComponentReleases))
			os.Exit(1)
		}

		// Find product by name or UUID
		productDir, productData, err := findProduct(contentDir, productReleaseProduct)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		// Create release directory
		releaseDir := filepath.Join(productDir, "releases", productReleaseVersion)

		// Check if release already exists
		if _, err := os.Stat(releaseDir); err == nil {
			fmt.Fprintf(os.Stderr, "Error: release version '%s' already exists for product '%s'\n", productReleaseVersion, productData.Name)
			os.Exit(1)
		}

		if err := os.MkdirAll(releaseDir, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to create release directory %s: %v\n", releaseDir, err)
			os.Exit(1)
		}

		// Generate UUID if not provided
		var relUuid string
		if productReleaseUuid != "" {
			relUuid = productReleaseUuid
		} else {
			relUuid = uuid.New().String()
		}

		// Set timestamps if not provided
		currentTime := time.Now().UTC().Format("2006-01-02T15:04:05Z")
		createdDate := productReleaseCreatedDate
		if createdDate == "" {
			createdDate = currentTime
		}
		relDate := productReleaseReleaseDate
		if relDate == "" {
			relDate = currentTime
		}

		// Build identifiers list
		identifiers := []OolongIdentifier{}
		for _, tei := range productReleaseTeis {
			identifiers = append(identifiers, OolongIdentifier{
				IdType:  "TEI",
				IdValue: tei,
			})
		}
		for _, purl := range productReleasePurls {
			identifiers = append(identifiers, OolongIdentifier{
				IdType:  "PURL",
				IdValue: purl,
			})
		}

		// Process component references
		components := []ProductReleaseComponent{}
		for i := 0; i < len(productReleaseComponents); i++ {
			componentIdentifier := productReleaseComponents[i]
			componentReleaseIdentifier := productReleaseComponentReleases[i]

			// Find component
			componentDir, componentData, err := findComponent(contentDir, componentIdentifier)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error finding component '%s': %v\n", componentIdentifier, err)
				os.Exit(1)
			}

			// Find component release
			componentReleaseUuid, err := findComponentRelease(componentDir, componentReleaseIdentifier)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error finding component release '%s' for component '%s': %v\n", componentReleaseIdentifier, componentData.Name, err)
				os.Exit(1)
			}

			components = append(components, ProductReleaseComponent{
				UUID:    componentData.UUID,
				Release: componentReleaseUuid,
			})
		}

		// Create product release structure
		release := ProductRelease{
			UUID:        relUuid,
			Version:     productReleaseVersion,
			CreatedDate: createdDate,
			ReleaseDate: relDate,
			PreRelease:  productReleasePrerelease,
			Identifiers: identifiers,
			Components:  components,
		}

		// Write release.yaml
		releaseYamlPath := filepath.Join(releaseDir, "release.yaml")
		if err := writeYAML(releaseYamlPath, release); err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to write release.yaml: %v\n", err)
			os.Exit(1)
		}

		// Create collections directory
		collectionsDir := filepath.Join(releaseDir, "collections")
		if err := os.MkdirAll(collectionsDir, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to create collections directory: %v\n", err)
			os.Exit(1)
		}

		// Create initial collection
		collection := Collection{
			Version: 1,
			Date:    currentTime,
			UpdateReason: UpdateReason{
				Type:    "INITIAL_RELEASE",
				Comment: "",
			},
			Artifacts: []string{},
		}

		collectionYamlPath := filepath.Join(collectionsDir, "1.yaml")
		if err := writeYAML(collectionYamlPath, collection); err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to write collection: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Successfully created product release: %s\n", productReleaseVersion)
		fmt.Printf("  Product: %s\n", productData.Name)
		fmt.Printf("  Directory: %s\n", releaseDir)
		fmt.Printf("  UUID: %s\n", relUuid)
		if len(components) > 0 {
			fmt.Printf("  Linked components: %d\n", len(components))
		}
		fmt.Printf("  Created initial collection: collections/1.yaml\n")
	},
}

// add_artifact_to_releasesCmd represents the add_artifact_to_releases command
var add_artifact_to_releasesCmd = &cobra.Command{
	Use:   "add_artifact_to_releases",
	Short: "Add an artifact to component and/or product releases",
	Long: `Links an artifact to one or more component releases and/or product releases.
For each release, creates a new collection version with the artifact added.
If the artifact is already in the latest collection, no changes are made.`,
	Run: func(cmd *cobra.Command, args []string) {
		if addArtifactToReleasesArtifactUuid == "" {
			fmt.Fprintf(os.Stderr, "Error: artifactuuid is required\n")
			os.Exit(1)
		}

		// Validate component and component_release flags match
		if len(addArtifactToReleasesComponents) != len(addArtifactToReleasesComponentReleases) {
			fmt.Fprintf(os.Stderr, "Error: number of --component flags (%d) must match number of --componentrelease flags (%d)\n", len(addArtifactToReleasesComponents), len(addArtifactToReleasesComponentReleases))
			os.Exit(1)
		}

		// Validate product and product_release flags match
		if len(addArtifactToReleasesProducts) != len(addArtifactToReleasesProductReleases) {
			fmt.Fprintf(os.Stderr, "Error: number of --product flags (%d) must match number of --productrelease flags (%d)\n", len(addArtifactToReleasesProducts), len(addArtifactToReleasesProductReleases))
			os.Exit(1)
		}

		// Check that at least one release is specified
		if len(addArtifactToReleasesComponents) == 0 && len(addArtifactToReleasesProducts) == 0 {
			fmt.Fprintf(os.Stderr, "Error: at least one component/componentrelease or product/productrelease pair must be specified\n")
			os.Exit(1)
		}

		type releaseInfo struct {
			type_        string // "component" or "product"
			name         string
			releaseDir   string
			releaseVersion string
		}

		var releases []releaseInfo

		// Validate and collect all component releases
		for i := 0; i < len(addArtifactToReleasesComponents); i++ {
			componentIdentifier := addArtifactToReleasesComponents[i]
			componentReleaseIdentifier := addArtifactToReleasesComponentReleases[i]

			// Find component
			componentDir, componentData, err := findComponent(contentDir, componentIdentifier)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error finding component '%s': %v\n", componentIdentifier, err)
				os.Exit(1)
			}

			// Find component release directory
			releaseDir, releaseVersion, _, err := findComponentReleaseDir(componentDir, componentReleaseIdentifier)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error finding component release '%s' for component '%s': %v\n", componentReleaseIdentifier, componentData.Name, err)
				os.Exit(1)
			}

			releases = append(releases, releaseInfo{
				type_:          "component",
				name:           componentData.Name,
				releaseDir:     releaseDir,
				releaseVersion: releaseVersion,
			})
		}

		// Validate and collect all product releases
		for i := 0; i < len(addArtifactToReleasesProducts); i++ {
			productIdentifier := addArtifactToReleasesProducts[i]
			productReleaseIdentifier := addArtifactToReleasesProductReleases[i]

			// Find product
			productDir, productData, err := findProduct(contentDir, productIdentifier)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error finding product '%s': %v\n", productIdentifier, err)
				os.Exit(1)
			}

			// Find product release directory
			releaseDir, releaseVersion, _, err := findProductReleaseDir(productDir, productReleaseIdentifier)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error finding product release '%s' for product '%s': %v\n", productReleaseIdentifier, productData.Name, err)
				os.Exit(1)
			}

			releases = append(releases, releaseInfo{
				type_:          "product",
				name:           productData.Name,
				releaseDir:     releaseDir,
				releaseVersion: releaseVersion,
			})
		}

		// Process each release
		for _, rel := range releases {
			collectionsDir := filepath.Join(rel.releaseDir, "collections")

			// Find the latest collection version
			latestVersion, err := findLatestCollectionVersion(collectionsDir)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error finding latest collection for %s release '%s': %v\n", rel.type_, rel.releaseVersion, err)
				os.Exit(1)
			}

			// Read the latest collection
			latestCollectionPath := filepath.Join(collectionsDir, fmt.Sprintf("%d.yaml", latestVersion))
			data, err := os.ReadFile(latestCollectionPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error reading collection %s: %v\n", latestCollectionPath, err)
				os.Exit(1)
			}

			var collection Collection
			if err := yaml.Unmarshal(data, &collection); err != nil {
				fmt.Fprintf(os.Stderr, "Error parsing collection %s: %v\n", latestCollectionPath, err)
				os.Exit(1)
			}

			// Check if artifact already exists in collection
			artifactExists := false
			for _, artifactUuid := range collection.Artifacts {
				if artifactUuid == addArtifactToReleasesArtifactUuid {
					artifactExists = true
					break
				}
			}

			if artifactExists {
				fmt.Printf("Artifact %s already added to %s '%s' release '%s'\n", addArtifactToReleasesArtifactUuid, rel.type_, rel.name, rel.releaseVersion)
				continue
			}

			// Create new collection with incremented version
			newVersion := latestVersion + 1
			newCollection := Collection{
				Version: newVersion,
				Date:    time.Now().UTC().Format("2006-01-02T15:04:05Z"),
				UpdateReason: UpdateReason{
					Type:    "ARTIFACT_ADDED",
					Comment: fmt.Sprintf("Added artifact %s", addArtifactToReleasesArtifactUuid),
				},
				Artifacts: append(collection.Artifacts, addArtifactToReleasesArtifactUuid),
			}

			// Write new collection
			newCollectionPath := filepath.Join(collectionsDir, fmt.Sprintf("%d.yaml", newVersion))
			if err := writeYAML(newCollectionPath, newCollection); err != nil {
				fmt.Fprintf(os.Stderr, "Error writing new collection %s: %v\n", newCollectionPath, err)
				os.Exit(1)
			}

			fmt.Printf("Added artifact %s to %s '%s' release '%s' (collection version %d)\n", addArtifactToReleasesArtifactUuid, rel.type_, rel.name, rel.releaseVersion, newVersion)
		}

		fmt.Printf("\nSuccessfully processed %d release(s)\n", len(releases))
	},
}

func init() {
	// Add flags to oolong command
	oolongCmd.PersistentFlags().StringVar(&contentDir, "contentdir", "", "Content directory path")
	oolongCmd.MarkFlagRequired("contentdir")

	// Add flags to add_product command
	add_productCmd.Flags().StringVar(&productName, "name", "", "Product name (required)")
	add_productCmd.Flags().StringVar(&productUuid, "uuid", "", "Product UUID (optional, will be generated if not provided)")
	add_productCmd.MarkFlagRequired("name")

	// Add flags to add_component command
	add_componentCmd.Flags().StringVar(&componentNameFlag, "name", "", "Component name (required)")
	add_componentCmd.Flags().StringVar(&componentUuid, "uuid", "", "Component UUID (optional, will be generated if not provided)")
	add_componentCmd.MarkFlagRequired("name")

	// Add flags to add_component_release command
	add_component_releaseCmd.Flags().StringVar(&releaseComponent, "component", "", "Component name or UUID (required)")
	add_component_releaseCmd.Flags().StringVar(&componentReleaseVersion, "version", "", "Release version (required)")
	add_component_releaseCmd.Flags().StringVar(&componentReleaseUuid, "uuid", "", "Release UUID (optional, will be generated if not provided)")
	add_component_releaseCmd.Flags().StringVar(&releaseCreatedDate, "createddate", "", "Created date in RFC3339 format (optional, defaults to current time)")
	add_component_releaseCmd.Flags().StringVar(&releaseReleaseDate, "releasedate", "", "Release date in RFC3339 format (optional, defaults to current time)")
	add_component_releaseCmd.Flags().BoolVar(&releasePrerelease, "prerelease", false, "Mark as pre-release (optional, defaults to false)")
	add_component_releaseCmd.Flags().StringArrayVar(&releaseTeis, "tei", []string{}, "TEI identifier (can be specified multiple times)")
	add_component_releaseCmd.Flags().StringArrayVar(&releasePurls, "purl", []string{}, "PURL identifier (can be specified multiple times)")
	add_component_releaseCmd.MarkFlagRequired("component")
	add_component_releaseCmd.MarkFlagRequired("version")

	// Add flags to add_product_release command
	add_product_releaseCmd.Flags().StringVar(&productReleaseProduct, "product", "", "Product name or UUID (required)")
	add_product_releaseCmd.Flags().StringVar(&productReleaseVersion, "version", "", "Release version (required)")
	add_product_releaseCmd.Flags().StringVar(&productReleaseUuid, "uuid", "", "Release UUID (optional, will be generated if not provided)")
	add_product_releaseCmd.Flags().StringVar(&productReleaseCreatedDate, "createddate", "", "Created date in RFC3339 format (optional, defaults to current time)")
	add_product_releaseCmd.Flags().StringVar(&productReleaseReleaseDate, "releasedate", "", "Release date in RFC3339 format (optional, defaults to current time)")
	add_product_releaseCmd.Flags().BoolVar(&productReleasePrerelease, "prerelease", false, "Mark as pre-release (optional, defaults to false)")
	add_product_releaseCmd.Flags().StringArrayVar(&productReleaseTeis, "tei", []string{}, "TEI identifier (can be specified multiple times)")
	add_product_releaseCmd.Flags().StringArrayVar(&productReleasePurls, "purl", []string{}, "PURL identifier (can be specified multiple times)")
	add_product_releaseCmd.Flags().StringArrayVar(&productReleaseComponents, "component", []string{}, "Component name or UUID to link (optional, can be specified multiple times, must be paired with --component_release)")
	add_product_releaseCmd.Flags().StringArrayVar(&productReleaseComponentReleases, "component_release", []string{}, "Component release version or UUID to link (optional, can be specified multiple times, must be paired with --component)")
	add_product_releaseCmd.MarkFlagRequired("product")
	add_product_releaseCmd.MarkFlagRequired("version")

	// Add flags to add_artifact command
	add_artifactCmd.Flags().StringVar(&artifactUuid, "uuid", "", "Artifact UUID (optional, will be generated if not provided)")
	add_artifactCmd.Flags().StringVar(&artifactName, "name", "", "Artifact name (required)")
	add_artifactCmd.Flags().StringVar(&artifactType, "type", "", "Artifact type: ATTESTATION, BOM, BUILD_META, CERTIFICATION, FORMULATION, LICENSE, RELEASE_NOTES, SECURITY_TXT, THREAT_MODEL, VULNERABILITIES, OTHER (required)")
	add_artifactCmd.Flags().StringVar(&artifactMediaType, "mediatype", "", "Media type / MIME type (required)")
	add_artifactCmd.Flags().StringVar(&artifactUrl, "url", "", "Artifact URL (required)")
	add_artifactCmd.Flags().StringVar(&artifactSignatureUrl, "signatureurl", "", "Signature URL (optional)")
	add_artifactCmd.Flags().StringVar(&artifactDescription, "description", "", "Artifact description (optional, defaults to empty)")
	add_artifactCmd.Flags().StringArrayVar(&artifactHashes, "hash", []string{}, "Hash in format algorithm=value, e.g., sha256=abcd (optional, can be specified multiple times)")
	add_artifactCmd.MarkFlagRequired("name")
	add_artifactCmd.MarkFlagRequired("type")
	add_artifactCmd.MarkFlagRequired("mediatype")
	add_artifactCmd.MarkFlagRequired("url")

	// Add flags to add_artifact_to_releases command
	add_artifact_to_releasesCmd.Flags().StringVar(&addArtifactToReleasesArtifactUuid, "artifactuuid", "", "Artifact UUID (required)")
	add_artifact_to_releasesCmd.Flags().StringArrayVar(&addArtifactToReleasesComponents, "component", []string{}, "Component name or UUID (optional, can be specified multiple times, must be paired with --componentrelease)")
	add_artifact_to_releasesCmd.Flags().StringArrayVar(&addArtifactToReleasesComponentReleases, "componentrelease", []string{}, "Component release version or UUID (optional, can be specified multiple times, must be paired with --component)")
	add_artifact_to_releasesCmd.Flags().StringArrayVar(&addArtifactToReleasesProducts, "product", []string{}, "Product name or UUID (optional, can be specified multiple times, must be paired with --productrelease)")
	add_artifact_to_releasesCmd.Flags().StringArrayVar(&addArtifactToReleasesProductReleases, "productrelease", []string{}, "Product release version or UUID (optional, can be specified multiple times, must be paired with --product)")
	add_artifact_to_releasesCmd.MarkFlagRequired("artifactuuid")

	// Add subcommands to oolong
	oolongCmd.AddCommand(add_productCmd)
	oolongCmd.AddCommand(add_componentCmd)
	oolongCmd.AddCommand(add_component_releaseCmd)
	oolongCmd.AddCommand(add_product_releaseCmd)
	oolongCmd.AddCommand(add_artifactCmd)
	oolongCmd.AddCommand(add_artifact_to_releasesCmd)
}

// isUUID checks if a string matches the UUID format
func isUUID(s string) bool {
	uuidPattern := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	return uuidPattern.MatchString(s)
}

// toSnakeCase converts a string to lowercase snake_case
func toSnakeCase(s string) string {
	// Replace spaces and hyphens with underscores
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, "-", "_")

	// Insert underscores before uppercase letters (for camelCase)
	re := regexp.MustCompile("([a-z0-9])([A-Z])")
	s = re.ReplaceAllString(s, "${1}_${2}")

	// Convert to lowercase
	s = strings.ToLower(s)

	// Remove any duplicate underscores
	re = regexp.MustCompile("_+")
	s = re.ReplaceAllString(s, "_")

	// Trim leading/trailing underscores
	s = strings.Trim(s, "_")

	return s
}

// writeYAML writes a struct to a YAML file
func writeYAML(path string, data interface{}) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := yaml.NewEncoder(file, yaml.Indent(2))
	if err := encoder.Encode(data); err != nil {
		return err
	}

	return nil
}

// findComponent searches for a component by name or UUID in the content directory
// Returns the component directory path and the component data
func findComponent(contentDir, identifier string) (string, *Component, error) {
	componentsDir := filepath.Join(contentDir, "components")

	// Check if components directory exists
	if _, err := os.Stat(componentsDir); os.IsNotExist(err) {
		return "", nil, fmt.Errorf("components directory not found: %s", componentsDir)
	}

	// Read all component directories
	entries, err := os.ReadDir(componentsDir)
	if err != nil {
		return "", nil, fmt.Errorf("failed to read components directory: %w", err)
	}

	// Check if identifier is a UUID (contains hyphens in UUID format)
	isUUIDFormat := isUUID(identifier)

	// Search through all component directories
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		componentYamlPath := filepath.Join(componentsDir, entry.Name(), "component.yaml")

		// Check if component.yaml exists
		if _, err := os.Stat(componentYamlPath); os.IsNotExist(err) {
			continue
		}

		// Read and parse component.yaml
		data, err := os.ReadFile(componentYamlPath)
		if err != nil {
			continue
		}

		var component Component
		if err := yaml.Unmarshal(data, &component); err != nil {
			continue
		}

		// Match by UUID or name
		if isUUIDFormat {
			if component.UUID == identifier {
				return filepath.Join(componentsDir, entry.Name()), &component, nil
			}
		} else {
			if component.Name == identifier {
				return filepath.Join(componentsDir, entry.Name()), &component, nil
			}
		}
	}

	if isUUIDFormat {
		return "", nil, fmt.Errorf("component with UUID '%s' not found", identifier)
	}
	return "", nil, fmt.Errorf("component with name '%s' not found", identifier)
}

// findProduct searches for a product by name or UUID in the content directory
// Returns the product directory path and the product data
func findProduct(contentDir, identifier string) (string, *Product, error) {
	productsDir := filepath.Join(contentDir, "products")

	// Check if products directory exists
	if _, err := os.Stat(productsDir); os.IsNotExist(err) {
		return "", nil, fmt.Errorf("products directory not found: %s", productsDir)
	}

	// Read all product directories
	entries, err := os.ReadDir(productsDir)
	if err != nil {
		return "", nil, fmt.Errorf("failed to read products directory: %w", err)
	}

	// Check if identifier is a UUID (contains hyphens in UUID format)
	isUUIDFormat := isUUID(identifier)

	// Search through all product directories
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		productYamlPath := filepath.Join(productsDir, entry.Name(), "product.yaml")

		// Check if product.yaml exists
		if _, err := os.Stat(productYamlPath); os.IsNotExist(err) {
			continue
		}

		// Read and parse product.yaml
		data, err := os.ReadFile(productYamlPath)
		if err != nil {
			continue
		}

		var product Product
		if err := yaml.Unmarshal(data, &product); err != nil {
			continue
		}

		// Match by UUID or name
		if isUUIDFormat {
			if product.UUID == identifier {
				return filepath.Join(productsDir, entry.Name()), &product, nil
			}
		} else {
			if product.Name == identifier {
				return filepath.Join(productsDir, entry.Name()), &product, nil
			}
		}
	}

	if isUUIDFormat {
		return "", nil, fmt.Errorf("product with UUID '%s' not found", identifier)
	}
	return "", nil, fmt.Errorf("product with name '%s' not found", identifier)
}

// findComponentRelease searches for a component release by version or UUID
// Returns the component release UUID
func findComponentRelease(componentDir, identifier string) (string, error) {
	_, _, uuid, err := findComponentReleaseDir(componentDir, identifier)
	return uuid, err
}

// findComponentReleaseDir searches for a component release by version or UUID
// Returns the release directory path, version, and UUID
func findComponentReleaseDir(componentDir, identifier string) (string, string, string, error) {
	releasesDir := filepath.Join(componentDir, "releases")

	// Check if releases directory exists
	if _, err := os.Stat(releasesDir); os.IsNotExist(err) {
		return "", "", "", fmt.Errorf("releases directory not found: %s", releasesDir)
	}

	// Read all release directories
	entries, err := os.ReadDir(releasesDir)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to read releases directory: %w", err)
	}

	// Check if identifier is a UUID (contains hyphens in UUID format)
	isUUIDFormat := isUUID(identifier)

	// Search through all release directories
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		releaseYamlPath := filepath.Join(releasesDir, entry.Name(), "release.yaml")

		// Check if release.yaml exists
		if _, err := os.Stat(releaseYamlPath); os.IsNotExist(err) {
			continue
		}

		// Read and parse release.yaml
		data, err := os.ReadFile(releaseYamlPath)
		if err != nil {
			continue
		}

		var release ComponentRelease
		if err := yaml.Unmarshal(data, &release); err != nil {
			continue
		}

		// Match by UUID or version
		if isUUIDFormat {
			if release.UUID == identifier {
				return filepath.Join(releasesDir, entry.Name()), release.Version, release.UUID, nil
			}
		} else {
			if release.Version == identifier {
				return filepath.Join(releasesDir, entry.Name()), release.Version, release.UUID, nil
			}
		}
	}

	if isUUIDFormat {
		return "", "", "", fmt.Errorf("component release with UUID '%s' not found", identifier)
	}
	return "", "", "", fmt.Errorf("component release with version '%s' not found", identifier)
}

// findProductReleaseDir searches for a product release by version or UUID
// Returns the release directory path, version, and UUID
func findProductReleaseDir(productDir, identifier string) (string, string, string, error) {
	releasesDir := filepath.Join(productDir, "releases")

	// Check if releases directory exists
	if _, err := os.Stat(releasesDir); os.IsNotExist(err) {
		return "", "", "", fmt.Errorf("releases directory not found: %s", releasesDir)
	}

	// Read all release directories
	entries, err := os.ReadDir(releasesDir)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to read releases directory: %w", err)
	}

	// Check if identifier is a UUID (contains hyphens in UUID format)
	isUUIDFormat := isUUID(identifier)

	// Search through all release directories
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		releaseYamlPath := filepath.Join(releasesDir, entry.Name(), "release.yaml")

		// Check if release.yaml exists
		if _, err := os.Stat(releaseYamlPath); os.IsNotExist(err) {
			continue
		}

		// Read and parse release.yaml
		data, err := os.ReadFile(releaseYamlPath)
		if err != nil {
			continue
		}

		var release ProductRelease
		if err := yaml.Unmarshal(data, &release); err != nil {
			continue
		}

		// Match by UUID or version
		if isUUIDFormat {
			if release.UUID == identifier {
				return filepath.Join(releasesDir, entry.Name()), release.Version, release.UUID, nil
			}
		} else {
			if release.Version == identifier {
				return filepath.Join(releasesDir, entry.Name()), release.Version, release.UUID, nil
			}
		}
	}

	if isUUIDFormat {
		return "", "", "", fmt.Errorf("product release with UUID '%s' not found", identifier)
	}
	return "", "", "", fmt.Errorf("product release with version '%s' not found", identifier)
}

// findLatestCollectionVersion finds the highest collection version number in a collections directory
func findLatestCollectionVersion(collectionsDir string) (int, error) {
	// Check if collections directory exists
	if _, err := os.Stat(collectionsDir); os.IsNotExist(err) {
		return 0, fmt.Errorf("collections directory not found: %s", collectionsDir)
	}

	// Read all collection files
	entries, err := os.ReadDir(collectionsDir)
	if err != nil {
		return 0, fmt.Errorf("failed to read collections directory: %w", err)
	}

	maxVersion := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		// Parse version from filename (e.g., "1.yaml" -> 1)
		filename := entry.Name()
		if !strings.HasSuffix(filename, ".yaml") {
			continue
		}

		versionStr := strings.TrimSuffix(filename, ".yaml")
		version, err := strconv.Atoi(versionStr)
		if err != nil {
			continue
		}

		if version > maxVersion {
			maxVersion = version
		}
	}

	if maxVersion == 0 {
		return 0, fmt.Errorf("no valid collection files found in %s", collectionsDir)
	}

	return maxVersion, nil
}
