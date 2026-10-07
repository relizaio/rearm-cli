package bomscore

import (
	"bytes"
	"encoding/xml"
	"strconv"
	"strings"

	cdx "github.com/CycloneDX/cyclonedx-go"
)

// xmlSignatureElement is the local name of an XML signature (XML-DSig) under the root.
const xmlSignatureElement = "Signature"

func loadCDXJSON(data []byte) (*Doc, error) {
	bom := new(cdx.BOM)
	if err := cdx.NewBOMDecoder(bytes.NewReader(data), cdx.BOMFileFormatJSON).Decode(bom); err != nil {
		return nil, unparsable(err)
	}
	if bom.SpecVersion == 0 {
		return nil, unsupported(reasonCDXSpecVersion)
	}
	// JSON: the top-level signature only; one on a component or service does not count.
	return cdxDoc(bom, SerializationJSON, bom.Signature != nil), nil
}

func loadCDXXML(data []byte) (*Doc, error) {
	bom := new(cdx.BOM)
	if err := cdx.NewBOMDecoder(bytes.NewReader(data), cdx.BOMFileFormatXML).Decode(bom); err != nil {
		return nil, unparsable(err)
	}
	// The decoder sets the version from the root namespace and leaves it zero for an unknown one.
	if bom.SpecVersion == 0 {
		return nil, unsupported(reasonCDXSpecVersion)
	}
	signed, err := xmlRootSignature(data)
	if err != nil {
		return nil, unparsable(err)
	}
	return cdxDoc(bom, SerializationXML, signed), nil
}

// xmlRootSignature: cyclonedx-go drops BOM.Signature for XML (xml:"-") and the CycloneDX XSD
// declares no signature element, so an XML signature sits in the root's extension point. It is
// present when a direct child of the root bom element has local name Signature and a namespace
// other than the document's CycloneDX namespace (the CycloneDX tools use XML-DSig,
// http://www.w3.org/2000/09/xmldsig#). Presence only; the signature is not verified.
func xmlRootSignature(data []byte) (bool, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	depth := 0
	rootSpace := ""
	for {
		tok, err := dec.Token()
		if err != nil {
			return false, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			if depth == 1 {
				rootSpace = t.Name.Space
			} else if depth == 2 && t.Name.Local == xmlSignatureElement && t.Name.Space != rootSpace {
				return true, nil
			}
		case xml.EndElement:
			depth--
			if depth == 0 {
				return false, nil
			}
		}
	}
}

func cdxDoc(bom *cdx.BOM, s Serialization, signed bool) *Doc {
	d := &Doc{
		Format:                 FormatCycloneDX,
		SpecVersion:            bom.SpecVersion.String(),
		Serialization:          s,
		SBOMVersion:            bom.Version,
		SignaturePresent:       signed,
		DeclaredNoDependencies: map[string]bool{},
		NotRepresentable:       map[Field]bool{},
	}
	if m := bom.Metadata; m != nil {
		d.Timestamp = m.Timestamp
		if m.Authors != nil {
			for _, a := range *m.Authors {
				d.Authors = append(d.Authors, a.Name)
			}
		}
		// metadata.manufacturer is the organization that created the BOM. The deprecated
		// metadata.manufacture (the maker of the described component) and metadata.supplier do
		// not count as the SBOM author.
		if m.Manufacturer != nil {
			d.Authors = append(d.Authors, m.Manufacturer.Name)
		}
		if m.Tools != nil {
			d.Tools = cdxTools(m.Tools)
		}
		if m.Lifecycles != nil {
			for _, l := range *m.Lifecycles {
				if isPresent(string(l.Phase)) {
					d.Lifecycles = append(d.Lifecycles, string(l.Phase))
				} else if isPresent(l.Name) {
					d.Lifecycles = append(d.Lifecycles, l.Name)
				}
			}
		}
		if m.Component != nil {
			d.SubjectNamed = true
			if strings.TrimSpace(m.Component.BOMRef) != "" {
				d.SubjectRefs = []string{m.Component.BOMRef}
			}
			// The subject and its parts are bom-refs a dependency may name, not components of
			// the set.
			d.addCDXComponentRefs([]cdx.Component{*m.Component})
		}
	}
	if bom.Components != nil {
		d.addCDXComponents(*bom.Components)
	}
	if bom.Services != nil {
		d.addCDXServiceRefs(*bom.Services)
	}
	if bom.Dependencies != nil {
		for _, dep := range *bom.Dependencies {
			e := Dependency{Ref: dep.Ref}
			if dep.Dependencies != nil {
				e.DependsOn = append(e.DependsOn, *dep.Dependencies...)
			}
			d.Dependencies = append(d.Dependencies, e)
		}
	}
	if bom.Compositions != nil {
		// Only aggregate complete states that a listed component's dependencies are all known;
		// incomplete, unknown, not_specified or any other value states nothing.
		for _, c := range *bom.Compositions {
			if c.Aggregate != cdx.CompositionAggregateComplete || c.Dependencies == nil {
				continue
			}
			for _, ref := range *c.Dependencies {
				d.DeclaredNoDependencies[string(ref)] = true
			}
		}
	}
	return d
}

