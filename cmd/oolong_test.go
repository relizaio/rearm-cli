package cmd

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

// The oolong commands write the TEA 1.0.0 content layout the Oolong server reads.

func TestOolongBuildIdentifiersRequiresTea100Syntax(t *testing.T) {
	ids, err := buildIdentifiers(
		[]string{"tei://example.com/uuid/22222222-2222-4222-8222-222222222222", "tei://example.com/purl/cGtnOmdlbmVyaWMvd2lkZ2V0"},
		[]string{"pkg:generic/widget@1.0.0"})
	if err != nil {
		t.Fatalf("valid identifiers refused: %v", err)
	}
	if len(ids) != 3 || ids[0].IdType != "TEI" || ids[2].IdType != "PURL" {
		t.Fatalf("unexpected identifiers %+v", ids)
	}
	for _, bad := range []string{"urn:tei:uuid:example.com:22222222-2222-4222-8222-222222222222", "tei://example.com/uuid", "tei://example.com/uuid/"} {
		if _, err := buildIdentifiers([]string{bad}, nil); err == nil {
			t.Errorf("TEI %q accepted", bad)
		}
	}
	if _, err := buildIdentifiers(nil, []string{"generic/widget"}); err == nil {
		t.Error("PURL without pkg: accepted")
	}
}

func TestOolongParseChecksums(t *testing.T) {
	sums, err := parseChecksums([]string{"sha256=8C5170DF0AE7A47F77B2F0B1F03C04500343AA2A26863098D2F133B9350D0F98", "SHA1=a9ac7d6e8b5aae33125cd3c3d58c9bb5e3924e35"})
	if err != nil {
		t.Fatal(err)
	}
	if sums[0].AlgType != "SHA-256" || sums[0].AlgValue != "8c5170df0ae7a47f77b2f0b1f03c04500343aa2a26863098d2f133b9350d0f98" || sums[1].AlgType != "SHA-1" {
		t.Fatalf("unexpected checksums %+v", sums)
	}
	for _, bad := range []string{
		"md5=0123456789abcdef0123456789abcdef",          // not a TEA 1.0.0 checksum type
		"sha256=abcd",                                   // wrong length
		"sha1=zzzz7d6e8b5aae33125cd3c3d58c9bb5e3924e35", // not hex
		"sha256",
	} {
		if _, err := parseChecksums([]string{bad}); err == nil {
			t.Errorf("hash %q accepted", bad)
		}
	}
}

