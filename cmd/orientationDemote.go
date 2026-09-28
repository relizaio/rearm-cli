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
	"regexp"
	"strings"
)

// The brief nests the orientation under its own part 7 (RD3-10 architecture round 2): the brief's parts
// keep their numbers, and the core and sections it embeds move one heading level down with their section
// numbers dropped, so "## 1. Prerequisites" becomes "### Prerequisites" and no two headings in the brief
// share a number. The document's front matter and its one title line are the brief's heading's to
// replace, so they are left out. Fenced code is never touched: a "# comment" in a shell example stays.

var (
	mdHeading     = regexp.MustCompile(`^(#{1,6}) (.*)$`)
	headingNumber = regexp.MustCompile(`^\d+(\.\d+)*[a-z]?\.?\s+`)
)

// demoteHeadings returns the markdown with every heading one level down and unnumbered.
func demoteHeadings(md string) string {
	lines := strings.Split(md, "\n")
	out := make([]string, 0, len(lines))
	inFence := false
	for i, l := range lines {
		// front matter: a leading --- block
		if i == 0 && l == "---" {
			for j := 1; j < len(lines); j++ {
				if lines[j] == "---" {
					lines = append(lines[:0:0], lines[j+1:]...)
					return demoteHeadings(strings.Join(lines, "\n"))
				}
			}
		}
		if strings.HasPrefix(strings.TrimSpace(l), "```") {
			inFence = !inFence
			out = append(out, l)
			continue
		}
		if !inFence {
			if m := mdHeading.FindStringSubmatch(l); m != nil {
				if len(m[1]) == 1 {
					continue // the document's title: the brief's own heading stands in for it
				}
				level := len(m[1]) + 1
				if level > 6 {
					level = 6
				}
				out = append(out, strings.Repeat("#", level)+" "+headingNumber.ReplaceAllString(m[2], ""))
				continue
			}
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}
