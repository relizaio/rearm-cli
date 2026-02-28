package cmd

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"io"

	"github.com/machinebox/graphql"
)

/*
sort map by length of keys to always prefer longer matches
*/
func sortSubstitutionMap(substitutionMap *map[string]Substitution) *[]KeyValueSorted {
	var sortedSubstitutions []KeyValueSorted
	for k, v := range *substitutionMap {
		kvs := KeyValueSorted{Key: k, Value: v, Length: len(k)}
		sortedSubstitutions = append(sortedSubstitutions, kvs)
	}
	sort.Slice(sortedSubstitutions, func(i, j int) bool {
		return sortedSubstitutions[i].Length > sortedSubstitutions[j].Length
	})
	return &sortedSubstitutions
}

func GetMatchingKeyFromSubstitution(subst Substitution) string {
	matchingKeyImage := subst.Registry + "/" + subst.Image
	return matchingKeyImage
}

func GetDigestedImageFromSubstitution(subst Substitution) string {
	digestedImage := subst.Registry + "/" + subst.Image
	if len(subst.Tag) > 0 {
		digestedImage += ":" + subst.Tag
	}
	if len(subst.Digest) > 0 {
		digestedImage += "@" + subst.Digest
	}
	return digestedImage
}

/*
This function takes as input a inFile file pointer, substitutionMap and parseMode string.
The contents of the inFile will be read and parsed according to the mappings of substitutionMap.
The output of the function is a slice of strings each representing a line to be written to
the CLI output (either outfile or stdout). If the inFile cannot be parsed for any reason
(ex: strict mode), then an error message will be displayed and a value of nil will be returned.

There are three modes for parsing input files: simple, extended and strict (default = "extended")
"simple"   mode: only replaces 'image' keys (suitable for k8s templates or compose files)
"extended" mode: replaces all keys present in substitution map (needed for helm values files)
"strict"   mode: if artifact is not found upstream, parsing fails, return nil array of lines

resolvedSp - result of resolving secrets and properties on the instance, if applicable
forDiff - boolean flag - if true, timestamps will be used instead of secrets
*/
func substituteCopyBasedOnMap(inFileOpened *os.File, substitutionMap *map[string]Substitution, parseMode string, resolvedSp SecretPropsRHResp) []string {
	resolvedProperties := map[string]string{}
	resolvedSecrets := map[string]ResolvedSecret{}

	for _, rpr := range resolvedSp.Responsewrapper.Properties {
		resolvedProperties[rpr.Key] = rpr.Value
	}

	for _, rsr := range resolvedSp.Responsewrapper.Secrets {
		resolvedSecrets[rsr.Key] = rsr
	}

	parseMode = strings.ToLower(parseMode)
	if parseMode != "simple" && parseMode != "extended" && parseMode != "strict" {
		fmt.Println("Error: '" + parseMode + "' is not a valid parsemode. Must be either 'simple' or 'extended'")
		os.Exit(1)
	}

	sortedSubstitutions := *(sortSubstitutionMap(substitutionMap))
	parsedLines := *(parseLines(inFileOpened, &sortedSubstitutions, &resolvedProperties, &resolvedSecrets))
	if parsedLines == nil {
		fmt.Println("Error: Failed to parse empty/non-existent input file: " + inFileOpened.Name())
	}
	return parsedLines
}

