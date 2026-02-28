/*
The MIT License (MIT)

Copyright (c) 2020 - 2022 Reliza Incorporated (Reliza (tm), https://reliza.io)

Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the "Software"),
to deal in the Software without restriction, including without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense,
and/or sell copies of the Software, and to permit persons to whom the Software is furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY,
WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.

*/

package tests

import (
	"os"
	"testing"

	"github.com/relizaio/rearm/cmd"
)

func TestReplaceTags(t *testing.T) {
	var replaceTagsVars cmd.ReplaceTagsVars
	replaceTagsVars.TagSourceFile = "mafia_tag_source_cdx.json"
	replaceTagsVars.TypeVal = "cyclonedx"
	replaceTagsVars.Infile = "values_mafia.yaml"

	replacedTags := cmd.ReplaceTags(replaceTagsVars)
	expectedReplacement, err := os.ReadFile("expected_values_mafia.yaml")
	if err != nil {
		t.Fatalf("failed reading expected values file")
	}
	if replacedTags != string(expectedReplacement) {
		t.Fatalf("replaced tags do not equal expected, actual = %s", replacedTags)
	}
}

func TestGetSubstitutionFromDigestedString1(t *testing.T) {
	digestedImage := "taleodor/mafia-express:tag@sha256:7205756e730e3c614f30509bdb33770f5816897abb49aa8308364fec1864882d"
	subst := cmd.GetSubstitutionFromDigestedString(digestedImage)
	if subst.Digest != "sha256:7205756e730e3c614f30509bdb33770f5816897abb49aa8308364fec1864882d" || subst.Tag != "tag" || subst.Image != "taleodor/mafia-express" || subst.Registry != "docker.io" {
		t.Fatalf("Substitution parse failed = %s", cmd.GetDigestedImageFromSubstitution(subst))
	}
}

func TestGetSubstitutionFromDigestedString2(t *testing.T) {
	digestedImage := "12345.dkr.ecr.us-east-1.amazonaws.com/mafia-express:tag@sha256:7205756e730e3c614f30509bdb33770f5816897abb49aa8308364fec1864882d"
	subst := cmd.GetSubstitutionFromDigestedString(digestedImage)
	if subst.Digest != "sha256:7205756e730e3c614f30509bdb33770f5816897abb49aa8308364fec1864882d" || subst.Tag != "tag" || subst.Image != "mafia-express" || subst.Registry != "12345.dkr.ecr.us-east-1.amazonaws.com" {
		t.Fatalf("Substitution parse failed = %s", cmd.GetDigestedImageFromSubstitution(subst))
	}
}

func TestDigestedStringFromSubstitution(t *testing.T) {
	var subst cmd.Substitution
	subst.Registry = "12345.dkr.ecr.us-east-1.amazonaws.com"
	subst.Image = "taleodor/mafia-express"
	subst.Tag = "tag"
	subst.Digest = "sha256:7205756e730e3c614f30509bdb33770f5816897abb49aa8308364fec1864882d"
	expDigestedImage := "12345.dkr.ecr.us-east-1.amazonaws.com/taleodor/mafia-express:tag@sha256:7205756e730e3c614f30509bdb33770f5816897abb49aa8308364fec1864882d"
	actualDigestedImage := cmd.GetDigestedImageFromSubstitution(subst)
	if expDigestedImage != actualDigestedImage {
		t.Fatalf("Images mismatch, actual image = %s", actualDigestedImage)
	}
}

func TestReplaceTagsBitnamiStyle(t *testing.T) {
	var replaceTagsVars cmd.ReplaceTagsVars
	replaceTagsVars.TagSourceFile = "mafia_tag_source_cdx.json"
	replaceTagsVars.TypeVal = "cyclonedx"
	replaceTagsVars.Infile = "values_mafia_bitnami_style.yaml"

	replacedTags := cmd.ReplaceTags(replaceTagsVars)
	expectedReplacement, err := os.ReadFile("expected_values_mafia_bitnami_style.yaml")

	// actualOutFile, _ := os.Create("actual_bitnami_out.yaml")
	// actualOutFile.WriteString(replacedTags)

	if err != nil {
		t.Fatalf("failed reading expected values file")
	}
	if replacedTags != string(expectedReplacement) {
		t.Fatalf("replaced tags do not equal expected, actual = %s", replacedTags)
	}
}

func TestIsInBitnamiParse(t *testing.T) {
	testLine1 := "    pullPolicy: IfNotPresent"
	inParse11 := cmd.IsInBitnamiParse(testLine1, 4)

	if !inParse11 {
		t.Fatalf("Failed in bitnami parse check, should be true with 4 whitespace prefix")
	}

	inParse12 := cmd.IsInBitnamiParse(testLine1, 5)

	if inParse12 {
		t.Fatalf("Failed in bitnami parse check, should be false with 5 whitespace prefix")
	}

	testLine2 := "    registry: docker.io"
	inParse21 := cmd.IsInBitnamiParse(testLine2, 4)

	if !inParse21 {
		t.Fatalf("Failed in bitnami parse check, should be true with 4 whitespace prefix")
	}
}

func TestReplaceTagsBitnamiStyleMerged(t *testing.T) {
	var replaceTagsVars cmd.ReplaceTagsVars
	replaceTagsVars.TagSourceFile = "mafia_tag_source_cdx.json"
	replaceTagsVars.TypeVal = "cyclonedx"
	replaceTagsVars.Infile = "values_mafia_bitnami_merged_style.yaml"

	replacedTags := cmd.ReplaceTags(replaceTagsVars)
	expectedReplacement, err := os.ReadFile("expected_values_mafia_bitnami_merged_style.yaml")

	// actualOutFile, _ := os.Create("actual_bitnami_out.yaml")
	// actualOutFile.WriteString(replacedTags)

	if err != nil {
		t.Fatalf("failed reading expected values file")
	}
	if replacedTags != string(expectedReplacement) {
		t.Fatalf("replaced tags do not equal expected, actual = %s", replacedTags)
	}
}
