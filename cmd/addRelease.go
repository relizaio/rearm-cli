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
)

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

	if len(identities) > 0 && len(identities) != len(odelId) {
		fmt.Println("number of --identities flags must be either zero or match number of --odelid flags")
		os.Exit(2)
	} else if len(identities) > 0 {
		for i, delIdentities := range identities {
			identityPairs := strings.Split(delIdentities, ",")
			var bomIdentities []BomIdentity
			for _, identityPair := range identityPairs {
				keyValue := strings.Split(identityPair, ":")
				if len(keyValue) != 2 {
					fmt.Println("Each tag should have key and value")
					os.Exit(2)
				}
				bomIdentities = append(bomIdentities, BomIdentity{
					IdenityType: keyValue[0],
					Identity:    keyValue[1],
				})
			}
			outboundDeliverables[i]["identities"] = bomIdentities
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
				fmt.Println("Error parsing Artifact Input: ", err)
			} else {
				artifactsObject := make([]Artifact, len(artifactsInput))
				for j, artifactInput := range artifactsInput {
					artifactsObject[j] = Artifact{}
					if artifactInput.FilePath != "" {
						fileBytes, err := os.ReadFile(artifactInput.FilePath)
						if err != nil {
							fmt.Println("Error reading file: ", err)
						} else {
							*filesCounter++
							currentIndex := strconv.Itoa(*filesCounter)

							(*locationMap)[currentIndex] = []string{"variables.releaseInputProg.outboundDeliverables." + strconv.Itoa(i) + ".artifacts." + strconv.Itoa(j) + ".file"}
							(*filesMap)[currentIndex] = fileBytes
							artifactInput.File = nil

						}
						artifactInput.FilePath = ""
						artifactInput.StripBom = strings.ToUpper(stripBom)
						artifactsObject[j] = artifactInput
					}
				}
				// TODO: replace file path with actual file
				outboundDeliverables[i]["artifacts"] = artifactsObject
			}
		}
	}
	return &outboundDeliverables
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
		if len(odelId) > 0 {
			body["outboundDeliverables"] = *buildOutboundDeliverables(&filesCounter, &locationMap, &filesMap)
		}

		if commit != "" {
			commitMap := map[string]interface{}{"uri": vcsUri, "type": vcsType, "commit": commit, "commitMessage": commitMessage}
			if vcsTag != "" {
				commitMap["vcsTag"] = vcsTag
			}
			if dateActual != "" {
				commitMap["dateActual"] = dateActual
			}
			if sceArts != "" {
				var sceArtifacts []Artifact
				err := json.Unmarshal([]byte(sceArts), &sceArtifacts)
				if err != nil {
					fmt.Println("Error parsing Artifact Input: ", err)
				} else {
					artifactsObject := make([]Artifact, len(sceArtifacts))
					for j, artifactInput := range sceArtifacts {
						if artifactInput.FilePath != "" {
							fileBytes, err := os.ReadFile(artifactInput.FilePath)
							artifactInput.FilePath = ""
							if err != nil {
								fmt.Println("Error reading file: ", err)
							} else {
								filesCounter++
								currentIndex := strconv.Itoa(filesCounter)

								locationMap[currentIndex] = []string{"variables.releaseInputProg.sourceCodeEntry.artifacts." + strconv.Itoa(j) + ".file"}
								filesMap[currentIndex] = fileBytes
								artifactInput.File = nil
							}
							artifactInput.FilePath = ""
							artifactInput.StripBom = strings.ToUpper(stripBom)
							artifactsObject[j] = artifactInput
						}
					}
					// TODO: replace file path with actual file
					commitMap["artifacts"] = artifactsObject
				}

			}
			body["sourceCodeEntry"] = commitMap
		}

		if len(commits) > 0 {
			// fmt.Println(commits)
			plainCommits, err := base64.StdEncoding.DecodeString(commits)
			if err != nil {
				fmt.Println(err)
				os.Exit(1)
			}
			indCommits := strings.Split(string(plainCommits), "\n")
			commitsInBody := make([]map[string]interface{}, len(indCommits)-1)
			for i := range indCommits {
				if len(indCommits[i]) > 0 {
					singleCommitEl := map[string]interface{}{}
					commitParts := strings.Split(indCommits[i], "|||")
					singleCommitEl["commit"] = commitParts[0]
					singleCommitEl["dateActual"] = commitParts[1]
					singleCommitEl["commitMessage"] = commitParts[2]
					if len(commitParts) > 3 {
						singleCommitEl["commitAuthor"] = commitParts[3]
						singleCommitEl["commitEmail"] = commitParts[4]
					}
					commitsInBody[i] = singleCommitEl

					// if commit is not present but we are here, use first line as commit
					if len(commit) < 1 && i == 0 {
						var commitMap map[string]interface{}
						if len(commitParts) > 3 {
							commitMap = map[string]interface{}{"commit": commitParts[0], "dateActual": commitParts[1], "commitMessage": commitParts[2], "commitAuthor": commitParts[3], "commitEmail": commitParts[4]}
						} else {
							commitMap = map[string]interface{}{"commit": commitParts[0], "dateActual": commitParts[1], "commitMessage": commitParts[2]}
						}
						if vcsTag != "" {
							commitMap["vcsTag"] = vcsTag
						}
						if vcsUri != "" {
							commitMap["uri"] = vcsUri
						}
						if vcsType != "" {
							commitMap["type"] = vcsType
						}

						body["sourceCodeEntry"] = commitMap
					}
				}
			}
			body["commits"] = commitsInBody
		}
		if releaseArts != "" {
			var releaseArtifacts []Artifact
			err := json.Unmarshal([]byte(releaseArts), &releaseArtifacts)
			if err != nil {
				fmt.Println("Error parsing Artifact Input: ", err)
			} else {
				artifactsObject := make([]Artifact, len(releaseArtifacts))
				for j, artifactInput := range releaseArtifacts {
					if artifactInput.FilePath != "" {
						fileBytes, err := os.ReadFile(artifactInput.FilePath)
						if err != nil {
							fmt.Println("Error reading file: ", err)
						} else {
							artifactInput.File = fileBytes

							filesCounter++
							currentIndex := strconv.Itoa(filesCounter)

							locationMap[currentIndex] = []string{"variables.releaseInputProg.artifacts." + strconv.Itoa(j) + ".file"}
							filesMap[currentIndex] = fileBytes
							artifactInput.File = nil
						}
						artifactInput.FilePath = ""
						artifactInput.StripBom = strings.ToUpper(stripBom)
						artifactsObject[j] = artifactInput
					}
				}
				// TODO: replace file path with actual file
				body["artifacts"] = artifactsObject
			}

		}

		if fsBomPath != "" {
			body["fsBom"] = RawBomInput{RawBom: ReadBomJsonFromFile(fsBomPath), BomType: "APPLICATION"}
		}

		if sceArts != "" {
			var sceArtifacts []Artifact
			err := json.Unmarshal([]byte(sceArts), &sceArtifacts)
			if err != nil {
				fmt.Println("Error parsing Artifact Input: ", err)
			} else {
				artifactsObject := make([]Artifact, len(sceArtifacts))
				for j, artifactInput := range sceArtifacts {
					if artifactInput.FilePath != "" {
						fileBytes, err := os.ReadFile(artifactInput.FilePath)
						if err != nil {
							fmt.Println("Error reading file: ", err)
						} else {
							filesCounter++
							currentIndex := strconv.Itoa(filesCounter)
							locationMap[currentIndex] = []string{"variables.releaseInputProg.sceArts." + strconv.Itoa(j) + ".file"}
							filesMap[currentIndex] = fileBytes
							artifactInput.File = nil
						}
						artifactInput.FilePath = ""
						artifactInput.StripBom = strings.ToUpper(stripBom)
						artifactsObject[j] = artifactInput
					}
				}
				body["sceArts"] = artifactsObject
			}

		}

		// 		fmt.Println(body)
		jsonBody, _ := json.Marshal(body)
		if debug == "true" {
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
			if bytesValue, ok := value.([]byte); ok {
				c.SetFileReader(key, key, bytes.NewReader(bytesValue))
			} else {
				// Handle error case: value is not []byte
				fmt.Printf("Warning: Value for key '%s' is not []byte\n", key)
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

		printResponse(err, resp)
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
	addreleaseCmd.PersistentFlags().StringArrayVar(&identities, "identities", []string{}, "Deliverable Identity IdenityType-Idenity Pairs (multiple allowed, separate several IdenityType-Idenity pairs for one Deliverable with commas, and seprate IdenityType-Idenity in a pair with colon)")
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
	rootCmd.AddCommand(addreleaseCmd)
}
