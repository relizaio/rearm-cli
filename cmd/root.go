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
var apiKeyId string
var apiKey string

var odelArtsJson []string
var odelBuildId []string
var odelBuildUri []string
var odelCiMeta []string
var odelDigests []string
var odelId []string
var odelType []string
var releaseArts string
var sceArts string

var odelVersion []string
var odelPublisher []string
var odelGroup []string
var odelPackage []string

var branch string
var product string
var cfgFile string
var commit string
var commitMessage string
var commits string // base64-encoded list of commits obtained with: git log $LATEST_COMMIT..$CURRENT_COMMIT --date=iso-strict --pretty='%H|||%ad|||%s' | base64 -w 0
var dateActual string
var dateStart []string
var dateEnd []string
var debug string
var defaultBranch string
var endpoint string
var environment string
var featureBranchVersioning string
var fsBomPath string
var hash string
var identities []string
var includeApi bool
var instance string
var manual bool
var metadata string
var modifier string
var namespace string
var onlyVersion bool
var infile string
var releaseId string
var releaseVersion string
var rearmUri string
var component string
var componentName string
var componentType string
var lifecycle string
var supportedOsArr []string
var supportedCpuArchArr []string
var tagKey string

var tagVal string

var tagsArr []string
var version string
var versionSchema string
var vcsName string
var vcsTag string
var vcsType string
var vcsUri string
var vcsUuid string

const (
	defaultConfigFilename = ".rearm"
	envPrefix             = "rearm"
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
	lifecycle
	org
	component
	branch
	parentReleases {
		release
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
		displayIdentifier
		org
		branch
		buildId
		buildUri
		cicdMeta
		digests
		isInternal
		type
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
	vcsRepositoryDetails {
		uri
		type
	}
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
									filesCounter++
									currentIndex := strconv.Itoa(filesCounter)

									locationMap[currentIndex] = []string{"variables.releaseInputProg.outboundDeliverables." + strconv.Itoa(i) + ".artifacts." + strconv.Itoa(j) + ".file"}
									filesMap[currentIndex] = fileBytes
									artifactInput.File = nil

								}
								artifactInput.FilePath = ""
								artifactsObject[j] = artifactInput
							}
						}
						// TODO: replace file path with actual file
						outboundDeliverables[i]["artifacts"] = artifactsObject
					}
				}
			}
			body["outboundDeliverables"] = outboundDeliverables
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

