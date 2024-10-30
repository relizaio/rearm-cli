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
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-resty/resty/v2"
	"github.com/machinebox/graphql"
	"github.com/mitchellh/go-homedir"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/spf13/viper"
)

var action string
var aggregated bool
var apiKeyId string
var apiKey string

// var artBuildId []string
// var dilBuildId []string
// var artBomFilePaths []string
// var dilCiMeta []string
// var dilDigests []string
// var artId []string
// var dilType []string

// var artifactType string
// var dilVersion []string
// var dilPublisher []string
// var dilGroup []string
// var dilPackage []string
var dilArtsJson []string
var dilBuildId []string
var dilBuildUri []string
var dilBomFilePaths []string
var dilCiMeta []string
var dilDigests []string
var dilId []string
var dilType []string
var releaseArts string
var sceArts string

// var dilifactType string
var dilVersion []string
var dilPublisher []string
var dilGroup []string
var dilPackage []string

// var dilName []string
var branch string
var bundle string
var cfgFile string
var closedDate string
var commit string
var commit2 string
var commitMessage string
var commits string // base64-encoded list of commits obtained with: git log $LATEST_COMMIT..$CURRENT_COMMIT --date=iso-strict --pretty='%H|||%ad|||%s' | base64 -w 0
var createdDate string
var dateActual string
var dateStart []string
var dateEnd []string
var debug string
var defaultBranch string
var endpoint string
var environment string
var featureBranchVersioning string
var filePath string
var fsBomPath string
var hash string
var identities []string
var includeApi bool
var instance string
var manual bool
var mergedDate string
var metadata string
var modifier string
var namespace string
var number string
var onlyVersion bool
var infile string
var outfile string
var releaseId string
var releaseVersion string
var rearmUri string
var component string
var componentName string
var componentType string
var state string
var status string
var supportedOsArr []string
var supportedCpuArchArr []string
var tagKey string

// var tagKeyArr []string
var tagVal string

// var tagValArr []string
var tagsArr []string
var targetBranch string
var title string
var version string
var version2 string
var versionSchema string
var vcsName string
var vcsTag string
var vcsType string
var vcsUri string
var vcsUuid string
var valueFiles []string

const (
	defaultConfigFilename = ".rearm"
	envPrefix             = ""
	configType            = "env"
)

type ErrorBody struct {
	Timestamp string
	Status    int
	Error     string
	Message   string
	Path      string
}

const RELEASE_GQL_DATA = `
	uuid
	createdType
	lastUpdatedBy
	createdDate
	version
	status
	org
	component
	branch
	parentReleases {
		release
		artifact
	}
	sourceCodeEntry
	artifacts
	notes
	endpoint
	commits
`

const FULL_RELEASE_GQL_DATA = RELEASE_GQL_DATA + `
	sourceCodeEntryDetails {
		uuid
		branch
		vcsUuid
		vcsBranch
		commit
		commits
		commitMessage
		vcsTag
		notes
		org
		dateActual
	}
	vcsRepository {
		uuid
		name
		org
		uri
		type
	}
	artifactDetails {
		uuid
		identifier
		org
		branch
		buildId
		buildUri
		cicdMeta
		digests
		isInternal
		artifactType {
			name
			aliases
		}
		notes
		tags {
            key
            value
        }
		dateFrom
		dateTo
		duration
		packageType
		version
		publisher
		group
		dependencies
	}
	componentDetails {
		uuid
		name
	}
`

const COMPONENT_GQL_DATA = `
	uuid
	name
	org
	type
	versionSchema
	vcsRepository
	featureBranchVersioning
	status
	apiKeyId
	apiKey
`

