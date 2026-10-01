package cmd

import (
	"strings"
	"testing"
)

const sha256Hex = "9F86D081884C7D659A2FEAA0C55AD015A3BF4F1B2B0B822CD15D6C15B0F00A08"

func TestDigestFlagBecomesADigestRecord(t *testing.T) {
	r, err := parseDigestFlag("sha256:" + sha256Hex)
	if err != nil {
		t.Fatal(err)
	}
	if r["algo"] != "SHA_256" || r["digest"] != strings.ToLower(sha256Hex) || r["scope"] != "ORIGINAL_FILE" {
		t.Errorf("record: %v", r)
	}
	for _, spelling := range []string{"SHA256", "sha-256", "SHA_256"} {
		if r, err := parseDigestFlag(spelling + ":" + sha256Hex); err != nil || r["algo"] != "SHA_256" {
			t.Errorf("%s: %v %v", spelling, r, err)
		}
	}
	if r, err := parseDigestFlag("sha3-256:" + sha256Hex + ":oci_storage"); err != nil || r["algo"] != "SHA3_256" || r["scope"] != "OCI_STORAGE" {
		t.Errorf("algo and scope: %v %v", r, err)
	}
	if r, err := parseDigestFlag("md5:" + strings.Repeat("a", 32)); err != nil || r["algo"] != "MD5" {
		t.Errorf("md5: %v %v", r, err)
	}
}

func TestDigestFlagRefusesWhatTheServerWould(t *testing.T) {
	for v, want := range map[string]string{
		sha256Hex:                                  "expected <algo>:<hex>",
		"sha256:" + sha256Hex + ":REARM:extra":     "expected <algo>:<hex>",
		"crc32:" + sha256Hex:                       "unknown algorithm",
		"sha256:" + sha256Hex[:10]:                 "is 64 hex characters, got 10",
		"sha256:" + strings.Repeat("z", 64):        "hexadecimal",
		"sha256:" + sha256Hex + ":AS_UPLOADED":     "computed by ReARM",
		"sha256:" + sha256Hex + ":raw_oci_storage": "computed by ReARM",
		"sha256:" + sha256Hex + ":SOMEWHERE":       "unknown scope",
	} {
		if _, err := parseDigestFlag(v); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: want %q, got %v", v, want, err)
		}
	}
}

func TestDigestFlagsStopAtTheFirstBadOne(t *testing.T) {
	recs, err := parseDigestFlags([]string{"sha256:" + sha256Hex, "md5:" + strings.Repeat("0", 32)})
	if err != nil || len(recs) != 2 {
		t.Fatalf("%v %v", recs, err)
	}
	if _, err := parseDigestFlags([]string{"sha256:" + sha256Hex, "nope"}); err == nil {
		t.Error("a bad value in the list is refused")
	}
	if recs, err := parseDigestFlags(nil); err != nil || recs != nil {
		t.Error("no --digest sends no records")
	}
}
