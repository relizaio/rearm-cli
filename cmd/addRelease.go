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
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-resty/resty/v2"
	"github.com/spf13/cobra"
)

var (
	odelArtsJson []string
	odelBuildId  []string
	odelBuildUri []string
	odelCiMeta   []string
	odelDigests  []string
	odelId       []string
	odelType     []string
	releaseArts  string
	sceArts      string

	odelVersion   []string
	odelPublisher []string
	odelGroup     []string
	odelPackage   []string

	createComponentIfMissing           bool
	createComponentVersionSchema       string
	createComponentBranchVersionSchema string
	rebuildRelease                     bool
	vcsDisplayName                     string
)

type Identifier struct {
	IdType  string `json:"idType"`
	IdValue string `json:"idValue"`
}

func buildOutboundDeliverables(filesCounter *int, locationMap *map[string][]string, filesMap *map[string]interface{}) *[]map[string]interface{} {
	outboundDeliverables := make([]map[string]interface{}, len(odelId))
	softwareMetadatas := make([]map[string]interface{}, len(odelId))
	for i, aid := range odelId {
		outboundDeliverables[i] = map[string]interface{}{"displayIdentifier": aid}
		softwareMetadatas[i] = map[string]interface{}{}
	}

	// now do some length validations and add elements
	if len(odelBuildId) > 0 && len(odelBuildId) != len(odelId) {
		fmt.Println("number of --odelBuildId flags must be either zero or match number of --odelid flags")
		os.Exit(2)
	} else if len(odelBuildId) > 0 {
		for i, abid := range odelBuildId {
			softwareMetadatas[i]["buildId"] = abid
		}
	}

	if len(odelBuildUri) > 0 && len(odelBuildUri) != len(odelId) {
		fmt.Println("number of --odelbuildUri flags must be either zero or match number of --odelid flags")
		os.Exit(2)
	} else if len(odelBuildUri) > 0 {
		for i, aburi := range odelBuildUri {
			softwareMetadatas[i]["buildUri"] = aburi
		}
	}

	if len(odelCiMeta) > 0 && len(odelCiMeta) != len(odelId) {
		fmt.Println("number of --odelcimeta flags must be either zero or match number of --odelid flags")
		os.Exit(2)
	} else if len(odelCiMeta) > 0 {
		for i, acm := range odelCiMeta {
			softwareMetadatas[i]["cicdMeta"] = acm
		}
	}

	if len(odelDigests) > 0 && len(odelDigests) != len(odelId) {
		fmt.Println("number of --odeldigests flags must be either zero or match number of --odelid flags")
		os.Exit(2)
	} else if len(odelDigests) > 0 {
		for i, ad := range odelDigests {
			adSpl := strings.Split(ad, ",")
			softwareMetadatas[i]["digests"] = adSpl
		}
	}

	if len(dateStart) > 0 && len(dateStart) != len(odelId) {
		fmt.Println("number of --datestart flags must be either zero or match number of --odelid flags")
		os.Exit(2)
	} else if len(dateStart) > 0 {
		for i, ds := range dateStart {
			softwareMetadatas[i]["dateFrom"] = ds
		}
	}

	if len(dateEnd) > 0 && len(dateEnd) != len(odelId) {
		fmt.Println("number of --dateEnd flags must be either zero or match number of --odelid flags")
		os.Exit(2)
	} else if len(dateEnd) > 0 {
		for i, de := range dateEnd {
			softwareMetadatas[i]["dateTo"] = de
		}
	}

	if len(odelPackage) > 0 && len(odelPackage) != len(odelId) {
		fmt.Println("number of --odelpackage flags must be either zero or match number of --odelid flags")
		os.Exit(2)
	} else if len(odelPackage) > 0 {
		for i, ap := range odelPackage {
			softwareMetadatas[i]["packageType"] = strings.ToUpper(ap)
		}
	}

	for i := range odelId {
		outboundDeliverables[i]["softwareMetadata"] = softwareMetadatas[i]
	}

	if len(odelType) > 0 && len(odelType) != len(odelId) {
		fmt.Println("number of --odeltype flags must be either zero or match number of --odelid flags")
		os.Exit(2)
	} else if len(odelType) > 0 {
		for i, at := range odelType {
			outboundDeliverables[i]["type"] = at
		}
	}

	if len(supportedOsArr) > 0 && len(supportedOsArr) != len(odelId) {
		fmt.Println("number of --osarr flags must be either zero or match number of --odelid flags")
		os.Exit(2)
	} else if len(supportedOsArr) > 0 {
		for i, ad := range supportedOsArr {
			adSpl := strings.Split(ad, ",")
			outboundDeliverables[i]["supportedOs"] = adSpl
		}
	}
	if len(supportedCpuArchArr) > 0 && len(supportedCpuArchArr) != len(odelId) {
		fmt.Println("number of --supportedcpuarcharr flags must be either zero or match number of --odelid flags")
		os.Exit(2)
	} else if len(supportedCpuArchArr) > 0 {
		for i, ad := range supportedCpuArchArr {
			adSpl := strings.Split(ad, ",")
			outboundDeliverables[i]["supportedCpuArchitectures"] = adSpl
		}
	}

	if len(odelVersion) > 0 && len(odelVersion) != len(odelId) {
		fmt.Println("number of --odelversion flags must be either zero or match number of --odelid flags")
		os.Exit(2)
	} else if len(odelVersion) > 0 {
		for i, av := range odelVersion {
			outboundDeliverables[i]["version"] = av
		}
	}

	if len(odelPublisher) > 0 && len(odelPublisher) != len(odelId) {
		fmt.Println("number of --odelpublisher flags must be either zero or match number of --odelid flags")
		os.Exit(2)
	} else if len(odelPublisher) > 0 {
		for i, ap := range odelPublisher {
			outboundDeliverables[i]["publisher"] = ap
		}
	}

	if len(odelGroup) > 0 && len(odelGroup) != len(odelId) {
		fmt.Println("number of --odelgroup flags must be either zero or match number of --odelid flags")
		os.Exit(2)
	} else if len(odelGroup) > 0 {
		for i, ag := range odelGroup {
			outboundDeliverables[i]["group"] = ag
		}
	}

	if len(tagsArr) > 0 && len(tagsArr) != len(odelId) {
		fmt.Println("number of --tagsarr flags must be either zero or match number of --odelid flags")
		os.Exit(2)
	} else if len(tagsArr) > 0 {
		for i, tags := range tagsArr {
			tagPairs := strings.Split(tags, ",")
			var tags []TagInput
			for _, tagPair := range tagPairs {
				keyValue := strings.Split(tagPair, ":")
				if len(keyValue) != 2 {
					fmt.Println("Each tag should have key and value")
					os.Exit(2)
				}
				tags = append(tags, TagInput{
					Key:   keyValue[0],
					Value: keyValue[1],
				})
			}
			outboundDeliverables[i]["tags"] = tags
		}
	}

	if len(identifiers) > 0 && len(identifiers) != len(odelId) {
		fmt.Println("number of --identifiers flags must be either zero or match number of --odelid flags")
		os.Exit(2)
	} else if len(identifiers) > 0 {
		for i, delIdentifiers := range identifiers {
			identityPairs := strings.Split(delIdentifiers, ",")
			var identifiers []Identifier
			for _, identityPair := range identityPairs {
				keyValue := strings.SplitN(identityPair, ":", 2)
				if len(keyValue) != 2 {
					fmt.Println("Each tag should have key and value")
					os.Exit(2)
				}
				identifiers = append(identifiers, Identifier{
					IdType:  keyValue[0],
					IdValue: keyValue[1],
				})
			}
			outboundDeliverables[i]["identifiers"] = identifiers
		}
	}
	if len(odelArtsJson) > 0 && len(odelArtsJson) != len(odelId) {
		fmt.Println("number of --odelartsjson flags must be either zero or match number of --odelid flags")
		os.Exit(2)
	} else if len(odelArtsJson) > 0 {
		for i, artifactsInputString := range odelArtsJson {
			var artifactsInput []Artifact
			err := json.Unmarshal([]byte(artifactsInputString), &artifactsInput)
			if err != nil {
				fmt.Fprintln(os.Stderr, "Error parsing Artifact Input: ", err)
				os.Exit(1)
			} else {
				injectCoverageTypeTags(&artifactsInput, artCoverageType)
				indexPrefix := "variables.releaseInputProg.outboundDeliverables." + strconv.Itoa(i) + ".artifacts."
				outboundDeliverables[i]["artifacts"] = *processArtifactsInput(&artifactsInput, indexPrefix, filesCounter, locationMap, filesMap)
			}
		}
	}
	return &outboundDeliverables
}

