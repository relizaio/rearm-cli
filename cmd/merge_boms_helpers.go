package cmd

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

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

// What a stderr line shows of the survivor is capped, so that each line is the folded node's
// own strings plus a constant and the whole of stderr stays linear in the inputs.
const (
	// keptLicenseKeysShown is how many of the survivor's licence keys a drop line shows.
	keptLicenseKeysShown = 4
	// keptStringBytes caps every survivor-side string a line shows (boundedQuote).
	keptStringBytes = 96
)

// boundedQuote is strconv.Quote(s) when s is at most keptStringBytes bytes long; otherwise it
// quotes the longest prefix of at most keptStringBytes bytes that ends on a rune boundary and
// appends "(+N bytes)" with the number of bytes cut.
func boundedQuote(s string) string {
	if len(s) <= keptStringBytes {
		return strconv.Quote(s)
	}
	cut := keptStringBytes
	// Back off to the start of the rune the cut falls in; at most utf8.UTFMax-1 bytes, so an
	// input that is not UTF-8 still keeps most of its prefix.
	for back := 0; back < utf8.UTFMax-1 && cut > 0 && !utf8.RuneStart(s[cut]); back++ {
		cut--
	}
	return strconv.Quote(s[:cut]) + fmt.Sprintf("(+%d bytes)", len(s)-cut)
}