type TagRecord struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "rearm",
	Short: "ReARM CLI client",
	Long:  `CLI client for programmatic actions on Reliza's ReARM.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		initConfig(cmd)
	},
}

var printversionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print CLI version",
	Long:  `Prints current version of the ReARM CLI`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("ReARM CLI version: " + Version)
	},
}

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Persisits API Key Id and API Key Secret",
	Long:  "This CLI command takes API Key Id and API Key Secret and writes them to a configuration file in home directory",
	Run: func(cmd *cobra.Command, args []string) {

		home, err := homedir.Dir()

		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		configPath := filepath.Join(home, defaultConfigFilename+"."+configType)
		if _, err := os.Stat(configPath); err == nil {
			// config file already exists, it will be overwritten
		} else if os.IsNotExist(err) {
			//create new config file
			if _, err := os.Create(configPath); err != nil { // perm 0666
				fmt.Println(err)
				os.Exit(1)
			}
		}

		viper.Set("apikey", apiKey)
		viper.Set("apikeyid", apiKeyId)
		viper.Set("uri", rearmUri)

		if err := viper.WriteConfigAs(configPath); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	},
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
		if len(status) > 0 {
			body["status"] = strings.ToUpper(status)
		}
		if len(endpoint) > 0 {
			body["endpoint"] = endpoint
		}
		if len(component) > 0 {
			body["component"] = component
		}
		if len(dilId) > 0 {
			deliverables := make([]map[string]interface{}, len(dilId))
			softwareMetadatas := make([]map[string]interface{}, len(dilId))
			for i, aid := range dilId {
				deliverables[i] = map[string]interface{}{"displayIdentifier": aid}
				softwareMetadatas[i] = map[string]interface{}{}
			}

			// now do some length validations and add elements
			if len(dilBuildId) > 0 && len(dilBuildId) != len(dilId) {
				fmt.Println("number of --dilBuildId flags must be either zero or match number of --dilid flags")
				os.Exit(2)
			} else if len(dilBuildId) > 0 {
				for i, abid := range dilBuildId {
					softwareMetadatas[i]["buildId"] = abid
				}
			}

			if len(dilBuildUri) > 0 && len(dilBuildUri) != len(dilId) {
				fmt.Println("number of --dilbuildUri flags must be either zero or match number of --dilid flags")
				os.Exit(2)
			} else if len(dilBuildUri) > 0 {
				for i, aburi := range dilBuildUri {
					softwareMetadatas[i]["buildUri"] = aburi
				}
			}

			if len(dilCiMeta) > 0 && len(dilCiMeta) != len(dilId) {
				fmt.Println("number of --dilcimeta flags must be either zero or match number of --dilid flags")
				os.Exit(2)
			} else if len(dilCiMeta) > 0 {
				for i, acm := range dilCiMeta {
					softwareMetadatas[i]["cicdMeta"] = acm
				}
			}

			if len(dilDigests) > 0 && len(dilDigests) != len(dilId) {
				fmt.Println("number of --dildigests flags must be either zero or match number of --dilid flags")
				os.Exit(2)
			} else if len(dilDigests) > 0 {
				for i, ad := range dilDigests {
					adSpl := strings.Split(ad, ",")
					softwareMetadatas[i]["digests"] = adSpl
				}
			}

			if len(dateStart) > 0 && len(dateStart) != len(dilId) {
				fmt.Println("number of --datestart flags must be either zero or match number of --dilid flags")
				os.Exit(2)
			} else if len(dateStart) > 0 {
				for i, ds := range dateStart {
					softwareMetadatas[i]["dateFrom"] = ds
				}
			}

			if len(dateEnd) > 0 && len(dateEnd) != len(dilId) {
				fmt.Println("number of --dateEnd flags must be either zero or match number of --dilid flags")
				os.Exit(2)
			} else if len(dateEnd) > 0 {
				for i, de := range dateEnd {
					softwareMetadatas[i]["dateTo"] = de
				}
			}

			if len(dilPackage) > 0 && len(dilPackage) != len(dilId) {
				fmt.Println("number of --dilpackage flags must be either zero or match number of --dilid flags")
				os.Exit(2)
			} else if len(dilPackage) > 0 {
				for i, ap := range dilPackage {
					softwareMetadatas[i]["packageType"] = strings.ToUpper(ap)
				}
			}

			for i, _ := range dilId {
				deliverables[i]["softwareMetadata"] = softwareMetadatas[i]
			}

			if len(dilType) > 0 && len(dilType) != len(dilId) {
				fmt.Println("number of --diltype flags must be either zero or match number of --dilid flags")
				os.Exit(2)
			} else if len(dilType) > 0 {
				for i, at := range dilType {
					deliverables[i]["type"] = at
				}
			}

			if len(supportedOsArr) > 0 && len(supportedOsArr) != len(dilId) {
				fmt.Println("number of --osarr flags must be either zero or match number of --dilid flags")
				os.Exit(2)
			} else if len(supportedOsArr) > 0 {
				for i, ad := range supportedOsArr {
					adSpl := strings.Split(ad, ",")
					deliverables[i]["supportedOs"] = adSpl
				}
			}
			if len(supportedCpuArchArr) > 0 && len(supportedCpuArchArr) != len(dilId) {
				fmt.Println("number of --supportedcpuarcharr flags must be either zero or match number of --dilid flags")
				os.Exit(2)
			} else if len(supportedCpuArchArr) > 0 {
				for i, ad := range supportedCpuArchArr {
					adSpl := strings.Split(ad, ",")
					deliverables[i]["supportedCpuArchitectures"] = adSpl
				}
			}

			if len(dilVersion) > 0 && len(dilVersion) != len(dilId) {
				fmt.Println("number of --dilversion flags must be either zero or match number of --dilid flags")
				os.Exit(2)
			} else if len(dilVersion) > 0 {
				for i, av := range dilVersion {
					deliverables[i]["version"] = av
				}
			}

			if len(dilPublisher) > 0 && len(dilPublisher) != len(dilId) {
				fmt.Println("number of --dilpublisher flags must be either zero or match number of --dilid flags")
				os.Exit(2)
			} else if len(dilPublisher) > 0 {
				for i, ap := range dilPublisher {
					deliverables[i]["publisher"] = ap
				}
			}

			if len(dilGroup) > 0 && len(dilGroup) != len(dilId) {
				fmt.Println("number of --dilgroup flags must be either zero or match number of --dilid flags")
				os.Exit(2)
			} else if len(dilGroup) > 0 {
				for i, ag := range dilGroup {
					deliverables[i]["group"] = ag
				}
			}

			if len(tagsArr) > 0 && len(tagsArr) != len(dilId) {
				fmt.Println("number of --tagsarr flags must be either zero or match number of --dilid flags")
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
					deliverables[i]["tags"] = tags
				}
			}

			if len(identities) > 0 && len(identities) != len(dilId) {
				fmt.Println("number of --identities flags must be either zero or match number of --dilid flags")
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
					deliverables[i]["identities"] = bomIdentities
				}
			}
			if len(dilArtsJson) > 0 && len(dilArtsJson) != len(dilId) {
				fmt.Println("number of --dilartsjson flags must be either zero or match number of --dilid flags")
				os.Exit(2)
			} else if len(dilArtsJson) > 0 {
				for i, artifactsInputString := range dilArtsJson {
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
									filesCounter++
									currentIndex := strconv.Itoa(filesCounter)

									locationMap[currentIndex] = []string{"variables.releaseInputProg.deliverables." + strconv.Itoa(i) + ".artifacts." + strconv.Itoa(j) + ".file"}
									filesMap[currentIndex] = fileBytes
									artifactInput.File = nil

								}
								artifactInput.FilePath = ""
								artifactsObject[j] = artifactInput
							}
						}
						// TODO: replace file path with actual file
						deliverables[i]["artifacts"] = artifactsObject
					}
				}
			}
			body["deliverables"] = deliverables
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

											locationMap[currentIndex] = []string{"variables.releaseInputProg.commits." + strconv.Itoa(i) + ".artifacts." + strconv.Itoa(j) + ".file"}
											filesMap[currentIndex] = fileBytes
											artifactInput.File = nil
										}
										artifactInput.FilePath = ""
										artifactsObject[j] = artifactInput
									}
								}
								// TODO: replace file path with actual file
								commitMap["artifacts"] = artifactsObject
							}

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

		// 		fmt.Println(body)
		jsonBody, _ := json.Marshal(body)
		fmt.Println(string(jsonBody))
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
		c := client.R().SetDebug(true)
		for key, value := range filesMap {
			if bytesValue, ok := value.([]byte); ok {
				c.SetFileReader(key, key, bytes.NewReader(bytesValue))
			} else {
				// Handle error case: value is not []byte
				fmt.Printf("Warning: Value for key '%s' is not []byte\n", key)
			}
		}

		resp, err := c.SetHeader("Content-Type", "multipart/form-data").
			SetHeader("User-Agent", "Reliza Go Client").
			SetHeader("Accept-Encoding", "gzip, deflate").
			SetHeader("Apollo-Require-Preflight", "true").
			SetMultipartFormData(operations).
			SetMultipartFormData(fileMapFd).
			SetBasicAuth(apiKeyId, apiKey).
			Post(rearmUri + "/graphql")

		printResponse(err, resp)
		// req := graphql.NewRequest(`
		// 	mutation ($releaseInputProg: ReleaseInputProg!) {
		// 		addReleaseProgrammatic(release:$releaseInputProg) {` + RELEASE_GQL_DATA + `}
		// 	}`,
		// )
		// req.Var("releaseInputProg", body)
		// fmt.Println(sendRequest(req, "addReleaseProgrammatic"))
	},
}