type Commit struct {
	Commit        string     `json:"commit"`
	CommitMessage string     `json:"commitMessage"`
	CommitAuthor  string     `json:"commitAuthor,omitempty"`
	CommitEmail   string     `json:"commitEmail,omitempty"`
	DateActual    string     `json:"dateActual,omitempty"`
	Uri           string     `json:"uri"`
	Type          string     `json:"type"` // vcs type
	VcsTag        string     `json:"vcsTag,omitempty"`
	Artifacts     []Artifact `json:"artifacts"`
}

func processArtifactsInput(artifactsInput *[]Artifact, indexPrefix string, filesCounter *int,
	locationMap *map[string][]string, filesMap *map[string]interface{}) *[]Artifact {
	// TODO: replace file path with actual file
	artifactsObject := make([]Artifact, len(*artifactsInput))
	for j, artifactInput := range *artifactsInput {
		artifactsObject[j] = *processSingleArtifactInput(&artifactInput, indexPrefix, j, filesCounter, locationMap, filesMap)
	}
	return &artifactsObject
}

func processSingleArtifactInput(artInput *Artifact, indexPrefix string, fileJCounter int, filesCounter *int,
	locationMap *map[string][]string, filesMap *map[string]interface{}) *Artifact {
	// TODO: replace file path with actual file
	if len((*artInput).Artifacts) > 0 {
		updIndex := indexPrefix + strconv.Itoa(fileJCounter) + ".artifacts."
		(*artInput).Artifacts = *processArtifactsInput(&(*artInput).Artifacts, updIndex, filesCounter, locationMap, filesMap)
	}
	// File path is required for artifacts
	if (*artInput).FilePath == "" {
		fmt.Fprintln(os.Stderr, "Error: filePath is required for each artifact")
		os.Exit(1)
	}
	fileBytes, err := os.ReadFile(artInput.FilePath)
	if err != nil {
		fmt.Println("Error reading file: ", err)
		os.Exit(1)
	}
	*filesCounter++
	currentIndex := strconv.Itoa(*filesCounter)

	(*locationMap)[currentIndex] = []string{indexPrefix + strconv.Itoa(fileJCounter) + ".file"}
	(*filesMap)[currentIndex] = FileData{
		Bytes:    fileBytes,
		Filename: sanitizeFilename(filepath.Base(artInput.FilePath)),
	}
	artInput.File = nil
	(*artInput).FilePath = ""
	(*artInput).StripBom = strings.ToUpper(stripBom)
	return artInput
}

