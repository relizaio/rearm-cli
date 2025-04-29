/*
The MIT License (MIT)

Copyright (c) 2020 - 2025 Reliza Incorporated (Reliza (tm), https://reliza.io)

Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the "Software"),
to deal in the Software without restriction, including without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense,
and/or sell copies of the Software, and to permit persons to whom the Software is furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY,
WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.

*/

package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"

	cdx "github.com/CycloneDX/cyclonedx-go"
	"github.com/package-url/packageurl-go"
	"github.com/spf13/cobra"
)

func init() {
	bomUtils.PersistentFlags().StringVarP(&infile, "infile", "f", "", "Input file path with bom json")
	bomUtils.PersistentFlags().StringVarP(&outfile, "outfile", "o", "", "Output file path to write bom json")
	fixOciPurlCmd.PersistentFlags().StringVar(&ociImage, "ociimage", "", "oci image with digest")

	rootCmd.AddCommand(bomUtils)
	bomUtils.AddCommand(fixOciPurlCmd)
}

func readJSON() ([]byte, error) {
	var data []byte
	// Read from stdin if no input file specified
	if infile == "" || infile == "-" {
		bytesData, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, err
		}
		data = bytesData
	} else {
		// Read from file
		data, err := os.ReadFile(infile)
		if err != nil {
			return nil, err
		}
		return data, nil
	}
	return data, nil
}

func readBom() (*cdx.BOM, error) {

	jsonData, err := readJSON()
	if err != nil {
		return nil, err
	}

	return readBomFromBytes(jsonData)
}

func readBomFromBytes(data []byte) (*cdx.BOM, error) {
	bom := new(cdx.BOM)
	decoder := cdx.NewBOMDecoder(bytes.NewReader(data), cdx.BOMFileFormatJSON)
	err := decoder.Decode(bom)
	return bom, err
}

func writeOutput(bom *cdx.BOM) error {
	buf := new(bytes.Buffer)
	err := cdx.NewBOMEncoder(buf, cdx.BOMFileFormatJSON).
		// SetPretty(true).
		Encode(bom)
	if err != nil {
		panic(err)
	}
	// Write to stdout if no output file specified
	if outfile == "" || outfile == "-" {
		_, err := os.Stdout.Write(buf.Bytes())
		return err
	}

	// Write to file
	return os.WriteFile(outfile, buf.Bytes(), 0644)
}

var (
	ociImage string
	outfile  string
)

var bomUtils = &cobra.Command{
	Use:   "bomutils",
	Short: "Set of commands to interact with xBOMS",
	Long:  `Set of commands to interact with xBOMS`,
}
var fixOciPurlCmd = &cobra.Command{
	Use:   "fixocipurl",
	Short: "Fix oci purl",
	Long:  `Fix purl for a given OCI image on an input cyclonedx BOM`,
	Run: func(cmd *cobra.Command, args []string) {
		fixOciPurlsFunc()
	},
}

func fixOciPurlsFunc() {
	data, err := readJSON()

	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	oldPurl, newPurl := preparePurlsForGivenOCIImage(ociImage)

	replaceddata := replaceStringInJSONBytes(data, oldPurl, newPurl)

	bom, err := readBomFromBytes(replaceddata)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	err = writeOutput(bom)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
func replaceStringInJSONBytes(data []byte, old string, new string) []byte {
	return bytes.ReplaceAll(data, []byte(old), []byte(new))
}
func preparePurlsForGivenOCIImage(givenOciImage string) (oldPurl, newPurlString string) {
	oldPurl = "pkg:oci/" + givenOciImage
	instance, err := packageurl.FromString(oldPurl)
	if err != nil {
		panic(err)
	}

	newPurl := packageurl.NewPackageURL("oci", "", instance.Name, instance.Version, packageurl.Qualifiers{{Key: "repository_url", Value: instance.Namespace}}, "")
	newPurlString = newPurl.String()
	return oldPurl, newPurlString
}