func TestOolongDatesAreUtc(t *testing.T) {
	got, err := parseOrDefaultDate("2026-01-15T10:00:00+02:00")
	if err != nil || got != "2026-01-15T08:00:00Z" {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := parseOrDefaultDate("2026-01-15"); err == nil {
		t.Error("date without time accepted")
	}
	now, err := parseOrDefaultDate("")
	if err != nil || !strings.HasSuffix(now, "Z") {
		t.Fatalf("default date %q, %v", now, err)
	}
}

func TestOolongUuidFlags(t *testing.T) {
	got, err := getOrGenerateUUID("ABCDEF01-2345-4678-89AB-CDEF01234567")
	if err != nil || got != "abcdef01-2345-4678-89ab-cdef01234567" {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := getOrGenerateUUID("not-a-uuid"); err == nil {
		t.Error("invalid UUID accepted")
	}
	generated, err := getOrGenerateUUID("")
	if err != nil || !isUUID(generated) {
		t.Fatalf("generated %q, %v", generated, err)
	}
}

const (
	testComponentUUID = "33333333-3333-4333-8333-333333333333"
	testReleaseUUID   = "44444444-4444-4444-8444-444444444444"
	testArtifactUUID  = "55555555-5555-4555-8555-555555555555"
)

func writeTestYAML(t *testing.T, path string, v interface{}) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeYAML(path, v); err != nil {
		t.Fatal(err)
	}
}

// oolongTestContent is one component release with an empty first collection and one artifact
// at revision 1.
func oolongTestContent(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	componentDir := filepath.Join(dir, "components", "widget_lib")
	writeTestYAML(t, filepath.Join(componentDir, "component.yaml"), Component{UUID: testComponentUUID, Name: "Widget Lib", Identifiers: []OolongIdentifier{}})
	releaseDir := filepath.Join(componentDir, "releases", "1.95")
	writeTestYAML(t, filepath.Join(releaseDir, "release.yaml"), ComponentRelease{UUID: testReleaseUUID, Component: testComponentUUID, Version: "1.95", CreatedDate: "2026-01-15T08:00:00Z", Identifiers: []OolongIdentifier{}, Distributions: []Distribution{}})
	writeTestYAML(t, filepath.Join(releaseDir, "collections", "1.yaml"), Collection{Version: 1, CreatedDate: "2026-01-15T08:00:00Z", BelongsTo: belongsToComponentRelease, UpdateReason: UpdateReason{Type: "INITIAL_RELEASE"}, Artifacts: []CollectionArtifact{}})
	writeArtifactRevision(t, dir, 1)
	return dir, releaseDir
}

func writeArtifactRevision(t *testing.T, dir string, version int) {
	t.Helper()
	writeTestYAML(t, filepath.Join(dir, "artifacts", testArtifactUUID, "v"+strconv.Itoa(version)+".yaml"), OolongArtifact{
		UUID: testArtifactUUID, Version: version, Name: "Widget SBOM", Type: "BOM", CreatedDate: "2026-01-15T08:00:00Z",
		Formats: []ArtifactFormat{{MediaType: "application/vnd.cyclonedx+json", Url: "https://example.com/widget.cdx.json",
			Checksums: []Checksum{{AlgType: "SHA-1", AlgValue: "a9ac7d6e8b5aae33125cd3c3d58c9bb5e3924e35"}}}},
	})
}

func readTestCollection(t *testing.T, releaseDir string, version int) Collection {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(releaseDir, "collections", strconv.Itoa(version)+".yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var c Collection
	if err := yaml.Unmarshal(data, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestOolongAddArtifactToReleasesWritesTea100Collections(t *testing.T) {
	dir, releaseDir := oolongTestContent(t)

	if err := addArtifactToReleases(dir, testArtifactUUID, []string{"Widget Lib"}, []string{"1.95"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	added := readTestCollection(t, releaseDir, 2)
	if added.BelongsTo != belongsToComponentRelease || added.UpdateReason.Type != "ARTIFACT_ADDED" ||
		len(added.Artifacts) != 1 || added.Artifacts[0] != (CollectionArtifact{UUID: testArtifactUUID, Version: 1}) {
		t.Fatalf("collection 2: %+v", added)
	}
	raw, _ := os.ReadFile(filepath.Join(releaseDir, "collections", "2.yaml"))
	if !strings.Contains(string(raw), "createdDate:") || strings.Contains(string(raw), "\ndate:") {
		t.Fatalf("collection 2 is not in the TEA 1.0.0 shape:\n%s", raw)
	}

	// The same revision again changes nothing.
	if err := addArtifactToReleases(dir, testArtifactUUID, []string{testComponentUUID}, []string{testReleaseUUID}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(releaseDir, "collections", "3.yaml")); !os.IsNotExist(err) {
		t.Fatal("re-adding the same revision wrote a new collection")
	}

	// A newer revision replaces the older one.
	writeArtifactRevision(t, dir, 2)
	if err := addArtifactToReleases(dir, testArtifactUUID, []string{"Widget Lib"}, []string{"1.95"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	updated := readTestCollection(t, releaseDir, 3)
	if updated.UpdateReason.Type != "ARTIFACT_UPDATED" || len(updated.Artifacts) != 1 || updated.Artifacts[0].Version != 2 {
		t.Fatalf("collection 3: %+v", updated)
	}
}

func TestOolongArtifactRefsLayout(t *testing.T) {
	dir, _ := oolongTestContent(t)
	refs, err := artifactRefs(dir, []string{strings.ToUpper(testArtifactUUID)})
	if err != nil || len(refs) != 1 || refs[0] != (CollectionArtifact{UUID: testArtifactUUID, Version: 1}) {
		t.Fatalf("refs %+v, %v", refs, err)
	}
	if _, err := artifactRefs(dir, []string{"99999999-9999-4999-8999-999999999999"}); err == nil {
		t.Error("missing artifact accepted")
	}
	legacy := "77777777-7777-4777-8777-777777777777"
	if err := os.WriteFile(filepath.Join(dir, "artifacts", legacy+".yaml"), []byte("name: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := artifactRefs(dir, []string{legacy}); err == nil || !strings.Contains(err.Error(), "pre-TEA 1.0.0 layout") {
		t.Errorf("legacy artifact: %v", err)
	}
}

func TestOolongOptionalFieldsAreOmitted(t *testing.T) {
	// The Oolong loader rejects an empty string where it expects a URI, and an empty checksum list.
	out, err := yaml.Marshal(Distribution{DistributionId: "66666666-6666-4666-8666-666666666666", Url: "https://example.com/x.jar"})
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"signatureUrl", "checksums", "description", "identifiers", "distributionType"} {
		if strings.Contains(string(out), field) {
			t.Errorf("empty %s written:\n%s", field, out)
		}
	}
}
