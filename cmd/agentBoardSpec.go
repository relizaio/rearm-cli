/*
The MIT License (MIT)

Copyright (c) 2020 - 2026 Reliza Incorporated (Reliza (tm), https://reliza.io)

Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files
(the "Software"), to deal in the Software without restriction, including without limitation the rights to use, copy, modify,
merge, publish, distribute, sublicense, and/or sell copies of the Software, and to permit persons to whom the Software is furnished
to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF
MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE
FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION
WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
*/

package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/relizaio/rearm-client-go/catalog"
	"github.com/spf13/cobra"
)

// Board and presets files (declarative boards):
//
//   rearm agent board apply -f board.yaml [--dry-run]
//   rearm agent board export --board <name|uuid> [-o board.yaml]
//   rearm agent presets apply -f presets.yaml [--dry-run]
//   rearm agent presets export [-o presets.yaml]
//
// The file's file:, promptFile: and coordinatorPromptFile: references are inlined relative to it
// before anything is sent. The change set prints in the same form as the other declarative kinds,
// and a result holding an error exits non-zero, dry run or not, so CI can gate on it.

var (
	specFile     string
	specDryRun   bool
	specNoSource bool
	specBoard    string
	specOut      string
)

var agentBoardApplyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Apply a board file (kind: BOARD): the board, its settings and its roles",
	Long: `Applies one board file as one transaction: if anything in it is wrong, nothing is
applied and every problem is listed. A field left out is untouched; a field set to null
is cleared. A role the file does not list is deactivated, never deleted; when tasks wait
on it the change says so. --dry-run shows the change set the apply would make.

Where the file comes from -- its repository, commit and path -- is read from git and
recorded on the board, unless --no-source.`,
	Run: func(cmd *cobra.Command, args []string) {
		runSpecApply("BOARD")
	},
}

var agentBoardExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export a board as a board file, by name or uuid",
	Run: func(cmd *cobra.Command, args []string) {
		if strings.TrimSpace(specBoard) == "" {
			fail("--board is required")
		}
		f, err := catalog.ExportBoard(context.Background(), rearmClient(), specBoard)
		if err != nil {
			fail(err.Error())
		}
		writeSpec(f)
	},
}

var agentPresetsCmd = &cobra.Command{
	Use:   "presets",
	Short: "The organization's role presets as a file (apply / export)",
}

var agentPresetsApplyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Apply a presets file (kind: ROLE_PRESETS)",
	Long: `Applies the organization's role presets from one file, as one transaction. With
authoritative: true, presets the file does not list are deactivated. Presets seed new
boards; applying them does not change existing boards.`,
	Run: func(cmd *cobra.Command, args []string) {
		runSpecApply("ROLE_PRESETS")
	},
}

var agentPresetsExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export the organization's role presets as a presets file",
	Run: func(cmd *cobra.Command, args []string) {
		f, err := catalog.ExportRolePresets(context.Background(), rearmClient())
		if err != nil {
			fail(err.Error())
		}
		writeSpec(f)
	},
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "Error:", msg)
	os.Exit(1)
}

func runSpecApply(kind string) {
	if strings.TrimSpace(specFile) == "" {
		fail("-f <file> is required")
	}
	f, err := catalog.Load(specFile)
	if err != nil {
		fail(err.Error())
	}
	switch f.(type) {
	case *catalog.BoardFile:
		if kind != "BOARD" {
			fail(specFile + " is a board file; apply it with `rearm agent board apply`")
		}
	case *catalog.RolePresetsFile:
		if kind != "ROLE_PRESETS" {
			fail(specFile + " is a presets file; apply it with `rearm agent presets apply`")
		}
	default:
		fail(fmt.Sprintf("%s is not a %s file", specFile, kind))
	}
	var source *catalog.Source
	if !specNoSource {
		source = specSource(specFile)
	}
	res, err := catalog.Apply(context.Background(), rearmClient(), f, specDryRun, source)
	if err != nil {
		fail(err.Error())
	}
	fmt.Print(catalog.Format(res))
	if res.Errors > 0 {
		os.Exit(1)
	}
}

// specSource reads where a spec file comes from out of git: the checkout's origin, its HEAD and the
// file's path in it. In GitHub Actions the repository and commit come from the job's environment,
// which names the commit the workflow ran for. Outside a checkout there is no source. A file with
// uncommitted changes records no commit: the bytes applied are not the bytes at HEAD.
func specSource(path string) *catalog.Source {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil
	}
	dir := filepath.Dir(abs)
	top, err := git(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil
	}
	src := &catalog.Source{}
	if rel, err := filepath.Rel(top, abs); err == nil {
		p := filepath.ToSlash(rel)
		src.Path = &p
	}
	repo := gitRemote(top)
	if gh := os.Getenv("GITHUB_REPOSITORY"); gh != "" {
		repo = strings.TrimSuffix(os.Getenv("GITHUB_SERVER_URL"), "/") + "/" + gh
		if !strings.Contains(repo, "://") {
			repo = "https://github.com/" + gh
		}
	}
	if repo != "" {
		src.Repo = &repo
	}
	dirty, _ := git(top, "status", "--porcelain", "--", abs)
	if strings.TrimSpace(dirty) != "" {
		fmt.Fprintln(os.Stderr, "note: "+path+" has uncommitted changes; no commit is recorded for this apply")
		return src
	}
	commit := os.Getenv("GITHUB_SHA")
	if commit == "" {
		commit, _ = git(top, "rev-parse", "HEAD")
	}
	if commit != "" {
		src.Commit = &commit
	}
	return src
}

func writeSpec(f any) {
	out, err := catalog.ToYAML(f)
	if err != nil {
		fail(err.Error())
	}
	if specOut == "" {
		fmt.Print(string(out))
		return
	}
	if err := os.WriteFile(specOut, out, 0o644); err != nil {
		fail(err.Error())
	}
	fmt.Fprintln(os.Stderr, "wrote "+specOut)
}

func init() {
	for _, c := range []*cobra.Command{agentBoardApplyCmd, agentPresetsApplyCmd} {
		c.Flags().StringVarP(&specFile, "file", "f", "", "the spec file (YAML or JSON)")
		c.Flags().BoolVar(&specDryRun, "dry-run", false, "show the change set without applying it")
		c.Flags().BoolVar(&specNoSource, "no-source", false, "do not record the file's repository, commit and path")
	}
	agentBoardExportCmd.Flags().StringVar(&specBoard, "board", "", "the board's name or uuid")
	for _, c := range []*cobra.Command{agentBoardExportCmd, agentPresetsExportCmd} {
		c.Flags().StringVarP(&specOut, "output", "o", "", "write to this file instead of stdout")
	}
	agentBoardCmd.AddCommand(agentBoardApplyCmd)
	agentBoardCmd.AddCommand(agentBoardExportCmd)
	agentPresetsCmd.AddCommand(agentPresetsApplyCmd)
	agentPresetsCmd.AddCommand(agentPresetsExportCmd)
	agentCmd.AddCommand(agentPresetsCmd)
}