var addDeliverableCmd = &cobra.Command{
	Use:   "addDeliverable",
	Short: "Add artifacts to a release",
	Long:  `This CLI command would connect to ReARM and add artifacts to a release using a valid API key.`,
	Run: func(cmd *cobra.Command, args []string) {
		if debug == "true" {
			fmt.Println("Using ReARM at", rearmUri)
		}

		body := map[string]interface{}{}
		if len(releaseId) > 0 {
			body["release"] = releaseId
		}
		if len(component) > 0 {
			body["component"] = component
		}
		if len(version) > 0 {
			body["version"] = version
		}

		if len(dilId) > 0 {
			// use artifacts, construct artifact array
			artifacts := make([]map[string]interface{}, len(dilId))
			for i, aid := range dilId {
				artifacts[i] = map[string]interface{}{"identifier": aid}
			}

			// now do some length validations and add elements
			if len(dilBuildId) > 0 && len(dilBuildId) != len(dilId) {
				fmt.Println("number of --dilbuildid flags must be either zero or match number of --dilid flags")
				os.Exit(2)
			} else if len(dilBuildId) > 0 {
				for i, abid := range dilBuildId {
					artifacts[i]["buildId"] = abid
				}
			}

			if len(dilBuildUri) > 0 && len(dilBuildUri) != len(dilId) {
				fmt.Println("number of --dilbuildUri flags must be either zero or match number of --dilid flags")
				os.Exit(2)
			} else if len(dilBuildUri) > 0 {
				for i, aburi := range dilBuildUri {
					artifacts[i]["buildUri"] = aburi
				}
			}

			if len(dilCiMeta) > 0 && len(dilCiMeta) != len(dilId) {
				fmt.Println("number of --dilcimeta flags must be either zero or match number of --dilid flags")
				os.Exit(2)
			} else if len(dilCiMeta) > 0 {
				for i, acm := range dilCiMeta {
					artifacts[i]["cicdMeta"] = acm
				}
			}

			if len(dilType) > 0 && len(dilType) != len(dilId) {
				fmt.Println("number of --diltype flags must be either zero or match number of --dilid flags")
				os.Exit(2)
			} else if len(dilType) > 0 {
				for i, at := range dilType {
					artifacts[i]["type"] = at
				}
			}

			if len(dilDigests) > 0 && len(dilDigests) != len(dilId) {
				fmt.Println("number of --dildigests flags must be either zero or match number of --dilid flags")
				os.Exit(2)
			} else if len(dilDigests) > 0 {
				for i, ad := range dilDigests {
					adSpl := strings.Split(ad, ",")
					artifacts[i]["digests"] = adSpl
				}
			}

			if len(dateStart) > 0 && len(dateStart) != len(dilId) {
				fmt.Println("number of --datestart flags must be either zero or match number of --dilid flags")
				os.Exit(2)
			} else if len(dateStart) > 0 {
				for i, ds := range dateStart {
					artifacts[i]["dateFrom"] = ds
				}
			}

			if len(dateEnd) > 0 && len(dateEnd) != len(dilId) {
				fmt.Println("number of --dateEnd flags must be either zero or match number of --dilid flags")
				os.Exit(2)
			} else if len(dateEnd) > 0 {
				for i, de := range dateEnd {
					artifacts[i]["dateTo"] = de
				}
			}

			if len(dilVersion) > 0 && len(dilVersion) != len(dilId) {
				fmt.Println("number of --dilversion flags must be either zero or match number of --dilid flags")
				os.Exit(2)
			} else if len(dilVersion) > 0 {
				for i, av := range dilVersion {
					artifacts[i]["version"] = av
				}
			}

			if len(dilPublisher) > 0 && len(dilPublisher) != len(dilId) {
				fmt.Println("number of --dilpublisher flags must be either zero or match number of --dilid flags")
				os.Exit(2)
			} else if len(dilPublisher) > 0 {
				for i, ap := range dilPublisher {
					artifacts[i]["publisher"] = ap
				}
			}

			if len(dilPackage) > 0 && len(dilPackage) != len(dilId) {
				fmt.Println("number of --dilpackage flags must be either zero or match number of --dilid flags")
				os.Exit(2)
			} else if len(dilPackage) > 0 {
				for i, ap := range dilPackage {
					artifacts[i]["packageType"] = strings.ToUpper(ap)
				}
			}

			if len(dilGroup) > 0 && len(dilGroup) != len(dilId) {
				fmt.Println("number of --dilgroup flags must be either zero or match number of --dilid flags")
				os.Exit(2)
			} else if len(dilGroup) > 0 {
				for i, ag := range dilGroup {
					artifacts[i]["group"] = ag
				}
			}

			// if len(tagKeyArr) > 0 && len(tagKeyArr) != len(dilId) {
			// 	fmt.Println("number of --tagkey flags must be either zero or match number of --dilid flags")
			// 	os.Exit(2)
			// } else if len(tagValArr) > 0 && len(tagValArr) != len(dilId) {
			// 	fmt.Println("number of --tagval flags must be either zero or match number of --dilid flags")
			// 	os.Exit(2)
			// } else if len(tagKeyArr) > 0 && len(tagValArr) < 1 {
			// 	fmt.Println("number of --tagval and --tagkey flags must be the same and must match number of --dilid flags")
			// 	os.Exit(2)
			// } else if len(tagKeyArr) > 0 {
			// 	for i, key := range tagKeyArr {
			// 		tagKeys := strings.Split(key, ",")
			// 		tagVals := strings.Split(tagValArr[i], ",")
			// 		if len(tagKeys) != len(tagVals) {
			// 			fmt.Println("number of keys and values per each --tagval and --tagkey flag must be the same")
			// 			os.Exit(2)
			// 		}

			// 		k := make([]TagRecord, 0)
			// 		for j := range tagKeys {
			// 			tr := TagRecord{
			// 				Key:   tagKeys[j],
			// 				Value: tagVals[j],
			// 			}
			// 			k = append(k, tr)
			// 		}
			// 		artifacts[i]["tags"] = k
			// 	}
			// }

			body["artifacts"] = artifacts
		}

		req := graphql.NewRequest(`
			mutation ($AddDeliverableInput: AddDeliverableInput) {
				addDeliverable(release: $AddDeliverableInput) {` + RELEASE_GQL_DATA + `}
			}
		`)
		req.Var("AddDeliverableInput", body)
		fmt.Println(sendRequest(req, "addDeliverable"))
	},
}

