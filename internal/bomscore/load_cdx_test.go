package bomscore

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
)

var xmlToolsBlock = regexp.MustCompile(`(?s)<tools>.*?</tools>`)
var xmlSignatureBlock = regexp.MustCompile(`(?s)\s*<ds:Signature .*?</ds:Signature>`)

func loadOK(t *testing.T, data []byte) *Doc {
	t.Helper()
	d, err := Load(data)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return d
}

// Test 17: the deprecated metadata.manufacture names the maker of the described component, not
// the SBOM's author, and does not count.
func TestLegacyManufactureIsNotTheAuthor(t *testing.T) {
	m := jsonFixture(t, "full.cdx.json")
	m["specVersion"] = "1.5"
	md := cdxMetadata(m)
	delete(md, "authors")
	delete(md, "manufacturer")
	md["manufacture"] = map[string]any{"name": "Example Devices Inc."}
	r := scoreOK(t, encode(t, m))
	for _, id := range []string{"cisa-2026.sbom-author", "ntia-2021.author-of-sbom-data", "fda.baseline.author-name"} {
		assertStatus(t, r, id, StatusFail)
	}
}

// Test 18, CycloneDX part: the legacy tools form in XML gives the tool list of the 1.4 JSON
// fixture; a tool without a version fails the version only; no tools fails both.
func TestCycloneDXTools(t *testing.T) {
	legacyXML := xmlToolsBlock.ReplaceAllString(string(readFixture(t, "full.cdx.xml")),
		"<tools><tool><vendor>Example</vendor><name>example-scanner</name><version>4.2.0</version></tool></tools>")
	fromXML := loadOK(t, []byte(legacyXML)).Tools
	from14 := loadOK(t, readFixture(t, "full-1.4.cdx.json")).Tools
	if len(fromXML) != 1 || !reflect.DeepEqual(fromXML, from14) {
		t.Errorf("legacy XML tools %+v, 1.4 JSON tools %+v, want one and the same", fromXML, from14)
	}

	m := jsonFixture(t, "full.cdx.json")
	tools := cdxMetadata(m)["tools"].(map[string]any)
	tools["components"] = append(tools["components"].([]any), map[string]any{"type": "application", "name": "second-tool"})
	r := scoreOK(t, encode(t, m), ProfileCISA2026)
	assertStatus(t, r, "cisa-2026.sbom-tool-version", StatusFail)
	assertStatus(t, r, "cisa-2026.sbom-tool-name", StatusPass)

	delete(cdxMetadata(m), "tools")
	r = scoreOK(t, encode(t, m), ProfileCISA2026)
	assertStatus(t, r, "cisa-2026.sbom-tool-version", StatusFail)
	assertStatus(t, r, "cisa-2026.sbom-tool-name", StatusFail)
}

// Test 21: the XML signature is a root-level Signature element in a foreign namespace; a
// signature inside a component (XML or JSON) does not count.
func TestXMLSignature(t *testing.T) {
	full := string(readFixture(t, "full.cdx.xml"))
	assertStatus(t, scoreOK(t, []byte(full), ProfileCISA2026), "cisa-2026.sbom-author-signature", StatusPass)

	signature := xmlSignatureBlock.FindString(full)
	if signature == "" {
		t.Fatal("fixture has no root-level ds:Signature")
	}
	unsigned := strings.Replace(full, signature, "", 1)
	assertStatus(t, scoreOK(t, []byte(unsigned), ProfileCISA2026), "cisa-2026.sbom-author-signature", StatusFail)

	inComponent := strings.Replace(unsigned, "<name>alpha</name>", "<name>alpha</name>"+signature, 1)
	if inComponent == unsigned {
		t.Fatal("could not move the signature into a component")
	}
	assertStatus(t, scoreOK(t, []byte(inComponent), ProfileCISA2026), "cisa-2026.sbom-author-signature", StatusFail)

	// A root-level Signature in the CycloneDX namespace is not a signature either.
	cdxNamespace := strings.Replace(full, signature, "<Signature>not a signature</Signature>", 1)
	assertStatus(t, scoreOK(t, []byte(cdxNamespace), ProfileCISA2026), "cisa-2026.sbom-author-signature", StatusFail)

	m := jsonFixture(t, "full.cdx.json")
	sig := m["signature"]
	delete(m, "signature")
	m["components"].([]any)[0].(map[string]any)["signature"] = sig
	assertStatus(t, scoreOK(t, encode(t, m), ProfileCISA2026), "cisa-2026.sbom-author-signature", StatusFail)
}

func TestCycloneDXXMLReadsTheSameDocAsJSON(t *testing.T) {
	fromJSON := loadOK(t, readFixture(t, "full.cdx.json"))
	fromXML := loadOK(t, readFixture(t, "full.cdx.xml"))
	fromJSON.Serialization = fromXML.Serialization
	if !reflect.DeepEqual(fromJSON, fromXML) {
		t.Errorf("XML Doc differs from JSON Doc:\njson %+v\nxml  %+v", fromJSON, fromXML)
	}
}
