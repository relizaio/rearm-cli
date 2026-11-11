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
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	tea "github.com/oolong-project/tea-client-go"
	"github.com/oolong-project/tea-client-go/teaapi"
)

// teaTextMax is the number of runes of one server-supplied value printed before it is cut.
const teaTextMax = 2048

// teaText makes a server-supplied string safe to print on a terminal: every rune that is
// not unicode.IsPrint (C0 and C1 controls, ESC, CR, LF, DEL, bidirectional and other
// format characters, non-ASCII spaces) is written as its Go escape (\x1b, \n, \u202e), an
// invalid UTF-8 byte as \xNN; the result is cut at teaTextMax runes with "..." appended.
// Escaping rather than dropping keeps two different values different on screen.
func teaText(s string) string {
	var b strings.Builder
	n := 0
	for i := 0; i < len(s); n++ {
		if n == teaTextMax {
			b.WriteString("...")
			break
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			fmt.Fprintf(&b, `\x%02x`, s[i])
		case unicode.IsPrint(r):
			b.WriteString(s[i : i+size])
		default:
			q := strconv.QuoteRune(r)
			b.WriteString(q[1 : len(q)-1])
		}
		i += size
	}
	return b.String()
}

// teaCut cuts s at teaTextMax runes with "..." appended, for the fields printed with %q
// (which escapes on its own).
func teaCut(s string) string {
	n := 0
	for i := range s {
		if n == teaTextMax {
			return s[:i] + "..."
		}
		n++
	}
	return s
}

func teaDeref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// teaTime prints a timestamp as a UTC instant.
func teaTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

// chooseTeaCandidate lists several discovery results on stderr and lets the person at the
// terminal choose one. Without a terminal it reads nothing: a caller with an open stdin
// must not hang. It is the FlowOptions.Select of the flow; a non-interactive selector of
// a later machine mode has the same signature.
func (tio teaIO) chooseTeaCandidate(ctx context.Context, cands []tea.Candidate) (int, error) {
	n := len(cands)
	fmt.Fprintf(tio.errOut, "Multiple discovery results found (%d results).\n", n)
	for i, c := range cands {
		name := ""
		version := ""
		if c.Release != nil {
			name = teaDeref(c.Release.ProductName)
			version = c.Release.Version
			if name == "" {
				name = "product " + c.Release.Product.String()
			}
		}
		root := ""
		if c.API != nil {
			root = c.API.Base().RootURL()
		}
		fmt.Fprintf(tio.errOut, "  %d) %s %s (product release %s, server %s)\n",
			i+1, teaText(name), teaText(version), c.Result.ProductReleaseUUID.String(), teaText(root))
	}
	if tio.stdin != teaStdinTerminal {
		return 0, &teaChoiceError{msg: fmt.Sprintf("the TEI resolves to %d product releases and standard input is not a terminal; run the command in a terminal to choose one", n)}
	}
	fmt.Fprint(tio.errOut, "Enter your choice (number): ")
	line, err := readTeaLine(tio.in, teaChoiceMaxBytes)
	refuse := func(reason string) (int, error) {
		return 0, &teaChoiceError{msg: fmt.Sprintf("%s: %s; enter a number from 1 to %d", errTeaAmbiguous.Error(), reason, n)}
	}
	switch {
	case errors.Is(err, errTeaLineTooLong):
		return refuse(fmt.Sprintf("the answer is longer than %d bytes", teaChoiceMaxBytes))
	case err != nil && !errors.Is(err, io.EOF):
		return refuse("standard input could not be read")
	}
	answer := strings.TrimSpace(line)
	if answer == "" {
		return refuse("no answer")
	}
	k, convErr := strconv.Atoi(answer)
	if convErr != nil {
		return refuse("the answer is not a number")
	}
	if k < 1 || k > n {
		return refuse(fmt.Sprintf("%d is out of range", k))
	}
	return k - 1, nil
}

// printTeaFlow prints a flow result on w, a warning per component that could not be read
// on errW, and reports whether any component failed.
func printTeaFlow(w, errW io.Writer, r *tea.FlowResult) bool {
	base := ""
	if r.API != nil {
		base = r.API.Base().String()
	}
	rel := r.Release
	fmt.Fprintf(w, "\n=== Product Information ===\n")
	fmt.Fprintf(w, "Product Name: %s\n", teaText(teaDeref(rel.ProductName)))
	fmt.Fprintf(w, "Version: %s\n", teaText(rel.Version))
	printTeaCLE(w, r.ReleaseCLE)
	printTeaArtifacts(w, base, rel.LatestCollection.Artifacts)
	fmt.Fprintln(w)

	partial := false
	for _, fc := range r.Components {
		if fc.Err != nil {
			partial = true
			fmt.Fprintf(errW, "Warning: component %s: %s\n", fc.Ref.Uuid.String(), teaText(fc.Err.Error()))
		}
		if fc.Release == nil {
			continue
		}
		name := teaComponentName(fc)
		if !fc.Pinned {
			fmt.Fprintf(w, "Note: Component '%s' does not have a pinned release. Selecting latest available release.\n", name)
		}
		fmt.Fprintf(w, "\n--- Component: %s ---\n", name)
		fmt.Fprintf(w, "Version: %s\n", teaText(fc.Release.Version))
		if fc.Err == nil {
			printTeaCLE(w, fc.CLE)
		}
		if len(fc.Release.LatestCollection.Artifacts) == 0 {
			fmt.Fprintln(w, "  No artifacts in the latest collection of this component release.")
			continue
		}
		printTeaArtifacts(w, base, fc.Release.LatestCollection.Artifacts)
	}
	return partial
}

