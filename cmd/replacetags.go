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

package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
)

var forDiff bool
var resolveProps bool // legacy behavior is to have this false (default)

func init() {
	replaceTagsCmd.PersistentFlags().StringVar(&infile, "infile", "", "Input file to parse, such as helm values file or docker compose file")
	replaceTagsCmd.PersistentFlags().StringVar(&outfile, "outfile", "", "Output file with parsed values (optional, if not supplied - outputs to stdout)")
	replaceTagsCmd.PersistentFlags().StringVar(&inDirectory, "indirectory", "", "Path to directory of input files to parse (either infile or indirectory is required)")
	replaceTagsCmd.PersistentFlags().StringVar(&outDirectory, "outdirectory", "", "Path to directory of output files (required if indirectory is used)")
	replaceTagsCmd.PersistentFlags().StringVar(&tagSourceFile, "tagsource", "", "Source file with tags (optional - specify either source file or instance id and revision)")
	replaceTagsCmd.PersistentFlags().StringVar(&environment, "env", "", "Environment for which to generate tags (optional)")
	replaceTagsCmd.PersistentFlags().StringVar(&instance, "instance", "", "Instance UUID for which to generate tags (optional)")
	replaceTagsCmd.PersistentFlags().StringVar(&instanceURI, "instanceuri", "", "Instance URI for which to generate tags (optional)")
	replaceTagsCmd.PersistentFlags().StringVar(&revision, "revision", "", "Instance revision for which to generate tags (optional)")
	replaceTagsCmd.PersistentFlags().StringVar(&definitionReferenceFile, "defsource", "", "Source file for definitions (optional). For helm, should be output of helm template command")
	replaceTagsCmd.PersistentFlags().StringVar(&typeVal, "type", "cyclonedx", "Type of source tags file: cyclonedx (default) or text")
	replaceTagsCmd.PersistentFlags().StringVar(&version, "version", "", "Bundle version for which to generate tags (optional - required when using bundle)")
	replaceTagsCmd.PersistentFlags().StringVar(&bundle, "bundle", "", "UUID or Name of bundle for which to generate tags when replacing by bundle and version (optional)")
	replaceTagsCmd.PersistentFlags().BoolVar(&bundleSpecificProps, "usenamespacebundle", false, "Set to true for new behavior where namespace and bundle are used for prop resolution (optional, default is 'false')")
	replaceTagsCmd.PersistentFlags().BoolVar(&provenance, "provenance", true, "Set --provenance=[true|false] flag to enable/disable adding provenance (metadata) to beginning of outfile. (optional)")
	replaceTagsCmd.PersistentFlags().StringVar(&parseMode, "parsemode", "extended", "Use to set the parse mode to either extended, simple, or strict (optional)")
	replaceTagsCmd.PersistentFlags().StringVar(&namespace, "namespace", "", "Use to define specific namespace for replace tagging (optional)")
	replaceTagsCmd.PersistentFlags().BoolVar(&forDiff, "fordiff", false, "(Optional) Set --fordiff=[true|false] flag to true to specify that secrets would be resolved by timestamp instead of sealed value. Setting to true disables provenance.")
	replaceTagsCmd.PersistentFlags().BoolVar(&resolveProps, "resolveprops", false, "(Optional) Set --resolveprops=[true|false] flag to specify whether to resolve instance properties and secrets on Reliza Hub.")

	devopsCmd.AddCommand(replaceTagsCmd)
}

// Modern way to parse templates (re-write over parse copy template)
var replaceTagsCmd = &cobra.Command{
	Use:   "replacetags",
	Short: "Replaces tags in k8s, helm or compose files",
	Long:  `Modern version of parse copy template`,
	Run: func(cmd *cobra.Command, args []string) {
		var replaceTagsVars ReplaceTagsVars
		replaceTagsVars.TagSourceFile = tagSourceFile
		replaceTagsVars.TypeVal = typeVal
		replaceTagsVars.ApiKeyId = apiKeyId
		replaceTagsVars.ApiKey = apiKey
		replaceTagsVars.Instance = instance
		replaceTagsVars.Revision = revision
		replaceTagsVars.InstanceURI = instanceURI
		replaceTagsVars.Bundle = bundle
		replaceTagsVars.Version = version
		replaceTagsVars.Environment = environment
		replaceTagsVars.Namespace = namespace
		replaceTagsVars.Infile = infile
		replaceTagsVars.Outfile = outfile
		replaceTagsVars.Indirectory = inDirectory
		ReplaceTags(replaceTagsVars)
	},
}

