package cmd

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"os"

	cdx "github.com/CycloneDX/cyclonedx-go"
	"github.com/spf13/cobra"
)

var (
	MergeStructure         string
	MergeGroup             string
	MergeName              string
	MergeVersion           string
	MergeRootComponentMode string
	MergeInputFiles        []string
	MergePurl              string
	MergeOutfile           string
)

var mergeBomsCmd = &cobra.Command{
	Use:   "merge-boms",
	Short: "Merge multiple CycloneDX BOMs into a single BOM",
	Run: func(cmd *cobra.Command, args []string) {
		// 1. Read all BOM objects from input files
		var boms []*cdx.BOM
		for _, path := range MergeInputFiles {
			data, err := os.ReadFile(path)
			if err != nil {
				fmt.Printf("Error reading file %s: %v\n", path, err)
				os.Exit(1)
			}
			bom := readBomFromBytes(data)
			boms = append(boms, bom)
		}

		// 2. Extract roots, components, dependencies
		var roots []*cdx.Component
		var allComponents []*cdx.Component
		var allDependencies []*cdx.Dependency

		for _, bom := range boms {
			if bom.Metadata != nil && bom.Metadata.Component != nil {
				roots = append(roots, bom.Metadata.Component)
			}
			if bom.Components != nil {
				for _, comp := range *bom.Components {
					allComponents = append(allComponents, &comp)
				}
			}
			if bom.Dependencies != nil {
				for _, dep := range *bom.Dependencies {
					allDependencies = append(allDependencies, &dep)
				}
			}
		}

		// Prepare deduplication map for FLAT mode
		componentMap := make(map[string]*cdx.Component) // key: bom-ref
		for _, comp := range allComponents {
			if comp.BOMRef != "" {
				componentMap[comp.BOMRef] = comp
			}
		}

		// 3. Merge logic
		// 3.1 Metadata: create new BOM object, set root from flags
		mergedBOM := &cdx.BOM{
			BOMFormat:    "CycloneDX",
			SpecVersion:  cdx.SpecVersion1_6,
			SerialNumber: generateSerialNumber(),
			Version:      1,
			Metadata: &cdx.Metadata{
				Component: &cdx.Component{
					Type:    cdx.ComponentTypeApplication,
					Name:    MergeName,
					Group:   MergeGroup,
					Version: MergeVersion,
				},
			},
		}

		// Set merged root component BOMRef and PackageURL
		mergedRootRef := setMergedRootComponent(
			mergedBOM.Metadata.Component,
			MergeGroup, MergeName, MergeVersion, MergePurl,
		)

		// 3.2 Components: FLAT or HIERARCHICAL
		if MergeStructure == "HIERARCHICAL" {
			hierComponents := mergeHierarchicalComponents(roots, boms)
			mergedBOM.Components = &hierComponents
		} else {
			// Default to FLAT when MergeStructure is empty or "FLAT"
			flatComponents := mergeFlatComponents(componentMap)
			mergedBOM.Components = &flatComponents
		}

		// 3.3 Dependencies: merge according to root component merge mode
		var mergedDependencies []cdx.Dependency
		switch MergeRootComponentMode {
		case "PRESERVE_UNDER_NEW_ROOT":
			mergedDependencies = mergeDependenciesPreserve(roots, allDependencies, mergedRootRef)
		case "FLATTEN_UNDER_NEW_ROOT":
			mergedDependencies = mergeDependenciesFlatten(roots, allDependencies, mergedRootRef)
		default:
			fmt.Printf("Unknown MergeRootComponentMode: %s\n", MergeRootComponentMode)
			os.Exit(1)
		}
		if len(mergedDependencies) > 0 {
			mergedBOM.Dependencies = &mergedDependencies
		}

		// 4. Output
		buf := new(bytes.Buffer)
		if err := cdx.NewBOMEncoder(buf, cdx.BOMFileFormatJSON).Encode(mergedBOM); err != nil {
			fmt.Printf("Error encoding merged BOM: %v\n", err)
			os.Exit(1)
		}
		if MergeOutfile == "" || MergeOutfile == "-" {
			_, err := os.Stdout.Write(buf.Bytes())
			if err != nil {
				fmt.Printf("Error writing to stdout: %v\n", err)
				os.Exit(1)
			}
		} else {
			if err := os.WriteFile(MergeOutfile, buf.Bytes(), 0644); err != nil {
				fmt.Printf("Error writing to file %s: %v\n", MergeOutfile, err)
				os.Exit(1)
			}
		}

	},
}

func init() {
	mergeBomsCmd.Flags().StringVar(&MergeStructure, "structure", "FLAT", "Structure of the merged BOM (FLAT, HIERARCHICAL)")
	mergeBomsCmd.Flags().StringVar(&MergeGroup, "group", "", "New root component group")
	mergeBomsCmd.Flags().StringVar(&MergeName, "name", "", "New root component name")
	mergeBomsCmd.Flags().StringVar(&MergeVersion, "version", "", "New root component version")
	mergeBomsCmd.Flags().StringVar(&MergeRootComponentMode, "root-component-merge-mode", "PRESERVE_UNDER_NEW_ROOT", "Root component merge mode (PRESERVE_UNDER_NEW_ROOT, FLATTEN_UNDER_NEW_ROOT)")
	mergeBomsCmd.Flags().StringSliceVar(&MergeInputFiles, "input-files", nil, "Input file paths for BOMs to merge")
	mergeBomsCmd.Flags().StringVar(&MergePurl, "purl", "", "Set bom-ref and purl for the root merged component")
	mergeBomsCmd.Flags().StringVar(&MergeOutfile, "outfile", "", "Output file path to write merged BOM (default: stdout)")

	bomUtils.AddCommand(mergeBomsCmd)
}

// generateSerialNumber creates a UUID-style serial number for the merged BOM
func generateSerialNumber() string {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		// Fallback to a simple timestamp-based serial number if random generation fails
		return fmt.Sprintf("urn:uuid:merged-bom-%d", len(b))
	}
	// Format as UUID v4
	b[6] = (b[6] & 0x0f) | 0x40 // Version 4
	b[8] = (b[8] & 0x3f) | 0x80 // Variant bits
	return fmt.Sprintf("urn:uuid:%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