func parseLines(inFileOpened *os.File, sortedSubstitutions *[]KeyValueSorted, resolvedProperties *map[string]string,
	resolvedSecrets *map[string]ResolvedSecret) *[]string {
	var parsedLines []string

	inScanner := bufio.NewScanner(inFileOpened)
	establishedWhiteSpacePrefix := 0

	var bitnamiLineCache []string
	lineindex := 0
	for inScanner.Scan() {
		line := inScanner.Text()
		isBitnamiImageStart, whiteSpacePrefix := isBitnamiImageStart(line)
		if (lineindex == 0 && strings.HasPrefix(line, "# Tags replaced with Reliza CLI")) || (lineindex == 1 && strings.HasPrefix(line, "# According to")) {
			// do nothing
		} else if isBitnamiImageStart {
			establishedWhiteSpacePrefix = whiteSpacePrefix + 2
			bitnamiLineCache = append(bitnamiLineCache, line)
		} else if len(bitnamiLineCache) > 0 && IsInBitnamiParse(line, establishedWhiteSpacePrefix) {
			bitnamiLineCache = append(bitnamiLineCache, line)
		} else if len(bitnamiLineCache) > 0 {
			parsedBitnamiLines := parseBitnamiLines(&bitnamiLineCache, sortedSubstitutions, resolvedProperties, resolvedSecrets, inFileOpened.Name())
			parsedLines = append(parsedLines, *parsedBitnamiLines...)
			bitnamiLineCache = []string{}
			line = parseLineOnScan(line, sortedSubstitutions, resolvedProperties, resolvedSecrets, inFileOpened.Name())
			parsedLines = append(parsedLines, line)
		} else {
			line = parseLineOnScan(line, sortedSubstitutions, resolvedProperties, resolvedSecrets, inFileOpened.Name())
			parsedLines = append(parsedLines, line)
		}
		lineindex++
	}
	if len(bitnamiLineCache) > 0 {
		parsedBitnamiLines := parseBitnamiLines(&bitnamiLineCache, sortedSubstitutions, resolvedProperties, resolvedSecrets, inFileOpened.Name())
		parsedLines = append(parsedLines, *parsedBitnamiLines...)
		bitnamiLineCache = []string{}
	}
	return &parsedLines
}

func parseBitnamiLines(bitnamiLineCache *[]string, sortedSubstitutions *[]KeyValueSorted,
	resolvedProperties *map[string]string, resolvedSecrets *map[string]ResolvedSecret, inFileName string) *[]string {
	parsedLines, isBitnami := validateAndParseBitnamiLines(bitnamiLineCache, sortedSubstitutions)
	if !isBitnami {
		parsedLines = []string{}
		for _, blc := range *bitnamiLineCache {
			line := parseLineOnScan(blc, sortedSubstitutions, resolvedProperties, resolvedSecrets, inFileName)
			parsedLines = append(parsedLines, line)
		}
	}
	return &parsedLines
}

/*
*
Sample non-merged:

	image:
	  registry: docker.io
	  repository: taleodor/mafia-express
	  tag: latest
	  digest: ""

Sample merged:

	image:
	  debug: false
	  digest: ""
	  pullPolicy: IfNotPresent
	  pullSecrets: []
	  registry: docker.io
	  repository: library/redis
	  tag: latest
*/
func validateAndParseBitnamiLines(bitnamiLineCache *[]string, sortedSubstitutions *[]KeyValueSorted) ([]string, bool) {
	var parsedLines []string
	var bitnamiSubst Substitution

	bitnamiCheckMap := map[string]bool{}
	isBitnami := true
	isTagAsDigest := true

	if len(*bitnamiLineCache) < 5 {
		isBitnami = false
	}

	if len(*bitnamiLineCache) < 4 {
		isTagAsDigest = false
	}

	if isBitnami || isTagAsDigest {
		for _, line := range *bitnamiLineCache {
			trimmedLine := strings.Trim(line, " ")
			if strings.HasPrefix(trimmedLine, "registry: ") {
				bitnamiCheckMap["registry"] = true
				bitnamiSubst.Registry = strings.ReplaceAll(trimmedLine, "registry: ", "")
			} else if strings.HasPrefix(trimmedLine, "repository: ") {
				bitnamiCheckMap["repository"] = true
				bitnamiSubst.Image = strings.ReplaceAll(trimmedLine, "repository: ", "")
			} else if strings.HasPrefix(trimmedLine, "tag: ") {
				bitnamiCheckMap["tag"] = true
				bitnamiSubst.Tag = strings.ReplaceAll(trimmedLine, "tag: ", "")
			} else if strings.HasPrefix(trimmedLine, "digest: ") {
				bitnamiCheckMap["digest"] = true
				bitnamiSubst.Digest = strings.ReplaceAll(trimmedLine, "digest: ", "")
			}
		}
	}

	if isBitnami && len(bitnamiCheckMap) < 4 {
		isBitnami = false
	}

	if isTagAsDigest && len(bitnamiCheckMap) < 3 {
		isTagAsDigest = false
	}

	if isBitnami || isTagAsDigest {
		matchKey := GetMatchingKeyFromSubstitution(bitnamiSubst)
		var replacedSubst Substitution
		for _, kvs := range *sortedSubstitutions {
			k := kvs.Key
			if isImageMatchingSubstitutionKey(matchKey, k) {
				replacedSubst = kvs.Value
				break
			}
		}

		if len(replacedSubst.Digest) > 0 {
			for _, line := range *bitnamiLineCache {
				trimmedLine := strings.Trim(line, " ")
				if strings.HasPrefix(trimmedLine, "registry: ") {
					lineSplit := strings.Split(line, ": ")
					parsedLines = append(parsedLines, lineSplit[0]+": "+replacedSubst.Registry)
				} else if strings.HasPrefix(trimmedLine, "repository: ") {
					lineSplit := strings.Split(line, ": ")
					parsedLines = append(parsedLines, lineSplit[0]+": "+replacedSubst.Image)
				} else if strings.HasPrefix(trimmedLine, "tag: ") {
					lineSplit := strings.Split(line, ": ")
					if isBitnami {
						parsedLines = append(parsedLines, lineSplit[0]+": "+replacedSubst.Tag)
					} else if isTagAsDigest {
						parsedLines = append(parsedLines, lineSplit[0]+": "+replacedSubst.Digest)
					}
				} else if strings.HasPrefix(trimmedLine, "digest: ") {
					lineSplit := strings.Split(line, ": ")
					parsedLines = append(parsedLines, lineSplit[0]+": "+replacedSubst.Digest)
				} else {
					parsedLines = append(parsedLines, line)
				}
			}
		}
	}

	if !isBitnami && !isTagAsDigest {
		parsedLines = *bitnamiLineCache
	}

	return parsedLines, isBitnami || isTagAsDigest
}