func GetSubstitutionFromDigestedString(ds string) Substitution {
	// sample ds = taleodor/mafia-express:tag@sha256:7205756e730e3c614f30509bdb33770f5816897abb49aa8308364fec1864882d

	var subst Substitution
	digestSplit := strings.Split(ds, "@")
	if len(digestSplit) > 1 {
		subst.Digest = digestSplit[1]
	}

	tagSplit := strings.Split(digestSplit[0], ":")

	// tagSplit may have 3 parts, if port is used as part of registry
	if len(tagSplit) > 1 {
		tagPart := tagSplit[len(tagSplit)-1]
		if !strings.Contains(tagPart, "/") {
			subst.Tag = tagPart
		}
	}

	var imagePart string

	if len(subst.Tag) > 0 {
		imagePart = strings.Replace(digestSplit[0], ":"+subst.Tag, "", -1)
	} else {
		imagePart = digestSplit[0]
	}

	imageSplit := strings.Split(imagePart, "/")

	if len(imageSplit) == 1 {
		subst.Registry = "docker.io"
		subst.Image = "library/" + imagePart
	} else if len(imageSplit) > 2 {
		subst.Registry = imageSplit[0]
		subst.Image = strings.Replace(imagePart, imageSplit[0]+"/", "", -1)
	} else if len(imageSplit) == 2 {
		if strings.Contains(imageSplit[0], ".") {
			subst.Registry = imageSplit[0]
			subst.Image = imageSplit[1]
		} else {
			subst.Registry = "docker.io"
			subst.Image = imagePart
		}
	}

	return subst
}

func constructSubstitutionMap(tagSourceMap *map[string]string) *map[string]Substitution {
	// scan definition reference file and identify all used tags (scan by "image:" pattern)
	substitutionMap := map[string]Substitution{}
	if definitionReferenceFile != "" {
		defScanMap := scanDefenitionReferenceFile()
		// combine 2 maps and come up with substitution map to apply to source (i.e. to source helm chart)
		// traverse defScanMap, map to tagSourceMap and put to substitution map
		for k := range defScanMap {
			// https://stackoverflow.com/questions/2050391/how-to-check-if-a-map-contains-a-key-in-go
			if tagVal, ok := (*tagSourceMap)[k]; ok {
				substitutionMap[k] = GetSubstitutionFromDigestedString(tagVal)
			}
		}
	} else {
		for tagSourceKey, tagSourceVal := range *tagSourceMap {
			substitutionMap[tagSourceKey] = GetSubstitutionFromDigestedString(tagSourceVal)
		}
	}
	return &substitutionMap
}

func ReplaceTags(replaceTagsVars ReplaceTagsVars) string {

	retOut := ""
	// v1 - takes inFile = inFile var, outFile = outfile, source txt file, definition reference file - i.e. result of helm template
	// inFile, outFile, tagSourceFile, definitionReferenceFile
	// type - typeVal: options - text, cyclonedx

	// 1st - scan tag source file and construct a map of generic tag to actual tag
	tagSourceMap := scanTags(replaceTagsVars)

	substitutionMap := *(constructSubstitutionMap(&tagSourceMap))

	// Check if input is infile or inDirectory (operating on directory or file?)
	if len(replaceTagsVars.Infile) > 0 && len(replaceTagsVars.Indirectory) == 0 {
		retOut = replaceTagsOnFile(&replaceTagsVars, &substitutionMap)
	} else if len(infile) == 0 && len(replaceTagsVars.Indirectory) > 0 {
		replaceTagsOnDirectory(&replaceTagsVars.Indirectory, &outDirectory, &substitutionMap)
	} else {
		// either infile and inDirectory provided (too many inputs), or neither provided
		fmt.Println("Error: Must supply either infile or indirectory (but not both)!")
	}
	return retOut
}

