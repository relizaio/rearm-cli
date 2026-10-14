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
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/relizaio/rearm/internal/bomscore"
	"github.com/spf13/cobra"
)

// Exit codes of bomutils verify-signature, aligned with bomutils score.
const (
	bomVerifySignatureExitVerified    = 0 // the signature verifies
	bomVerifySignatureExitRefused     = 1 // unreadable or unsupported input, or an unusable key file
	bomVerifySignatureExitUsage       = 2 // unknown --format value
	bomVerifySignatureExitNotVerified = 3 // any other outcome, the line is still written
)

var (
	bomVerifySignatureKey    string
	bomVerifySignatureFormat string
)

// BomVerifySignatureOptions are the inputs of one bomutils verify-signature run. Infile and Outfile
// empty or "-" mean stdin and stdout; Key empty verifies against the embedded publicKey.
type BomVerifySignatureOptions struct {
	Infile  string
	Outfile string
	Key     string
	Format  BomScoreFormat
}

var bomVerifySignatureCmd = &cobra.Command{
	Use:   "verify-signature",
	Args:  cobra.NoArgs,
	Short: "Verify the root JSF signature of a CycloneDX JSON SBOM",
	Long: `Verify the root signature (CycloneDX JSF: the document with signature.value removed,
canonicalised with RFC 8785 JCS; Ed25519, ES256, ES384, ES512) of one CycloneDX JSON SBOM.

With --key the signature is verified with that public key: a PEM SubjectPublicKeyInfo, a JWK, or a
JWKS (the key whose kid equals the signature's keyId; a REVOKED key does not verify). To check that
an export was signed by its ReARM organization, save the organization's public JWKS to a file and
pass it with --key. Without --key the signature is verified against the publicKey it embeds: that
proves the document was not changed since it was signed under that key, and says nothing about who
holds the key (integrity, not authorship).

Exit codes: 0 verified; 1 unreadable, unsupported or unparsable input, or an unreadable or
unsupported --key file; 2 unknown --format value; 3 not verified (no signature, malformed,
unsupported algorithm or form, key mismatch, revoked key, no key; the line is still written).`,
	Run: func(cmd *cobra.Command, args []string) {
		code := RunBomVerifySignature(BomVerifySignatureOptions{
			Infile:  infile,
			Outfile: outfile,
			Key:     bomVerifySignatureKey,
			Format:  BomScoreFormat(bomVerifySignatureFormat),
		}, os.Stdin, os.Stdout, os.Stderr)
		if code != bomVerifySignatureExitVerified {
			os.Exit(code)
		}
	},
}

func init() {
	bomVerifySignatureCmd.Flags().StringVar(&bomVerifySignatureKey, "key", "",
		"public key file: PEM, JWK or JWKS (for example the organization's saved JWKS); without it the embedded publicKey is used")
	bomVerifySignatureCmd.Flags().StringVar(&bomVerifySignatureFormat, "format", string(BomScoreFormatText), "output format: text or json")
}

// RunBomVerifySignature verifies the input's root signature and writes one line (text) or the
// input.signature object (json); it returns the exit code. Its own errors go to stderr, so stdout
// carries the result or nothing.
func RunBomVerifySignature(opts BomVerifySignatureOptions, stdin io.Reader, stdout, stderr io.Writer) int {
	if opts.Format != BomScoreFormatText && opts.Format != BomScoreFormatJSON {
		fmt.Fprintf(stderr, "unknown --format %q; valid formats: %s, %s\n", opts.Format, BomScoreFormatText, BomScoreFormatJSON)
		return bomVerifySignatureExitUsage
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
		return bomVerifySignatureExitRefused
	}
	rep, err := bomscore.VerifySignature(input, opts.Key)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return bomVerifySignatureExitRefused
	}

	var out []byte
	if opts.Format == BomScoreFormatJSON {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			fmt.Fprintln(stderr, err)
			return bomVerifySignatureExitRefused
		}
		out = buf.Bytes()
	} else {
		out = []byte(verifySignatureLine(rep, opts.Key) + "\n")
	}
	if opts.Outfile == "" || opts.Outfile == "-" {
		_, err = stdout.Write(out)
	} else {
		err = os.WriteFile(opts.Outfile, out, DefaultFileMode)
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return bomVerifySignatureExitRefused
	}
	if rep.Verification != bomscore.VerificationVerified {
		return bomVerifySignatureExitNotVerified
	}
	return bomVerifySignatureExitVerified
}

// verifySignatureLine is the text output: what verified the signature, or why it is not verified.
func verifySignatureLine(rep bomscore.SignatureReport, key string) string {
	// The detail is already printable; algorithm and keyId are the document's own strings.
	rep.Algorithm, rep.KeyID = bomscore.PrintableText(rep.Algorithm), bomscore.PrintableText(rep.KeyID)
	switch {
	case rep.Verification != bomscore.VerificationVerified:
		return fmt.Sprintf("not verified (%s): %s", rep.Verification, rep.Detail)
	case key == "":
		keyID := rep.KeyID
		if keyID == "" {
			keyID = "none"
		}
		return fmt.Sprintf("verified with the embedded publicKey: integrity under that key, not authorship; algorithm %s, keyId %s", rep.Algorithm, keyID)
	case rep.KeyID == "":
		return fmt.Sprintf("verified with the given key from %s; algorithm %s", key, rep.Algorithm)
	default:
		return fmt.Sprintf("verified with key %s from %s; algorithm %s", rep.KeyID, key, rep.Algorithm)
	}
}