func isImageMatchingSubstitutionKey(image string, substKey string) bool {
	imageMatch := strings.Replace(image, "docker.io/library/", "", 1)
	imageMatch = strings.Replace(imageMatch, "docker.io/", "", 1)
	substKeyMatch := strings.Replace(substKey, "docker.io/library/", "", 1)
	substKeyMatch = strings.Replace(substKeyMatch, "docker.io/", "", 1)
	return (imageMatch == substKeyMatch)
}

/*
*
Returns true if is start and returns number of whitespace before image
*/
func isBitnamiImageStart(line string) (bool, int) {
	isBitnamiImageStart := false
	if strings.Trim(line, " ") == "image:" {
		isBitnamiImageStart = true
	}
	whiteSpacePrefix := strings.Split(line, "image:")[0]
	return isBitnamiImageStart, len(whiteSpacePrefix)
}

func IsInBitnamiParse(line string, whitespacePrefix int) bool {
	isInParse := false
	re := regexp.MustCompile(`^\s{` + strconv.Itoa(whitespacePrefix) + `}[^\s]`)
	if re.MatchString(line) {
		isInParse = true
	}
	return isInParse
}

func parseLineOnScan(line string, sortedSubstitutions *[]KeyValueSorted, resolvedProperties *map[string]string,
	resolvedSecrets *map[string]ResolvedSecret, inFileName string) string {
	// resolve props and secrets first
	pspArr := parseLineToSecrets(line)
	for _, psp := range pspArr {
		if len((*resolvedProperties)[psp.Key]) < 1 && len(psp.Default) > 0 {
			(*resolvedProperties)[psp.Key] = psp.Default
		}
		// locate value corresponding to key
		if psp.Type == "PROPERTY" {
			propVal, isPropExists := (*resolvedProperties)[psp.Key]
			if !isPropExists {
				fmt.Println("Property " + psp.Key + " not set; also make sure that --resolveprops flag is set to true; exiting...")
				os.Exit(1)
			}
			line = strings.ReplaceAll(line, psp.Wholetext, propVal)
		} else if psp.Type == "SECRET" || psp.Type == "PLAINSECRET" {
			rs, isSecretExists := (*resolvedSecrets)[psp.Key]
			if !isSecretExists {
				fmt.Println("Secret " + psp.Key + " not set or not available; also make sure that --resolveprops flag is set to true; exiting...")
				os.Exit(1)
			}
			if forDiff {
				ts := fmt.Sprintf("%d", rs.Timestamp)
				line = strings.ReplaceAll(line, psp.Wholetext, ts)
			} else if psp.Type == "SECRET" {
				line = strings.ReplaceAll(line, psp.Wholetext, rs.Secret)
			} else if psp.Type == "PLAINSECRET" {
				createNamespaceIfMissing(namespace)
				plainSecret := resolvePlainSecret(rs.Secret, namespace)
				line = strings.ReplaceAll(line, psp.Wholetext, plainSecret)
			}
		}
	}

	matchFound := false // flag used for strict mode to indicate if we fail to find image match (for strict mode)

	// check if line contains any key of substitution map
	for _, kvs := range *sortedSubstitutions {

		k := kvs.Key
		v := GetDigestedImageFromSubstitution(kvs.Value)

		// have a version stripping docker.io and docker.io/library if it's present
		// for this establish base image text
		baseImageText := ""

		if strings.Contains(line, k+":") || strings.Contains(line, k+"@") || strings.HasSuffix(line, k) {
			baseImageText = k
		}

		if len(baseImageText) < 1 {
			// try stripping docker.io
			contText := strings.Replace(k, "docker.io/", "", 1)
			if strings.Contains(line, contText+":") || strings.Contains(line, contText+"@") || strings.HasSuffix(line, contText) {
				baseImageText = contText
			}
		}

		if len(baseImageText) < 1 {
			// try stripping docker.io/library
			contText := strings.Replace(k, "docker.io/library/", "", 1)
			// note that exact match is too loose here, so instead we only look for image: pattern
			if !strings.Contains(line, "//"+contText) &&
				(strings.Contains(line, contText+":") || strings.Contains(line, contText+"@") || strings.Contains(strings.ToLower(line), "image: "+contText) ||
					strings.Contains(strings.ToLower(line), "image:"+contText)) {
				baseImageText = contText
			}
		}

		if len(baseImageText) > 0 && !strings.HasSuffix(line, ":") && !strings.Contains(line, baseImageText+": ") {
			// if simple mode, only substitute if line begins with 'image:' key
			re := regexp.MustCompile(`^\s*image:`)
			// if parseMode is not simple, always substitute line, if parseMode is simple, only substitute line if it has an 'image' tag (ie: matches regex)
			if parseMode != "simple" || re.MatchString(line) {
				//split line before image name and concat with substitution map value
				parts := strings.Split(line, baseImageText)

				// remove beginning quotes if present
				startLine := parts[0]
				re := regexp.MustCompile("\"$")
				startLine = re.ReplaceAllLiteralString(startLine, "")
				re = regexp.MustCompile("'$")
				startLine = re.ReplaceAllLiteralString(startLine, "")

				matchFound = true
				line = startLine + v
				break
			}
		}
	}

	// strict mode: if line has an image tag, but no matching key found in substitution map, exit process with error code
	re := regexp.MustCompile(`(?i)^\s*image:`)
	if !matchFound && parseMode == "strict" && re.MatchString(line) {
		fmt.Println("Error: Failed to parse infile '" + inFileName + "'. Parse mode is set to 'strict' and cannot find artifact in substitution map: \n\t" + strings.TrimSpace(line))
		os.Exit(1)
	}
	return line
}

