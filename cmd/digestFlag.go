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
	"fmt"
	"regexp"
	"strings"
)

// A --digest value is <algo>:<hex>[:<scope>], e.g. sha256:9f86d0...08 or sha256:9f86d0...08:OCI_STORAGE.
// It becomes one DigestRecordInput {algo, digest, scope}; the server rejects a bare string there.

// Hex length of each checksum type the API knows (TeaArtifactChecksumType), so a truncated or
// mistyped digest is caught here rather than recorded against the artifact.
var digestHexLength = map[string]int{
	"MD5":         32,
	"SHA_1":       40,
	"SHA_256":     64,
	"SHA_384":     96,
	"SHA_512":     128,
	"SHA3_256":    64,
	"SHA3_384":    96,
	"SHA3_512":    128,
	"BLAKE2B_256": 64,
	"BLAKE2B_384": 96,
	"BLAKE2B_512": 128,
	"BLAKE3":      64,
}

// Spellings people write for the same algorithm, after upper-casing and turning '-' into '_'.
var digestAlgoAliases = map[string]string{
	"SHA1":   "SHA_1",
	"SHA256": "SHA_256",
	"SHA384": "SHA_384",
	"SHA512": "SHA_512",
}

// Scopes a client may declare. AS_UPLOADED and RAW_OCI_STORAGE are computed by ReARM and the
// server refuses them on upload; ORIGINAL_FILE ("the file as the publisher had it") is the default.
var declarableDigestScopes = map[string]bool{"ORIGINAL_FILE": true, "OCI_STORAGE": true, "REARM": true}

var serverDerivedDigestScopes = map[string]bool{"AS_UPLOADED": true, "RAW_OCI_STORAGE": true}

var hexDigest = regexp.MustCompile(`^[0-9a-fA-F]+$`)

// parseDigestFlag turns one --digest value into a DigestRecordInput.
func parseDigestFlag(v string) (map[string]interface{}, error) {
	parts := strings.Split(strings.TrimSpace(v), ":")
	if len(parts) < 2 || len(parts) > 3 {
		return nil, fmt.Errorf("--digest %q: expected <algo>:<hex>[:<scope>], e.g. sha256:<64 hex chars>", v)
	}
	algo := strings.ReplaceAll(strings.ToUpper(strings.TrimSpace(parts[0])), "-", "_")
	if alias, ok := digestAlgoAliases[algo]; ok {
		algo = alias
	}
	want, ok := digestHexLength[algo]
	if !ok {
		return nil, fmt.Errorf("--digest %q: unknown algorithm %q", v, parts[0])
	}
	hex := strings.TrimSpace(parts[1])
	if !hexDigest.MatchString(hex) {
		return nil, fmt.Errorf("--digest %q: the digest must be hexadecimal", v)
	}
	if len(hex) != want {
		return nil, fmt.Errorf("--digest %q: a %s digest is %d hex characters, got %d", v, algo, want, len(hex))
	}
	scope := "ORIGINAL_FILE"
	if len(parts) == 3 {
		scope = strings.ToUpper(strings.TrimSpace(parts[2]))
		if serverDerivedDigestScopes[scope] {
			return nil, fmt.Errorf("--digest %q: scope %s is computed by ReARM and cannot be supplied", v, scope)
		}
		if !declarableDigestScopes[scope] {
			return nil, fmt.Errorf("--digest %q: unknown scope %q (ORIGINAL_FILE, OCI_STORAGE or REARM)", v, parts[2])
		}
	}
	return map[string]interface{}{"algo": algo, "digest": strings.ToLower(hex), "scope": scope}, nil
}

// parseDigestFlags parses every --digest value, stopping at the first bad one.
func parseDigestFlags(values []string) ([]map[string]interface{}, error) {
	var out []map[string]interface{}
	for _, v := range values {
		r, err := parseDigestFlag(v)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}