func buildCommitMap(filesCounter *int, locationMap *map[string][]string, filesMap *map[string]interface{}) *Commit {
	var commitObj Commit
	// TODO commit author and email is missing from here
	commitObj.Uri = vcsUri
	commitObj.Type = vcsType
	commitObj.Commit = commit
	commitObj.CommitMessage = commitMessage
	commitObj.VcsTag = vcsTag
	commitObj.DateActual = dateActual
	if sceArts != "" {
		var sceArtifacts []Artifact
		err := json.Unmarshal([]byte(sceArts), &sceArtifacts)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error parsing Artifact Input: ", err)
			os.Exit(1)
		} else {
			injectCoverageTypeTags(&sceArtifacts, artCoverageType)
			indexPrefix := "variables.releaseInputProg.sourceCodeEntry.artifacts."
			artifactsObject := *processArtifactsInput(&sceArtifacts, indexPrefix, filesCounter, locationMap, filesMap)
			commitObj.Artifacts = artifactsObject
		}
	}
	return &commitObj
}

func buildCommitsInBody() *[]Commit {
	plainCommits, err := base64.StdEncoding.DecodeString(commits)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	indCommits := strings.Split(string(plainCommits), "\n")
	commitsInBody := make([]Commit, len(indCommits)-1)
	for i := range indCommits {
		if len(indCommits[i]) > 0 {
			var singleCommitEl Commit
			commitParts := strings.Split(indCommits[i], "|||")
			singleCommitEl.Commit = commitParts[0]
			singleCommitEl.DateActual = commitParts[1]
			singleCommitEl.CommitMessage = commitParts[2]
			if len(commitParts) > 3 {
				singleCommitEl.CommitAuthor = commitParts[3]
				singleCommitEl.CommitEmail = commitParts[4]
			}
			commitsInBody[i] = singleCommitEl
		}
	}
	return &commitsInBody
}