var downloadableArtifactCmd = &cobra.Command{
	Use:   "addDownloadableArtifact",
	Short: "Add a downloadable artifact to a release using valid API key",
	Long:  `This CLI command would connect to ReARM add downloadable artifact to a release.`,
	Run: func(cmd *cobra.Command, args []string) {
		if debug == "true" {
			fmt.Println("Using ReARM at", rearmUri)
		}

		body := map[string]string{}
		if len(releaseId) > 0 {
			body["uuid"] = releaseId
		}
		if len(releaseVersion) > 0 {
			body["version"] = releaseVersion
		}
		if len(component) > 0 {
			body["component"] = component
		}
		// if len(artifactType) > 0 {
		// 	body["artifactType"] = artifactType
		// }
		client := resty.New()
		session, _ := getSession()
		if session != nil {
			client.SetHeader("X-CSRF-Token", session.Token)
			client.SetHeader("Cookie", "JSESSIONID="+session.JSessionId)
		}
		resp, err := client.R().
			SetHeader("Content-Type", "application/json").
			SetHeader("User-Agent", "Reliza Go Client").
			SetHeader("Accept-Encoding", "gzip, deflate").
			SetHeader("Accept-Encoding", "gzip, deflate").
			SetFile("file", filePath).
			SetFormData(body).
			SetBasicAuth(apiKeyId, apiKey).
			Post(rearmUri + "/api/programmatic/v1/artifact/upload")

		printResponse(err, resp)

	},
}

var createComponentCmd = &cobra.Command{
	Use:   "createcomponent",
	Short: "Create new component",
	Long:  `This CLI command would connect to ReARM which would create a new component `,
	Run: func(cmd *cobra.Command, args []string) {
		if debug == "true" {
			fmt.Println("Using ReARM at", rearmUri)
		}

		body := map[string]interface{}{"name": componentName}
		if len(componentType) > 0 {
			body["type"] = strings.ToUpper(componentType)
			if strings.ToUpper(componentType) == "BUNDLE" {
				body["type"] = "BUNDLE"
			}
		}
		if len(defaultBranch) > 0 {
			body["defaultBranch"] = strings.ToUpper(defaultBranch)
		}
		if len(versionSchema) > 0 {
			body["versionSchema"] = versionSchema
		}
		if len(featureBranchVersioning) > 0 {
			body["featureBranchVersioning"] = featureBranchVersioning
		}
		if len(vcsUuid) > 0 {
			body["vcsRepositoryUuid"] = vcsUuid
		}

		if len(vcsUri) > 0 {
			vcsRepository := map[string]string{"uri": vcsUri}
			if len(vcsName) > 0 {
				vcsRepository["name"] = vcsName
			}
			if len(vcsType) > 0 {
				vcsRepository["type"] = vcsType
			}
			body["vcsRepository"] = vcsRepository
		}

		body["includeApi"] = includeApi

		req := graphql.NewRequest(`
			mutation ($CreateComponentInput: CreateComponentInput!) {
				createComponentProgrammatic(component:$CreateComponentInput) {` + COMPONENT_GQL_DATA + `}
			}
		`)
		req.Var("CreateComponentInput", body)
		fmt.Println(sendRequest(req, "createComponentProgrammatic"))
	},
}

