package cmd

import (
	"testing"

	cdx "github.com/CycloneDX/cyclonedx-go"
)

func resetSkipState() {
	skipPatterns = nil
	rootSkipPurls = nil
}

// The root self-skip must match the metadata component's purl exactly. The
// previous implementation appended ".*<name>.*" to skipPatterns — regex
// syntax fed into a substring matcher — which could only ever match a purl
// containing the literal characters ".*", so the feature had never fired.
func TestRootSelfSkipMatchesExactPurl(t *testing.T) {
	resetSkipState()
	defer resetSkipState()

	bom := &cdx.BOM{
		Metadata: &cdx.Metadata{
			Component: &cdx.Component{PackageURL: "pkg:npm/myapp@1.2.3"},
		},
	}
	addMetadataComponentSelfSkip(bom)

	if !shouldSkipPurl("pkg:npm/myapp@1.2.3") {
		t.Error("the BOM root's own purl must be skipped")
	}
	if !shouldSkipPurl("PKG:NPM/MyApp@1.2.3") {
		t.Error("root skip must be case-insensitive, matching the pattern matcher")
	}
}

// Exact, not substring: other versions of the root's package appearing as
// real dependencies must still be enriched, as must packages whose purl
// merely contains the root's name.
func TestRootSelfSkipDoesNotOverSkip(t *testing.T) {
	resetSkipState()
	defer resetSkipState()

	bom := &cdx.BOM{
		Metadata: &cdx.Metadata{
			Component: &cdx.Component{PackageURL: "pkg:npm/app@1.0"},
		},
	}
	addMetadataComponentSelfSkip(bom)

	if shouldSkipPurl("pkg:npm/app@1.0.1") {
		t.Error("a different version of the root package is a real dependency, not the root")
	}
	if shouldSkipPurl("pkg:npm/application@2.0") {
		t.Error("a package whose purl merely contains the root name must not be skipped")
	}
}

func TestRootSelfSkipToleratesMissingMetadata(t *testing.T) {
	resetSkipState()
	defer resetSkipState()

	addMetadataComponentSelfSkip(&cdx.BOM{})
	addMetadataComponentSelfSkip(&cdx.BOM{Metadata: &cdx.Metadata{}})
	addMetadataComponentSelfSkip(&cdx.BOM{
		Metadata: &cdx.Metadata{Component: &cdx.Component{}},
	})

	if len(rootSkipPurls) != 0 {
		t.Errorf("no root purl should be registered, got %v", rootSkipPurls)
	}
}

func TestSkipPatternsRemainSubstringAndCaseInsensitive(t *testing.T) {
	resetSkipState()
	defer resetSkipState()

	skipPatterns = []string{"pkg:generic/"}
	if !shouldSkipPurl("pkg:generic/some-file.h?path=%2Fusr%2Finclude") {
		t.Error("configured substring pattern must match")
	}
	if !shouldSkipPurl("PKG:GENERIC/OTHER") {
		t.Error("substring matching must stay case-insensitive")
	}
	if shouldSkipPurl("pkg:npm/left-pad@1.3.0") {
		t.Error("non-matching purl must not be skipped")
	}
}