var addODeliverableCmd = &cobra.Command{
	Use:   "addodeliverable",
	Short: "Add outbound deliverables to a release",
	Long:  `This CLI command would connect to ReARM and add outbound deliverables to a release using a valid API key.`,
	Run: func(cmd *cobra.Command, args []string) {
		if debug == "true" {
			fmt.Println("Using ReARM at", rearmUri)
		}

		locationMap := make(map[string][]string)
		filesMap := make(map[string]interface{})
		filesCounter := 0

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

		if len(odelId) > 0 {
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
									filesCounter++
									currentIndex := strconv.Itoa(filesCounter)

									locationMap[currentIndex] = []string{"variables.addODeliverableInput.deliverables." + strconv.Itoa(i) + ".artifacts." + strconv.Itoa(j) + ".file"}
									filesMap[currentIndex] = fileBytes
									artifactInput.File = nil

								}
								artifactInput.FilePath = ""
								artifactsObject[j] = artifactInput
							}
						}
						// TODO: replace file path with actual file
						outboundDeliverables[i]["artifacts"] = artifactsObject
					}
				}
			}
			body["deliverables"] = outboundDeliverables
		}

		jsonBody, _ := json.Marshal(body)
		if debug == "true" {
			fmt.Println(string(jsonBody))
		}

		od := make(map[string]interface{})
		od["operationName"] = "addOutboundDeliverablesProgrammatic"
		od["variables"] = map[string]interface{}{"addODeliverableInput": body}
		od["query"] = `mutation addOutboundDeliverablesProgrammatic($addODeliverableInput: AddODeliverableInput!) {addOutboundDeliverablesProgrammatic(deliverables:$addODeliverableInput) {` + RELEASE_GQL_DATA + `}}`

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
			SetHeader("User-Agent", "Reliza Go Client").
			SetHeader("Accept-Encoding", "gzip, deflate").
			SetHeader("Apollo-Require-Preflight", "true").
			SetMultipartFormData(operations).
			SetMultipartFormData(fileMapFd).
			SetBasicAuth(apiKeyId, apiKey).
			Post(rearmUri + "/graphql")

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
			if strings.ToUpper(componentType) == "PRODUCT" {
				body["type"] = "PRODUCT"
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
			body["vcs"] = vcsUuid
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
			body["lifecycle"] = "DRAFT"
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
	rootCmd.PersistentFlags().StringVarP(&rearmUri, "uri", "u", "", "FQDN of ReARM server")
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

	addODeliverableCmd.PersistentFlags().StringVar(&releaseId, "releaseid", "", "UUID of release to add deliverable to (either releaseid or component, branch, and version must be set)")
	addODeliverableCmd.PersistentFlags().StringVar(&component, "component", "", "Component UUID for this release if org-wide key is used")
	addODeliverableCmd.PersistentFlags().StringVar(&version, "version", "", "Release version")
	addODeliverableCmd.PersistentFlags().StringArrayVar(&odelId, "odelid", []string{}, "Deliverable ID (multiple allowed)")
	addODeliverableCmd.PersistentFlags().StringArrayVar(&odelBuildId, "odelbuildid", []string{}, "Deliverable Build ID (multiple allowed)")
	addODeliverableCmd.PersistentFlags().StringArrayVar(&odelBuildUri, "odelbuilduri", []string{}, "Deliverable Build URI (multiple allowed)")
	addODeliverableCmd.PersistentFlags().StringArrayVar(&odelCiMeta, "odelcimeta", []string{}, "Deliverable CI Meta (multiple allowed)")
	addODeliverableCmd.PersistentFlags().StringArrayVar(&odelType, "odeltype", []string{}, "Deliverable Type (multiple allowed)")
	addODeliverableCmd.PersistentFlags().StringArrayVar(&odelDigests, "odeldigests", []string{}, "Deliverable Digests (multiple allowed, separate several digests for one Deliverable with commas)")
	addODeliverableCmd.PersistentFlags().StringArrayVar(&tagsArr, "tagsarr", []string{}, "Deliverable Tag Key-Value Pairs (multiple allowed, separate several tag key-value pairs for one Deliverable with commas, and seprate key-value in a pair with colon)")
	addODeliverableCmd.PersistentFlags().StringArrayVar(&identities, "identities", []string{}, "Deliverable Identity IdenityType-Idenity Pairs (multiple allowed, separate several IdenityType-Idenity pairs for one Deliverable with commas, and seprate IdenityType-Idenity in a pair with colon)")
	addODeliverableCmd.PersistentFlags().StringArrayVar(&dateStart, "datestart", []string{}, "Deliverable Build Start date and time (optional, multiple allowed)")
	addODeliverableCmd.PersistentFlags().StringArrayVar(&dateEnd, "dateend", []string{}, "Deliverable Build End date and time (optional, multiple allowed)")
	addODeliverableCmd.PersistentFlags().StringArrayVar(&odelVersion, "odelversion", []string{}, "Deliverable version, if different from release (multiple allowed)")
	addODeliverableCmd.PersistentFlags().StringArrayVar(&odelPackage, "odelpackage", []string{}, "Deliverable package type (multiple allowed)")
	addODeliverableCmd.PersistentFlags().StringArrayVar(&odelPublisher, "odelpublisher", []string{}, "Deliverable publisher (multiple allowed)")
	addODeliverableCmd.PersistentFlags().StringArrayVar(&odelGroup, "odelgroup", []string{}, "Deliverable group (multiple allowed)")
	addODeliverableCmd.PersistentFlags().StringArrayVar(&supportedOsArr, "osarr", []string{}, "Deliverable supported OS array (multiple allowed, use comma seprated values for each deliverable)")
	addODeliverableCmd.PersistentFlags().StringArrayVar(&supportedCpuArchArr, "cpuarr", []string{}, "Deliverable supported CPU array (multiple allowed, use comma seprated values for each deliverable)")
	addODeliverableCmd.PersistentFlags().StringArrayVar(&odelArtsJson, "odelartsjson", []string{}, "Deliverable Artifacts json array (multiple allowed, use a json array for each deliverable)")

	// flags for createcomponent command
	createComponentCmd.PersistentFlags().StringVar(&componentName, "name", "", "Name of component to create")
	createComponentCmd.MarkPersistentFlagRequired("name")
	createComponentCmd.PersistentFlags().StringVar(&componentType, "type", "", "Specify to create either a component or product")
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

	rootCmd.AddCommand(loginCmd)
	rootCmd.AddCommand(printversionCmd)
	rootCmd.AddCommand(addreleaseCmd)
	rootCmd.AddCommand(addODeliverableCmd)
	rootCmd.AddCommand(checkReleaseByHashCmd)
	rootCmd.AddCommand(createComponentCmd)
	rootCmd.AddCommand(getVersionCmd)
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
