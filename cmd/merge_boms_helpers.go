package cmd

import (
	"fmt"
	"log"
	"sort"

	cdx "github.com/CycloneDX/cyclonedx-go"
)

// mergeFlatComponents deduplicates and returns flat components in sorted order
func mergeFlatComponents(componentMap map[string]*cdx.Component) []cdx.Component {
	// Sort by key for deterministic output
	sortedKeys := make([]string, 0, len(componentMap))
	for key := range componentMap {
		sortedKeys = append(sortedKeys, key)
	}
	sort.Strings(sortedKeys)

	uniqueComponents := make([]cdx.Component, 0, len(componentMap))
	for _, key := range sortedKeys {
		uniqueComponents = append(uniqueComponents, *componentMap[key])
	}
	return uniqueComponents
}

// mergeComponentMetadata merges metadata from two components with the same key
func mergeComponentMetadata(existing, new *cdx.Component) {
	// Merge licenses
	if new.Licenses != nil {
		if existing.Licenses == nil {
			// Create a copy to avoid pointer aliasing
			licensesCopy := make(cdx.Licenses, len(*new.Licenses))
			copy(licensesCopy, *new.Licenses)
			existing.Licenses = &licensesCopy
		} else {
			// Deduplicate licenses by ID/Name
			licenseMap := make(map[string]bool)
			for _, lic := range *existing.Licenses {
				if lic.License != nil {
					if lic.License.ID != "" {
						licenseMap[lic.License.ID] = true
					} else if lic.License.Name != "" {
						licenseMap[lic.License.Name] = true
					}
				}
			}
			for _, lic := range *new.Licenses {
				if lic.License != nil {
					key := lic.License.ID
					if key == "" {
						key = lic.License.Name
					}
					if key != "" && !licenseMap[key] {
						*existing.Licenses = append(*existing.Licenses, lic)
						licenseMap[key] = true
					}
				}
			}
		}
	}

	// Merge hashes
	if new.Hashes != nil {
		if existing.Hashes == nil {
			// Create a copy to avoid pointer aliasing
			hashesCopy := make([]cdx.Hash, len(*new.Hashes))
			copy(hashesCopy, *new.Hashes)
			existing.Hashes = &hashesCopy
		} else {
			// Deduplicate hashes by algorithm
			hashMap := make(map[cdx.HashAlgorithm]string)
			for _, hash := range *existing.Hashes {
				hashMap[hash.Algorithm] = hash.Value
			}
			for _, hash := range *new.Hashes {
				if _, exists := hashMap[hash.Algorithm]; !exists {
					*existing.Hashes = append(*existing.Hashes, hash)
					hashMap[hash.Algorithm] = hash.Value
				} else if hashMap[hash.Algorithm] != hash.Value {
					log.Printf("Warning: Hash mismatch for component %s, algorithm %s: %s vs %s",
						existing.BOMRef, hash.Algorithm, hashMap[hash.Algorithm], hash.Value)
				}
			}
		}
	}

	// Merge properties
	if new.Properties != nil {
		if existing.Properties == nil {
			// Create a copy to avoid pointer aliasing
			propertiesCopy := make([]cdx.Property, len(*new.Properties))
			copy(propertiesCopy, *new.Properties)
			existing.Properties = &propertiesCopy
		} else {
			// Deduplicate properties by name
			propertyMap := make(map[string]string)
			for _, prop := range *existing.Properties {
				propertyMap[prop.Name] = prop.Value
			}
			for _, prop := range *new.Properties {
				if _, exists := propertyMap[prop.Name]; !exists {
					*existing.Properties = append(*existing.Properties, prop)
					propertyMap[prop.Name] = prop.Value
				} else if propertyMap[prop.Name] != prop.Value {
					log.Printf("Warning: Property value mismatch for component %s, property %s: %s vs %s",
						existing.BOMRef, prop.Name, propertyMap[prop.Name], prop.Value)
				}
			}
		}
	}

	// Merge external references
	if new.ExternalReferences != nil {
		if existing.ExternalReferences == nil {
			// Create a copy to avoid pointer aliasing
			refsCopy := make([]cdx.ExternalReference, len(*new.ExternalReferences))
			copy(refsCopy, *new.ExternalReferences)
			existing.ExternalReferences = &refsCopy
		} else {
			// Deduplicate by URL
			refMap := make(map[string]bool)
			for _, ref := range *existing.ExternalReferences {
				refMap[ref.URL] = true
			}
			for _, ref := range *new.ExternalReferences {
				if !refMap[ref.URL] {
					*existing.ExternalReferences = append(*existing.ExternalReferences, ref)
					refMap[ref.URL] = true
				}
			}
		}
	}
}