/*
This function addds some extra meta data info as comments to the top of the outfile
that is created by the replacetags command. If no outfile is specified, the data
will instead be written directly the stdout.

The first line notes the version of reliza-cli that ran the command to generate the outfile, as
well as the date the file was generated.
The second line contains info about where the replaced tags were sourced from.
*/
func addProvenanceToReplaceTagsOutput(outFileOpened *os.File, apiKeyId string, tagSourceFile string, environment string, instance string, instanceURI string, revision string, version string, product string) {
	var provenanceLine1 string
	var provenanceLine2 string

	// First line: current reliza-cli version and current datetime
	currentDateTimeFormatted := time.Now().UTC().Format(time.RFC3339)
	relizaCliCurrentVersion := Version // This is not really getting updated atm??
	provenanceLine1 = "# Tags replaced with Reliza CLI version " + relizaCliCurrentVersion + " on " + currentDateTimeFormatted

	// Second line: where tags come from, either:
	// (tagsource file) or (environment) or (product+version)
	// or (instance+revision) or (instanceuri+revision) , revision is optional, otherwise uses latest revision
	// or (apiKeyId suffix, if using apiKeyId+apiKey pair from instance)
	if tagSourceFile != "" {
		provenanceLine2 = "# According to tag source file " + tagSourceFile
	} else if len(environment) > 0 {
		provenanceLine2 = "# According to the latest approved images in  " + environment + " environment."
	} else if len(product) > 0 && len(version) > 0 {
		provenanceLine2 = "# According to product " + product + " version " + version
	} else if len(instance) > 0 {
		if len(revision) > 0 {
			provenanceLine2 = "# According to revision " + revision + " of the instance " + instance
		} else {
			// no revision specified, using latest
			provenanceLine2 = "# According to latest approved images for the instance " + instance
		}
	} else if len(instanceURI) > 0 {
		if len(revision) > 0 {
			provenanceLine2 = "# According to revision " + revision + " of the instance at " + instanceURI
		} else {
			// no revision specified, using latest
			provenanceLine2 = "# According to latest approved images for the instance at " + instanceURI
		}
	} else if strings.HasPrefix(apiKeyId, "INSTANCE__") {
		instUUIDFromAPIKeyId := apiKeyId[10:37] // remove first 10 chars
		if len(revision) > 0 {
			provenanceLine2 = "# According to revision " + revision + " of the instance " + instUUIDFromAPIKeyId
		} else {
			// no revision specified, using latest
			provenanceLine2 = "# According to latest approved images for the instance " + instUUIDFromAPIKeyId
		}
	} else if strings.HasPrefix(apiKeyId, "CLUSTER__") {
		instUUIDFromAPIKeyId := apiKeyId[9:36] // remove first 9 chars
		if len(revision) > 0 {
			provenanceLine2 = "# According to revision " + revision + " of the instance " + instUUIDFromAPIKeyId
		} else {
			// no revision specified, using latest
			provenanceLine2 = "# According to latest approved images for the instance " + instUUIDFromAPIKeyId
		}
	} else {
		// should have at least one of those things
		provenanceLine2 = "missing replacetags input"
	}

	// Write provenance data to outfile (or stdout if no outfile)
	if outFileOpened != nil {
		outFileOpened.WriteString(provenanceLine1 + "\n")
		outFileOpened.WriteString(provenanceLine2 + "\n")
	} else {
		// If no outfile specified, write to stdout
		fmt.Print(provenanceLine1 + "\n")
		fmt.Print(provenanceLine2 + "\n")
	}
}

