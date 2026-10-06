package bomscore

import (
	"bytes"
	"strings"

	"github.com/spdx/tools-golang/json"
	"github.com/spdx/tools-golang/spdx"
	"github.com/spdx/tools-golang/spdx/v2/common"
	"github.com/spdx/tools-golang/tagvalue"
	"github.com/spdx/tools-golang/yaml"
)

// SPDX creator types and the special value of a relationship end that states "none".
const (
	spdxCreatorPerson       = "Person"
	spdxCreatorOrganization = "Organization"
	spdxCreatorTool         = "Tool"
	spdxNone                = "NONE"
	spdxNoAssertion         = "NOASSERTION"
)

// spdxIdentifierTypes are the external reference types that identify a package.
var spdxIdentifierTypes = map[IdentifierKind]bool{
	IdentifierPURL: true, IdentifierCPE22: true, IdentifierCPE23: true, IdentifierSWH: true, IdentifierGitoid: true,
}

// spdxDependencyTypes are the relationship types that state a dependency.
var spdxDependencyTypes = map[string]bool{
	common.TypeRelationshipDependsOn:    true,
	common.TypeRelationshipDependencyOf: true,
	common.TypeRelationshipContains:     true,
	common.TypeRelationshipContainedBy:  true,
}

// loadSPDX reads SPDX 2.1 to 2.3 (version already gated) with the tools-golang reader of the
// serialization; every reader returns the v2_3 model.
func loadSPDX(data []byte, version string, s Serialization) (*Doc, error) {
	var (
		doc *spdx.Document
		err error
	)
	switch s {
	case SerializationJSON:
		doc, err = json.Read(bytes.NewReader(data))
	case SerializationYAML:
		doc, err = yaml.Read(bytes.NewReader(data))
	default:
		doc, err = tagvalue.Read(bytes.NewReader(data))
	}
	if err != nil {
		return nil, unparsable(err)
	}
	return spdxDoc(doc, version, s), nil
}

func spdxDoc(doc *spdx.Document, version string, s Serialization) *Doc {
	d := &Doc{
		Format:        FormatSPDX,
		SpecVersion:   version,
		Serialization: s,
		NotRepresentable: map[Field]bool{
			FieldSignature:         true,
			FieldGenerationContext: true,
			FieldSBOMVersion:       true,
			FieldSupportLevel:      true,
			// validUntilDate is new in SPDX 2.3.
			FieldEndOfSupport: version != "2.3",
		},
	}
	if ci := doc.CreationInfo; ci != nil {
		d.Timestamp = ci.Created
		for _, c := range ci.Creators {
			switch c.CreatorType {
			case spdxCreatorPerson, spdxCreatorOrganization:
				d.Authors = append(d.Authors, c.Creator)
			case spdxCreatorTool:
				d.Tools = append(d.Tools, spdxTool(c.Creator))
			}
		}
	}

	packages := map[common.ElementID]bool{}
	for _, p := range doc.Packages {
		if p != nil {
			packages[p.PackageSPDXIdentifier] = true
		}
	}
	described := map[common.ElementID]bool{}
	isDocument := func(id common.DocElementID) bool {
		return id.DocumentRefID == "" && id.SpecialID == "" && id.ElementRefID == doc.SPDXIdentifier
	}
	isPackage := func(id common.DocElementID) bool {
		return id.DocumentRefID == "" && id.SpecialID == "" && packages[id.ElementRefID]
	}
	isNone := func(id common.DocElementID) bool { return id.SpecialID == spdxNone }
	for _, r := range doc.Relationships {
		if r == nil {
			continue
		}
		switch {
		case r.Relationship == common.TypeRelationshipDescribe && isDocument(r.RefA) && isPackage(r.RefB):
			described[r.RefB.ElementRefID] = true
		case r.Relationship == common.TypeRelationshipDescribeBy && isDocument(r.RefB) && isPackage(r.RefA):
			described[r.RefA.ElementRefID] = true
		case spdxDependencyTypes[r.Relationship]:
			// Between packages of this document, or from a package to NONE (no dependencies). A
			// CONTAINS to a file (the readers fold hasFiles into such relationships) or an end of
			// NOASSERTION does not count.
			if (isPackage(r.RefA) && (isPackage(r.RefB) || isNone(r.RefB))) || (isNone(r.RefA) && isPackage(r.RefB)) {
				d.HasDependencies = true
			}
		}
	}

	for _, p := range doc.Packages {
		if p == nil || described[p.PackageSPDXIdentifier] {
			continue
		}
		comp := Comp{
			Ref:        common.RenderElementID(p.PackageSPDXIdentifier),
			Name:       p.PackageName,
			Version:    p.PackageVersion,
			ValidUntil: p.ValidUntilDate,
			Licenses:   []string{p.PackageLicenseDeclared, p.PackageLicenseConcluded},
		}
		if p.PackageSupplier != nil {
			comp.Producers = append(comp.Producers, p.PackageSupplier.Supplier)
		}
		if p.PackageOriginator != nil {
			comp.Producers = append(comp.Producers, p.PackageOriginator.Originator)
		}
		purl := ""
		for _, ref := range p.PackageExternalReferences {
			if ref == nil {
				continue
			}
			kind := IdentifierKind(ref.RefType)
			if !spdxIdentifierTypes[kind] {
				continue
			}
			comp.Identifiers = append(comp.Identifiers, Identifier{Kind: kind, Value: ref.Locator})
			if kind == IdentifierPURL && purl == "" {
				purl = ref.Locator
			}
		}
		for _, c := range p.PackageChecksums {
			comp.Hashes = append(comp.Hashes, Hash{Algorithm: string(c.Algorithm), Value: c.Value})
		}
		comp.DisplayID = displayID(purl, comp.Name, comp.Version, comp.Ref, len(d.Components)+1)
		d.Components = append(d.Components, comp)
	}
	return d
}

// spdxTool splits "Tool: toolidentifier-version" (SPDX 2.3, Creator) at its last "-". The
// version is empty unless both sides of that "-" are non-empty.
func spdxTool(text string) Tool {
	t := strings.TrimSpace(text)
	i := strings.LastIndex(t, "-")
	if i <= 0 || i == len(t)-1 {
		return Tool{Name: t}
	}
	return Tool{Name: t, Version: t[i+1:]}
}