// teaComponentName is the release's componentName, else the component's name (read for an
// unpinned ref), else "component <uuid>"; escaped for printing.
func teaComponentName(fc tea.FlowComponent) string {
	if fc.Release != nil && teaDeref(fc.Release.ComponentName) != "" {
		return teaText(*fc.Release.ComponentName)
	}
	if fc.Component != nil && fc.Component.Name != "" {
		return teaText(fc.Component.Name)
	}
	return "component " + fc.Ref.Uuid.String()
}

// printTeaArtifacts prints the artifact blocks of a collection. A format without an
// external url prints the server's download endpoint for it.
func printTeaArtifacts(w io.Writer, base string, artifacts []teaapi.Artifact) {
	for _, a := range artifacts {
		label := teaText(string(a.Type))
		if name := teaDeref(a.Name); name != "" {
			label = fmt.Sprintf("%s (%s)", teaText(name), label)
		}
		fmt.Fprintf(w, "\n  Artifact: %s\n", label)
		for _, f := range a.Formats {
			u := teaDeref(f.Url)
			if u == "" {
				u = fmt.Sprintf("%s/artifact/%s/%d/download?mediaType=%s", base, a.Uuid.String(), a.Version, url.QueryEscape(f.MediaType))
			}
			fmt.Fprintf(w, "    - Description: %s\n", teaText(teaDeref(f.Description)))
			fmt.Fprintf(w, "      Media Type: %s\n", teaText(f.MediaType))
			fmt.Fprintf(w, "      URL: %s\n", teaText(u))
			if sig := teaDeref(f.SignatureUrl); sig != "" {
				fmt.Fprintf(w, "      Signature URL: %s\n", teaText(sig))
			}
		}
	}
}

// printTeaCLE prints lifecycle events, omitting absent fields; nothing when there are none.
func printTeaCLE(w io.Writer, cle *teaapi.Cle) {
	if cle == nil || len(cle.Events) == 0 {
		return
	}
	fmt.Fprintf(w, "  Lifecycle Events (CLE):\n")
	for _, ev := range cle.Events {
		var b strings.Builder
		fmt.Fprintf(&b, "    [%d] type=%s effective=%s published=%s", ev.Id, teaText(string(ev.Type)), teaTime(ev.Effective), teaTime(ev.Published))
		if v := teaDeref(ev.Version); v != "" {
			fmt.Fprintf(&b, " version=%s", teaText(v))
		}
		if ev.Versions != nil {
			var parts []string
			for _, v := range *ev.Versions {
				switch {
				case teaDeref(v.Version) != "":
					parts = append(parts, teaText(*v.Version))
				case teaDeref(v.Range) != "":
					parts = append(parts, teaText(*v.Range))
				}
			}
			if len(parts) > 0 {
				fmt.Fprintf(&b, " versions=%s", strings.Join(parts, ","))
			}
		}
		if v := teaDeref(ev.License); v != "" {
			fmt.Fprintf(&b, " license=%s", teaText(v))
		}
		if v := teaDeref(ev.SupportId); v != "" {
			fmt.Fprintf(&b, " supportId=%s", teaText(v))
		}
		if v := teaDeref(ev.SupersededByVersion); v != "" {
			fmt.Fprintf(&b, " supersededBy=%s", teaText(v))
		}
		if v := teaDeref(ev.Description); v != "" {
			fmt.Fprintf(&b, " description=%q", teaCut(v))
		}
		if v := teaDeref(ev.Reason); v != "" {
			fmt.Fprintf(&b, " reason=%q", teaCut(v))
		}
		if ev.References != nil && len(*ev.References) > 0 {
			refs := make([]string, 0, len(*ev.References))
			for _, ref := range *ev.References {
				refs = append(refs, teaText(ref))
			}
			fmt.Fprintf(&b, " references=%s", strings.Join(refs, ","))
		}
		fmt.Fprintln(w, b.String())
	}
}