func scanTagFile(tagSourceFile string, typeVal string) map[string]string {
	tagFile, fileOpenErr := os.Open(tagSourceFile)
	if fileOpenErr != nil {
		fmt.Println("Error opening tagSourceFile = " + tagSourceFile)
		fmt.Println(fileOpenErr)
		os.Exit(1)
	}

	tagSourceMap := map[string]string{}

	if typeVal == "cyclonedx" {
		cycloneBytes, ioReadErr := io.ReadAll(tagFile)
		if ioReadErr != nil {
			fmt.Println("Error opening tagFile = " + tagSourceFile)
			fmt.Println(ioReadErr)
			os.Exit(1)
		}
		var bomJSON map[string]interface{}
		json.Unmarshal(cycloneBytes, &bomJSON)
		extractComponentsFromCycloneJSON(bomJSON, tagSourceMap)

	} else if typeVal == "text" {
		tagScanner := bufio.NewScanner(tagFile)
		for tagScanner.Scan() {
			line := tagScanner.Text()
			parseImageNameIntoMap(line, tagSourceMap)
		}
	}
	return tagSourceMap
}

func extractComponentsFromCycloneJSON(bomJSON map[string]interface{}, tagSourceMap map[string]string) map[string]string {
	var bomComponents []interface{}
	if components, ok := bomJSON["components"]; ok {
		bomComponents = components.([]interface{})
	} else {
		fmt.Println("Error: CycloneDX BOM components are empty!")
		os.Exit(1)
	}

	// bomComponents := bomJSON["components"].([]interface{})
	for _, bomc := range bomComponents {
		// check that type is container
		if bomc.(map[string]interface{})["type"] == "container" {

			resolvedImage := false

			// name is a mandatory field in cyclonedx spec
			contName := bomc.(map[string]interface{})["name"].(string)
			// version field may contain the tag
			contVersion := ""
			if bomc.(map[string]interface{})["version"] != nil {
				contVersion = bomc.(map[string]interface{})["version"].(string)
			}
			// Check if there's a containerSafeVersion property - use that instead for Docker tags
			if properties, ok := bomc.(map[string]interface{})["properties"].([]interface{}); ok {
				for _, prop := range properties {
					if propMap, ok := prop.(map[string]interface{}); ok {
						if propMap["name"] == "reliza:containerSafeVersion" {
							if safeVersion, ok := propMap["value"].(string); ok {
								contVersion = safeVersion
								break
							}
						}
					}
				}
			}

			// 1st try to parse purl if present

			if bomc.(map[string]interface{})["purl"] != nil {
				purl := bomc.(map[string]interface{})["purl"].(string)
				// sample purl pkg:docker/test-cont@sha256:testsha256hash?repository_url=123.dkr.ecr.us-east-1.amazonaws.com
				// remove pkg:docker/ thing first - must be there
				purl = strings.ReplaceAll(purl, "pkg:docker/", "")
				// check if the image is not on docker hub - split by ?repository_url= if present, otherewise repository is docker hub (ignore)
				purlImageName := purl
				if strings.Contains(purl, "?repository_url=") {
					purlImageName = strings.Split(purl, "?repository_url=")[1] + "/" + strings.Split(purl, "?repository_url=")[0]
				}
				parseImageNameIntoMap(purlImageName, tagSourceMap)
				resolvedImage = true
			}
			if !resolvedImage && bomc.(map[string]interface{})["hashes"] != nil {
				// if purl is not set - use name and hash if present, but only if hashes contain SHA-256 algorithm
				for _, hashEntry := range bomc.(map[string]interface{})["hashes"].([]interface{}) {
					alg := hashEntry.(map[string]interface{})["alg"].(string)
					if strings.Compare(alg, "SHA-256") == 0 {
						// take name and attach hash, include version/tag if present
						fullImageName := stripImageHashTag(contName)
						if len(contVersion) > 0 {
							fullImageName += ":" + contVersion
						}
						fullImageName += "@sha256:" + hashEntry.(map[string]interface{})["content"].(string)
						parseImageNameIntoMap(fullImageName, tagSourceMap)
						resolvedImage = true
						break
					}
				}
			}
			if !resolvedImage {
				// if both purl and hashes are not set - use only name and treat it same as text file case
				parseImageNameIntoMap(contName, tagSourceMap)
			}
		}
	}
	return tagSourceMap
}

