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
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
)

var (
	sbomComponentUuid string
	sbomBranchUuid    string
)

type SbomProbingRun struct {
	RunId  string `json:"runId"`
	Status string `json:"status"`
}

type SbomProbingResult struct {
	Status  string                  `json:"status"`
	Metrics *DependencyTrackMetrics `json:"metrics"`
}

type DependencyTrackMetrics struct {
	DependencyTrackFullUri               string          `json:"dependencyTrackFullUri"`
	DtrackSubmissionFailed               bool            `json:"dtrackSubmissionFailed"`
	DtrackSubmissionAttempts             *int            `json:"dtrackSubmissionAttempts"`
	DtrackSubmissionFailureReason        string          `json:"dtrackSubmissionFailureReason"`
	LastScanned                          string          `json:"lastScanned"`
	FirstScanned                         string          `json:"firstScanned"`
	Critical                             *int            `json:"critical"`
	High                                 *int            `json:"high"`
	Medium                               *int            `json:"medium"`
	Low                                  *int            `json:"low"`
	Unassigned                           *int            `json:"unassigned"`
	Vulnerabilities                      *int            `json:"vulnerabilities"`
	VulnerableComponents                 *int            `json:"vulnerableComponents"`
	Components                           *int            `json:"components"`
	Suppressed                           *int            `json:"suppressed"`
	FindingsTotal                        *int            `json:"findingsTotal"`
	FindingsAudited                      *int            `json:"findingsAudited"`
	FindingsUnaudited                    *int            `json:"findingsUnaudited"`
	InheritedRiskScore                   *int            `json:"inheritedRiskScore"`
	PolicyViolationsFail                 *int            `json:"policyViolationsFail"`
	PolicyViolationsWarn                 *int            `json:"policyViolationsWarn"`
	PolicyViolationsInfo                 *int            `json:"policyViolationsInfo"`
	PolicyViolationsTotal                *int            `json:"policyViolationsTotal"`
	PolicyViolationsAudited              *int            `json:"policyViolationsAudited"`
	PolicyViolationsUnaudited            *int            `json:"policyViolationsUnaudited"`
	PolicyViolationsSecurityTotal        *int            `json:"policyViolationsSecurityTotal"`
	PolicyViolationsSecurityAudited      *int            `json:"policyViolationsSecurityAudited"`
	PolicyViolationsSecurityUnaudited    *int            `json:"policyViolationsSecurityUnaudited"`
	PolicyViolationsLicenseTotal         *int            `json:"policyViolationsLicenseTotal"`
	PolicyViolationsLicenseAudited       *int            `json:"policyViolationsLicenseAudited"`
	PolicyViolationsLicenseUnaudited     *int            `json:"policyViolationsLicenseUnaudited"`
	PolicyViolationsOperationalTotal     *int            `json:"policyViolationsOperationalTotal"`
	PolicyViolationsOperationalAudited   *int            `json:"policyViolationsOperationalAudited"`
	PolicyViolationsOperationalUnaudited *int            `json:"policyViolationsOperationalUnaudited"`
	VulnerabilityDetails                 []Vulnerability `json:"vulnerabilityDetails"`
	ViolationDetails                     []Violation     `json:"violationDetails"`
	WeaknessDetails                      []Weakness      `json:"weaknessDetails"`
}

