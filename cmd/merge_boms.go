package cmd

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"io"
	"os"

	cdx "github.com/CycloneDX/cyclonedx-go"
	"github.com/spf13/cobra"
)

const (
	unnamedComponentPlaceholder = "<unnamed>"
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
		code := RunMergeBoms(MergeBomsOptions{
			Structure:         MergeStructure,
			Group:             MergeGroup,
			Name:              MergeName,
			Version:           MergeVersion,
			RootComponentMode: MergeRootComponentMode,
			InputFiles:        MergeInputFiles,
			Purl:              MergePurl,
			Outfile:           MergeOutfile,
		}, os.Stdout, os.Stderr)
		if code != mergeBomsExitMerged {
			os.Exit(code)
		}
	},
}

// Exit codes of merge-boms.
const (
	mergeBomsExitMerged = 0
	mergeBomsExitError  = 1
)

// MergeBomsOptions holds the merge-boms flag values. RunMergeBoms supplies no default: the
// flag defaults (FLAT, PRESERVE_UNDER_NEW_ROOT) live in cobra.
type MergeBomsOptions struct {
	// Structure is HIERARCHICAL, or anything else for FLAT (matched case-sensitively).
	Structure         string
	Group             string
	Name              string
	Version           string
	RootComponentMode string
	InputFiles        []string
	Purl              string
	// Outfile empty or "-" writes the merged document to stdout.
	Outfile string
}

// RunMergeBoms merges the input BOMs and writes the merged document to stdout, or to Outfile
// when it is set and not "-"; it returns the exit code (0 merged, 1 on any error). Errors and
// the lines about what a fold lost go to stderr.
func RunMergeBoms(opts MergeBomsOptions, stdout, stderr io.Writer) int {
	// Validate required flags
	if len(opts.InputFiles) == 0 {
		fmt.Fprintln(stderr, "Error: --input-files is required")
		return mergeBomsExitError
	}
	if opts.Name == "" {
		fmt.Fprintln(stderr, "Error: --name is required")
		return mergeBomsExitError
	}
	if opts.Version == "" {
		fmt.Fprintln(stderr, "Error: --version is required")
		return mergeBomsExitError
	}

	// 1. Read all BOM objects from input files
	var boms []*cdx.BOM
	for _, path := range opts.InputFiles {
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(stderr, "Error reading file %s: %v\n", path, err)
			return mergeBomsExitError
		}
		bom, err := decodeBom(data)
		if err != nil {
			fmt.Fprintf(stderr, "Error decoding file %s: %v\n", path, err)
			return mergeBomsExitError
		}
		boms = append(boms, bom)
	}

	// 2. Extract roots, components, dependencies
	var roots []*cdx.Component
	var rootInputs []int // rootInputs[i] is the index in InputFiles of roots[i]
	var allComponents []*cdx.Component
	var allDependencies []*cdx.Dependency

	for i, bom := range boms {
		if bom.Metadata != nil && bom.Metadata.Component != nil {
			roots = append(roots, bom.Metadata.Component)
			rootInputs = append(rootInputs, i)
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
				Name:    opts.Name,
				Group:   opts.Group,
				Version: opts.Version,
			},
		},
	}

	// Set merged root component BOMRef and PackageURL
	mergedRootRef := setMergedRootComponent(
		mergedBOM.Metadata.Component,
		opts.Group, opts.Name, opts.Version, opts.Purl,
	)

	// 3.2 Components: FLAT or HIERARCHICAL. Only FLAT folds: under HIERARCHICAL no root or
	// component is touched, so each input's nodes are nested as they were read.
	if opts.Structure == "HIERARCHICAL" {
		hierComponents := mergeHierarchicalComponents(roots, boms, stderr)
		mergedBOM.Components = &hierComponents
	} else {
		// Default to FLAT when the structure is empty, "FLAT" or anything else
		componentMap := foldFlatComponents(roots, rootInputs, allComponents, stderr)
		flatComponents := mergeFlatComponents(componentMap)
		mergedBOM.Components = &flatComponents
	}

	// 3.3 Dependencies: merge according to root component merge mode
	var mergedDependencies []cdx.Dependency
	switch opts.RootComponentMode {
	case "PRESERVE_UNDER_NEW_ROOT":
		mergedDependencies = mergeDependenciesPreserve(roots, allDependencies, mergedRootRef)
	case "FLATTEN_UNDER_NEW_ROOT":
		mergedDependencies = mergeDependenciesFlatten(roots, allDependencies, mergedRootRef)
	default:
		fmt.Fprintf(stderr, "Error: Unknown MergeRootComponentMode: %s\n", opts.RootComponentMode)
		return mergeBomsExitError
	}
	if len(mergedDependencies) > 0 {
		mergedBOM.Dependencies = &mergedDependencies
	}

	// 4. Output
	buf := new(bytes.Buffer)
	if err := cdx.NewBOMEncoder(buf, cdx.BOMFileFormatJSON).Encode(mergedBOM); err != nil {
		fmt.Fprintf(stderr, "Error encoding merged BOM: %v\n", err)
		return mergeBomsExitError
	}
	if opts.Outfile == "" || opts.Outfile == "-" {
		if _, err := stdout.Write(buf.Bytes()); err != nil {
			fmt.Fprintf(stderr, "Error writing to stdout: %v\n", err)
			return mergeBomsExitError
		}
	} else {
		if err := os.WriteFile(opts.Outfile, buf.Bytes(), 0644); err != nil {
			fmt.Fprintf(stderr, "Error writing to file %s: %v\n", opts.Outfile, err)
			return mergeBomsExitError
		}
	}
	return mergeBomsExitMerged
}