func buildReleaseArts(filesCounter *int, locationMap *map[string][]string, filesMap *map[string]interface{}) *[]Artifact {
	var releaseArtifacts []Artifact
	var artifactsObject []Artifact
	err := json.Unmarshal([]byte(releaseArts), &releaseArtifacts)
	if err != nil {
		fmt.Println("Error parsing Release Artifact Input: ", err)
		os.Exit(1)
	} else {
		injectCoverageTypeTags(&releaseArtifacts, artCoverageType)
		indexPrefix := "variables.releaseInputProg.artifacts."
		artifactsObject = *processArtifactsInput(&releaseArtifacts, indexPrefix, filesCounter, locationMap, filesMap)
	}
	// TODO: replace file path with actual file
	return &artifactsObject
}

func buildSceArts(filesCounter *int, locationMap *map[string][]string, filesMap *map[string]interface{}) *[]Artifact {
	var sceArtifacts []Artifact
	var artifactsObject []Artifact
	err := json.Unmarshal([]byte(sceArts), &sceArtifacts)
	if err != nil {
		fmt.Println("Error parsing SCE Artifact Input: ", err)
		os.Exit(1)
	} else {
		injectCoverageTypeTags(&sceArtifacts, artCoverageType)
		indexPrefix := "variables.releaseInputProg.sceArts."
		artifactsObject = *processArtifactsInput(&sceArtifacts, indexPrefix, filesCounter, locationMap, filesMap)
	}
	return &artifactsObject
}