func replaceTagsOnFile(replaceTagsVars *ReplaceTagsVars, substitutionMap *map[string]Substitution) string {
	retOut := ""

	infile := replaceTagsVars.Infile
	outfile := replaceTagsVars.Outfile

	fileInfo, err := os.Stat(infile)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	} else if fileInfo.IsDir() {
		fmt.Println("Error: infile must be a path to a file, not a directory!")
		os.Exit(1)
	}
	// Open infile if not directory:
	var inFileOpened *os.File
	var inFileOpenedError error
	inFileOpened, inFileOpenedError = os.Open(infile)
	if inFileOpenedError != nil {
		fmt.Println("Error opening infile: " + infile)
		fmt.Println(inFileOpenedError)
		os.Exit(1)
	}

	// retrieve secrets and props from infile
	sp := parseSecretsPropsFromInFile(inFileOpened)
	resolvedSp := resolveSecretPropsOnRelizaHub(sp)
	// fmt.Println(resolvedSp)

	// reopen in file for substitution
	inFileOpened, inFileOpenedError = os.Open(infile)
	if inFileOpenedError != nil {
		fmt.Println("Error opening infile: " + infile)
		fmt.Println(inFileOpenedError)
		os.Exit(1)
	}

	// Parse infile and get slice of lines to be written to outfile/stdout
	parsedLines := substituteCopyBasedOnMap(inFileOpened, substitutionMap, parseMode, resolvedSp)

	// Close infile
	inFileCloseError := inFileOpened.Close()
	if inFileCloseError != nil {
		fmt.Println("Error closing infile: " + infile)
		fmt.Println(inFileCloseError)
		os.Exit(1)
	}

	// write parsed lines to outfile/stdout if parsing did not fail
	if parsedLines != nil {
		// Create outFile to write to, if outfile not specified, write to stdout
		var outFileOpened *os.File
		var outFileOpenedError error
		if len(outfile) > 0 {
			//fmt.Println("Opening output file...")
			if outfile == infile {
				os.Remove(infile)
			}
			outFileOpened, outFileOpenedError = os.Create(outfile)
			if outFileOpenedError != nil {
				fmt.Println("Error opening outfile: " + outfile)
				fmt.Println(outFileOpenedError)
				os.Exit(1)
			}
		}

		// need to add provenance first, because can only write to stdout sequentially
		if !forDiff && provenance {
			addProvenanceToReplaceTagsOutput(outFileOpened, apiKeyId, tagSourceFile, environment, instance, instanceURI, revision, version, bundle)
		}
		for _, line := range parsedLines {
			if outFileOpened != nil {
				outFileOpened.WriteString(line + "\n")
			} else {
				retOut += line + "\n"
				fmt.Print(line + "\n")
			}
		}

		if outFileOpened != nil { // outfile might not exist if writing to stdout
			outFileCloseError := outFileOpened.Close()
			if outFileCloseError != nil {
				fmt.Println("Error closing outfile: " + outfile)
				fmt.Println(outFileCloseError)
				os.Exit(1)
			}
		}
	} else {
		fmt.Println("Error parsing input file")
		os.Exit(1)
	}

	return retOut
}

func isDirectory(dir *string) bool {
	isDir := false
	dirInfo, err := os.Stat(*dir)
	if err != nil {
		fmt.Println(err)
	} else if dirInfo.IsDir() {
		isDir = true
	}
	return isDir
}

