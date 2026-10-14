package bomscore

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

// Design SCORE-38: the exportreleasebom stderr notice reads the reliza:export:* stamp on
// metadata.properties by exact name and exact member key, the last occurrence wins, and the signed
// line needs a root signature naming the stamped keyId. Every case asserts the whole line, spelled
// out as the design writes it.

const (
	noticeKey1    = "00000000-0000-4000-8000-000000000001"
	noticeKey2    = "00000000-0000-4000-8000-000000000002"
	lineNoKey     = "not signed: no organization SBOM signing key is configured (Organization settings, SBOM export signing)"
	lineSigning   = "not signed: signing failed on the server (SIGNING_ERROR)"
	lineSigned1   = "signed with organization key " + noticeKey1
	lineNoKeyID   = "signing record inconsistent: reliza:export:signed is true but reliza:export:signatureKeyId is missing"
	lineNoRootSig = "signing record inconsistent: reliza:export:signed is true but the document has no root signature"
	linePrefixMis = "signing record inconsistent: reliza:export:signatureKeyId is "

	nameSigned = "reliza:export:signed"
	nameKeyID  = "reliza:export:signatureKeyId"
	nameReason = "reliza:export:unsignedReason"
)

// q is a JSON string literal.
func q(t *testing.T, s string) string {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal %q: %v", s, err)
	}
	return string(b)
}

// propKV is a property object with the given member keys, name and raw JSON value.
func propKV(t *testing.T, nameKey, name, valueKey, rawValue string) string {
	return "{" + q(t, nameKey) + ":" + q(t, name) + "," + q(t, valueKey) + ":" + rawValue + "}"
}

// prop is a property object {"name":name,"value":value}.
func prop(t *testing.T, name, value string) string {
	return propKV(t, "name", name, "value", q(t, value))
}

// noticeDoc is a CycloneDX document whose metadata.properties holds the given entries (joined by
// commas); rest is appended to the root object (a root signature, components).
func noticeDoc(props []string, rest string) []byte {
	return []byte(`{"bomFormat":"CycloneDX","specVersion":"1.6","metadata":{"properties":[` +
		strings.Join(props, ",") + `]}` + rest + `}`)
}

// rootSig is a root signature member naming keyID.
func rootSig(t *testing.T, keyID string) string {
	return `,"signature":{"algorithm":"Ed25519","keyId":` + q(t, keyID) + `,"value":"AAAA"}`
}

func stampNoKey(t *testing.T) []string {
	return []string{prop(t, nameSigned, "false"), prop(t, nameReason, "NO_KEY")}
}

func stampSigned(t *testing.T, keyID string) []string {
	return []string{prop(t, nameSigned, "true"), prop(t, nameKeyID, keyID)}
}

