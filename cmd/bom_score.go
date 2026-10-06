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
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/relizaio/rearm/internal/bomscore"
	"github.com/spf13/cobra"
)

// BomScoreFormat is the report format of bomutils score.
type BomScoreFormat string

const (
	BomScoreFormatText BomScoreFormat = "text"
	BomScoreFormatJSON BomScoreFormat = "json"
)

// Exit codes of bomutils score.
const (
	bomScoreExitScored   = 0 // scored, whatever the verdict
	bomScoreExitRefused  = 1 // unsupported or unparsable input, or an I/O error
	bomScoreExitUsage    = 2 // unknown --profile or --format value
	bomScoreExitNotReady = 3 // --fail-on-not-ready and a profile NOT_READY or UNKNOWN
)

var (
	bomScoreProfiles       []string
	bomScoreFormat         string
	bomScoreFailOnNotReady bool
)

// BomScoreOptions are the inputs of one bomutils score run. Infile and Outfile empty or "-" mean
// stdin and stdout.
type BomScoreOptions struct {
	Infile         string
	Outfile        string
	Profiles       []string
	Format         BomScoreFormat
	FailOnNotReady bool
}

var bomScoreCmd = &cobra.Command{
	Use:   "score",
	Args:  cobra.NoArgs,
	Short: "Score an SBOM against the CISA 2026, NTIA 2021 and FDA minimum elements",
	Long: `Score one CycloneDX (JSON, XML; 1.0 to 1.7) or SPDX (JSON, YAML, tag-value; 2.1 to 2.3) file
against the CISA 2026 minimum elements (cisa-2026), the NTIA 2021 minimum elements (ntia-2021) or the
FDA premarket SBOM content (fda), and write a versioned report. Local only: no server, no credentials.

Exit codes: 0 scored; 1 unsupported or unparsable input; 2 unknown --profile or --format value;
3 with --fail-on-not-ready when a requested profile is NOT_READY or UNKNOWN (the report is still written).`,
	Run: func(cmd *cobra.Command, args []string) {
		code := RunBomScore(BomScoreOptions{
			Infile:         infile,
			Outfile:        outfile,
			Profiles:       bomScoreProfiles,
			Format:         BomScoreFormat(bomScoreFormat),
			FailOnNotReady: bomScoreFailOnNotReady,
		}, os.Stdin, os.Stdout, os.Stderr)
		if code != bomScoreExitScored {
			os.Exit(code)
		}
	},
}

func init() {
	bomScoreCmd.Flags().StringArrayVar(&bomScoreProfiles, "profile", []string{string(bomscore.ProfileCISA2026)},
		"profile to score, repeatable: "+strings.Join(bomscore.ProfileKeys(), ", "))
	bomScoreCmd.Flags().StringVar(&bomScoreFormat, "format", string(BomScoreFormatText), "report format: text or json")
	bomScoreCmd.Flags().BoolVar(&bomScoreFailOnNotReady, "fail-on-not-ready", false,
		"exit 3 when a requested profile is NOT_READY or UNKNOWN")
}

// RunBomScore scores the input and writes the report; it returns the exit code. Its own errors
// go to stderr, so stdout carries the report or nothing.
func RunBomScore(opts BomScoreOptions, stdin io.Reader, stdout, stderr io.Writer) int {
	if opts.Format != BomScoreFormatText && opts.Format != BomScoreFormatJSON {
		fmt.Fprintf(stderr, "unknown --format %q; valid formats: %s, %s\n", opts.Format, BomScoreFormatText, BomScoreFormatJSON)
		return bomScoreExitUsage
	}
	profiles := opts.Profiles
	if len(profiles) == 0 {
		profiles = []string{string(bomscore.ProfileCISA2026)}
	}
	valid := map[string]bool{}
	for _, k := range bomscore.ProfileKeys() {
		valid[k] = true
	}
	for _, p := range profiles {
		if !valid[p] {
			fmt.Fprintf(stderr, "unknown --profile %q; valid profiles: %s\n", p, strings.Join(bomscore.ProfileKeys(), ", "))
			return bomScoreExitUsage
		}
	}

	var (
		input []byte
		err   error
	)
	if opts.Infile == "" || opts.Infile == "-" {
		input, err = io.ReadAll(stdin)
	} else {
		input, err = os.ReadFile(opts.Infile)
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return bomScoreExitRefused
	}

	report, err := bomscore.Score(input, profiles, Version)
	if err != nil {
		var unknown *bomscore.UnknownProfileError
		if errors.As(err, &unknown) {
			fmt.Fprintln(stderr, err)
			return bomScoreExitUsage
		}
		fmt.Fprintln(stderr, err)
		return bomScoreExitRefused
	}
	for _, e := range report.Errors {
		fmt.Fprintf(stderr, "check %s failed to evaluate: %s\n", e.Check, e.Message)
	}

	var out []byte
	if opts.Format == BomScoreFormatJSON {
		if out, err = report.JSON(); err != nil {
			fmt.Fprintln(stderr, err)
			return bomScoreExitRefused
		}
	} else {
		out = []byte(report.Text())
	}
	if opts.Outfile == "" || opts.Outfile == "-" {
		_, err = stdout.Write(out)
	} else {
		err = os.WriteFile(opts.Outfile, out, DefaultFileMode)
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return bomScoreExitRefused
	}

	return BomScoreExitCode(report, opts.FailOnNotReady)
}

// BomScoreExitCode is the exit code of a written report: 3 with failOnNotReady when any profile is
// NOT_READY or UNKNOWN, else 0.
func BomScoreExitCode(report bomscore.Report, failOnNotReady bool) int {
	if failOnNotReady {
		for _, p := range report.Profiles {
			if p.Verdict != bomscore.VerdictReady {
				return bomScoreExitNotReady
			}
		}
	}
	return bomScoreExitScored
}