// mergeComponentMetadata folds a duplicate node into the survivor of its key: licences by the
// licence rule (mergeLicenses), hashes by algorithm, properties by name, external references
// by URL. A hash or property the survivor holds with another value is dropped with a line.
// Those lines come once per entry of the duplicate, so they show the key bounded too: one
// duplicate with many entries would otherwise repeat a long key on every line.
func mergeComponentMetadata(existing, new *cdx.Component, key string, stderr io.Writer) {
	mergeLicenses(existing, new, key, stderr)

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
					fmt.Fprintf(stderr, "merge-boms: component %s: hash %q dropped beside the kept value: %q (kept: %s)\n",
						boundedQuote(key), string(hash.Algorithm), hash.Value, boundedQuote(hashMap[hash.Algorithm]))
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
					fmt.Fprintf(stderr, "merge-boms: component %s: property %q dropped beside the kept value: %q (kept: %s)\n",
						boundedQuote(key), prop.Name, prop.Value, boundedQuote(propertyMap[prop.Name]))
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

// mergeLicenses folds the duplicate's licence list D into the survivor's list S by rebom's
// de-dup rule (foldLicenseLists), where an absent list and [] are both absent:
//  1. D absent: S unchanged.
//  2. S absent: S becomes a copy of D.
//  3. An expression on either side: S stands whole; one line when D holds a key S lacks.
//  4. Otherwise the union by licenseKey, survivor first.
func mergeLicenses(existing, new *cdx.Component, key string, stderr io.Writer) {
	if licensesAbsent(new.Licenses) {
		return
	}
	if licensesAbsent(existing.Licenses) {
		// Create a copy to avoid pointer aliasing
		licensesCopy := make(cdx.Licenses, len(*new.Licenses))
		copy(licensesCopy, *new.Licenses)
		existing.Licenses = &licensesCopy
		return
	}
	kept := make(map[string]bool)
	for _, lic := range *existing.Licenses {
		if k := licenseKey(lic); k != "" {
			kept[k] = true
		}
	}
	if holdsExpression(*existing.Licenses) || holdsExpression(*new.Licenses) {
		var lost []string
		for _, lic := range *new.Licenses {
			if k := licenseKey(lic); k != "" && !kept[k] {
				lost = append(lost, formatLicenseKey(lic, strconv.Quote))
				kept[k] = true // each lost key is named once
			}
		}
		if len(lost) > 0 {
			fmt.Fprintf(stderr, "merge-boms: component %q: licences dropped beside the kept list: [%s] (kept: %s)\n",
				key, strings.Join(lost, " "), formatKeptLicenses(*existing.Licenses))
		}
		return
	}
	for _, lic := range *new.Licenses {
		if k := licenseKey(lic); k != "" && !kept[k] {
			*existing.Licenses = append(*existing.Licenses, lic)
			kept[k] = true
		}
	}
}

// licensesAbsent is true for no licences key and for [].
func licensesAbsent(l *cdx.Licenses) bool {
	return l == nil || len(*l) == 0
}

func holdsExpression(l cdx.Licenses) bool {
	for _, lic := range l {
		if lic.Expression != "" {
			return true
		}
	}
	return false
}

// licenseKey is rebom's licenseKey: expression:<text>, else id:<id> or name:<name> followed by
// NUL, the acknowledgement, NUL and the licence bom-ref; "" for an entry with none of these.
func licenseKey(lic cdx.LicenseChoice) string {
	if lic.Expression != "" {
		return "expression:" + lic.Expression
	}
	if lic.License == nil {
		return ""
	}
	var k string
	switch {
	case lic.License.ID != "":
		k = "id:" + lic.License.ID
	case lic.License.Name != "":
		k = "name:" + lic.License.Name
	default:
		return ""
	}
	return k + "\x00" + string(lic.License.Acknowledgement) + "\x00" + lic.License.BOMRef
}

// formatLicenseKey writes a keyed entry as a stderr line shows it, each input string through
// quote: expression:"..." or id:"..." / name:"..." with ,acknowledgement:"..." and
// ,bom-ref:"..." when set.
func formatLicenseKey(lic cdx.LicenseChoice, quote func(string) string) string {
	if lic.Expression != "" {
		return "expression:" + quote(lic.Expression)
	}
	var b strings.Builder
	if lic.License.ID != "" {
		b.WriteString("id:" + quote(lic.License.ID))
	} else {
		b.WriteString("name:" + quote(lic.License.Name))
	}
	if lic.License.Acknowledgement != "" {
		b.WriteString(",acknowledgement:" + quote(string(lic.License.Acknowledgement)))
	}
	if lic.License.BOMRef != "" {
		b.WriteString(",bom-ref:" + quote(lic.License.BOMRef))
	}
	return b.String()
}

// formatKeptLicenses shows the survivor's first keptLicenseKeysShown keyed entries, each
// string bounded, and counts the rest: [k1 k2 k3 k4 +N more].
func formatKeptLicenses(l cdx.Licenses) string {
	shown := make([]string, 0, keptLicenseKeysShown)
	more := 0
	for _, lic := range l {
		if licenseKey(lic) == "" {
			continue
		}
		if len(shown) < keptLicenseKeysShown {
			shown = append(shown, formatLicenseKey(lic, boundedQuote))
		} else {
			more++
		}
	}
	if more > 0 {
		shown = append(shown, fmt.Sprintf("+%d more", more))
	}
	return "[" + strings.Join(shown, " ") + "]"
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
					// Deduplicate and sort existing dependencies for deterministic output
					depsMap := make(map[string]bool)
					for _, d := range *depCopy.Dependencies {
						depsMap[d] = true
					}
					uniqueDeps := make([]string, 0, len(depsMap))
					for d := range depsMap {
						uniqueDeps = append(uniqueDeps, d)
					}
					sort.Strings(uniqueDeps)
					depCopy.Dependencies = &uniqueDeps
				}
				depMap[dep.Ref] = &depCopy
			}
		}
	}
	return depMap
}

// mergeHierarchicalComponents creates hierarchical components with children
func mergeHierarchicalComponents(roots []*cdx.Component, boms []*cdx.BOM, stderr io.Writer) []cdx.Component {
	// Validate that roots and boms have matching lengths
	if len(roots) != len(boms) {
		fmt.Fprintf(stderr, "Warning: roots length (%d) != boms length (%d) - using minimum length\n", len(roots), len(boms))
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
	// Each input root's bom-ref once: two roots of one bom-ref fold into one node
	rootRefs := []string{}
	seenRootRefs := make(map[string]bool)
	for _, root := range roots {
		if root != nil && root.BOMRef != "" && !seenRootRefs[root.BOMRef] {
			seenRootRefs[root.BOMRef] = true
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