func replaceTagsOnDirectory(indir *string, outdir *string, substitutionMap *map[string]Substitution) {
	// If parsing files from input directory, an output directory path should be provided, not an output file path.
	if len(outfile) > 0 {
		fmt.Println("Error: please only provide '--outdirectory' flag (no '--outfile') when using '--indirectory' as input instead of '--infile'.")
		os.Exit(1)
	}
	// Check that outDirectory has value. Cannot write to stdout when parsing multiple files from a directory.
	if len(outDirectory) == 0 {
		fmt.Println("Error: '--outdirectory' flag is not set. Must supply a path to an output directory when using --indirectory flag.")
		os.Exit(1)
	}

	_, err := os.ReadDir(*outdir)
	if err == nil && *outdir != *indir {
		fmt.Println("Error: output directory already exists " + *outdir)
		os.Exit(1)
	}

	err1 := os.MkdirAll(*outdir, os.FileMode(0770))
	if err1 != nil {
		fmt.Println("Error: could not create directory " + *outdir)
		fmt.Println(err1)
		os.Exit(1)
	}

	var fileNames []string
	files, err := os.ReadDir(*indir)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	for _, f := range files {
		fileNames = append(fileNames, f.Name())
	}

	for _, fileName := range fileNames {
		curinfile := filepath.Join(*indir, fileName)
		curoutfile := filepath.Join(*outdir, fileName)
		if debug == "true" {
			fmt.Println("curinfile = " + curinfile + " , curoutfile = " + curoutfile)
		}
		if isDirectory(&curinfile) {
			replaceTagsOnDirectory(&curinfile, &curoutfile, substitutionMap)
		} else {
			var replaceTagsVars ReplaceTagsVars
			replaceTagsVars.TagSourceFile = tagSourceFile
			replaceTagsVars.TypeVal = typeVal
			replaceTagsVars.ApiKeyId = apiKeyId
			replaceTagsVars.ApiKey = apiKey
			replaceTagsVars.Instance = instance
			replaceTagsVars.Revision = revision
			replaceTagsVars.InstanceURI = instanceURI
			replaceTagsVars.Bundle = bundle
			replaceTagsVars.Version = version
			replaceTagsVars.Environment = environment
			replaceTagsVars.Namespace = namespace
			replaceTagsVars.Infile = curinfile
			replaceTagsVars.Outfile = curoutfile
			replaceTagsOnFile(&replaceTagsVars, substitutionMap)
		}
	}
}

func scanDefenitionReferenceFile() map[string]string {
	fmt.Println("Scanning definition references...")
	defFile, fileOpenErr := os.Open(definitionReferenceFile)
	if fileOpenErr != nil {
		fmt.Println(fileOpenErr)
		os.Exit(1)
	}

	// map to store definition images to their replacements -> will be applied on source files
	defScanMap := map[string]string{}

	defScanner := bufio.NewScanner(defFile)
	// input files must be utf-8 !!!
	for defScanner.Scan() {
		line := defScanner.Text()
		if strings.Contains(strings.ToLower(line), "image: ") {
			// extract actual image
			imageLineArray := strings.Split(strings.ToLower(line), "image: ")
			image := imageLineArray[1]
			// remove beginning and ending quotes if present
			re := regexp.MustCompile("^\"")
			image = re.ReplaceAllLiteralString(image, "")
			re = regexp.MustCompile("\"$")
			image = re.ReplaceAllLiteralString(image, "")
			re = regexp.MustCompile("^'")
			image = re.ReplaceAllLiteralString(image, "")
			re = regexp.MustCompile("'$")
			image = re.ReplaceAllLiteralString(image, "")
			// parse and add to map
			if strings.Contains(image, "@") {
				tagSplit := strings.Split(image, "@")
				defScanMap[tagSplit[0]] = image
			} else if strings.Contains(line, ":") {
				tagSplit := strings.SplitN(image, ":", 2)
				defScanMap[tagSplit[0]] = image
			} else {
				defScanMap[image] = image
			}
		}
	}
	return defScanMap
}

