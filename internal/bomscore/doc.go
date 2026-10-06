// Package bomscore scores one SBOM file against the CISA 2026 minimum elements, the NTIA 2021
// minimum elements and the FDA premarket SBOM content, and writes a versioned, deterministic report.
//
// The engine is stateless: Score reads the input bytes, Load turns them into a format-neutral Doc
// (load_cdx.go and load_spdx.go are the only files that import the two SBOM libraries), and each
// profile is an ordered list of declared checks (check.go) whose evaluators (fields.go) read the
// Doc and nothing else. No I/O, no clock, no network.
package bomscore

// Format is the SBOM standard of the input.
type Format string

const (
	FormatCycloneDX Format = "CycloneDX"
	FormatSPDX      Format = "SPDX"
)

// Serialization is how the input was written.
type Serialization string

const (
	SerializationJSON     Serialization = "json"
	SerializationXML      Serialization = "xml"
	SerializationYAML     Serialization = "yaml"
	SerializationTagValue Serialization = "tag-value"
)

// Field names a data field that one input format may have no place for. A field in
// Doc.NotRepresentable fails its checks with the note notRepresentableNote gives.
type Field string

const (
	FieldSignature         Field = "signature"
	FieldGenerationContext Field = "generation-context"
	FieldSBOMVersion       Field = "sbom-version"
	FieldSupportLevel      Field = "support-level"
	FieldEndOfSupport      Field = "end-of-support"
)

// Tool is one tool that produced the SBOM. For SPDX the Creator text "Tool: name-1.0" is split at
// its last "-"; Version is empty when either side of that "-" is empty or there is none.
type Tool struct {
	Name    string
	Version string
}

// IdentifierKind is the CycloneDX field of an identifier, or the SPDX external reference type.
type IdentifierKind string

const (
	IdentifierPURL      IdentifierKind = "purl"
	IdentifierCPE       IdentifierKind = "cpe"
	IdentifierSWID      IdentifierKind = "swid"
	IdentifierSWHID     IdentifierKind = "swhid"
	IdentifierOmniborID IdentifierKind = "omniborId"
	// SPDX external reference types; purl is shared with CycloneDX.
	IdentifierCPE22  IdentifierKind = "cpe22Type"
	IdentifierCPE23  IdentifierKind = "cpe23Type"
	IdentifierSWH    IdentifierKind = "swh"
	IdentifierGitoid IdentifierKind = "gitoid"
)

// Identifier is one unique identifier of a component.
type Identifier struct {
	Kind  IdentifierKind
	Value string
}

// Hash is one component hash.
type Hash struct {
	Algorithm string
	Value     string
}

// Property is one CycloneDX component property.
type Property struct {
	Name  string
	Value string
}

// Comp is one component of the component set: every CycloneDX component, nested ones included,
// without metadata.component; every SPDX package except the ones the document describes.
type Comp struct {
	DisplayID   string // purl, else name@version (name alone without a version), else bom-ref / SPDXID, else #<position>
	Ref         string // CycloneDX bom-ref, SPDX SPDXID
	Name        string
	Version     string
	Producers   []string // CycloneDX manufacturer, supplier, authors, author; SPDX supplier, originator
	Identifiers []Identifier
	Hashes      []Hash
	Licenses    []string // CycloneDX license id, name or expression; SPDX licenseDeclared, licenseConcluded
	Properties  []Property
	ValidUntil  string // SPDX 2.3 validUntilDate
}

// Dependency is one CycloneDX dependencies[] entry.
type Dependency struct {
	Ref       string
	DependsOn []string
}

// Doc is the format-neutral model the checks read.
type Doc struct {
	Format        Format
	SpecVersion   string // "1.6", "2.3": no "SPDX-" prefix
	Serialization Serialization

	Authors          []string // CycloneDX metadata.authors[].name and metadata.manufacturer.name; SPDX Person and Organization creators
	Tools            []Tool
	Timestamp        string
	Lifecycles       []string // CycloneDX metadata.lifecycles[] phase or name
	SBOMVersion      int
	SignaturePresent bool
	// HasDependencies: CycloneDX dependencies[] has an entry; SPDX has a dependency relationship
	// between packages of the document (or from a package to NONE).
	HasDependencies bool

	Components []Comp

	// CycloneDX only, for the structure checks.
	Dependencies []Dependency
	Refs         []string // every bom-ref of a component (metadata.component and its parts included) or service

	NotRepresentable map[Field]bool
}

func (d *Doc) notRepresentable(f Field) bool {
	return d.NotRepresentable[f]
}
