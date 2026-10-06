package bomscore

import (
	"bufio"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"strconv"
	"strings"

	"sigs.k8s.io/yaml"
)

// RefusalKind tells a refused input (format or version not supported) from an accepted one the
// library could not read.
type RefusalKind string

const (
	RefusalUnsupported RefusalKind = "unsupported SBOM"
	RefusalUnparsable  RefusalKind = "unparsable SBOM"
)

// The fixed reasons of an unsupported input.
const (
	reasonEmpty          = "empty input"
	reasonNotSBOM        = "not a CycloneDX or SPDX document"
	reasonCDXSpecVersion = "CycloneDX specVersion missing or not 1.0 to 1.7"
	reasonSPDX3          = "SPDX 3 is not supported"
	reasonSPDXRDF        = "SPDX RDF/XML is not supported"
)

// RefusalError is returned by Load and Score for an input that is not scored. Its text is the one
// line the command prints: "unsupported SBOM: <reason>" or "unparsable SBOM: <library error>".
type RefusalError struct {
	Kind   RefusalKind
	Reason string
}

func (e *RefusalError) Error() string { return string(e.Kind) + ": " + e.Reason }

func unsupported(reason string) error {
	return &RefusalError{Kind: RefusalUnsupported, Reason: reason}
}

func unparsable(err error) error {
	return &RefusalError{Kind: RefusalUnparsable, Reason: err.Error()}
}

func spdxVersionUnsupported(version string, s Serialization) error {
	return unsupported("SPDX " + version + " is not supported in " + string(s))
}

// Local names of the XML root elements Load tells apart.
const (
	xmlRootCycloneDX = "bom"
	xmlRootRDF       = "RDF"
)

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// Load detects the format of input, refuses what is not supported and returns the Doc.
func Load(input []byte) (*Doc, error) {
	data := bytes.TrimPrefix(input, utf8BOM)
	trimmed := bytes.TrimLeft(data, " \t\r\n")
	if len(trimmed) == 0 {
		return nil, unsupported(reasonEmpty)
	}
	switch trimmed[0] {
	case '{':
		return loadJSON(data)
	case '<':
		return loadXML(data)
	default:
		return loadTextual(data)
	}
}

func loadJSON(data []byte) (*Doc, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		if bytes.Contains(data, []byte(`"bomFormat"`)) || bytes.Contains(data, []byte(`"spdxVersion"`)) {
			return nil, unparsable(err)
		}
		return nil, unsupported(reasonNotSBOM)
	}
	if raw, ok := top["bomFormat"]; ok {
		var bomFormat string
		if json.Unmarshal(raw, &bomFormat) == nil && bomFormat == string(FormatCycloneDX) {
			var specVersion string
			if err := json.Unmarshal(top["specVersion"], &specVersion); err != nil || !cdxSpecVersions[specVersion] {
				return nil, unsupported(reasonCDXSpecVersion)
			}
			return loadCDXJSON(data)
		}
	}
	if raw, ok := top["spdxVersion"]; ok {
		version, ok := spdxVersionOf(raw)
		if !ok {
			return nil, spdxVersionUnsupported(version, SerializationJSON)
		}
		return loadSPDX(data, version, SerializationJSON)
	}
	if _, ok := top["@context"]; ok {
		return nil, unsupported(reasonSPDX3)
	}
	return nil, unsupported(reasonNotSBOM)
}

// cdxSpecVersions are the CycloneDX versions cyclonedx-go v0.11.0 reads.
var cdxSpecVersions = map[string]bool{
	"1.0": true, "1.1": true, "1.2": true, "1.3": true, "1.4": true, "1.5": true, "1.6": true, "1.7": true,
}

var spdxVersions = map[string]bool{"2.1": true, "2.2": true, "2.3": true}

// spdxVersionOf reads a JSON spdxVersion value: the version for the refusal message and whether it
// is one of 2.1, 2.2, 2.3. A value that is not a string is named as written.
func spdxVersionOf(raw json.RawMessage) (string, bool) {
	var s string
	if trimmed := bytes.TrimSpace(raw); len(trimmed) == 0 || trimmed[0] != '"' || json.Unmarshal(trimmed, &s) != nil {
		return "version " + string(trimmed), false
	}
	return spdxVersionText(s)
}

// spdxVersionText: the text after "SPDX-" and whether it is a supported version. A value without
// the prefix is not an SPDX version string and is named quoted ("SPDX version \"2.3\" is not
// supported in json"), so the refusal does not read as if 2.3 were refused.
func spdxVersionText(s string) (string, bool) {
	s = strings.TrimSpace(s)
	v, found := strings.CutPrefix(s, "SPDX-")
	if !found {
		return "version " + strconv.Quote(s), false
	}
	return v, spdxVersions[v]
}

func loadXML(data []byte) (*Doc, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, unsupported(reasonNotSBOM)
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch start.Name.Local {
		case xmlRootCycloneDX:
			return loadCDXXML(data)
		case xmlRootRDF:
			return nil, unsupported(reasonSPDXRDF)
		default:
			return nil, unsupported(reasonNotSBOM)
		}
	}
}

// loadTextual: SPDX tag-value when a line starts with "SPDXVersion:", else SPDX YAML when the text
// parses as YAML with a top-level spdxVersion.
func loadTextual(data []byte) (*Doc, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), len(data)+1)
	for scanner.Scan() {
		if value, found := strings.CutPrefix(scanner.Text(), "SPDXVersion:"); found {
			version, ok := spdxVersionText(value)
			if !ok {
				return nil, spdxVersionUnsupported(version, SerializationTagValue)
			}
			return loadSPDX(data, version, SerializationTagValue)
		}
	}
	var top map[string]any
	if err := yaml.Unmarshal(data, &top); err != nil {
		return nil, unsupported(reasonNotSBOM)
	}
	raw, ok := top["spdxVersion"]
	if !ok {
		return nil, unsupported(reasonNotSBOM)
	}
	s, isString := raw.(string)
	if !isString {
		b, _ := json.Marshal(raw)
		return nil, spdxVersionUnsupported("version "+string(b), SerializationYAML)
	}
	version, supported := spdxVersionText(s)
	if !supported {
		return nil, spdxVersionUnsupported(version, SerializationYAML)
	}
	return loadSPDX(data, version, SerializationYAML)
}