func parseSecretsPropsFromInFile(inFileOpened *os.File) SecretProps {
	var sp SecretProps
	sp.Secrets = map[string]bool{}
	sp.Properties = map[string]bool{}

	inScanner := bufio.NewScanner(inFileOpened)
	for inScanner.Scan() {
		line := inScanner.Text()

		// each piece we are interested in looks like `$RELIZA{PROPERTY.FQDN}`
		pspArr := parseLineToSecrets(line)
		for _, psp := range pspArr {
			if psp.Type == "PROPERTY" {
				sp.Properties[psp.Key] = true
			} else if psp.Type == "SECRET" || psp.Type == "PLAINSECRET" {
				sp.Secrets[psp.Key] = true
			}
		}
	}
	return sp
}

func resolveSecretPropsOnRelizaHub(sp SecretProps) SecretPropsRHResp {
	var secretsInp []string
	var propsInp []string
	for sk := range sp.Secrets {
		secretsInp = append(secretsInp, sk)
	}
	for sp := range sp.Properties {
		propsInp = append(propsInp, sp)
	}

	return retrieveInstancePropsSecrets(propsInp, secretsInp)
}

func parseLineToSecrets(line string) []PropSecretParse {
	var psp []PropSecretParse
	if strings.Contains(line, "$RELIZA") {
		rlzParts := strings.Split(line, "$RELIZA{")
		for _, rlzPart := range rlzParts {
			if strings.HasPrefix(rlzPart, "PROPERTY") {
				rp1 := strings.Split(rlzPart, "PROPERTY.")[1]
				rp2 := strings.Split(rp1, "}")[0]
				var psp1 PropSecretParse
				psp1.Type = "PROPERTY"
				if strings.Contains(rp2, ":") {
					psp1.Key = strings.Split(rp2, ":")[0]
					psp1.Default = strings.Split(rp2, ":")[1]
				} else {
					psp1.Key = rp2
				}
				psp1.Wholetext = "$RELIZA{PROPERTY." + rp2 + "}"
				psp = append(psp, psp1)
			} else if strings.HasPrefix(rlzPart, "SECRET") {
				rp1 := strings.Split(rlzPart, "SECRET.")[1]
				rp2 := strings.Split(rp1, "}")[0]
				var psp2 PropSecretParse
				psp2.Type = "SECRET"
				if strings.Contains(rp2, ":") {
					psp2.Key = strings.Split(rp2, ":")[0]
					psp2.Default = strings.Split(rp2, ":")[1]
				} else {
					psp2.Key = rp2
				}
				psp2.Wholetext = "$RELIZA{SECRET." + rp2 + "}"
				psp = append(psp, psp2)
			} else if strings.HasPrefix(rlzPart, "PLAINSECRET") {
				rp1 := strings.Split(rlzPart, "PLAINSECRET.")[1]
				rp2 := strings.Split(rp1, "}")[0]
				var psp2 PropSecretParse
				psp2.Type = "PLAINSECRET"
				if strings.Contains(rp2, ":") {
					psp2.Key = strings.Split(rp2, ":")[0]
					psp2.Default = strings.Split(rp2, ":")[1]
				} else {
					psp2.Key = rp2
				}
				psp2.Wholetext = "$RELIZA{PLAINSECRET." + rp2 + "}"
				psp = append(psp, psp2)
			}
		}
	}
	return psp
}

type SecretProps struct {
	Secrets    map[string]bool `json:"secrets"`
	Properties map[string]bool `json:"properties"`
}

type PropSecretParse struct {
	Type      string // PROPERTY or SECRET
	Key       string // key known to Reliza Hub
	Default   string // default value of property or secret
	Wholetext string // Whole string to substitute including $RELIZA prefix and {}
}

type ResolvedSecret struct {
	Secret    string `json:"value"`
	Timestamp int64  `json:"lastUpdated"`
	Key       string `json:"key"`
}

type ResolvedProperty struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type ReplaceTagsVars struct {
	TagSourceFile string
	TypeVal       string
	ApiKeyId      string
	ApiKey        string
	Instance      string
	Revision      string
	InstanceURI   string
	Bundle        string
	Version       string
	Environment   string
	Namespace     string
	Infile        string
	Indirectory   string
	Outfile       string
}

type Substitution struct {
	Registry string
	Image    string
	Digest   string
	Tag      string
}
