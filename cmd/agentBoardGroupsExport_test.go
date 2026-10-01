package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	rearm "github.com/relizaio/rearm-client-go"
)

// A board's task groups in its export (task RD2-30): `rearm agent board export` writes them before
// the roles, in the board's order, each leading with its key and without empty members.
func TestBoardExportWritesTheGroups(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"exportBoardProgrammatic": map[string]any{
			"kind": "BOARD", "version": 1, "name": "platform",
			"groups": []any{
				map[string]any{"key": "ui-work", "dependsOn": []any{"core-work"}, "status": "OPEN", "name": nil},
				map[string]any{"key": "core-work", "name": "The core", "status": "CLOSED"},
			},
			"roles": []any{map[string]any{"name": "coder", "prompt": "code it"}},
		}}})
	}))
	defer srv.Close()
	c, err := rearm.New(srv.URL, "id", "secret", rearm.WithoutTokenExchange())
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "platform.yaml")
	apiClient, specBoard, specOut = c, "platform", out
	defer func() { apiClient, specBoard, specOut = nil, "", "" }()
	agentBoardExportCmd.Run(agentBoardExportCmd, nil)
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	want := "groups:\n    - key: ui-work\n      dependsOn:\n        - core-work\n      status: OPEN\n" +
		"    - key: core-work\n      name: The core\n      status: CLOSED\nroles:\n"
	if !strings.Contains(s, want) {
		t.Errorf("the export does not write the groups as a file declares them:\n%s", s)
	}
}
