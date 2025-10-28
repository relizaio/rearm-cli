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

	// Add subcommands to oolong
	oolongCmd.AddCommand(add_productCmd)
	oolongCmd.AddCommand(add_componentCmd)
	oolongCmd.AddCommand(add_component_releaseCmd)
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
	isUUID := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`).MatchString(identifier)

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
		if isUUID {
			if component.UUID == identifier {
				return filepath.Join(componentsDir, entry.Name()), &component, nil
			}
		} else {
			if component.Name == identifier {
				return filepath.Join(componentsDir, entry.Name()), &component, nil
			}
		}
	}

	if isUUID {
		return "", nil, fmt.Errorf("component with UUID '%s' not found", identifier)
	}
	return "", nil, fmt.Errorf("component with name '%s' not found", identifier)
}
