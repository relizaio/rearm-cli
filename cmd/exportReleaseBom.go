/*
The MIT License (MIT)

Copyright (c) 2020 - 2026 Reliza Incorporated (Reliza (tm), https://reliza.io)

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
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	rearm "github.com/relizaio/rearm-client-go"
	"github.com/relizaio/rearm/internal/bomscore"
	"github.com/spf13/cobra"
)

// rearm exportreleasebom and rearm scorereleasebom (task SCORE-24): the merged release / product
// SBOM that the Export Release BOM dialog downloads, and its readiness score, without the UI.
// Both call the programmatic twins of releaseSbomExport / releaseSbomScore; the defaults are the
// dialog's. stdout carries the document (or the report) and nothing else, so a pipe into
// rearm bomutils score stays clean: debug lines and errors go to stderr.

// releaseBomExit ends the process with the command's exit code; a test seam.
var releaseBomExit = os.Exit

// Media types of exportreleasebom.
const (
	releaseBomMediaJSON  = "JSON"
	releaseBomMediaCSV   = "CSV"
	releaseBomMediaExcel = "EXCEL"
)

// sbomScoreErrorCode is extensions.code of a refused server-side score.
const sbomScoreErrorCode = "SBOM_SCORE_ERROR"

var (
	releaseBomStructures    = []string{"FLAT", "HIERARCHICAL"}
	releaseBomBelongsTo     = []string{"DELIVERABLE", "RELEASE", "SCE"}
	releaseBomMediaTypes    = []string{releaseBomMediaJSON, releaseBomMediaCSV, releaseBomMediaExcel}
	releaseBomCoverageTypes = []string{"DEV", "TEST", "BUILD_TIME"}
)

// releaseBomCoverageNone is the --excludecoveragetypes value that sends null: exclude nothing.
const releaseBomCoverageNone = "none"

// releaseBomSelection names the release and the merge options both commands share.
type releaseBomSelection struct {
	releaseId             string
	component             string
	version               string
	tldOnly               bool
	ignoreDev             bool
	structure             string
	belongsTo             string
	excludeCoverageTypes  []string
	excludeFileComponents bool
}

// releaseBomExportOptions are the inputs of one exportreleasebom run.
type releaseBomExportOptions struct {
	releaseBomSelection
	mediaType               string
	includeSupportMetadata  bool
	includeInternalMetadata bool
	outfile                 string
}

// releaseBomScoreOptions are the inputs of one scorereleasebom run.
type releaseBomScoreOptions struct {
	releaseBomSelection
	profiles []string
}

func addReleaseBomSelectionFlags(cmd *cobra.Command, s *releaseBomSelection) {
	cmd.Flags().StringVar(&s.releaseId, "releaseid", "", "UUID of the release (or use --component and --version)")
	cmd.Flags().StringVar(&s.component, "component", "", "Component or product UUID, or its name when it is unique in the organization (with --version)")
	cmd.Flags().StringVar(&s.version, "version", "", "Exact version of the release on --component")
	cmd.Flags().BoolVar(&s.tldOnly, "tldonly", true, "Top-level dependencies only; --tldonly=false carries transitive components")
	cmd.Flags().BoolVar(&s.ignoreDev, "ignoredev", false, "Leave development dependencies out")
	cmd.Flags().StringVar(&s.structure, "structure", "FLAT", "BOM structure: "+strings.Join(releaseBomStructures, " or "))
	cmd.Flags().StringVar(&s.belongsTo, "belongsto", "", "Only SBOMs that belong to: "+strings.Join(releaseBomBelongsTo, ", ")+" (default: all)")
	cmd.Flags().StringSliceVar(&s.excludeCoverageTypes, "excludecoveragetypes", append([]string(nil), releaseBomCoverageTypes...),
		"SBOM coverage types to leave out, comma-separated or repeated: "+strings.Join(releaseBomCoverageTypes, ", ")+"; "+releaseBomCoverageNone+" leaves nothing out")
	cmd.Flags().BoolVar(&s.excludeFileComponents, "excludefilecomponents", false, "Leave components of type file out of the merged BOM")
}

func newExportReleaseBomCmd() *cobra.Command {
	o := &releaseBomExportOptions{}
	cmd := &cobra.Command{
		Use:   "exportreleasebom",
		Args:  cobra.NoArgs,
		Short: "Export the merged SBOM of a release or product release, as the Export Release BOM dialog does",
		Long: `Writes the merged CycloneDX SBOM of a release (for a product release, the product SBOM) that the
Export Release BOM dialog of the ReARM UI downloads, with the dialog's defaults: top-level only, flat,
all SBOMs, development, test and build-time SBOMs excluded, support and internal metadata off.

Name the release by --releaseid, or by --component and --version. The document goes to stdout byte
for byte (nothing else does), or to --outfile. CSV is text; EXCEL needs --outfile.
Every export stores the merged BOM in ReARM's cache, as the UI export does.

Examples:
  rearm exportreleasebom -i $APIKEY_ID -k $APIKEY -u $REARM_URI --releaseid <release uuid> > product.cdx.json
  rearm exportreleasebom --component my-product --version 1.2.3 | rearm bomutils score --profile cisa-2026`,
		Run: func(cmd *cobra.Command, args []string) {
			if code := runExportReleaseBom(o, cmd.OutOrStdout(), cmd.ErrOrStderr()); code != 0 {
				releaseBomExit(code)
			}
		},
	}
	addReleaseBomSelectionFlags(cmd, &o.releaseBomSelection)
	cmd.Flags().StringVar(&o.mediaType, "mediatype", releaseBomMediaJSON, "Document format: "+strings.Join(releaseBomMediaTypes, ", "))
	cmd.Flags().BoolVar(&o.includeSupportMetadata, "includesupportmetadata", false, "Include the support-status metadata (refused where the organization does not disclose it)")
	cmd.Flags().BoolVar(&o.includeInternalMetadata, "includeinternalmetadata", false, "Include ReARM's internal metadata")
	cmd.Flags().StringVar(&o.outfile, "outfile", "", "Write the document to this file instead of stdout (required for EXCEL)")
	return cmd
}

func newScoreReleaseBomCmd() *cobra.Command {
	o := &releaseBomScoreOptions{}
	cmd := &cobra.Command{
		Use:   "scorereleasebom",
		Args:  cobra.NoArgs,
		Short: "Score the merged SBOM of a release or product release on the server",
		Long: `Scores the merged SBOM that exportreleasebom exports for the same options against the requested
profiles on the server, and writes the report JSON (the rearm bomutils
score --format json report) to stdout. Exit 0 whenever a report came back, ready or not.

Support and internal metadata follow the organization's default, as the UI Score button does, so the
scored document can differ from a default exportreleasebom (which leaves support metadata out) where
the organization discloses support metadata.

Examples:
  rearm scorereleasebom --releaseid <release uuid> --profile cisa-2026 --profile fda`,
		Run: func(cmd *cobra.Command, args []string) {
			if code := runScoreReleaseBom(o, cmd.OutOrStdout(), cmd.ErrOrStderr()); code != 0 {
				releaseBomExit(code)
			}
		},
	}
	addReleaseBomSelectionFlags(cmd, &o.releaseBomSelection)
	cmd.Flags().StringArrayVar(&o.profiles, "profile", nil, "Profile to score, repeatable (required): "+strings.Join(bomscore.ProfileKeys(), ", "))
	return cmd
}

func init() {
	rootCmd.AddCommand(newExportReleaseBomCmd())
	rootCmd.AddCommand(newScoreReleaseBomCmd())
}

// oneOf returns the value upper-cased when it is one of the allowed values.
func oneOf(flag, value string, allowed []string) (string, error) {
	v := strings.ToUpper(strings.TrimSpace(value))
	for _, a := range allowed {
		if v == a {
			return v, nil
		}
	}
	return "", fmt.Errorf("--%s %q is not one of %s", flag, value, strings.Join(allowed, ", "))
}

// variables validates the selection and returns the request variables it maps to.
func (s *releaseBomSelection) variables() (map[string]interface{}, error) {
	byRelease := strings.TrimSpace(s.releaseId) != ""
	byComponent := strings.TrimSpace(s.component) != "" || strings.TrimSpace(s.version) != ""
	if byRelease == byComponent || (byComponent && (strings.TrimSpace(s.component) == "" || strings.TrimSpace(s.version) == "")) {
		return nil, errors.New("name the release by --releaseid, or by --component and --version (exactly one of the two)")
	}
	vars := map[string]interface{}{
		"tldOnly":               s.tldOnly,
		"ignoreDev":             s.ignoreDev,
		"excludeFileComponents": s.excludeFileComponents,
	}
	if byRelease {
		vars["release"] = strings.TrimSpace(s.releaseId)
	} else {
		vars["componentId"] = strings.TrimSpace(s.component)
		vars["version"] = strings.TrimSpace(s.version)
	}
	structure, err := oneOf("structure", s.structure, releaseBomStructures)
	if err != nil {
		return nil, err
	}
	vars["structure"] = structure
	vars["belongsTo"] = nil
	if strings.TrimSpace(s.belongsTo) != "" {
		b, err := oneOf("belongsto", s.belongsTo, releaseBomBelongsTo)
		if err != nil {
			return nil, err
		}
		vars["belongsTo"] = b
	}
	coverage, err := coverageTypes(s.excludeCoverageTypes)
	if err != nil {
		return nil, err
	}
	vars["excludeCoverageTypes"] = coverage
	return vars, nil
}

// coverageTypes maps --excludecoveragetypes: none alone is null, anything else a validated list.
func coverageTypes(values []string) (interface{}, error) {
	if len(values) == 1 && strings.EqualFold(strings.TrimSpace(values[0]), releaseBomCoverageNone) {
		return nil, nil
	}
	out := make([]string, 0, len(values))
	for _, v := range values {
		if strings.EqualFold(strings.TrimSpace(v), releaseBomCoverageNone) {
			return nil, fmt.Errorf("--excludecoveragetypes %s cannot be combined with other values", releaseBomCoverageNone)
		}
		c, err := oneOf("excludecoveragetypes", v, releaseBomCoverageTypes)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("--excludecoveragetypes needs a value; use %s to leave nothing out", releaseBomCoverageNone)
	}
	return out, nil
}

func releaseBomDebug(stderr io.Writer, format string, a ...interface{}) {
	if debug == "true" {
		fmt.Fprintf(stderr, format+"\n", a...)
	}
}

// releaseBomError prints the error line; a refused score names its reason.
func releaseBomError(stderr io.Writer, err error) {
	var gqlErrs rearm.GraphQLErrors
	if errors.As(err, &gqlErrs) {
		for _, e := range gqlErrs {
			if code, _ := e.Extensions["code"].(string); code == sbomScoreErrorCode {
				reason, _ := e.Extensions["reason"].(string)
				fmt.Fprintf(stderr, "Error: score refused: %s: %s\n", reason, e.Message)
				return
			}
		}
	}
	fmt.Fprintln(stderr, "Error:", describeError(err))
}

// releaseBomField runs the operation and returns the string the root field holds; what names the
// result in the error a null answer gets.
func releaseBomField(query, field, what string, vars map[string]interface{}, stderr io.Writer) (string, bool) {
	data, err := sendGraphQLRequest(query, vars)
	if err != nil {
		releaseBomError(stderr, err)
		return "", false
	}
	value, ok := data[field].(string)
	if !ok {
		fmt.Fprintf(stderr, "Error: empty %s: the server returned no document\n", what)
		return "", false
	}
	return value, true
}

func runExportReleaseBom(o *releaseBomExportOptions, stdout, stderr io.Writer) int {
	vars, err := o.variables()
	if err != nil {
		fmt.Fprintln(stderr, "Error:", err)
		return 1
	}
	mediaType, err := oneOf("mediatype", o.mediaType, releaseBomMediaTypes)
	if err != nil {
		fmt.Fprintln(stderr, "Error:", err)
		return 1
	}
	if mediaType == releaseBomMediaExcel && strings.TrimSpace(o.outfile) == "" {
		fmt.Fprintln(stderr, "Error: --mediatype EXCEL needs --outfile (a workbook is not written to stdout)")
		return 1
	}
	if strings.TrimSpace(o.outfile) == "-" {
		fmt.Fprintln(stderr, "Error: --outfile - is not supported; leave --outfile out to write to stdout")
		return 1
	}
	vars["mediaType"] = mediaType
	vars["includeSupportMetadata"] = o.includeSupportMetadata
	vars["includeInternalMetadata"] = o.includeInternalMetadata
	releaseBomDebug(stderr, "Using ReARM at %s", rearmUri)
	releaseBomDebug(stderr, "releaseSbomExportProgrammatic variables: %v", vars)

	doc, ok := releaseBomField(rearm.ReleaseSbomExportProgrammatic_Operation, "releaseSbomExportProgrammatic", "export", vars, stderr)
	if !ok {
		return 1
	}
	content := []byte(doc)
	if mediaType == releaseBomMediaExcel {
		content, err = base64.StdEncoding.DecodeString(doc)
		if err != nil {
			fmt.Fprintln(stderr, "Error: the EXCEL export is not valid base64:", err)
			return 1
		}
	}
	if o.outfile == "" {
		if _, err := stdout.Write(content); err != nil {
			fmt.Fprintln(stderr, "Error writing the document:", err)
			return 1
		}
		return 0
	}
	if dir := filepath.Dir(o.outfile); dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			fmt.Fprintln(stderr, "Error creating output directory:", err)
			return 1
		}
	}
	if err := os.WriteFile(o.outfile, content, 0644); err != nil {
		fmt.Fprintln(stderr, "Error writing file:", err)
		return 1
	}
	fmt.Fprintln(stdout, o.outfile)
	return 0
}

func runScoreReleaseBom(o *releaseBomScoreOptions, stdout, stderr io.Writer) int {
	vars, err := o.variables()
	if err != nil {
		fmt.Fprintln(stderr, "Error:", err)
		return 1
	}
	if len(o.profiles) == 0 {
		fmt.Fprintln(stderr, "Error: --profile is required (repeatable):", strings.Join(bomscore.ProfileKeys(), ", "))
		return 1
	}
	vars["profiles"] = o.profiles
	// The UI Score button's choice: the organization's default document, never a silently stripped one.
	vars["includeSupportMetadata"] = nil
	vars["includeInternalMetadata"] = nil
	releaseBomDebug(stderr, "Using ReARM at %s", rearmUri)
	releaseBomDebug(stderr, "releaseSbomScoreProgrammatic variables: %v", vars)

	report, ok := releaseBomField(rearm.ReleaseSbomScoreProgrammatic_Operation, "releaseSbomScoreProgrammatic", "score", vars, stderr)
	if !ok {
		return 1
	}
	if _, err := io.WriteString(stdout, report); err != nil {
		fmt.Fprintln(stderr, "Error writing the report:", err)
		return 1
	}
	return 0
}
