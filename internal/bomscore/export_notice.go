package bomscore

import "encoding/json"

// The document properties the server stamps on metadata.properties of a merged JSON export
// (SCORE-31 3.5, SCORE-33); exported so a test outside the package can spell them out.
const (
	ExportPropSigned         = "reliza:export:signed"
	ExportPropSignatureKeyID = "reliza:export:signatureKeyId"
	ExportPropUnsignedReason = "reliza:export:unsignedReason"
)

// The unsigned reasons the server writes.
const (
	exportUnsignedNoKey   = "NO_KEY"
	exportUnsignedSigning = "SIGNING_ERROR"
)

// The lines ExportSigningNotice returns; a value read from the document follows the prefix.
const (
	noticeSigned               = "signed with organization key "
	noticeNotSigned            = "not signed"
	noticeNotSignedNoKey       = "not signed: no organization SBOM signing key is configured (Organization settings, SBOM export signing)"
	noticeNotSignedSigning     = "not signed: signing failed on the server (SIGNING_ERROR)"
	noticeNotSignedReason      = "not signed: "
	noticeKeyIDMissing         = "signing record inconsistent: reliza:export:signed is true but reliza:export:signatureKeyId is missing"
	noticeNoRootSignature      = "signing record inconsistent: reliza:export:signed is true but the document has no root signature"
	noticeKeyIDMismatchPrefix  = "signing record inconsistent: reliza:export:signatureKeyId is "
	noticeKeyIDMismatchNamesID = " but the root signature names keyId "
	noticeKeyIDMismatchNoID    = " but the root signature names no keyId"
)

// ExportSigningNotice returns the one line exportreleasebom prints on stderr about the signing
// record of a JSON export (design SCORE-38), or "" when the document carries no
// reliza:export:signed property, or is not a JSON object. It verifies nothing: it reads the stamp on
// metadata.properties and compares it with the root signature member.
//
// Every member is read by exact key from raw JSON objects (no struct decode, so no case folding),
// property names are compared byte for byte, and of a name that occurs more than once the last
// entry wins, the position the server writes its stamp at. The signed line needs the root signature
// to be present and to name the stamped keyId; otherwise the line says what is missing.
func ExportSigningNotice(content []byte) string {
	var top map[string]json.RawMessage
	if json.Unmarshal(content, &top) != nil {
		return ""
	}
	props, ok := exportStamp(top)
	if !ok {
		return ""
	}
	signed, ok := props[ExportPropSigned]
	if !ok {
		return ""
	}
	if signed != "true" {
		switch reason := props[ExportPropUnsignedReason]; reason {
		case exportUnsignedNoKey:
			return noticeNotSignedNoKey
		case exportUnsignedSigning:
			return noticeNotSignedSigning
		case "":
			return noticeNotSigned
		default:
			return noticeNotSignedReason + PrintableText(reason)
		}
	}
	keyID := props[ExportPropSignatureKeyID]
	if keyID == "" {
		return noticeKeyIDMissing
	}
	sig := readJSONSignature(top[rootSignature])
	switch {
	case !sig.Present:
		return noticeNoRootSignature
	case sig.KeyID == "":
		return noticeKeyIDMismatchPrefix + PrintableText(keyID) + noticeKeyIDMismatchNoID
	case sig.KeyID != keyID:
		return noticeKeyIDMismatchPrefix + PrintableText(keyID) + noticeKeyIDMismatchNamesID + PrintableText(sig.KeyID)
	}
	return noticeSigned + PrintableText(keyID)
}

// exportStamp reads the three stamp properties from metadata.properties: name and value by exact
// member key, the name a JSON string, the last entry of a name kept. A value that is not a string
// reads as "". ok is false when metadata is not an object or properties is not an array.
func exportStamp(top map[string]json.RawMessage) (props map[string]string, ok bool) {
	var metadata map[string]json.RawMessage
	if json.Unmarshal(top["metadata"], &metadata) != nil {
		return nil, false
	}
	var entries []json.RawMessage
	if json.Unmarshal(metadata["properties"], &entries) != nil {
		return nil, false
	}
	props = map[string]string{}
	for _, e := range entries {
		var p map[string]json.RawMessage
		if json.Unmarshal(e, &p) != nil {
			continue
		}
		name, isString := jsonString(p["name"])
		if !isString {
			continue
		}
		switch name {
		case ExportPropSigned, ExportPropSignatureKeyID, ExportPropUnsignedReason:
			value, _ := jsonString(p["value"])
			props[name] = value
		}
	}
	return props, true
}