// deduplicateDependencies deduplicates dependencies by Ref and merges their Dependencies arrays
func deduplicateDependencies(allDependencies []*cdx.Dependency) map[string]*cdx.Dependency {
	depMap := make(map[string]*cdx.Dependency)
	for _, dep := range allDependencies {
		if dep != nil && dep.Ref != "" {
			if existing, exists := depMap[dep.Ref]; exists {
				// Merge Dependencies arrays - handle all cases (not just both non-nil)
				if dep.Dependencies != nil || existing.Dependencies != nil {
					mergedDeps := make(map[string]bool)
					if existing.Dependencies != nil {
						for _, d := range *existing.Dependencies {
							mergedDeps[d] = true
						}
					}
					if dep.Dependencies != nil {
						for _, d := range *dep.Dependencies {
							mergedDeps[d] = true
						}
					}
					uniqueDeps := make([]string, 0, len(mergedDeps))
					for d := range mergedDeps {
						uniqueDeps = append(uniqueDeps, d)
					}
					// CRITICAL: Sort for deterministic output
					sort.Strings(uniqueDeps)
					existing.Dependencies = &uniqueDeps
				}
			} else {
				// First time seeing this ref
				depCopy := *dep
				// Ensure Dependencies field is not nil to prevent JSON validation issues
				if depCopy.Dependencies == nil {
					emptyDeps := []string{}
					depCopy.Dependencies = &emptyDeps
				} else {
					// Sort existing dependencies for deterministic output
					deps := make([]string, len(*depCopy.Dependencies))
					copy(deps, *depCopy.Dependencies)
					sort.Strings(deps)
					depCopy.Dependencies = &deps
				}
				depMap[dep.Ref] = &depCopy
			}
		}
	}
	return depMap
}

// mergeHierarchicalComponents creates hierarchical components with children
func mergeHierarchicalComponents(roots []*cdx.Component, boms []*cdx.BOM) []cdx.Component {
	// Validate that roots and boms have matching lengths
	if len(roots) != len(boms) {
		log.Printf("Warning: roots length (%d) != boms length (%d) - using minimum length", len(roots), len(boms))
	}

	minLen := len(roots)
	if len(boms) < minLen {
		minLen = len(boms)
	}

	hierarchicalComponents := make([]cdx.Component, 0, minLen)
	for i := 0; i < minLen; i++ {
		root := roots[i]
		if root == nil {
			continue
		}
		bom := boms[i]
		var children []cdx.Component
		if bom.Components != nil {
			for _, comp := range *bom.Components {
				children = append(children, comp)
			}
		}
		rootCopy := *root
		if len(children) > 0 {
			rootCopy.Components = &children
		}
		hierarchicalComponents = append(hierarchicalComponents, rootCopy)
	}
	return hierarchicalComponents
}

// mergeDependenciesPreserve creates dependencies for PRESERVE_UNDER_NEW_ROOT mode
func mergeDependenciesPreserve(roots []*cdx.Component, allDependencies []*cdx.Dependency, mergedRootRef string) []cdx.Dependency {
	rootRefs := []string{}
	for _, root := range roots {
		if root != nil && root.BOMRef != "" {
			rootRefs = append(rootRefs, root.BOMRef)
		}
	}
	// Sort for deterministic output
	sort.Strings(rootRefs)

	mergedDependencies := []cdx.Dependency{
		{
			Ref:          mergedRootRef,
			Dependencies: &rootRefs,
		},
	}

	// Use shared deduplication logic
	depMap := deduplicateDependencies(allDependencies)

	// Append deduplicated dependencies in sorted order for deterministic output
	sortedRefs := make([]string, 0, len(depMap))
	for ref := range depMap {
		sortedRefs = append(sortedRefs, ref)
	}
	sort.Strings(sortedRefs)
	for _, ref := range sortedRefs {
		mergedDependencies = append(mergedDependencies, *depMap[ref])
	}

	return mergedDependencies
}

// mergeDependenciesFlatten creates dependencies for FLATTEN_UNDER_NEW_ROOT mode
func mergeDependenciesFlatten(roots []*cdx.Component, allDependencies []*cdx.Dependency, mergedRootRef string) []cdx.Dependency {
	// Use shared deduplication logic
	depMap := deduplicateDependencies(allDependencies)

	// Flatten dependencies from root components
	flattenedDepsMap := make(map[string]bool)
	for _, root := range roots {
		if root != nil && depMap[root.BOMRef] != nil && depMap[root.BOMRef].Dependencies != nil {
			for _, d := range *depMap[root.BOMRef].Dependencies {
				flattenedDepsMap[d] = true
			}
		}
	}
	// Sort flattened dependencies for deterministic output
	flattenedDependsOn := make([]string, 0, len(flattenedDepsMap))
	for d := range flattenedDepsMap {
		flattenedDependsOn = append(flattenedDependsOn, d)
	}
	sort.Strings(flattenedDependsOn)

	mergedDependencies := []cdx.Dependency{
		{
			Ref:          mergedRootRef,
			Dependencies: &flattenedDependsOn,
		},
	}

	// Add non-root dependencies in sorted order for deterministic output
	sortedRefs := make([]string, 0, len(depMap))
	for ref := range depMap {
		isRoot := false
		for _, root := range roots {
			if root != nil && ref == root.BOMRef {
				isRoot = true
				break
			}
		}
		if !isRoot {
			sortedRefs = append(sortedRefs, ref)
		}
	}
	sort.Strings(sortedRefs)
	for _, ref := range sortedRefs {
		mergedDependencies = append(mergedDependencies, *depMap[ref])
	}

	return mergedDependencies
}

// setMergedRootComponent sets the BOMRef and PackageURL for the merged root
func setMergedRootComponent(component *cdx.Component, group, name, version, purl string) string {
	if purl != "" {
		component.PackageURL = purl
		component.BOMRef = purl
	} else {
		// Handle empty group to avoid leading slash
		if group != "" {
			component.BOMRef = fmt.Sprintf("%s/%s@%s", group, name, version)
		} else {
			component.BOMRef = fmt.Sprintf("%s@%s", name, version)
		}
	}
	return component.BOMRef
}