var getVersionCmd = &cobra.Command{
	Use:   "getversion",
	Short: "Get next version for branch for a particular component",
	Long: `This CLI command would connect to ReARM which would generate next Atomic version for particular component.
			Component would be identified by the API key that is used`,
	Run: func(cmd *cobra.Command, args []string) {
		if debug == "true" {
			fmt.Println("Using ReARM at", rearmUri)
		}

		body := map[string]interface{}{"branch": branch}
		if len(component) > 0 {
			body["component"] = component
		}
		if len(modifier) > 0 {
			body["modifier"] = modifier
		}
		if len(metadata) > 0 {
			body["metadata"] = metadata
		}
		if len(action) > 0 {
			body["action"] = action
		}

		if len(versionSchema) > 0 {
			body["versionSchema"] = versionSchema
		}

		if commit != "" || commitMessage != "" {
			commitMap := map[string]string{"uri": vcsUri, "type": vcsType, "commit": commit, "commitMessage": commitMessage}
			if vcsTag != "" {
				commitMap["vcsTag"] = vcsTag
			}
			if dateActual != "" {
				commitMap["dateActual"] = dateActual
			}
			body["sourceCodeEntry"] = commitMap
		}
		if manual {
			body["status"] = "draft"
		}

		if len(commits) > 0 {
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
						var commitMap map[string]string
						if len(commitParts) > 3 {
							commitMap = map[string]string{"commit": commitParts[0], "dateActual": commitParts[1], "commitMessage": commitParts[2], "commitAuthor": commitParts[3], "commitEmail": commitParts[4]}
						} else {
							commitMap = map[string]string{"commit": commitParts[0], "dateActual": commitParts[1], "commitMessage": commitParts[2]}
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

		body["onlyVersion"] = onlyVersion

		req := graphql.NewRequest(`
			mutation ($GetNewVersionInput: GetNewVersionInput!) {
				getNewVersionProgrammatic(newVersionInput:$GetNewVersionInput) {
					version
					dockerTagSafeVersion
				}
			}
		`)
		req.Var("GetNewVersionInput", body)
		fmt.Println(sendRequest(req, "getNewVersionProgrammatic"))
	},
}

var checkReleaseByHashCmd = &cobra.Command{
	Use:   "checkhash",
	Short: "Checks whether artifact with this hash is present for particular component",
	Long: `This CLI command would connect to ReARM which would check if the artifact was already submitted as a part of some
			existing release of the current component.
			Component would be identified by the API key that is used`,
	Run: func(cmd *cobra.Command, args []string) {
		if debug == "true" {
			fmt.Println("Using ReARM at", rearmUri)
		}

		req := graphql.NewRequest(`
			query ($hash: String!, $componentId: ID) {
				getReleaseByHashProgrammatic(hash: $hash, componentId: $componentId) {` + RELEASE_GQL_DATA + `}
			}
		`)
		req.Var("hash", hash)
		if len(component) > 0 {
			req.Var("componentId", component)
		}
		resp := sendRequest(req, "getReleaseByHashProgrammatic")
		if resp == "null" {
			resp = "{}"
		}
		fmt.Println(resp)
	},
}

var getLatestReleaseCmd = &cobra.Command{
	Use:   "getlatestrelease",
	Short: "Obtains latest release for Component or Bundle",
	Long: `This CLI command would connect to ReARM and would obtain latest release for specified Component and Branch
			or specified Bundle and Feature Set.`,
	Run: func(cmd *cobra.Command, args []string) {
		getLatestReleaseFunc(debug, rearmUri, component, bundle, branch, tagKey, tagVal, apiKeyId, apiKey, status)
	},
}

var prDataCmd = &cobra.Command{
	Use:   "prdata",
	Short: "Sends pull request data to ReARM",
	Long:  `This CLI command would stream pull request data from ci to ReARM`,
	Run: func(cmd *cobra.Command, args []string) {
		if debug == "true" {
			fmt.Println("Using ReARM at", rearmUri)
		}

		body := map[string]interface{}{"branch": branch}

		if len(state) > 0 {
			body["state"] = state
		}
		if len(component) > 0 {
			body["component"] = component
		}

		if len(targetBranch) > 0 {
			body["targetBranch"] = targetBranch
		}
		if len(endpoint) > 0 {
			body["endpoint"] = endpoint
		}
		if len(title) > 0 {
			body["title"] = title
		}
		if len(createdDate) > 0 {
			body["createdDate"] = createdDate
		}
		if len(closedDate) > 0 {
			body["closedDate"] = closedDate
		}
		if len(mergedDate) > 0 {
			body["mergedDate"] = mergedDate
		}
		if len(number) > 0 {
			body["number"] = number
		}
		if commit != "" {
			commitMap := map[string]string{"uri": vcsUri, "type": vcsType, "commit": commit, "commitMessage": commitMessage}
			if vcsTag != "" {
				commitMap["vcsTag"] = vcsTag
			}
			if dateActual != "" {
				commitMap["dateActual"] = dateActual
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
						var commitMap map[string]string
						if len(commitParts) > 3 {
							commitMap = map[string]string{"commit": commitParts[0], "dateActual": commitParts[1], "commitMessage": commitParts[2], "commitAuthor": commitParts[3], "commitEmail": commitParts[4]}
						} else {
							commitMap = map[string]string{"commit": commitParts[0], "dateActual": commitParts[1], "commitMessage": commitParts[2]}
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

		if debug == "true" {
			fmt.Println(body)
		}
		req := graphql.NewRequest(`
			mutation ($PullRequestInput: PullRequestInput) {
				setPRData(pullRequest:$PullRequestInput)
			}
		`)
		req.Var("PullRequestInput", body)
		fmt.Println(sendRequest(req, "prdata"))
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {

	// Here you will define your flags and configuration settings.
	// Cobra supports persistent flags, which, if defined here,
	// will be global for your application.

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.rearm.yaml)")
	rootCmd.PersistentFlags().StringVarP(&rearmUri, "uri", "u", "https://app.relizahub.com", "FQDN of ReARM server")
	rootCmd.PersistentFlags().StringVarP(&apiKey, "apikey", "k", "", "API Key Secret")
	rootCmd.PersistentFlags().StringVarP(&apiKeyId, "apikeyid", "i", "", "API Key ID")
	rootCmd.PersistentFlags().StringVarP(&debug, "debug", "d", "false", "If set to true, print debug details")

	// flags for addrelease command
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
	addreleaseCmd.PersistentFlags().StringArrayVar(&dilId, "dilid", []string{}, "Deliverable ID (multiple allowed)")
	addreleaseCmd.PersistentFlags().StringArrayVar(&dilBuildId, "dilbuildid", []string{}, "Deliverable Build ID (multiple allowed)")
	addreleaseCmd.PersistentFlags().StringArrayVar(&dilBuildUri, "dilbuilduri", []string{}, "Deliverable Build URI (multiple allowed)")
	addreleaseCmd.PersistentFlags().StringArrayVar(&dilCiMeta, "dilcimeta", []string{}, "Deliverable CI Meta (multiple allowed)")
	addreleaseCmd.PersistentFlags().StringArrayVar(&dilType, "diltype", []string{}, "Deliverable Type (multiple allowed)")
	addreleaseCmd.PersistentFlags().StringArrayVar(&dilDigests, "dildigests", []string{}, "Deliverable Digests (multiple allowed, separate several digests for one Deliverable with commas)")
	addreleaseCmd.PersistentFlags().StringArrayVar(&tagsArr, "tagsarr", []string{}, "Deliverable Tag Key-Value Pairs (multiple allowed, separate several tag key-value pairs for one Deliverable with commas, and seprate key-value in a pair with colon)")
	addreleaseCmd.PersistentFlags().StringArrayVar(&identities, "identities", []string{}, "Deliverable Identity IdenityType-Idenity Pairs (multiple allowed, separate several IdenityType-Idenity pairs for one Deliverable with commas, and seprate IdenityType-Idenity in a pair with colon)")
	addreleaseCmd.PersistentFlags().StringArrayVar(&dateStart, "datestart", []string{}, "Deliverable Build Start date and time (optional, multiple allowed)")
	addreleaseCmd.PersistentFlags().StringArrayVar(&dateEnd, "dateend", []string{}, "Deliverable Build End date and time (optional, multiple allowed)")
	addreleaseCmd.PersistentFlags().StringArrayVar(&dilVersion, "dilversion", []string{}, "Deliverable version, if different from release (multiple allowed)")
	addreleaseCmd.PersistentFlags().StringArrayVar(&dilPackage, "dilpackage", []string{}, "Deliverable package type (multiple allowed)")
	// addreleaseCmd.PersistentFlags().StringArrayVar(&dilName, "dilname", []string{}, "Deliverable name (multiple allowed)")
	addreleaseCmd.PersistentFlags().StringArrayVar(&dilPublisher, "dilpublisher", []string{}, "Deliverable publisher (multiple allowed)")
	addreleaseCmd.PersistentFlags().StringArrayVar(&dilGroup, "dilgroup", []string{}, "Deliverable group (multiple allowed)")
	addreleaseCmd.PersistentFlags().StringArrayVar(&dilBomFilePaths, "dilboms", []string{}, "Deliverable Sbom file paths (multiple allowed)")
	addreleaseCmd.PersistentFlags().StringArrayVar(&supportedOsArr, "osarr", []string{}, "Deliverable supported OS array (multiple allowed, use comma seprated values for each deliverable)")
	addreleaseCmd.PersistentFlags().StringArrayVar(&supportedCpuArchArr, "cpuarr", []string{}, "Deliverable supported CPU array (multiple allowed, use comma seprated values for each deliverable)")
	addreleaseCmd.PersistentFlags().StringArrayVar(&dilArtsJson, "dilartsjson", []string{}, "Deliverable Artifacts json array (multiple allowed, use a json array for each deliverable)")
	addreleaseCmd.PersistentFlags().StringVar(&releaseArts, "releasearts", "", "Release Artifacts json array")
	addreleaseCmd.PersistentFlags().StringVar(&sceArts, "scearts", "", "Source Code Entry Artifacts json array")
	addreleaseCmd.PersistentFlags().StringVar(&status, "status", "", "Status of release - set to 'rejected' for failed releases, otherwise 'completed' is used (optional).")

	addDeliverableCmd.PersistentFlags().StringVar(&releaseId, "releaseid", "", "UUID of release to add artifact to (either releaseid or component, branch, and version must be set)")
	addDeliverableCmd.PersistentFlags().StringVar(&component, "component", "", "Component UUID for this release if org-wide key is used")
	addDeliverableCmd.PersistentFlags().StringVar(&version, "version", "", "Release version")
	addDeliverableCmd.PersistentFlags().StringArrayVar(&dilId, "dilid", []string{}, "Artifact ID (multiple allowed)")
	addDeliverableCmd.PersistentFlags().StringArrayVar(&dilBuildId, "dilbuildid", []string{}, "Artifact Build ID (multiple allowed)")
	addDeliverableCmd.PersistentFlags().StringArrayVar(&dilBuildId, "dilBuildId", []string{}, "Artifact Build URI (multiple allowed)")
	addDeliverableCmd.PersistentFlags().StringArrayVar(&dilCiMeta, "dilCiMeta", []string{}, "Artifact CI Meta (multiple allowed)")
	addDeliverableCmd.PersistentFlags().StringArrayVar(&dilType, "dilType", []string{}, "Artifact Type (multiple allowed)")
	addDeliverableCmd.PersistentFlags().StringArrayVar(&dilDigests, "dilDigests", []string{}, "Artifact Digests (multiple allowed, separate several digests for one artifact with commas)")
	// addDeliverableCmd.PersistentFlags().StringArrayVar(&tagKeyArr, "tagkey", []string{}, "Artifact Tag Keys (multiple allowed, separate several tag keys for one artifact with commas)")
	// addDeliverableCmd.PersistentFlags().StringArrayVar(&tagValArr, "tagval", []string{}, "Artifact Tag Values (multiple allowed, separate several tag values for one artifact with commas)")
	addDeliverableCmd.PersistentFlags().StringArrayVar(&dateStart, "datestart", []string{}, "Artifact Build Start date and time (optional, multiple allowed)")
	addDeliverableCmd.PersistentFlags().StringArrayVar(&dateEnd, "dateend", []string{}, "Artifact Build End date and time (optional, multiple allowed)")
	addDeliverableCmd.PersistentFlags().StringArrayVar(&dilVersion, "dilVersion", []string{}, "Artifact version, if different from release (multiple allowed)")
	addDeliverableCmd.PersistentFlags().StringArrayVar(&dilPackage, "dilPackage", []string{}, "Artifact package type (multiple allowed)")
	addDeliverableCmd.PersistentFlags().StringArrayVar(&dilPublisher, "dilPublisher", []string{}, "Artifact publisher (multiple allowed)")
	addDeliverableCmd.PersistentFlags().StringArrayVar(&dilGroup, "dilGroup", []string{}, "Artifact group (multiple allowed)")

	// flags for is approval needed check command
	downloadableArtifactCmd.PersistentFlags().StringVar(&releaseId, "releaseid", "", "UUID of release (either releaseid or releaseversion and component must be set)")
	downloadableArtifactCmd.PersistentFlags().StringVar(&releaseVersion, "releaseversion", "", "Version of release (either releaseid or releaseversion and component must be set)")
	downloadableArtifactCmd.PersistentFlags().StringVar(&component, "component", "", "UUID of component or bundle for release (either instance and component or releaseid or releaseversion and component must be set)")
	downloadableArtifactCmd.PersistentFlags().StringVarP(&filePath, "file", "f", "", "Path to the artifact")
	// downloadableArtifactCmd.PersistentFlags().StringVar(&dilType, "dilType", "GENERIC", "Type of artifact - can be (TEST_REPORT, SECURITY_SCAN, DOCUMENTATION, GENERIC) or some user defined value")

	// flags for createcomponent command
	createComponentCmd.PersistentFlags().StringVar(&componentName, "name", "", "Name of component to create")
	createComponentCmd.MarkPersistentFlagRequired("name")
	createComponentCmd.PersistentFlags().StringVar(&componentType, "type", "", "Specify to create either a component or bundle")
	createComponentCmd.MarkPersistentFlagRequired("type")
	createComponentCmd.PersistentFlags().StringVar(&defaultBranch, "defaultbranch", "main", "Default branch name of component, default set to main. Available names are either main or master.")
	createComponentCmd.PersistentFlags().StringVar(&versionSchema, "versionschema", "semver", "Version schema of component, default set to semver. Available version schemas: https://github.com/relizaio/versioning")
	createComponentCmd.PersistentFlags().StringVar(&featureBranchVersioning, "featurebranchversioning", "Branch.Micro", "Feature branch version schema of component (Optional, default set to Branch.Micro")
	createComponentCmd.PersistentFlags().StringVar(&vcsUuid, "vcsuuid", "", "Vcs repository UUID (if retreiving existing vcs repository, either vcsuuid or vcsuri must be set)")
	createComponentCmd.PersistentFlags().StringVar(&vcsUri, "vcsuri", "", "Vcs repository URI, if existing repository with uri does not exist and vcsname and vcstype are not set, will attempt to autoparse github, gitlab, and bitbucket uri's")
	createComponentCmd.PersistentFlags().StringVar(&vcsName, "vcsname", "", "Name of vcs repository (Optional - required if creating new vcs repository and uri cannot be parsed)")
	createComponentCmd.PersistentFlags().StringVar(&vcsType, "vcstype", "", "Type of vcs type (Optional - required if creating new vcs repository and uri cannot be parsed)")
	createComponentCmd.PersistentFlags().BoolVar(&includeApi, "includeapi", false, "(Optional) Set --includeapi flag to create and return api key and id for created component during command")

	// flags for get version command
	getVersionCmd.PersistentFlags().StringVarP(&branch, "branch", "b", "", "Name of VCS Branch used")
	getVersionCmd.MarkPersistentFlagRequired("branch")
	getVersionCmd.PersistentFlags().StringVar(&component, "component", "", "Component UUID for this release if org-wide key is used")
	getVersionCmd.PersistentFlags().StringVar(&action, "action", "", "Bump action name: bump | bumppatch | bumpminor | bumpmajor | bumpdate")
	getVersionCmd.PersistentFlags().StringVar(&metadata, "metadata", "", "Version metadata")
	getVersionCmd.PersistentFlags().StringVar(&modifier, "modifier", "", "Version modifier")
	getVersionCmd.PersistentFlags().StringVar(&versionSchema, "pin", "", "Version pin if creating new branch")
	getVersionCmd.PersistentFlags().StringVar(&vcsUri, "vcsuri", "", "URI of VCS repository")
	getVersionCmd.PersistentFlags().StringVar(&vcsType, "vcstype", "", "Type of VCS repository: git, svn, mercurial")
	getVersionCmd.PersistentFlags().StringVar(&commit, "commit", "", "Commit id (required to create Source Code Entry for new release)")
	getVersionCmd.PersistentFlags().StringVar(&commitMessage, "commitmessage", "", "Commit message or subject (optional)")
	getVersionCmd.PersistentFlags().StringVar(&commits, "commits", "", "Base64-encoded list of commits associated with this release, can be obtained with 'git log --date=iso-strict --pretty='%H|||%ad|||%s' | base64 -w 0' command (optional)")
	getVersionCmd.PersistentFlags().StringVar(&vcsTag, "vcstag", "", "VCS Tag")
	getVersionCmd.PersistentFlags().StringVar(&dateActual, "date", "", "Commit date and time in iso strict format, use git log --date=iso-strict (optional).")
	getVersionCmd.PersistentFlags().BoolVar(&manual, "manual", false, "(Optional) Set --manual flag to indicate a manual release.")
	getVersionCmd.PersistentFlags().BoolVar(&onlyVersion, "onlyversion", false, "(Optional) Set --onlyVersion flag to retrieve next version only and not create a release.")

	// flags for check release by hash command
	checkReleaseByHashCmd.PersistentFlags().StringVar(&hash, "hash", "", "Hash of artifact to check")
	checkReleaseByHashCmd.PersistentFlags().StringVar(&component, "component", "", "Component UUID from ReARM for which to check artifact hash (optional, required for org-wide keys)")

	// flags for latest component or bundle release
	getLatestReleaseCmd.PersistentFlags().StringVar(&component, "component", "", "Component or Bundle UUID from ReARM of component or bundle from which to obtain latest release")
	getLatestReleaseCmd.PersistentFlags().StringVar(&bundle, "bundle", "", "Bundle UUID from ReARM to condition component release to this bundle bundle (optional)")
	getLatestReleaseCmd.PersistentFlags().StringVarP(&branch, "branch", "b", "", "Name of branch or Feature Set from ReARM for which latest release is requested, if not supplied UI mapping is used (optional)")
	getLatestReleaseCmd.PersistentFlags().StringVar(&environment, "env", "", "Environment to obtain approvals details from (optional)")
	getLatestReleaseCmd.PersistentFlags().StringVar(&instance, "instance", "", "Instance ID for which to check release (optional)")
	getLatestReleaseCmd.PersistentFlags().StringVar(&namespace, "namespace", "", "Namespace within instance for which to check release, only matters if instance is supplied (optional)")
	getLatestReleaseCmd.PersistentFlags().StringVar(&tagKey, "tagkey", "", "Tag key to use for picking artifact (optional)")
	getLatestReleaseCmd.PersistentFlags().StringVar(&tagVal, "tagval", "", "Tag value to use for picking artifact (optional)")
	getLatestReleaseCmd.PersistentFlags().StringVar(&status, "status", "", "Status of the release, default is completed (optional)")

	prDataCmd.PersistentFlags().StringVarP(&branch, "branch", "b", "", "Name of VCS Branch used")
	prDataCmd.PersistentFlags().StringVarP(&state, "state", "s", "", "State of the Pull Request")
	prDataCmd.PersistentFlags().StringVarP(&targetBranch, "targetBranch", "t", "", "Name of target branch")
	prDataCmd.PersistentFlags().StringVar(&component, "component", "", "Component UUID if org-wide key is used")
	prDataCmd.PersistentFlags().StringVar(&endpoint, "endpoint", "", "HTML endpoint of the Pull Request")
	prDataCmd.PersistentFlags().StringVar(&title, "title", "", "Title of the Pull Request")
	prDataCmd.PersistentFlags().StringVar(&createdDate, "createdDate", "", "Datetime when the Pull Request was created")
	prDataCmd.PersistentFlags().StringVar(&closedDate, "closedDate", "", "Datetime when the Pull Request was closed")
	prDataCmd.PersistentFlags().StringVar(&mergedDate, "mergedDate", "", "Datetime when the Pull Request was merged")
	prDataCmd.PersistentFlags().StringVar(&number, "number", "", "Number of the Pull Request")
	prDataCmd.PersistentFlags().StringVar(&commit, "commit", "", "SHA of current commit on the Pull Request (will be merged with existing list)")
	prDataCmd.PersistentFlags().StringVar(&commitMessage, "commitmessage", "", "Commit message or subject (optional)")
	prDataCmd.PersistentFlags().StringVar(&vcsUri, "vcsuri", "", "URI of VCS repository")
	prDataCmd.PersistentFlags().StringVar(&vcsType, "vcstype", "", "Type of VCS repository: git, svn, mercurial")
	prDataCmd.PersistentFlags().StringVar(&commits, "commits", "", "Base64-encoded list of commits associated with this pull request event, can be obtained with 'git log --date=iso-strict --pretty='%H|||%ad|||%s' | base64 -w 0' command (optional)")
	prDataCmd.PersistentFlags().StringVar(&vcsTag, "vcstag", "", "VCS Tag")
	prDataCmd.PersistentFlags().StringVar(&dateActual, "commitdate", "", "Commit date and time in iso strict format, use git log --date=iso-strict (optional).")

	rootCmd.AddCommand(loginCmd)
	rootCmd.AddCommand(printversionCmd)
	rootCmd.AddCommand(addreleaseCmd)
	rootCmd.AddCommand(addDeliverableCmd)
	rootCmd.AddCommand(checkReleaseByHashCmd)
	rootCmd.AddCommand(getLatestReleaseCmd)
	rootCmd.AddCommand(createComponentCmd)
	rootCmd.AddCommand(getVersionCmd)
	rootCmd.AddCommand(downloadableArtifactCmd)
	rootCmd.AddCommand(prDataCmd)
}

func sendRequest(req *graphql.Request, endpoint string) string {
	return sendRequestWithUri(req, endpoint, rearmUri+"/graphql")
}

func sendRequestWithUri(req *graphql.Request, endpoint string, uri string) string {
	session, _ := getSession()
	// if err != nil {
	// 	fmt.Printf("Error making API request: %s\n", err)
	// 	os.Exit(1)
	// }

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Reliza Go Client")
	req.Header.Set("Accept-Encoding", "gzip, deflate")
	if session != nil {
		req.Header.Set("X-CSRF-Token", session.Token)
		req.Header.Set("Cookie", "JSESSIONID="+session.JSessionId)
	}
	if len(apiKeyId) > 0 && len(apiKey) > 0 {
		auth := base64.StdEncoding.EncodeToString([]byte(apiKeyId + ":" + apiKey))
		req.Header.Add("Authorization", "Basic "+auth)
	}

	var respData map[string]interface{}
	client := graphql.NewClient(uri)
	if err := client.Run(context.Background(), req, &respData); err != nil {
		printGqlError(err)
		os.Exit(1)
	}

	jsonResponse, _ := json.Marshal(respData[endpoint])
	return string(jsonResponse)
}

func printResponse(err error, resp *resty.Response) {
	if debug == "true" {
		// Explore response object
		fmt.Println("Response Info:")
		fmt.Println("Error      :", err)
		fmt.Println("Status Code:", resp.StatusCode())
		fmt.Println("Status     :", resp.Status())
		fmt.Println("Time       :", resp.Time())
		fmt.Println("Received At:", resp.ReceivedAt())
		fmt.Println("Body       :\n", resp)
		fmt.Println()
	} else {
		fmt.Println(resp)
	}

	if resp.StatusCode() != 200 {
		fmt.Println("Error Response Info:")
		fmt.Println("Error      :", err)
		var jsonError ErrorBody
		errJson := json.Unmarshal(resp.Body(), &jsonError)
		if errJson != nil {
			fmt.Println("Error when decoding error json data: ", errJson)
		}
		fmt.Println("Error Message:", jsonError.Message)
		fmt.Println("Status Code:", resp.StatusCode())
		fmt.Println("Status     :", resp.Status())
		fmt.Println("Time       :", resp.Time())
		fmt.Println("Received At:", resp.ReceivedAt())
		os.Exit(1)
	}
}

// initConfig reads in config file and ENV variables if set.
func initConfig(cmd *cobra.Command) {
	v := viper.New()

	if cfgFile != "" {
		// Use config file from the flag.
		v.SetConfigFile(cfgFile)
	} else {
		// Find home directory.
		home, err := homedir.Dir()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		// Search config in home directory with name ".rearm" (without extension).
		v.AddConfigPath(home)
		v.SetConfigName(defaultConfigFilename)
	}
	v.SetEnvPrefix(envPrefix)

	// Attempt to read the config file.
	if err := v.ReadInConfig(); err != nil {
		if debug == "true" {
			fmt.Println(err)
		}
	} else {
		if debug == "true" {
			fmt.Println("Using config file:", v.ConfigFileUsed())
		}
	}

	v.AutomaticEnv() // read in environment variables that match
	bindFlags(cmd, v)

}

// Bind each cobra flag to its associated viper configuration (config file and environment variable)
func bindFlags(cmd *cobra.Command, v *viper.Viper) {
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		// Environment variables can't have dashes in them, so bind them to their equivalent
		// keys with underscores, e.g. --favorite-color to STING_FAVORITE_COLOR
		if strings.Contains(f.Name, "-") {
			envVarSuffix := strings.ToUpper(strings.ReplaceAll(f.Name, "-", "_"))
			v.BindEnv(f.Name, fmt.Sprintf("%s_%s", envPrefix, envVarSuffix))
		}

		// Apply the viper config value to the flag when the flag is not set and viper has a value
		if !f.Changed && v.IsSet(f.Name) {
			val := v.Get(f.Name)
			cmd.Flags().Set(f.Name, fmt.Sprintf("%v", val))
		}
	})
}

func printGqlError(err error) {
	splitError := strings.Split(err.Error(), ":")
	fmt.Println("Error: ", splitError[len(splitError)-1])
}

func getSession() (*RequestSession, error) {
	client := resty.New()
	var result map[string]string
	resp, err := client.R().
		SetHeader("Content-Type", "application/json").
		SetHeader("User-Agent", "Reliza Go Client").
		SetHeader("Accept-Encoding", "gzip, deflate").
		SetResult(&result).
		Get(rearmUri + "/api/manual/v1/fetchCsrf")

	if err != nil {
		return nil, err
	}
	// Extract cookies
	session, err := getJSessionIDCookieAndToken(resp)

	if err != nil {
		return nil, err
	}

	return session, err
}

func getJSessionIDCookieAndToken(resp *resty.Response) (*RequestSession, error) {
	// Extract cookies
	cookies := resp.Cookies()
	var jsessionid string
	for _, cookie := range cookies {
		if cookie.Name == "JSESSIONID" {
			jsessionid = cookie.Value
			break
		}
	}

	if jsessionid == "" {
		return nil, fmt.Errorf("JSESSIONID cookie not found")
	}

	// Assume the token is returned in the response body as a JSON object
	var result map[string]interface{}
	err := json.Unmarshal(resp.Body(), &result)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling JSON: %s", err)
	}

	token, ok := result["token"].(string)
	if !ok {
		return nil, fmt.Errorf("token not found in the response body")
	}

	return &RequestSession{JSessionId: jsessionid, Token: token}, nil
}

type RequestSession struct {
	JSessionId string
	Token      string
}