var addreleaseCmd = &cobra.Command{
	Use:   "addrelease",
	Short: "Creates release on ReARM",
	Long: `This CLI command would create new releases on ReARM
			for authenticated component.`,
	Run: func(cmd *cobra.Command, args []string) {
		if debug == "true" {
			fmt.Println("Using ReARM at", rearmUri)
		}
		locationMap := make(map[string][]string)
		filesMap := make(map[string]interface{})
		filesCounter := 0

		body := map[string]interface{}{"branch": branch, "version": version}
		if len(lifecycle) > 0 {
			body["lifecycle"] = strings.ToUpper(lifecycle)
		}
		if len(endpoint) > 0 {
			body["endpoint"] = endpoint
		}
		if len(component) > 0 {
			body["component"] = component
		}
		// Add VCS-based component identification parameters
		if len(vcsUri) > 0 {
			body["vcsUri"] = vcsUri
			if len(repoPath) > 0 {
				body["repoPath"] = repoPath
			}
			if len(vcsDisplayName) > 0 {
				body["vcsDisplayName"] = vcsDisplayName
			}
		}
		if rebuildRelease {
			body["rebuildRelease"] = true
		}
		// Add createComponentIfMissing options (requires org-wide read-write key)
		if createComponentIfMissing {
			body["createComponentIfMissing"] = true
			if len(createComponentVersionSchema) > 0 {
				body["createComponentVersionSchema"] = createComponentVersionSchema
			}
			if len(createComponentBranchVersionSchema) > 0 {
				body["createComponentFeatureBranchVersionSchema"] = createComponentBranchVersionSchema
			}
		}
		if len(odelId) > 0 {
			body["outboundDeliverables"] = *buildOutboundDeliverables(&filesCounter, &locationMap, &filesMap)
		}

		if commit != "" {
			body["sourceCodeEntry"] = *buildCommitMap(&filesCounter, &locationMap, &filesMap)
		}

		if len(commits) > 0 {
			// fmt.Println(commits)
			bodyCommits := *buildCommitsInBody()
			body["commits"] = bodyCommits
			// if commit is not present but we are here, use first line as commit
			if len(commit) < 1 && len(bodyCommits) > 0 {
				mainCommitFromBody := bodyCommits[0]
				if vcsTag != "" {
					mainCommitFromBody.VcsTag = vcsTag
				}
				if vcsUri != "" {
					mainCommitFromBody.Uri = vcsUri
				}
				if vcsType != "" {
					mainCommitFromBody.Type = vcsType
				}

				body["sourceCodeEntry"] = mainCommitFromBody
			}
		}
		if releaseArts != "" {
			body["artifacts"] = *buildReleaseArts(&filesCounter, &locationMap, &filesMap)
		}

		if fsBomPath != "" {
			body["fsBom"] = RawBomInput{RawBom: ReadBomJsonFromFile(fsBomPath), BomType: "APPLICATION"}
		}

		if sceArts != "" {
			body["sceArts"] = *buildSceArts(&filesCounter, &locationMap, &filesMap)
		}

		if debug == "true" {
			jsonBody, _ := json.Marshal(body)
			fmt.Println(string(jsonBody))
		}

		od := make(map[string]interface{})
		od["operationName"] = "addReleaseProgrammatic"
		od["variables"] = map[string]interface{}{"releaseInputProg": body}
		od["query"] = `mutation addReleaseProgrammatic($releaseInputProg: ReleaseInputProg!) {addReleaseProgrammatic(release:$releaseInputProg) {` + RELEASE_GQL_DATA + `}}`

		jsonOd, _ := json.Marshal(od)
		operations := map[string]string{"operations": string(jsonOd)}

		fileMapJson, _ := json.Marshal(locationMap)
		fileMapFd := map[string]string{"map": string(fileMapJson)}
		// write a wrapper to send the gql upload request via post form data
		client := resty.New()
		session, _ := getSession()
		if session != nil {
			client.SetHeader("X-CSRF-Token", session.Token)
			client.SetHeader("Cookie", "JSESSIONID="+session.JSessionId)
		}
		if len(apiKeyId) > 0 && len(apiKey) > 0 {
			auth := base64.StdEncoding.EncodeToString([]byte(apiKeyId + ":" + apiKey))
			client.SetHeader("Authorization", "Basic "+auth)
		}
		c := client.R()
		for key, value := range filesMap {
			if fileData, ok := value.(FileData); ok {
				c.SetFileReader(key, fileData.Filename, bytes.NewReader(fileData.Bytes))
			} else {
				// Handle error case: value is not FileData
				fmt.Printf("Warning: Value for key '%s' is not FileData\n", key)
			}
		}

		resp, err := c.SetHeader("Content-Type", "multipart/form-data").
			SetHeader("User-Agent", "ReARM CLI").
			SetHeader("Accept-Encoding", "gzip, deflate").
			SetHeader("Apollo-Require-Preflight", "true").
			SetMultipartFormData(operations).
			SetMultipartFormData(fileMapFd).
			SetBasicAuth(apiKeyId, apiKey).
			Post(rearmUri + "/graphql")

		handleResponse(err, resp)
	},
}

