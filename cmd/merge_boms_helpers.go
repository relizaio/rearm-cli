package cmd

import (
	"fmt"

	cdx "github.com/CycloneDX/cyclonedx-go"
)

// mergeFlatComponents deduplicates and returns flat components
func mergeFlatComponents(componentMap map[string]*cdx.Component) []cdx.Component {
	uniqueComponents := make([]cdx.Component, 0, len(componentMap))
	for _, comp := range componentMap {
		uniqueComponents = append(uniqueComponents, *comp)
	}
	return uniqueComponents
}

// mergeHierarchicalComponents creates hierarchical components with children
func mergeHierarchicalComponents(roots []*cdx.Component, boms []*cdx.BOM) []cdx.Component {
	hierarchicalComponents := make([]cdx.Component, 0, len(roots))
	for i, root := range roots {
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
	mergedDependencies := []cdx.Dependency{
		{
			Ref:          mergedRootRef,
			Dependencies: &rootRefs,
		},
	}
	for _, dep := range allDependencies {
		if dep != nil {
			// Ensure Dependencies field is not nil to prevent JSON validation issues
			if dep.Dependencies == nil {
				emptyDeps := []string{}
				depCopy := *dep
				depCopy.Dependencies = &emptyDeps
				mergedDependencies = append(mergedDependencies, depCopy)
			} else {
				mergedDependencies = append(mergedDependencies, *dep)
			}
		}
	}
	return mergedDependencies
}

// mergeDependenciesFlatten creates dependencies for FLATTEN_UNDER_NEW_ROOT mode
func mergeDependenciesFlatten(roots []*cdx.Component, allDependencies []*cdx.Dependency, mergedRootRef string) []cdx.Dependency {
	depMap := make(map[string]*cdx.Dependency)
	for _, dep := range allDependencies {
		if dep != nil {
			depMap[dep.Ref] = dep
		}
	}
	flattenedDependsOn := []string{}
	for _, root := range roots {
		if root != nil && depMap[root.BOMRef] != nil && depMap[root.BOMRef].Dependencies != nil {
			flattenedDependsOn = append(flattenedDependsOn, (*depMap[root.BOMRef].Dependencies)...)
		}
	}
	mergedDependencies := []cdx.Dependency{
		{
			Ref:          mergedRootRef,
			Dependencies: &flattenedDependsOn,
		},
	}
	for _, dep := range allDependencies {
		isRoot := false
		for _, root := range roots {
			if root != nil && dep.Ref == root.BOMRef {
				isRoot = true
				break
			}
		}
		if !isRoot && dep != nil {
			// Ensure Dependencies field is not nil to prevent JSON validation issues
			if dep.Dependencies == nil {
				emptyDeps := []string{}
				depCopy := *dep
				depCopy.Dependencies = &emptyDeps
				mergedDependencies = append(mergedDependencies, depCopy)
			} else {
				mergedDependencies = append(mergedDependencies, *dep)
			}
		}
	}
	return mergedDependencies
}

// setMergedRootComponent sets the BOMRef and PackageURL for the merged root
func setMergedRootComponent(component *cdx.Component, group, name, version, purl string) string {
	if purl != "" {
		component.PackageURL = purl
		component.BOMRef = purl
	} else {
		component.BOMRef = fmt.Sprintf("%s/%s@%s", group, name, version)
	}
	return component.BOMRef
}
