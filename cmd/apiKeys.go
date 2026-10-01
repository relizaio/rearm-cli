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
	"strings"

	rearm "github.com/relizaio/rearm-client-go"
	"github.com/relizaio/rearm-client-go/catalog"
	"github.com/spf13/cobra"
)

// API keys as declarative configuration (task RD3-11):
//
//   rearm apikey apply -f keys.yaml [--dry-run]
//   rearm apikey export [--key <name> ...] [-o keys.yaml]
//   rearm apikey list [--key <name> ...]
//   rearm apikey mint <key> --slot <1|2> [--rotate]
//
// A file declares a key's identity and settings, never a secret. A secret is minted apart and
// printed once: ReARM keeps only its hash. Every one of them needs CONFIGURATION_WRITE, and a
// caller acts only on keys no stronger than itself.

var (
	apiKeyNames  []string
	apiKeySlot   int
	apiKeyRotate bool
)

var apiKeyCmd = &cobra.Command{
	Use:   "apikey",
	Short: "FREEFORM API keys as declarative configuration: apply, export, list, and mint a secret",
}

var apiKeyApplyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Apply an API keys file (kind: API_KEYS): identity and settings by name, never a secret",
	Long: `Applies an API_KEYS file, keyed by each key's declared name. It declares FREEFORM keys only:
ORGANIZATION and ORGANIZATION_RW keys are deprecated. A key it creates has no
secret; mint one with ` + "`rearm apikey mint`" + `. A setting left out is untouched; one set to
null is cleared. A key that fails is one error line and the others land; with authoritative:
true, declared keys the file leaves out are deactivated when the organization's declarative
prune setting is ARCHIVE. --dry-run shows the change set the apply would make.

Where the file comes from -- its repository, commit and path -- is read from git and recorded
on the keys, unless --no-source.`,
	Run: func(cmd *cobra.Command, args []string) {
		runSpecApply("API_KEYS")
	},
}

var apiKeyExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export the organization's declared API keys as an API_KEYS file (no secrets)",
	Long: `Writes the organization's declared keys as an API_KEYS file, with no secret in it. Without
--key the file says authoritative: true: applied again, it claims every declared key, and when the
organization's declarative prune setting is ARCHIVE a declared key the file no longer lists is
deactivated. With --key it lists only those keys and is not authoritative.`,
	Run: func(cmd *cobra.Command, args []string) {
		f, err := catalog.ExportApiKeys(context.Background(), rearmClient(), apiKeyNames)
		if err != nil {
			printRefusal(err)
			os.Exit(1)
		}
		writeSpec(f)
	},
}

var apiKeyListCmd = &cobra.Command{
	Use:   "list",
	Short: "The organization's API keys: key ids, declared names and secret slots, never a value",
	Long: `Lists every live key of the organization, declared or not, or with --key the keys
declared under those names: each with the key id a client presents, its type, status and
settings, and its secrets' slots (active, created, expires, last used) -- never a value.`,
	Run: func(cmd *cobra.Command, args []string) {
		data, err := sendGraphQLRequest(rearm.ApiKeys_Operation, apiKeyListVariables(apiKeyNames))
		if err != nil {
			printRefusal(err)
			os.Exit(1)
		}
		emitJson(data["apiKeysProgrammatic"])
	},
}

// apiKeyListVariables sends the names only when some were given: none lists every key.
func apiKeyListVariables(names []string) map[string]interface{} {
	vars := map[string]interface{}{}
	if len(names) > 0 {
		vars["keys"] = names
	}
	return vars
}

var apiKeyMintCmd = &cobra.Command{
	Use:   "mint <key>",
	Short: "Mint a secret for an API key and print it once",
	Long: `Mints a secret in slot 1 or 2 of a declared FREEFORM key, named by its declared name, key id or uuid, and
prints it once: ReARM keeps only a hash, so it cannot be shown again. A slot that already holds
a secret is left alone unless --rotate, which replaces it -- the old value stops working at
once. The secret's expiry follows the key's secretExpiresDays. Refused for a key no file
declares (an organization admin can declare a key made by hand first), for a key stronger than
the caller, and for a key a person holds (its holder mints it on the keys page).`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		vars, err := apiKeyMintVariables(args[0], apiKeySlot, apiKeyRotate)
		if err != nil {
			fail(err.Error())
		}
		data, err := sendGraphQLRequest(rearm.MintApiKeySecret_Operation, vars)
		if err != nil {
			printRefusal(err)
			os.Exit(1)
		}
		out, _ := data["mintApiKeySecretProgrammatic"].(map[string]interface{})
		if note := apiKeyMintNote(out); note != "" {
			fmt.Fprintln(os.Stderr, note)
		}
		emitJson(out)
	},
}

// apiKeyMintVariables checks the slot before anything is sent.
func apiKeyMintVariables(key string, slot int, rotate bool) (map[string]interface{}, error) {
	if strings.TrimSpace(key) == "" {
		return nil, fmt.Errorf("name the key: its declared name, key id or uuid")
	}
	if slot != 1 && slot != 2 {
		return nil, fmt.Errorf("--slot is 1 or 2")
	}
	return map[string]interface{}{"key": key, "slot": slot, "rotate": rotate}, nil
}

// apiKeyMintNote says on stderr why no secret came back, when none did.
func apiKeyMintNote(out map[string]interface{}) string {
	if minted, _ := out["minted"].(bool); minted {
		return ""
	}
	return fmt.Sprintf("note: slot %v already holds a secret, which cannot be shown again; --rotate replaces it", out["slot"])
}

func init() {
	apiKeyApplyCmd.Flags().StringVarP(&specFile, "file", "f", "", "the API keys file (YAML or JSON)")
	apiKeyApplyCmd.Flags().BoolVar(&specDryRun, "dry-run", false, "show the change set without applying it")
	apiKeyApplyCmd.Flags().BoolVar(&specNoSource, "no-source", false, "do not record the file's repository, commit and path")
	for _, c := range []*cobra.Command{apiKeyExportCmd, apiKeyListCmd} {
		c.Flags().StringSliceVar(&apiKeyNames, "key", nil, "a declared key name; repeat for several")
	}
	apiKeyExportCmd.Flags().StringVarP(&specOut, "output", "o", "", "write to this file instead of stdout")
	apiKeyMintCmd.Flags().IntVar(&apiKeySlot, "slot", 1, "the secret slot, 1 or 2")
	apiKeyMintCmd.Flags().BoolVar(&apiKeyRotate, "rotate", false, "replace the secret the slot holds; its old value stops working")
	apiKeyCmd.AddCommand(apiKeyApplyCmd, apiKeyExportCmd, apiKeyListCmd, apiKeyMintCmd)
	rootCmd.AddCommand(apiKeyCmd)
}
