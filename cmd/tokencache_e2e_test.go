package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

// TestTokenCacheEndToEndServer is the counting fake server for the tester's end-to-end check of
// TPF-67 (architecture 4.3): two consecutive rearm invocations with one key make one token POST.
// It is the K rig's server (tokenCache_test.go): the token endpoint issues JWT-shaped tokens and
// counts its POSTs; /api/programmatic/graphql answers {"data":{}} to a bearer it issued (with that
// answer `rearm getversion -b main --component <uuid>` prints null and exits 0) and 401
// invalid_token to anything else.
//
// It serves until <out>/stop exists, or for REARM_TEST_TOKENCACHE_DEADLINE seconds (default 300),
// and passes either way: the evidence is the files, not the test's output (go test buffers that).
//   - REARM_TEST_TOKENCACHE_SERVE=1 runs it (skipped otherwise);
//   - REARM_TEST_TOKENCACHE_OUT is an existing empty directory;
//   - <out>/url is the server URL, <out>/counts one JSON line {"token":n,"graphql":m,"other":k} with
//     the cumulative counts since start, rewritten after every request; both written then renamed.
func TestTokenCacheEndToEndServer(t *testing.T) {
	if os.Getenv("REARM_TEST_TOKENCACHE_SERVE") != "1" {
		t.Skip("the tester's end-to-end server: set REARM_TEST_TOKENCACHE_SERVE=1")
	}
	out := os.Getenv("REARM_TEST_TOKENCACHE_OUT")
	if fi, err := os.Stat(out); out == "" || err != nil || !fi.IsDir() {
		t.Fatalf("REARM_TEST_TOKENCACHE_OUT must name an existing directory, got %q", out)
	}
	deadline := 300 * time.Second
	if v := os.Getenv("REARM_TEST_TOKENCACHE_DEADLINE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			deadline = time.Duration(n) * time.Second
		}
	}
	publish := func(name, content string) {
		tmp := filepath.Join(out, "."+name+".tmp")
		if err := os.WriteFile(tmp, []byte(content), 0o644); err == nil {
			_ = os.Rename(tmp, filepath.Join(out, name))
		}
	}
	srv := newTokenCacheServer()
	defer srv.Close()
	var mu sync.Mutex
	srv.mu.Lock()
	srv.changed = func(token, graphql, other int) {
		mu.Lock()
		defer mu.Unlock()
		publish("counts", fmt.Sprintf(`{"token":%d,"graphql":%d,"other":%d}`+"\n", token, graphql, other))
	}
	srv.changed(0, 0, 0)
	srv.mu.Unlock()
	publish("url", srv.URL)
	t.Logf("serving at %s; touch %s to stop", srv.URL, filepath.Join(out, "stop"))
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		if _, err := os.Stat(filepath.Join(out, "stop")); err == nil {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Logf("deadline of %s reached without a stop file", deadline)
}