// SbomMetricsIntOutput contains only integer fields for normal (non-debug) output.
type SbomMetricsIntOutput struct {
	DtrackSubmissionAttempts             *int `json:"dtrackSubmissionAttempts,omitempty"`
	Critical                             *int `json:"critical,omitempty"`
	High                                 *int `json:"high,omitempty"`
	Medium                               *int `json:"medium,omitempty"`
	Low                                  *int `json:"low,omitempty"`
	Unassigned                           *int `json:"unassigned,omitempty"`
	Vulnerabilities                      *int `json:"vulnerabilities,omitempty"`
	VulnerableComponents                 *int `json:"vulnerableComponents,omitempty"`
	Components                           *int `json:"components,omitempty"`
	Suppressed                           *int `json:"suppressed,omitempty"`
	FindingsTotal                        *int `json:"findingsTotal,omitempty"`
	FindingsAudited                      *int `json:"findingsAudited,omitempty"`
	FindingsUnaudited                    *int `json:"findingsUnaudited,omitempty"`
	InheritedRiskScore                   *int `json:"inheritedRiskScore,omitempty"`
	PolicyViolationsFail                 *int `json:"policyViolationsFail,omitempty"`
	PolicyViolationsWarn                 *int `json:"policyViolationsWarn,omitempty"`
	PolicyViolationsInfo                 *int `json:"policyViolationsInfo,omitempty"`
	PolicyViolationsTotal                *int `json:"policyViolationsTotal,omitempty"`
	PolicyViolationsAudited              *int `json:"policyViolationsAudited,omitempty"`
	PolicyViolationsUnaudited            *int `json:"policyViolationsUnaudited,omitempty"`
	PolicyViolationsSecurityTotal        *int `json:"policyViolationsSecurityTotal,omitempty"`
	PolicyViolationsSecurityAudited      *int `json:"policyViolationsSecurityAudited,omitempty"`
	PolicyViolationsSecurityUnaudited    *int `json:"policyViolationsSecurityUnaudited,omitempty"`
	PolicyViolationsLicenseTotal         *int `json:"policyViolationsLicenseTotal,omitempty"`
	PolicyViolationsLicenseAudited       *int `json:"policyViolationsLicenseAudited,omitempty"`
	PolicyViolationsLicenseUnaudited     *int `json:"policyViolationsLicenseUnaudited,omitempty"`
	PolicyViolationsOperationalTotal     *int `json:"policyViolationsOperationalTotal,omitempty"`
	PolicyViolationsOperationalAudited   *int `json:"policyViolationsOperationalAudited,omitempty"`
	PolicyViolationsOperationalUnaudited *int `json:"policyViolationsOperationalUnaudited,omitempty"`
}

type Vulnerability struct {
	Purl          string                  `json:"purl"`
	VulnId        string                  `json:"vulnId"`
	Severity      string                  `json:"severity"`
	Aliases       []VulnerabilityAliasDto `json:"aliases"`
	Sources       []FindingSourceDto      `json:"sources"`
	Severities    []SeveritySourceDto     `json:"severities"`
	AnalysisState string                  `json:"analysisState"`
	AnalysisDate  string                  `json:"analysisDate"`
	AttributedAt  string                  `json:"attributedAt"`
}

type Violation struct {
	Purl             string             `json:"purl"`
	Type             string             `json:"type"`
	License          string             `json:"license"`
	ViolationDetails string             `json:"violationDetails"`
	Sources          []FindingSourceDto `json:"sources"`
	AnalysisState    string             `json:"analysisState"`
	AnalysisDate     string             `json:"analysisDate"`
	AttributedAt     string             `json:"attributedAt"`
}

type Weakness struct {
	CweId         string             `json:"cweId"`
	RuleId        string             `json:"ruleId"`
	Location      string             `json:"location"`
	Fingerprint   string             `json:"fingerprint"`
	Severity      string             `json:"severity"`
	Sources       []FindingSourceDto `json:"sources"`
	AnalysisState string             `json:"analysisState"`
	AnalysisDate  string             `json:"analysisDate"`
	AttributedAt  string             `json:"attributedAt"`
}

type FindingSourceDto struct {
	Artifact      string `json:"artifact"`
	Release       string `json:"release"`
	Variant       string `json:"variant"`
	AnalysisState string `json:"analysisState"`
	AnalysisDate  string `json:"analysisDate"`
}

type VulnerabilityAliasDto struct {
	Type    string `json:"type"`
	AliasId string `json:"aliasId"`
}

type SeveritySourceDto struct {
	Source   string `json:"source"`
	Severity string `json:"severity"`
}

func init() {
	probeSbomCmd.PersistentFlags().StringVar(&infile, "infile", "", "Path to SBOM file (required)")
	probeSbomCmd.MarkPersistentFlagRequired("infile")
	probeSbomCmd.PersistentFlags().StringVar(&sbomComponentUuid, "componentuuid", "", "Component UUID (optional)")
	probeSbomCmd.PersistentFlags().StringVar(&sbomBranchUuid, "branchuuid", "", "Branch UUID (optional)")
	rootCmd.AddCommand(probeSbomCmd)
}