func init() {
	addreleaseCmd.PersistentFlags().StringVarP(&branch, "branch", "b", "", "Name of VCS Branch used")
	addreleaseCmd.PersistentFlags().StringVarP(&version, "version", "v", "", "Release version")
	addreleaseCmd.MarkPersistentFlagRequired("version")
	addreleaseCmd.MarkPersistentFlagRequired("branch")
	addreleaseCmd.PersistentFlags().StringVar(&endpoint, "endpoint", "", "Test endpoint for this release")
	addreleaseCmd.PersistentFlags().StringVar(&component, "component", "", "Component UUID for this release if org-wide key is used")
	addreleaseCmd.PersistentFlags().StringVar(&vcsUri, "vcsuri", "", "URI of VCS repository")
	addreleaseCmd.PersistentFlags().StringVar(&repoPath, "repo-path", "", "Repository path for monorepo components")
	addreleaseCmd.PersistentFlags().StringVar(&vcsDisplayName, "vcs-display-name", "", "Display name for VCS repository (optional, used when auto-creating VCS)")
	addreleaseCmd.PersistentFlags().StringVar(&vcsType, "vcstype", "", "Type of VCS repository: git, svn, mercurial")
	addreleaseCmd.PersistentFlags().StringVar(&commit, "commit", "", "Commit id")
	addreleaseCmd.PersistentFlags().StringVar(&commitMessage, "commitmessage", "", "Commit message or subject (optional)")
	addreleaseCmd.PersistentFlags().StringVar(&commits, "commits", "", "Base64-encoded list of commits associated with this release, can be obtained with 'git log --date=iso-strict --pretty='%H|||%ad|||%s' | base64 -w 0' command (optional)")
	addreleaseCmd.PersistentFlags().StringVar(&vcsTag, "vcstag", "", "VCS Tag")
	addreleaseCmd.PersistentFlags().StringVar(&dateActual, "date", "", "Commit date and time in iso strict format, use git log --date=iso-strict (optional).")
	addreleaseCmd.PersistentFlags().StringArrayVar(&odelId, "odelid", []string{}, "Deliverable ID (multiple allowed)")
	addreleaseCmd.PersistentFlags().StringArrayVar(&odelBuildId, "odelbuildid", []string{}, "Deliverable Build ID (multiple allowed)")
	addreleaseCmd.PersistentFlags().StringArrayVar(&odelBuildUri, "odelbuilduri", []string{}, "Deliverable Build URI (multiple allowed)")
	addreleaseCmd.PersistentFlags().StringArrayVar(&odelCiMeta, "odelcimeta", []string{}, "Deliverable CI Meta (multiple allowed)")
	addreleaseCmd.PersistentFlags().StringArrayVar(&odelType, "odeltype", []string{}, "Deliverable Type (multiple allowed)")
	addreleaseCmd.PersistentFlags().StringArrayVar(&odelDigests, "odeldigests", []string{}, "Deliverable Digests (multiple allowed, separate several digests for one Deliverable with commas)")
	addreleaseCmd.PersistentFlags().StringArrayVar(&tagsArr, "tagsarr", []string{}, "Deliverable Tag Key-Value Pairs (multiple allowed, separate several tag key-value pairs for one Deliverable with commas, and seprate key-value in a pair with colon)")
	addreleaseCmd.PersistentFlags().StringArrayVar(&identifiers, "odelidentifiers", []string{}, "Deliverable Identifiers IdentifierType-IdentifierValue Pairs (multiple allowed, separate several IdentityType-Identity pairs for one Deliverable with commas, and seprate IdentityType-Identity in a pair with colon)")
	addreleaseCmd.PersistentFlags().StringArrayVar(&dateStart, "datestart", []string{}, "Deliverable Build Start date and time (optional, multiple allowed)")
	addreleaseCmd.PersistentFlags().StringArrayVar(&dateEnd, "dateend", []string{}, "Deliverable Build End date and time (optional, multiple allowed)")
	addreleaseCmd.PersistentFlags().StringArrayVar(&odelVersion, "odelversion", []string{}, "Deliverable version, if different from release (multiple allowed)")
	addreleaseCmd.PersistentFlags().StringArrayVar(&odelPackage, "odelpackage", []string{}, "Deliverable package type (multiple allowed)")
	// addreleaseCmd.PersistentFlags().StringArrayVar(&odelName, "odelname", []string{}, "Deliverable name (multiple allowed)")
	addreleaseCmd.PersistentFlags().StringArrayVar(&odelPublisher, "odelpublisher", []string{}, "Deliverable publisher (multiple allowed)")
	addreleaseCmd.PersistentFlags().StringArrayVar(&odelGroup, "odelgroup", []string{}, "Deliverable group (multiple allowed)")
	addreleaseCmd.PersistentFlags().StringArrayVar(&supportedOsArr, "osarr", []string{}, "Deliverable supported OS array (multiple allowed, use comma seprated values for each deliverable)")
	addreleaseCmd.PersistentFlags().StringArrayVar(&supportedCpuArchArr, "cpuarr", []string{}, "Deliverable supported CPU array (multiple allowed, use comma seprated values for each deliverable)")
	addreleaseCmd.PersistentFlags().StringArrayVar(&odelArtsJson, "odelartsjson", []string{}, "Deliverable Artifacts json array (multiple allowed, use a json array for each deliverable)")
	addreleaseCmd.PersistentFlags().StringVar(&releaseArts, "releasearts", "", "Release Artifacts json array")
	addreleaseCmd.PersistentFlags().StringVar(&sceArts, "scearts", "", "Source Code Entry Artifacts json array")
	addreleaseCmd.PersistentFlags().StringVar(&lifecycle, "lifecycle", "DRAFT", "Lifecycle of release - set to 'REJECTED' for failed releases, otherwise 'DRAFT' or 'ASSEMBLED' are possible options (optional, default value is 'DRAFT').")
	addreleaseCmd.PersistentFlags().StringVar(&stripBom, "stripbom", "true", "(Optional) Set --stripbom false to disable striping bom for digest matching.")
	addreleaseCmd.PersistentFlags().BoolVar(&createComponentIfMissing, "createcomponent", false, "(Optional) Create component if it doesn't exist. Requires organization-wide read-write API key.")
	addreleaseCmd.PersistentFlags().StringVar(&createComponentVersionSchema, "createcomponent-version-schema", "", "(Optional) Version schema for new component (e.g., 'semver'). Only used with --createcomponent. Requires organization-wide read-write API key.")
	addreleaseCmd.PersistentFlags().StringVar(&createComponentBranchVersionSchema, "createcomponent-branch-version-schema", "", "(Optional) Feature branch version schema for new component. Only used with --createcomponent. Requires organization-wide read-write API key.")
	addreleaseCmd.PersistentFlags().BoolVar(&rebuildRelease, "rebuild", false, "(Optional) Allow rebuilding release on repeated CI reruns. Default is false.")
	addreleaseCmd.PersistentFlags().StringVar(&artCoverageType, "artcoveragetype", "", "(Optional) Comma-separated artifact coverage types to apply to all artifacts (e.g., 'DEV', 'TEST', 'BUILD_TIME', or 'DEV,BUILD_TIME')")
	rootCmd.AddCommand(addreleaseCmd)
}