// cdxTools is the union of the legacy tools array, tools.components and tools.services.
func cdxTools(tc *cdx.ToolsChoice) []Tool {
	var tools []Tool
	if tc.Tools != nil {
		for _, t := range *tc.Tools {
			tools = append(tools, Tool{Name: t.Name, Version: t.Version})
		}
	}
	if tc.Components != nil {
		for _, c := range *tc.Components {
			tools = append(tools, Tool{Name: c.Name, Version: c.Version})
		}
	}
	if tc.Services != nil {
		for _, s := range *tc.Services {
			tools = append(tools, Tool{Name: s.Name, Version: s.Version})
		}
	}
	return tools
}

// addCDXComponents flattens components depth first, nested ones after their parent.
func (d *Doc) addCDXComponents(components []cdx.Component) {
	for i := range components {
		c := &components[i]
		d.Components = append(d.Components, cdxComp(c, len(d.Components)+1))
		if c.BOMRef != "" {
			d.Refs = append(d.Refs, c.BOMRef)
		}
		if c.Components != nil {
			d.addCDXComponents(*c.Components)
		}
	}
}

// addCDXComponentRefs records the bom-refs of components outside the component set.
func (d *Doc) addCDXComponentRefs(components []cdx.Component) {
	for i := range components {
		if components[i].BOMRef != "" {
			d.Refs = append(d.Refs, components[i].BOMRef)
		}
		if components[i].Components != nil {
			d.addCDXComponentRefs(*components[i].Components)
		}
	}
}

// addCDXServiceRefs records the bom-refs of services, nested ones included.
func (d *Doc) addCDXServiceRefs(services []cdx.Service) {
	for i := range services {
		if services[i].BOMRef != "" {
			d.Refs = append(d.Refs, services[i].BOMRef)
		}
		if services[i].Services != nil {
			d.addCDXServiceRefs(*services[i].Services)
		}
	}
}

func cdxComp(c *cdx.Component, position int) Comp {
	comp := Comp{Ref: c.BOMRef, Name: c.Name, Version: c.Version, IsFile: c.Type == cdx.ComponentTypeFile}
	if c.Manufacturer != nil {
		comp.Producers = append(comp.Producers, c.Manufacturer.Name)
	}
	if c.Supplier != nil {
		comp.Producers = append(comp.Producers, c.Supplier.Name)
	}
	if c.Authors != nil {
		for _, a := range *c.Authors {
			comp.Producers = append(comp.Producers, a.Name)
		}
	}
	comp.Producers = append(comp.Producers, c.Author)

	comp.Identifiers = append(comp.Identifiers, Identifier{Kind: IdentifierPURL, Value: c.PackageURL}, Identifier{Kind: IdentifierCPE, Value: c.CPE})
	if c.SWID != nil {
		comp.Identifiers = append(comp.Identifiers, Identifier{Kind: IdentifierSWID, Value: c.SWID.TagID})
	}
	if c.SWHID != nil {
		for _, v := range *c.SWHID {
			comp.Identifiers = append(comp.Identifiers, Identifier{Kind: IdentifierSWHID, Value: v})
		}
	}
	if c.OmniborID != nil {
		for _, v := range *c.OmniborID {
			comp.Identifiers = append(comp.Identifiers, Identifier{Kind: IdentifierOmniborID, Value: v})
		}
	}
	if c.Hashes != nil {
		for _, h := range *c.Hashes {
			comp.Hashes = append(comp.Hashes, Hash{Algorithm: string(h.Algorithm), Value: h.Value})
		}
	}
	if c.Licenses != nil {
		for _, l := range *c.Licenses {
			if l.License != nil {
				comp.Licenses = append(comp.Licenses, l.License.ID, l.License.Name)
			}
			comp.Licenses = append(comp.Licenses, l.Expression)
		}
	}
	if c.Properties != nil {
		for _, p := range *c.Properties {
			comp.Properties = append(comp.Properties, Property{Name: p.Name, Value: p.Value})
		}
	}
	comp.DisplayID = displayID(c.PackageURL, c.Name, c.Version, c.BOMRef, position)
	return comp
}

// displayID: purl, else name@version (the name alone when there is no version), else the bom-ref
// or SPDXID, else "#<position>" in the component set.
func displayID(purl, name, version, ref string, position int) string {
	switch {
	case isPresent(purl):
		return purl
	case isPresent(name) && isPresent(version):
		return name + "@" + version
	case isPresent(name):
		return name
	case ref != "":
		return ref
	default:
		return "#" + strconv.Itoa(position)
	}
}
