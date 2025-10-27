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

// addProductCmd represents the addproduct command
var addProductCmd = &cobra.Command{
	Use:   "addproduct",
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

// addComponentCmd represents the addcomponent command
var addComponentCmd = &cobra.Command{
	Use:   "addcomponent",
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

func init() {
	// Add flags to oolong command
	oolongCmd.PersistentFlags().StringVar(&contentDir, "contentdir", "", "Content directory path")
	oolongCmd.MarkFlagRequired("contentdir")

	// Add flags to addproduct command
	addProductCmd.Flags().StringVar(&productName, "name", "", "Product name (required)")
	addProductCmd.Flags().StringVar(&productUuid, "uuid", "", "Product UUID (optional, will be generated if not provided)")
	addProductCmd.MarkFlagRequired("name")

	// Add flags to addcomponent command
	addComponentCmd.Flags().StringVar(&componentNameFlag, "name", "", "Component name (required)")
	addComponentCmd.Flags().StringVar(&componentUuid, "uuid", "", "Component UUID (optional, will be generated if not provided)")
	addComponentCmd.MarkFlagRequired("name")

	// Add subcommands to oolong
	oolongCmd.AddCommand(addProductCmd)
	oolongCmd.AddCommand(addComponentCmd)
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