/**
* This adds value into tag source map based on image name
 */
func parseImageNameIntoMap(imageName string, tagSourceMap map[string]string) {
	strippedImageName := stripImageHashTag(imageName)
	tagSourceMap[strippedImageName] = imageName
}

func stripImageHashTag(imageName string) string {
	strippedImageName := imageName
	strippedImageName = strings.Replace(strippedImageName, "http://", "", -1)
	strippedImageName = strings.Replace(strippedImageName, "https://", "", -1)
	strippedImageName = strings.Replace(strippedImageName, "oci://", "", -1)
	if strings.Contains(strippedImageName, "@") {
		sourceTagSplit := strings.Split(strippedImageName, "@")
		strippedImageName = sourceTagSplit[0]
	}
	if strings.Contains(strippedImageName, ":") {
		sourceTagSplit := strings.SplitN(strippedImageName, ":", 2)
		strippedImageName = sourceTagSplit[0]
	}
	return strippedImageName
}

func scanTags(replaceTagsVars ReplaceTagsVars) map[string]string {
	tagSourceMap := map[string]string{}
	if replaceTagsVars.TagSourceFile != "" {
		tagSourceMap = scanTagFile(replaceTagsVars.TagSourceFile, replaceTagsVars.TypeVal)
	} else if len(replaceTagsVars.Product) > 0 {
		cycloneBytes := getProductVersionCycloneDxExportV1(replaceTagsVars.ApiKeyId, replaceTagsVars.ApiKey, replaceTagsVars.Product, replaceTagsVars.Environment, replaceTagsVars.Version)
		var bomJSON map[string]interface{}
		json.Unmarshal(cycloneBytes, &bomJSON)
		extractComponentsFromCycloneJSON(bomJSON, tagSourceMap)
	} else if len(replaceTagsVars.Environment) > 0 {
		cycloneBytes := getEnvironmentCycloneDxExportV1(replaceTagsVars.ApiKeyId, replaceTagsVars.ApiKey, replaceTagsVars.Environment)
		var bomJSON map[string]interface{}
		json.Unmarshal(cycloneBytes, &bomJSON)
		extractComponentsFromCycloneJSON(bomJSON, tagSourceMap)
	} else if len(replaceTagsVars.Instance) > 0 || len(replaceTagsVars.InstanceURI) > 0 || strings.HasPrefix(replaceTagsVars.ApiKeyId, "INSTANCE__") || strings.HasPrefix(replaceTagsVars.ApiKeyId, "CLUSTER__") {
		cycloneBytes := getInstanceRevisionCycloneDxExportV1(replaceTagsVars.ApiKeyId, replaceTagsVars.ApiKey, replaceTagsVars.Instance, replaceTagsVars.Revision, replaceTagsVars.InstanceURI, replaceTagsVars.Namespace)
		var bomJSON map[string]interface{}
		json.Unmarshal(cycloneBytes, &bomJSON)
		extractComponentsFromCycloneJSON(bomJSON, tagSourceMap)
	} else {
		fmt.Println("Scan Tags Failed! specify either tagsource or instance or product and version")
		os.Exit(1)
	}
	return tagSourceMap
}

