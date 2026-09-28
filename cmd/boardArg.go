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
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// A board named by its uuid or by its name (task RD3-3). The server takes a uuid on every board verb --
// agents stay uuid-only there -- so the CLI resolves a name through the boards list it already has: a
// name is unique per organization (declarative-boards D11). One extra call per name; nothing is cached.

// Only what the lookup needs, not the whole board the list operations select.
const (
	boardNamesProgrammatic = `query AgentBoardNamesProgrammatic { agentBoardsProgrammatic { uuid name } }`
	boardNamesOfOrg        = `query AgentBoardNamesOfOrg($orgUuid: ID!) { agentBoardsOfOrg(orgUuid: $orgUuid) { uuid name } }`
)

type namedBoard struct {
	uuid string
	name string
}

// boardLister gives the boards a name is looked up in.
type boardLister func() ([]namedBoard, error)

// resolveBoardArgWith is the board a verb's argument names: a uuid passes through without a call; a name
// is looked up by exact match in list.
func resolveBoardArgWith(arg string, list boardLister) (string, error) {
	a := strings.TrimSpace(arg)
	if a == "" {
		return "", fmt.Errorf("a board is required: its uuid or its name")
	}
	if _, err := uuid.Parse(a); err == nil {
		return a, nil
	}
	boards, err := list()
	if err != nil {
		return "", fmt.Errorf("could not read the boards to find %q: %w", a, err)
	}
	var found []string
	for _, b := range boards {
		if b.name == a {
			found = append(found, b.uuid)
		}
	}
	switch len(found) {
	case 0:
		return "", fmt.Errorf("no board named %q in this organization; run rearm agent board list", a)
	case 1:
		return found[0], nil
	default:
		return "", fmt.Errorf("%d boards named %q; use the uuid", len(found), a)
	}
}

func namedBoardsOf(data map[string]interface{}, key string) []namedBoard {
	rows, _ := data[key].([]interface{})
	out := make([]namedBoard, 0, len(rows))
	for _, r := range rows {
		m, ok := r.(map[string]interface{})
		if !ok {
			continue
		}
		id, _ := m["uuid"].(string)
		name, _ := m["name"].(string)
		out = append(out, namedBoard{uuid: id, name: name})
	}
	return out
}

// agentBoards lists the boards the calling key reads, as the agent verbs see them.
func agentBoards() ([]namedBoard, error) {
	data, err := sendGraphQLRequest(boardNamesProgrammatic, map[string]interface{}{})
	if err != nil {
		return nil, err
	}
	return namedBoardsOf(data, "agentBoardsProgrammatic"), nil
}

// personBoards lists the organization's boards, as the `rearm boards` verbs see them.
func personBoards() ([]namedBoard, error) {
	org, err := boardsOrg()
	if err != nil {
		return nil, err
	}
	data, err := sendGraphQLRequest(boardNamesOfOrg, map[string]interface{}{"orgUuid": org})
	if err != nil {
		return nil, err
	}
	return namedBoardsOf(data, "agentBoardsOfOrg"), nil
}

// boardArg is an agent verb's board, its uuid or its name; it exits with the reason when neither resolves.
func boardArg(arg string) string {
	id, err := resolveBoardArgWith(arg, agentBoards)
	if err != nil {
		fail(err.Error())
	}
	return id
}

// personBoardArg is the same for the `rearm boards` verbs, which read the organization's boards.
func personBoardArg(arg string) string {
	id, err := resolveBoardArgWith(arg, personBoards)
	if err != nil {
		fail(err.Error())
	}
	return id
}