var probeSbomCmd = &cobra.Command{
	Use:   "probesbom",
	Short: "Probe SBOM for security metrics via Dependency-Track",
	Long: `Submit an SBOM for security probing and wait for the results.

The command submits the SBOM to ReARM, then polls every 10 seconds until
probing is complete. On completion it prints the integer security metrics.
Use --debug true to print the full metrics payload.

Example:
  rearm-cli probesbom -i <api-key-id> -k <api-key-secret> -u http://localhost:8086 \
    --infile sbom.json

  # With optional component and branch:
  rearm-cli probesbom -i <api-key-id> -k <api-key-secret> -u http://localhost:8086 \
    --infile sbom.json --componentuuid <component-uuid> --branchuuid <branch-uuid>`,
	Run: func(cmd *cobra.Command, args []string) {
		probeSbomFunc()
	},
}

func probeSbomFunc() {
	// Read SBOM file
	sbomBytes, err := os.ReadFile(infile)
	if err != nil {
		fmt.Println("Error reading SBOM file:", err)
		os.Exit(1)
	}
	sbomContent := string(sbomBytes)

	if debug == "true" {
		fmt.Println("Using ReARM at", rearmUri)
		fmt.Println("Submitting SBOM probe for", infile)
	}

	// Step 1: Submit SBOM for probing
	mutation := `
		mutation probeSbomProgrammatic($sbom: String!, $componentUuid: ID, $branchUuid: ID) {
			probeSbomProgrammatic(sbom: $sbom, componentUuid: $componentUuid, branchUuid: $branchUuid) {
				runId
				status
			}
		}
	`
	variables := map[string]any{
		"sbom": sbomContent,
	}
	if len(sbomComponentUuid) > 0 {
		variables["componentUuid"] = sbomComponentUuid
	}
	if len(sbomBranchUuid) > 0 {
		variables["branchUuid"] = sbomBranchUuid
	}

	data, err := sendGraphQLRequest(mutation, variables, rearmUri+"/graphql")
	if err != nil {
		printGqlError(err)
		os.Exit(1)
	}

	// Parse the run ID
	runData, ok := data["probeSbomProgrammatic"]
	if !ok {
		fmt.Println("Error: unexpected response from probeSbomProgrammatic")
		os.Exit(1)
	}
	runBytes, _ := json.Marshal(runData)
	var probingRun SbomProbingRun
	if err := json.Unmarshal(runBytes, &probingRun); err != nil {
		fmt.Println("Error parsing probing run response:", err)
		os.Exit(1)
	}

	if debug == "true" {
		fmt.Println("Probing run ID:", probingRun.RunId, "initial status:", probingRun.Status)
	}

	// Step 2: Spinner goroutine while polling
	done := make(chan struct{})
	go func() {
		symbols := []string{"|", "/", "-", "\\"}
		i := 0
		for {
			select {
			case <-done:
				fmt.Print("\r                                          \r") // clear spinner line
				return
			case <-time.After(1 * time.Second):
				fmt.Printf("\rWaiting for SBOM probing result... %s", symbols[i%4])
				i++
			}
		}
	}()

	// Step 3: Poll every 10 seconds
	pollQuery := `
		query getSbomProbingResult($runId: String!) {
			getSbomProbingResult(runId: $runId) {
				status
				metrics {
					dependencyTrackFullUri
					dtrackSubmissionFailed
					dtrackSubmissionAttempts
					dtrackSubmissionFailureReason
					lastScanned
					firstScanned
					critical
					high
					medium
					low
					unassigned
					vulnerabilities
					vulnerableComponents
					components
					suppressed
					findingsTotal
					findingsAudited
					findingsUnaudited
					inheritedRiskScore
					policyViolationsFail
					policyViolationsWarn
					policyViolationsInfo
					policyViolationsTotal
					policyViolationsAudited
					policyViolationsUnaudited
					policyViolationsSecurityTotal
					policyViolationsSecurityAudited
					policyViolationsSecurityUnaudited
					policyViolationsLicenseTotal
					policyViolationsLicenseAudited
					policyViolationsLicenseUnaudited
					policyViolationsOperationalTotal
					policyViolationsOperationalAudited
					policyViolationsOperationalUnaudited
					vulnerabilityDetails {
						purl vulnId severity analysisState analysisDate attributedAt
						aliases { type aliasId }
						sources { artifact release variant analysisState analysisDate }
						severities { source severity }
					}
					violationDetails {
						purl type license violationDetails analysisState analysisDate attributedAt
						sources { artifact release variant analysisState analysisDate }
					}
					weaknessDetails {
						cweId ruleId location fingerprint severity analysisState analysisDate attributedAt
						sources { artifact release variant analysisState analysisDate }
					}
				}
			}
		}
	`
	pollVars := map[string]any{
		"runId": probingRun.RunId,
	}

	for {
		time.Sleep(10 * time.Second)

		pollData, err := sendGraphQLRequest(pollQuery, pollVars, rearmUri+"/graphql")
		if err != nil {
			close(done)
			time.Sleep(200 * time.Millisecond) // let spinner clear
			printGqlError(err)
			os.Exit(1)
		}

		resultRaw, ok := pollData["getSbomProbingResult"]
		if !ok {
			close(done)
			time.Sleep(200 * time.Millisecond)
			fmt.Println("Error: unexpected response from getSbomProbingResult")
			os.Exit(1)
		}

		resultBytes, _ := json.Marshal(resultRaw)
		var probingResult SbomProbingResult
		if err := json.Unmarshal(resultBytes, &probingResult); err != nil {
			close(done)
			time.Sleep(200 * time.Millisecond)
			fmt.Println("Error parsing probing result:", err)
			os.Exit(1)
		}

		if probingResult.Status == "DONE" {
			close(done)
			time.Sleep(200 * time.Millisecond) // let spinner goroutine clear the line

			if probingResult.Metrics == nil {
				fmt.Println("{}")
				return
			}

			if debug == "true" {
				metricsJSON, _ := json.MarshalIndent(probingResult.Metrics, "", "  ")
				fmt.Println(string(metricsJSON))
			} else {
				out := SbomMetricsIntOutput{
					DtrackSubmissionAttempts:             probingResult.Metrics.DtrackSubmissionAttempts,
					Critical:                             probingResult.Metrics.Critical,
					High:                                 probingResult.Metrics.High,
					Medium:                               probingResult.Metrics.Medium,
					Low:                                  probingResult.Metrics.Low,
					Unassigned:                           probingResult.Metrics.Unassigned,
					Vulnerabilities:                      probingResult.Metrics.Vulnerabilities,
					VulnerableComponents:                 probingResult.Metrics.VulnerableComponents,
					Components:                           probingResult.Metrics.Components,
					Suppressed:                           probingResult.Metrics.Suppressed,
					FindingsTotal:                        probingResult.Metrics.FindingsTotal,
					FindingsAudited:                      probingResult.Metrics.FindingsAudited,
					FindingsUnaudited:                    probingResult.Metrics.FindingsUnaudited,
					InheritedRiskScore:                   probingResult.Metrics.InheritedRiskScore,
					PolicyViolationsFail:                 probingResult.Metrics.PolicyViolationsFail,
					PolicyViolationsWarn:                 probingResult.Metrics.PolicyViolationsWarn,
					PolicyViolationsInfo:                 probingResult.Metrics.PolicyViolationsInfo,
					PolicyViolationsTotal:                probingResult.Metrics.PolicyViolationsTotal,
					PolicyViolationsAudited:              probingResult.Metrics.PolicyViolationsAudited,
					PolicyViolationsUnaudited:            probingResult.Metrics.PolicyViolationsUnaudited,
					PolicyViolationsSecurityTotal:        probingResult.Metrics.PolicyViolationsSecurityTotal,
					PolicyViolationsSecurityAudited:      probingResult.Metrics.PolicyViolationsSecurityAudited,
					PolicyViolationsSecurityUnaudited:    probingResult.Metrics.PolicyViolationsSecurityUnaudited,
					PolicyViolationsLicenseTotal:         probingResult.Metrics.PolicyViolationsLicenseTotal,
					PolicyViolationsLicenseAudited:       probingResult.Metrics.PolicyViolationsLicenseAudited,
					PolicyViolationsLicenseUnaudited:     probingResult.Metrics.PolicyViolationsLicenseUnaudited,
					PolicyViolationsOperationalTotal:     probingResult.Metrics.PolicyViolationsOperationalTotal,
					PolicyViolationsOperationalAudited:   probingResult.Metrics.PolicyViolationsOperationalAudited,
					PolicyViolationsOperationalUnaudited: probingResult.Metrics.PolicyViolationsOperationalUnaudited,
				}
				outJSON, _ := json.Marshal(out)
				fmt.Println(string(outJSON))
			}
			return
		}
		// Still PENDING — continue polling
	}
}