// foldFlatComponents keys every root, then every top-level component, by purl else bom-ref.
// The first holder of a key in input order survives and a later holder folds into it; a root
// is keyed before any component. It prints one line per root fold, per keyless root and per
// keyless component.
func foldFlatComponents(roots []*cdx.Component, rootInputs []int, allComponents []*cdx.Component, stderr io.Writer) map[string]*cdx.Component {
	componentMap := make(map[string]*cdx.Component) // key: purl or bom-ref
	rootInputOfKey := make(map[string]int)

	// Add root components to the map first (they may be referenced in dependencies)
	for i, root := range roots {
		key := componentKey(root)
		if key == "" {
			rootName := root.Name
			if rootName == "" {
				rootName = unnamedComponentPlaceholder
			}
			fmt.Fprintf(stderr, "merge-boms: input %d root %q has neither purl nor bom-ref and is dropped\n",
				rootInputs[i], rootName)
			continue
		}
		if existing, exists := componentMap[key]; exists {
			fmt.Fprintf(stderr, "merge-boms: input %d root %q folds into root of input %d (key %q); "+
				"fields outside the fold (supplier, description, nested components) are the survivor's\n",
				rootInputs[i], root.BOMRef, rootInputOfKey[key], key)
			mergeComponentMetadata(existing, root, key, stderr)
		} else {
			componentMap[key] = root
			rootInputOfKey[key] = rootInputs[i]
		}
	}

	droppedCount := 0
	for _, comp := range allComponents {
		key := componentKey(comp)
		if key != "" {
			if existing, exists := componentMap[key]; exists {
				// Merge metadata from duplicate component
				mergeComponentMetadata(existing, comp, key, stderr)
			} else {
				componentMap[key] = comp
			}
		} else {
			// Warn about components without identifiers
			droppedCount++
			compName := comp.Name
			if compName == "" {
				compName = unnamedComponentPlaceholder
			}
			fmt.Fprintf(stderr, "Warning: Dropping component '%s' (type: %s) - missing both PackageURL and BOMRef\n",
				compName, comp.Type)
		}
	}
	if droppedCount > 0 {
		fmt.Fprintf(stderr, "Warning: Dropped %d component(s) without identifiers\n", droppedCount)
	}
	return componentMap
}

// componentKey is the fold key of a node: its purl, else its bom-ref, else "".
func componentKey(c *cdx.Component) string {
	if c.PackageURL != "" {
		return c.PackageURL
	}
	return c.BOMRef
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