func cat(parts ...[]string) []string {
	var out []string
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func wantNotice(t *testing.T, label string, doc []byte, want string) {
	t.Helper()
	if got := ExportSigningNotice(doc); got != want {
		t.Errorf("%s: notice %q, want %q", label, got, want)
	}
}

// Fixture mutation helpers over the jsf fixtures.

func metadataProps(t *testing.T, m map[string]any) []any {
	t.Helper()
	md, _ := m["metadata"].(map[string]any)
	props, ok := md["properties"].([]any)
	if !ok {
		t.Fatalf("fixture has no metadata.properties")
	}
	return props
}

func setFixtureProp(t *testing.T, m map[string]any, name string, value any) {
	t.Helper()
	for _, p := range metadataProps(t, m) {
		if e := p.(map[string]any); e["name"] == name {
			e["value"] = value
			return
		}
	}
	t.Fatalf("fixture has no property %s", name)
}

func dropFixtureProp(t *testing.T, m map[string]any, name string) {
	t.Helper()
	md := m["metadata"].(map[string]any)
	var kept []any
	for _, p := range metadataProps(t, m) {
		if p.(map[string]any)["name"] != name {
			kept = append(kept, p)
		}
	}
	md["properties"] = kept
}

// T-1
func TestExportSigningNoticeServedDocuments(t *testing.T) {
	if ExportPropSigned != nameSigned || ExportPropSignatureKeyID != nameKeyID || ExportPropUnsignedReason != nameReason {
		t.Fatalf("the property names are %q, %q, %q", ExportPropSigned, ExportPropSignatureKeyID, ExportPropUnsignedReason)
	}
	wantNotice(t, "signed-ed25519", readFixture(t, "jsf/signed-ed25519.cdx.json"), lineSigned1)
	wantNotice(t, "keyid-only", readFixture(t, "jsf/keyid-only.cdx.json"), lineSigned1)
	wantNotice(t, "unsigned", readFixture(t, "jsf/unsigned.cdx.json"), lineNoKey)
	wantNotice(t, "signed-ed25519 pretty", readFixture(t, "jsf/signed-ed25519.pretty.cdx.json"), lineSigned1)
	wantNotice(t, "signing error", noticeDoc([]string{prop(t, nameSigned, "false"), prop(t, nameReason, "SIGNING_ERROR")}, ""), lineSigning)
}

// The two Unicode letters Go's encoding/json folds onto ASCII when it matches struct fields (and
// strings.EqualFold does too), built from their UTF-8 bytes so the source stays ASCII.
var (
	longS  = string([]byte{0xc5, 0xbf})       // U+017F LATIN SMALL LETTER LONG S, folds to s
	kelvin = string([]byte{0xe2, 0x84, 0xaa}) // U+212A KELVIN SIGN, folds to k
)

func TestExportSigningNoticeFoldLettersAreWhatTheyClaim(t *testing.T) {
	if r, _ := utf8.DecodeRuneInString(longS); r != 0x017f || !strings.EqualFold(longS, "s") {
		t.Errorf("longS is %U", r)
	}
	if r, _ := utf8.DecodeRuneInString(kelvin); r != 0x212a || !strings.EqualFold(kelvin, "k") {
		t.Errorf("kelvin is %U", r)
	}
}

// nameVariants are look-alikes of a property name: none of them is the name.
func nameVariants(name string) map[string]string {
	segs := strings.Split(name, ":")
	for i, s := range segs {
		segs[i] = strings.ToUpper(s[:1]) + s[1:]
	}
	v := map[string]string{
		"leading space":        " " + name,
		"trailing space":       name + " ",
		"leading tab":          "\t" + name,
		"trailing newline":     name + "\n",
		"trailing no-break":    name + "\u00a0",
		"upper case":           strings.ToUpper(name),
		"capitalised":          strings.Join(segs, ":"),
		"last letter upper":    name[:len(name)-1] + strings.ToUpper(name[len(name)-1:]),
		"x- prefix":            "x-" + name,
		"-x suffix":            name + "-x",
		"trailing colon":       name + ":",
		"dots for colons":      strings.ReplaceAll(name, ":", "."),
		"long s for s":         strings.Replace(name, "s", longS, 1),
		"long s for every s":   strings.ReplaceAll(name, "s", longS),
		"x prefix no dash":     "x" + name,
		"leading and trailing": " " + name + " ",
	}
	if strings.ContainsAny(name, "kK") {
		v["kelvin for k"] = strings.NewReplacer("k", kelvin, "K", kelvin).Replace(name)
	}
	return v
}

// T-2
func TestExportSigningNoticeExactNames(t *testing.T) {
	// A forged value per name, chosen so a loose match would change the line.
	forged := map[string]string{nameSigned: "true", nameKeyID: "forged", nameReason: "SIGNING_ERROR"}
	for _, name := range []string{nameSigned, nameKeyID, nameReason} {
		for label, variant := range nameVariants(name) {
			if variant == name {
				t.Fatalf("%s %s: the variant is the name", name, label)
			}
			fake := prop(t, variant, forged[name])
			l := name + " " + label
			wantNotice(t, l+", before the NO_KEY stamp", noticeDoc(cat([]string{fake}, stampNoKey(t)), ""), lineNoKey)
			wantNotice(t, l+", after the NO_KEY stamp", noticeDoc(cat(stampNoKey(t), []string{fake}), ""), lineNoKey)
			wantNotice(t, l+", after the signed stamp", noticeDoc(cat(stampSigned(t, noticeKey1), []string{fake}), rootSig(t, noticeKey1)), lineSigned1)
			if name == nameSigned {
				wantNotice(t, l+", alone", noticeDoc([]string{fake}, rootSig(t, noticeKey1)), "")
			}
		}
	}

	// The member keys name and value are read by exact key too.
	for _, k := range []struct{ nameKey, valueKey string }{
		{"Name", "Value"}, {"NAME", "VALUE"}, {"nAME", "vALUE"}, {"name ", "value "}, {" name", " value"},
		{"Name", "value"}, {"name", "Value"}, {"name", "VALUE"}, {"name", "valu" + "e\u00a0"},
	} {
		l := k.nameKey + "/" + k.valueKey
		forgedSigned := propKV(t, k.nameKey, nameSigned, k.valueKey, `"true"`)
		wantNotice(t, l+" signed before the NO_KEY stamp", noticeDoc(cat([]string{forgedSigned}, stampNoKey(t)), ""), lineNoKey)
		// With the exact name key the entry is the property, without a value: it reads as "" and,
		// being last, replaces the stamp's value; with a look-alike name key it is no property.
		wantReason, wantKey := lineNoKey, lineSigned1
		if k.nameKey == "name" {
			wantReason, wantKey = "not signed", lineNoKeyID
		}
		forgedReason := propKV(t, k.nameKey, nameReason, k.valueKey, `"SIGNING_ERROR"`)
		wantNotice(t, l+" reason after the NO_KEY stamp", noticeDoc(cat(stampNoKey(t), []string{forgedReason}), ""), wantReason)
		forgedKey := propKV(t, k.nameKey, nameKeyID, k.valueKey, `"forged"`)
		wantNotice(t, l+" keyId after the signed stamp", noticeDoc(cat(stampSigned(t, noticeKey1), []string{forgedKey}), rootSig(t, noticeKey1)), wantKey)
		if k.nameKey != "name" {
			wantNotice(t, l+" signed alone", noticeDoc([]string{forgedSigned}, rootSig(t, noticeKey1)), "")
		}
	}
	// A property with the exact name but a look-alike value key holds no value: signed reads as
	// the empty string, the not-signed branch, and the NO_KEY reason before it stands.
	wantNotice(t, "exact name, Value key, after the NO_KEY stamp",
		noticeDoc(cat(stampNoKey(t), []string{propKV(t, "name", nameSigned, "Value", `"true"`)}), rootSig(t, noticeKey1)), lineNoKey)
	wantNotice(t, "Name/Value signed=true before the stamp (run 1)",
		noticeDoc(cat([]string{`{"Name":"reliza:export:signed","Value":"true"}`}, stampNoKey(t)), ""), lineNoKey)
	wantNotice(t, "Name/Value signed=true alone", noticeDoc([]string{`{"Name":"reliza:export:signed","Value":"true"}`}, ""), "")

	// Entries that are not objects, or whose name is not a string, are skipped.
	for _, e := range []string{`"reliza:export:signed"`, `null`, `[]`, `5`, `{"name":5,"value":"true"}`,
		`{"name":["reliza:export:signed"],"value":"true"}`, `{"name":null,"value":"true"}`, `{"value":"true"}`} {
		wantNotice(t, "entry "+e+" after the NO_KEY stamp", noticeDoc(cat(stampNoKey(t), []string{e}), ""), lineNoKey)
	}

	// The stamp is read on metadata.properties only.
	stamp := "[" + strings.Join(stampSigned(t, noticeKey1), ",") + "]"
	for label, doc := range map[string]string{
		"components[0].properties":      `{"metadata":{"properties":[]},"components":[{"name":"a","properties":` + stamp + `}]` + rootSig(t, noticeKey1) + `}`,
		"metadata.component.properties": `{"metadata":{"component":{"name":"a","properties":` + stamp + `}}` + rootSig(t, noticeKey1) + `}`,
		"services[0].properties":        `{"metadata":{},"services":[{"name":"a","properties":` + stamp + `}]` + rootSig(t, noticeKey1) + `}`,
		"root properties":               `{"metadata":{},"properties":` + stamp + rootSig(t, noticeKey1) + `}`,
	} {
		wantNotice(t, "stamp on "+label, []byte(doc), "")
	}

	// The tester's export of SCORE-33 run 1 (tests/SCORE-33/run-1-probe-out.tar.gz,
	// forged-nokey.cdx.json, copied byte for byte): a NO_KEY export carrying a forged
	// {"Name":...,"Value":"true"} entry, a capitalised name and a trailing-space name ahead of the
	// server's stamp, and a signature on a component. It printed "signed with organization key
	// <unknown>" in run 1.
	wantNotice(t, "notice/forged-nokey.cdx.json", readFixture(t, "notice/forged-nokey.cdx.json"), lineNoKey)
}

// T-3
func TestExportSigningNoticeLastOccurrenceWins(t *testing.T) {
	wantNotice(t, "signed first, NO_KEY last",
		noticeDoc(cat(stampSigned(t, noticeKey1), stampNoKey(t)), rootSig(t, noticeKey1)), lineNoKey)
	wantNotice(t, "NO_KEY first, signed last",
		noticeDoc(cat(stampNoKey(t), stampSigned(t, noticeKey1)), rootSig(t, noticeKey1)), lineSigned1)
	wantNotice(t, "keyId twice",
		noticeDoc([]string{prop(t, nameSigned, "true"), prop(t, nameKeyID, noticeKey1), prop(t, nameKeyID, "other")}, rootSig(t, noticeKey1)),
		"signing record inconsistent: reliza:export:signatureKeyId is other but the root signature names keyId "+noticeKey1)
	wantNotice(t, "keyId twice, matching last",
		noticeDoc([]string{prop(t, nameSigned, "true"), prop(t, nameKeyID, "other"), prop(t, nameKeyID, noticeKey1)}, rootSig(t, noticeKey1)), lineSigned1)
	wantNotice(t, "reason twice",
		noticeDoc([]string{prop(t, nameSigned, "false"), prop(t, nameReason, "NO_KEY"), prop(t, nameReason, "SIGNING_ERROR")}, ""), lineSigning)
	wantNotice(t, "reason twice, NO_KEY last",
		noticeDoc([]string{prop(t, nameSigned, "false"), prop(t, nameReason, "SIGNING_ERROR"), prop(t, nameReason, "NO_KEY")}, ""), lineNoKey)
	wantNotice(t, "signed true, false, true",
		noticeDoc([]string{prop(t, nameSigned, "true"), prop(t, nameSigned, "false"), prop(t, nameKeyID, noticeKey1), prop(t, nameSigned, "true")}, rootSig(t, noticeKey1)), lineSigned1)
	wantNotice(t, "signed false, true, false",
		noticeDoc([]string{prop(t, nameSigned, "false"), prop(t, nameSigned, "true"), prop(t, nameKeyID, noticeKey1), prop(t, nameSigned, "false")}, rootSig(t, noticeKey1)), "not signed")

	// Duplicate members of one JSON object: the last one is read, at every level.
	wantNotice(t, "property with two value members, false last",
		noticeDoc([]string{`{"name":"reliza:export:signed","value":"true","value":"false"}`, prop(t, nameReason, "NO_KEY")}, rootSig(t, noticeKey1)), lineNoKey)
	wantNotice(t, "property with two value members, true last",
		noticeDoc([]string{`{"name":"reliza:export:signed","value":"false","value":"true"}`, prop(t, nameKeyID, noticeKey1)}, rootSig(t, noticeKey1)), lineSigned1)
	wantNotice(t, "property with two name members, the stamp name first",
		noticeDoc(cat(stampNoKey(t), []string{`{"name":"reliza:export:signed","name":"x","value":"true"}`}), ""), lineNoKey)
	wantNotice(t, "two root signatures, matching last",
		noticeDoc(stampSigned(t, noticeKey1), rootSig(t, noticeKey2)+rootSig(t, noticeKey1)), lineSigned1)
	wantNotice(t, "two root signatures, other last",
		noticeDoc(stampSigned(t, noticeKey1), rootSig(t, noticeKey1)+rootSig(t, noticeKey2)),
		linePrefixMis+noticeKey1+" but the root signature names keyId "+noticeKey2)
	wantNotice(t, "signature with two keyId members",
		noticeDoc(stampSigned(t, noticeKey1), `,"signature":{"algorithm":"Ed25519","keyId":"`+noticeKey2+`","keyId":"`+noticeKey1+`","value":"AAAA"}`), lineSigned1)
	twoMetadata := `{"metadata":{"properties":[` + strings.Join(stampSigned(t, noticeKey1), ",") + `]},` +
		`"metadata":{"properties":[` + strings.Join(stampNoKey(t), ",") + `]}` + rootSig(t, noticeKey1) + `}`
	wantNotice(t, "two metadata members, NO_KEY last", []byte(twoMetadata), lineNoKey)
	twoProperties := `{"metadata":{"properties":[` + strings.Join(stampSigned(t, noticeKey1), ",") + `],` +
		`"properties":[` + strings.Join(stampNoKey(t), ",") + `]}` + rootSig(t, noticeKey1) + `}`
	wantNotice(t, "two properties members, NO_KEY last", []byte(twoProperties), lineNoKey)
}

// T-4
func TestExportSigningNoticeNeedsTheRootSignature(t *testing.T) {
	m := jsonFixture(t, "jsf/signed-ed25519.cdx.json")

	removed := clone(t, m)
	delete(removed, "signature")
	wantNotice(t, "signature removed", encode(t, removed), lineNoRootSig)

	null := clone(t, m)
	null["signature"] = nil
	wantNotice(t, "signature null", encode(t, null), lineNoRootSig)

	moved := clone(t, m)
	sig := moved["signature"]
	delete(moved, "signature")
	comps, _ := moved["components"].([]any)
	if len(comps) == 0 {
		t.Fatalf("fixture has no component")
	}
	comps[0].(map[string]any)["signature"] = sig
	wantNotice(t, "signature on components[0]", encode(t, moved), lineNoRootSig)

	upper := clone(t, m)
	upper["Signature"] = upper["signature"]
	delete(upper, "signature")
	wantNotice(t, "root member Signature", encode(t, upper), lineNoRootSig)

	wantNotice(t, "signed stamp, no signature anywhere", noticeDoc(stampSigned(t, noticeKey1), ""), lineNoRootSig)
	wantNotice(t, "the real root signature", encode(t, m), lineSigned1)
}

// T-5
func TestExportSigningNoticeKeyIdMismatch(t *testing.T) {
	m := jsonFixture(t, "jsf/signed-ed25519.cdx.json")

	other := clone(t, m)
	setFixtureProp(t, other, nameKeyID, "other")
	wantNotice(t, "property other", encode(t, other),
		"signing record inconsistent: reliza:export:signatureKeyId is other but the root signature names keyId "+noticeKey1)

	noKeyID := clone(t, m)
	delete(noKeyID["signature"].(map[string]any), "keyId")
	wantNotice(t, "signature without keyId", encode(t, noKeyID),
		"signing record inconsistent: reliza:export:signatureKeyId is "+noticeKey1+" but the root signature names no keyId")

	numKeyID := clone(t, m)
	numKeyID["signature"].(map[string]any)["keyId"] = 1
	wantNotice(t, "signature keyId a number", encode(t, numKeyID),
		"signing record inconsistent: reliza:export:signatureKeyId is "+noticeKey1+" but the root signature names no keyId")

	wantNotice(t, "signature a string", noticeDoc(stampSigned(t, noticeKey1), `,"signature":"`+noticeKey1+`"`),
		"signing record inconsistent: reliza:export:signatureKeyId is "+noticeKey1+" but the root signature names no keyId")

	wantNotice(t, "signers-two", readFixture(t, "jsf/signers-two.cdx.json"), lineSigned1)
	wantNotice(t, "chain", readFixture(t, "jsf/chain.cdx.json"), lineSigned1)

	swapped := jsonFixture(t, "jsf/signers-two.cdx.json")
	signers := swapped["signature"].(map[string]any)["signers"].([]any)
	signers[0], signers[1] = signers[1], signers[0]
	wantNotice(t, "signers-two swapped", encode(t, swapped),
		"signing record inconsistent: reliza:export:signatureKeyId is "+noticeKey1+" but the root signature names keyId "+noticeKey2)

	wantNotice(t, "malformed (no value), keyId matches", readFixture(t, "jsf/malformed.cdx.json"), lineSigned1)

	caseDiff := clone(t, m)
	caseDiff["signature"].(map[string]any)["keyId"] = "abcd-key"
	setFixtureProp(t, caseDiff, nameKeyID, "ABCD-KEY")
	wantNotice(t, "keyId differs in case", encode(t, caseDiff),
		"signing record inconsistent: reliza:export:signatureKeyId is ABCD-KEY but the root signature names keyId abcd-key")

	space := clone(t, m)
	setFixtureProp(t, space, nameKeyID, noticeKey1+" ")
	wantNotice(t, "keyId with a trailing space", encode(t, space),
		"signing record inconsistent: reliza:export:signatureKeyId is "+noticeKey1+"  but the root signature names keyId "+noticeKey1)

	kelvinKey := clone(t, m)
	kelvinKey["signature"].(map[string]any)["keyId"] = "key-" + kelvin
	setFixtureProp(t, kelvinKey, nameKeyID, "key-k")
	wantNotice(t, "keyId differs by a Kelvin sign", encode(t, kelvinKey),
		"signing record inconsistent: reliza:export:signatureKeyId is key-k but the root signature names keyId key-"+kelvin)
}

// T-6
func TestExportSigningNoticeMissingKeyId(t *testing.T) {
	for label, keyProp := range map[string][]string{
		"absent":    nil,
		"empty":     {prop(t, nameKeyID, "")},
		"a number":  {propKV(t, "name", nameKeyID, "value", "5")},
		"null":      {propKV(t, "name", nameKeyID, "value", "null")},
		"an object": {propKV(t, "name", nameKeyID, "value", `{"id":"`+noticeKey1+`"}`)},
		"no value":  {`{"name":"reliza:export:signatureKeyId"}`},
	} {
		props := cat([]string{prop(t, nameSigned, "true")}, keyProp)
		wantNotice(t, "keyId "+label+", no signature", noticeDoc(props, ""), lineNoKeyID)
		wantNotice(t, "keyId "+label+", with a root signature", noticeDoc(props, rootSig(t, noticeKey1)), lineNoKeyID)
	}
	noKey := jsonFixture(t, "jsf/signed-ed25519.cdx.json")
	dropFixtureProp(t, noKey, nameKeyID)
	wantNotice(t, "signed-ed25519 without the keyId property", encode(t, noKey), lineNoKeyID)
}

// T-7
func TestExportSigningNoticeNotSigned(t *testing.T) {
	for reason, want := range map[string]string{
		`"NO_KEY"`:        lineNoKey,
		`"SIGNING_ERROR"`: lineSigning,
		`"LATER"`:         "not signed: LATER",
		`""`:              "not signed",
		`5`:               "not signed",
		`null`:            "not signed",
		`"no_key"`:        "not signed: no_key",
		`" NO_KEY"`:       "not signed:  NO_KEY",
	} {
		wantNotice(t, "reason "+reason, noticeDoc([]string{prop(t, nameSigned, "false"), propKV(t, "name", nameReason, "value", reason)}, ""), want)
	}
	wantNotice(t, "no reason", noticeDoc([]string{prop(t, nameSigned, "false")}, ""), "not signed")

	// Only the exact string true is the signed branch, even with a matching keyId and root signature.
	for _, v := range []string{`"TRUE"`, `"True"`, `" true"`, `"true "`, `"yes"`, `"1"`, `""`, `true`, `1`, `null`, `{}`, `["true"]`, `"tru` + `e\u0000"`} {
		props := []string{propKV(t, "name", nameSigned, "value", v), prop(t, nameKeyID, noticeKey1)}
		wantNotice(t, "signed "+v, noticeDoc(props, rootSig(t, noticeKey1)), "not signed")
		wantNotice(t, "signed "+v+" with NO_KEY", noticeDoc(cat(props, []string{prop(t, nameReason, "NO_KEY")}), rootSig(t, noticeKey1)), lineNoKey)
	}
	wantNotice(t, "signed with no value member", noticeDoc([]string{`{"name":"reliza:export:signed"}`, prop(t, nameKeyID, noticeKey1)}, rootSig(t, noticeKey1)), "not signed")
	// The JSON escape of the exact string is the exact string.
	wantNotice(t, "signed spelt with JSON escapes", noticeDoc([]string{`{"n\u0061me":"reliza:export:sign\u0065d","value":"tru\u0065"}`, prop(t, nameKeyID, noticeKey1)}, rootSig(t, noticeKey1)), lineSigned1)
}

// T-8
func TestExportSigningNoticeNoStamp(t *testing.T) {
	for label, doc := range map[string]string{
		"keyId and reason, no signed": string(noticeDoc([]string{prop(t, nameKeyID, noticeKey1), prop(t, nameReason, "NO_KEY")}, rootSig(t, noticeKey1))),
		"properties empty":            string(noticeDoc(nil, rootSig(t, noticeKey1))),
		"properties absent":           `{"bomFormat":"CycloneDX","metadata":{}}`,
		"properties null":             `{"metadata":{"properties":null}}`,
		"properties an object":        `{"metadata":{"properties":{"name":"reliza:export:signed","value":"true"}}}`,
		"properties a string":         `{"metadata":{"properties":"reliza:export:signed"}}`,
		"metadata absent":             `{"bomFormat":"CycloneDX","properties":[{"name":"reliza:export:signed","value":"false"}]}`,
		"metadata null":               `{"metadata":null}`,
		"metadata a string":           `{"metadata":"reliza:export:signed"}`,
		"metadata an array":           `{"metadata":[{"properties":[{"name":"reliza:export:signed","value":"false"}]}]}`,
		"Metadata member":             `{"Metadata":{"properties":[{"name":"reliza:export:signed","value":"false"}]}}`,
		"Properties member":           `{"metadata":{"Properties":[{"name":"reliza:export:signed","value":"false"}]}}`,
		"root array":                  `[{"metadata":{"properties":[{"name":"reliza:export:signed","value":"false"}]}}]`,
		"null":                        `null`,
		"empty":                       ``,
		"not JSON":                    "name,version\nlib,1.0\n",
		"truncated":                   `{"metadata":{"properties":[{"name":"reliza:export:signed","value":"false"}]`,
		"XML": `<?xml version="1.0"?><bom xmlns="http://cyclonedx.org/schema/bom/1.6"><metadata><properties>` +
			`<property name="reliza:export:signed">false</property><property name="reliza:export:unsignedReason">NO_KEY</property>` +
			`</properties></metadata></bom>`,
	} {
		wantNotice(t, label, []byte(doc), "")
	}
}

// T-9
func TestExportSigningNoticePrintable(t *testing.T) {
	odd := "k\n1\u202e2\u200b3"
	const oddEscaped = `k\u000a1\u202e2\u200b3`
	got := ExportSigningNotice(noticeDoc(stampSigned(t, odd), rootSig(t, odd)))
	if got != "signed with organization key "+oddEscaped {
		t.Errorf("escaped keyId: %q", got)
	}
	got = ExportSigningNotice(noticeDoc(stampSigned(t, odd), rootSig(t, noticeKey1)))
	if got != linePrefixMis+oddEscaped+" but the root signature names keyId "+noticeKey1 {
		t.Errorf("escaped property keyId: %q", got)
	}
	got = ExportSigningNotice(noticeDoc(stampSigned(t, noticeKey1), rootSig(t, "s\u200bk\n")))
	if got != linePrefixMis+noticeKey1+` but the root signature names keyId s\u200bk\u000a` {
		t.Errorf("escaped signature keyId: %q", got)
	}
	got = ExportSigningNotice(noticeDoc([]string{prop(t, nameSigned, "false"), prop(t, nameReason, "LA\nTER\u202e")}, ""))
	if got != `not signed: LA\u000aTER\u202e` {
		t.Errorf("escaped reason: %q", got)
	}
	for _, line := range []string{
		ExportSigningNotice(noticeDoc(stampSigned(t, odd), rootSig(t, odd))),
		ExportSigningNotice(noticeDoc([]string{prop(t, nameSigned, "false"), prop(t, nameReason, "a\nb\rc")}, "")),
	} {
		if strings.ContainsAny(line, "\n\r") {
			t.Errorf("more than one line: %q", line)
		}
	}
}