func getProductVersionCycloneDxExportV1(apiKeyId string, apiKey string, product string,
	environment string, version string) []byte {

	if len(product) <= 0 && (len(version) <= 0 || len(environment) <= 0) {
		//throw error and exit
		fmt.Println("Error: Product name and either version or environment must be provided!")
		os.Exit(1)
	}

	client := graphql.NewClient(rearmUri + "/graphql")
	req := graphql.NewRequest(`
		query ($productName: String!, $productVersion: String, $environment: String) {
			exportAsBomProg(productName: $productName, productVersion: $productVersion, environment: $environment)
		}
	`)
	req.Var("productName", product)
	req.Var("productVersion", version)
	req.Var("environment", environment)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Reliza Go Client")
	req.Header.Set("Accept-Encoding", "gzip, deflate")

	if len(apiKeyId) > 0 && len(apiKey) > 0 {
		auth := base64.StdEncoding.EncodeToString([]byte(apiKeyId + ":" + apiKey))
		req.Header.Add("Authorization", "Basic "+auth)
	}
	session, _ := getSession()
	if session != nil {
		req.Header.Set("X-CSRF-Token", session.Token)
		req.Header.Set("Cookie", "JSESSIONID="+session.JSessionId)
	}

	var respData map[string]string
	if err := client.Run(context.Background(), req, &respData); err != nil {
		printGqlError(err)
		os.Exit(1)
	}

	return []byte(respData["exportAsBomProg"])
}

func getEnvironmentCycloneDxExportV1(apiKeyId string, apiKey string, environment string) []byte {

	if len(environment) <= 0 {
		//throw error and exit
		fmt.Println("environment not specified!")
		os.Exit(1)
	}

	client := graphql.NewClient(rearmUri + "/graphql")
	req := graphql.NewRequest(`
		query ($environment: String!) {
			exportAsBomProgByEnv(environment: $environment)
		}
	`)
	req.Var("environment", environment)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Reliza Go Client")
	req.Header.Set("Accept-Encoding", "gzip, deflate")

	if len(apiKeyId) > 0 && len(apiKey) > 0 {
		auth := base64.StdEncoding.EncodeToString([]byte(apiKeyId + ":" + apiKey))
		req.Header.Add("Authorization", "Basic "+auth)
	}
	session, _ := getSession()
	if session != nil {
		req.Header.Set("X-CSRF-Token", session.Token)
		req.Header.Set("Cookie", "JSESSIONID="+session.JSessionId)
	}

	var respData map[string]string
	if err := client.Run(context.Background(), req, &respData); err != nil {
		printGqlError(err)
		os.Exit(1)
	}

	return []byte(respData["exportAsBomProgByEnv"])
}

type KeyValueSorted struct {
	Key    string
	Value  Substitution
	Length int
}
