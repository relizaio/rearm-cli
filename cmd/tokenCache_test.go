package cmd

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	rearm "github.com/relizaio/rearm-client-go"
)

// TPF-67: in API-key mode the access token outlives the process in <credentials file>.tokens, so
// consecutive rearm processes of one key make one exchange, not one per process (K1 to K13 of the
// architecture). The rig is a fake server counting token POSTs and issuing JWT-shaped tokens; each
// "process" is apiClient = nil and a new request, as a new rearm invocation builds its client anew.

const (
	tcKeyID  = "FREEFORM__4324b741-0000-4000-8000-000000000001__ord__1"
	tcSecret = "tc-secret-5b0e7c1d9a2f4e3b8c6d0a1f2e3d4c5b"
)

// tokenCacheServer issues a JWT-shaped token per POST on rearm.TokenPath and answers
// rearm.ProgrammaticPath with {"data":{}} for a bearer it issued and has not revoked, 401
// invalid_token for any other.
type tokenCacheServer struct {
	*httptest.Server
	mu      sync.Mutex
	posts   int
	graphql int
	other   int
	issued  map[string]bool
	revoked map[string]bool
	bearers []string
	// nonce makes this server's tokens its own (two servers never issue the same bytes)
	nonce string
	// changed, when set, is called with the cumulative counts after every request (under the lock)
	changed func(token, graphql, other int)
}

func newTokenCacheServer() *tokenCacheServer {
	nonce := make([]byte, 8)
	_, _ = rand.Read(nonce)
	s := &tokenCacheServer{issued: map[string]bool{}, revoked: map[string]bool{}, nonce: hex.EncodeToString(nonce)}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	return s
}

func b64(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }

func (s *tokenCacheServer) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer func() {
		if s.changed != nil {
			s.changed(s.posts, s.graphql, s.other)
		}
	}()
	switch r.URL.Path {
	case rearm.TokenPath:
		s.posts++
		id, _, ok := r.BasicAuth()
		if !ok || r.Method != http.MethodPost {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_client","error_description":"no key"}`))
			return
		}
		exp := time.Now().Add(time.Hour).Unix()
		tok := b64(`{"alg":"HS256","typ":"JWT"}`) + "." + b64(fmt.Sprintf(`{"iss":%q,"sub":%q,"exp":%d,"jti":"jti-%d"}`, s.nonce, id, exp, s.posts)) + "." + b64(fmt.Sprintf("signature-%s-%d", s.nonce, s.posts))
		s.issued[tok] = true
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = fmt.Fprintf(w, `{"access_token":%q,"token_type":"Bearer","expires_in":3600}`, tok)
	case rearm.ProgrammaticPath:
		s.graphql++
		bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		s.bearers = append(s.bearers, bearer)
		if !s.issued[bearer] || s.revoked[bearer] {
			w.Header().Set("WWW-Authenticate", `Bearer realm="rearm", error="invalid_token", error_description="The access token is invalid"`)
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"invalid_token"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{}}`))
	default:
		s.other++
		w.WriteHeader(http.StatusNotFound)
	}
}

func (s *tokenCacheServer) tokenPosts() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.posts
}

func (s *tokenCacheServer) seen() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.bearers...)
}

func (s *tokenCacheServer) revoke(tok string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.revoked[tok] = true
}

// tokenCacheEnv is every variable that turns the cache off or on: a developer's own CI=true (or a
// test run inside a CI job) must not change what the K cases see.
var tokenCacheEnv = []string{"CI", "GITHUB_ACTIONS", "GITLAB_CI", "TF_BUILD", "JENKINS_URL", "REARM_NO_TOKEN_CACHE", "REARM_TOKEN_CACHE"}

func clearTokenCacheEnv(t *testing.T) {
	t.Helper()
	for _, name := range tokenCacheEnv {
		t.Setenv(name, "")
	}
}

// withTokenCache isolates a K case: the seven cache variables cleared, a temp HOME and credentials
// file (homedir caches, so cfgFile is set as withSessionFile does), the globals restored, a fresh
// counting server and the key mode pointed at it.
func withTokenCache(t *testing.T) *tokenCacheServer {
	t.Helper()
	clearTokenCacheEnv(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("REARM_REFRESHTOKEN", "")
	t.Setenv("REARM_ACCESSTOKEN", "")
	withSessionGlobals(t)
	savedClient, savedFromFile, savedInUse, savedDebug, savedTrust := apiClient, sessionFromFile, sessionStoreInUse, debug, cacheFileTrusted
	t.Cleanup(func() {
		apiClient, sessionFromFile, sessionStoreInUse, debug, cacheFileTrusted = savedClient, savedFromFile, savedInUse, savedDebug, savedTrust
	})
	apiClient, sessionFromFile, sessionStoreInUse, debug = nil, false, false, "false"
	srv := newTokenCacheServer()
	t.Cleanup(srv.Close)
	rearmUri, apiKeyId, apiKey = srv.URL, tcKeyID, tcSecret
	return srv
}

// process is one rearm invocation: a client built anew, one request.
func process(t *testing.T) {
	t.Helper()
	apiClient = nil
	if _, err := sendGraphQLRequest("query Ping { __typename }", nil); err != nil {
		t.Fatalf("request failed: %v", err)
	}
}

// processWithDebug is process with debug = "true", returning stdout and stderr.
func processWithDebug(t *testing.T) (string, string) {
	t.Helper()
	debug = "true"
	defer func() { debug = "false" }()
	return both(t, func() { process(t) })
}

func cachePath() string { return cfgFile + ".tokens" }

func cacheExists() bool {
	_, err := os.Lstat(cachePath())
	return err == nil
}

func readCacheFile(t *testing.T) tokenCacheFile {
	t.Helper()
	raw, err := os.ReadFile(cachePath())
	if err != nil {
		t.Fatal(err)
	}
	var doc tokenCacheFile
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("the cache file is not JSON: %v", err)
	}
	if doc.V != 1 {
		t.Fatalf("the cache file is not v1: %s", raw)
	}
	return doc
}

func writeCacheFile(t *testing.T, doc tokenCacheFile) {
	t.Helper()
	raw, _ := json.Marshal(doc)
	if err := writeFile0600(cachePath(), raw); err != nil {
		t.Fatal(err)
	}
}

// expectedKey is recomputed here, independently of entryKey: hex(HMAC-SHA256(secret, uri + "\n" + key id)).
func expectedKey(uri, keyID, secret string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(strings.TrimRight(uri, "/") + "\n" + keyID))
	return hex.EncodeToString(m.Sum(nil))
}

func onlyEntry(t *testing.T) (string, tokenCacheEntry) {
	t.Helper()
	doc := readCacheFile(t)
	if len(doc.Entries) != 1 {
		t.Fatalf("expected one entry, got %d", len(doc.Entries))
	}
	for k, e := range doc.Entries {
		return k, e
	}
	return "", tokenCacheEntry{}
}

func modeOf(t *testing.T, path string) os.FileMode {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Mode().Perm()
}

func skipUnlessUnixUser(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("unix modes only")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores the modes this case relies on")
	}
}

func noSecretOrToken(t *testing.T, what, text, token string) {
	t.Helper()
	if strings.Contains(text, tcSecret) {
		t.Fatalf("%s carries the secret", what)
	}
	if token != "" && strings.Contains(text, token) {
		t.Fatalf("%s carries the token", what)
	}
}

func TestTokenCacheStoresAFreshExchange(t *testing.T) { // K1, K10
	srv := withTokenCache(t)
	process(t)
	if srv.tokenPosts() != 1 {
		t.Fatalf("one POST, got %d", srv.tokenPosts())
	}
	if sessionStoreInUse {
		t.Fatal("key mode must not use the session store")
	}
	if runtime.GOOS != "windows" {
		if m := modeOf(t, cachePath()); m != 0o600 {
			t.Fatalf("the cache file is mode 0600, got %#o", m)
		}
	}
	key, e := onlyEntry(t)
	if key != expectedKey(srv.URL, tcKeyID, tcSecret) {
		t.Fatalf("the entry key is the HMAC of the uri and key id under the secret, got %s", key)
	}
	seen := srv.seen()
	if len(seen) != 1 || e.Token != seen[0] {
		t.Fatalf("the stored token is byte for byte the issued bearer")
	}
	if e.Expiry.Before(time.Now().Add(59*time.Minute)) || e.Expiry.After(time.Now().Add(61*time.Minute)) {
		t.Fatalf("the stored expiry is the server's (an hour), got %s", e.Expiry)
	}
	raw, _ := os.ReadFile(cachePath())
	if !strings.Contains(string(raw), `"expiry":"`+e.Expiry.UTC().Format(time.RFC3339)+`"`) {
		t.Fatalf("the expiry is RFC3339 UTC: %s", raw)
	}
	digest := sha256.Sum256([]byte(tcSecret))
	for _, s := range []string{tcSecret, hex.EncodeToString(digest[:]), base64.RawURLEncoding.EncodeToString(digest[:]),
		base64.URLEncoding.EncodeToString(digest[:]), base64.StdEncoding.EncodeToString(digest[:])} {
		if strings.Contains(string(raw), s) {
			t.Fatalf("the cache file holds the secret or a digest of it (%s)", s)
		}
	}
	parts := strings.Split(e.Token, ".")
	if len(parts) != 3 {
		t.Fatalf("the stored token is the JWT-shaped bearer, got %q", e.Token)
	}
	claims, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || strings.Contains(string(claims), tcSecret) {
		t.Fatalf("the decoded claims hold no secret (%v): %s", err, claims)
	}

	t.Run("under debug", func(t *testing.T) {
		srv := withTokenCache(t)
		out, errOut := processWithDebug(t)
		_, e := onlyEntry(t)
		if srv.tokenPosts() != 1 || e.Token == "" {
			t.Fatalf("one POST and a stored token, got %d", srv.tokenPosts())
		}
		noSecretOrToken(t, "stdout", out, e.Token)
		noSecretOrToken(t, "stderr", errOut, e.Token)
	})
}

func TestTokenCacheIsReusedByTheNextProcess(t *testing.T) { // K2
	srv := withTokenCache(t)
	process(t)
	_, first := onlyEntry(t)
	process(t)
	if srv.tokenPosts() != 1 {
		t.Fatalf("the second process makes no POST, got %d in total", srv.tokenPosts())
	}
	seen := srv.seen()
	if len(seen) != 2 || seen[1] != first.Token {
		t.Fatalf("the second process sends the stored bearer")
	}
	out, errOut := processWithDebug(t)
	if srv.tokenPosts() != 1 {
		t.Fatalf("the cache-hit path makes no POST, got %d in total", srv.tokenPosts())
	}
	noSecretOrToken(t, "stdout", out, first.Token)
	noSecretOrToken(t, "stderr", errOut, first.Token)
	if strings.Contains(errOut, "token cache") {
		t.Fatalf("the cache-hit path prints nothing about the token: %q", errOut)
	}
}

func TestTokenCacheExpiryTriggersANewExchange(t *testing.T) { // K3
	srv := withTokenCache(t)
	process(t)
	key, e := onlyEntry(t)
	e.Expiry = time.Now().Add(30 * time.Second).UTC().Truncate(time.Second)
	writeCacheFile(t, tokenCacheFile{V: 1, Entries: map[string]tokenCacheEntry{key: e}})
	process(t)
	if srv.tokenPosts() != 2 {
		t.Fatalf("a token inside its last minute is exchanged again, got %d POSTs", srv.tokenPosts())
	}
	_, now := onlyEntry(t)
	if now.Token == e.Token || !now.Expiry.After(time.Now().Add(59*time.Minute)) {
		t.Fatalf("the file holds the new token and expiry, got %+v", now)
	}
}

func TestTokenCacheMissesForAnotherKeySecretOrServer(t *testing.T) { // K4
	t.Run("another key id, another secret, then pruned", func(t *testing.T) {
		srv := withTokenCache(t)
		process(t)
		firstKey := expectedKey(srv.URL, tcKeyID, tcSecret)
		apiKeyId = tcKeyID + "0"
		process(t)
		if srv.tokenPosts() != 2 || len(readCacheFile(t).Entries) != 2 {
			t.Fatalf("another key id (same secret) misses: one more POST and a second entry, got %d", srv.tokenPosts())
		}
		apiKeyId, apiKey = tcKeyID, tcSecret+"-rotated"
		process(t)
		doc := readCacheFile(t)
		if srv.tokenPosts() != 3 || len(doc.Entries) != 3 {
			t.Fatalf("a changed secret (same key id) misses: one more POST and a third entry, got %d", srv.tokenPosts())
		}
		if _, ok := doc.Entries[firstKey]; !ok {
			t.Fatal("the first entry is left in place")
		}
		e := doc.Entries[firstKey]
		e.Expiry = time.Now().Add(-time.Minute).UTC().Truncate(time.Second)
		doc.Entries[firstKey] = e
		writeCacheFile(t, doc)
		apiKeyId = tcKeyID + "1"
		process(t) // any write
		if _, ok := readCacheFile(t).Entries[firstKey]; ok {
			t.Fatal("an expired entry is pruned at the next write")
		}
	})
	t.Run("another server", func(t *testing.T) {
		first := withTokenCache(t)
		process(t)
		_, e := onlyEntry(t)
		second := newTokenCacheServer()
		t.Cleanup(second.Close)
		rearmUri = second.URL
		process(t)
		if first.tokenPosts() != 1 || second.tokenPosts() != 1 {
			t.Fatalf("one POST at each server, got %d and %d", first.tokenPosts(), second.tokenPosts())
		}
		for _, b := range second.seen() {
			if b == e.Token {
				t.Fatal("the second server was shown the first server's token")
			}
		}
		doc := readCacheFile(t)
		_, a := doc.Entries[expectedKey(first.URL, tcKeyID, tcSecret)]
		_, b := doc.Entries[expectedKey(second.URL, tcKeyID, tcSecret)]
		if len(doc.Entries) != 2 || !a || !b {
			t.Fatalf("two entries under two HMAC keys, got %d", len(doc.Entries))
		}
	})
}

func TestTokenCacheRejectedTokenIsReplaced(t *testing.T) { // K5
	srv := withTokenCache(t)
	process(t)
	_, old := onlyEntry(t)
	srv.revoke(old.Token) // the secret was rotated or the key deactivated and reactivated
	process(t)
	if srv.tokenPosts() != 2 {
		t.Fatalf("one re-exchange, got %d POSTs in total", srv.tokenPosts())
	}
	_, now := onlyEntry(t)
	seen := srv.seen()
	if now.Token == old.Token || len(seen) != 3 || seen[1] != old.Token || seen[2] != now.Token {
		t.Fatalf("the refused token, then its replacement, which the file now holds")
	}
}

func TestTokenCacheOptOut(t *testing.T) { // K6
	for _, v := range []string{"true", "1", "TRUE"} {
		t.Run(v, func(t *testing.T) {
			srv := withTokenCache(t)
			t.Setenv("REARM_NO_TOKEN_CACHE", v)
			process(t)
			_, errOut := processWithDebug(t)
			if srv.tokenPosts() != 2 || cacheExists() {
				t.Fatalf("two POSTs and no file, got %d", srv.tokenPosts())
			}
			if !strings.Contains(errOut, "token cache off: REARM_NO_TOKEN_CACHE") {
				t.Fatalf("the debug line names the opt-out, got %q", errOut)
			}
		})
	}
}

func TestTokenCacheUnwritableDegradesSilently(t *testing.T) { // K7
	skipUnlessUnixUser(t)
	srv := withTokenCache(t)
	ro := filepath.Join(t.TempDir(), "ro")
	if err := os.Mkdir(ro, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(ro, 0o700) })
	cfgFile = filepath.Join(ro, ".rearm.env")
	process(t)
	out, errOut := processWithDebug(t)
	if srv.tokenPosts() != 2 || cacheExists() {
		t.Fatalf("one POST per process and no file, got %d", srv.tokenPosts())
	}
	if n := strings.Count(errOut, "token cache"); n != 1 {
		t.Fatalf("one stderr line about the cache, got %d in %q", n, errOut)
	}
	for _, tok := range srv.seen() {
		noSecretOrToken(t, "stdout", out, tok)
		noSecretOrToken(t, "stderr", errOut, tok)
	}
}

// TestTokenCacheChildProcess is K8's child: a rearm process in key mode on the shared cache file,
// making three requests when the parent says go.
func TestTokenCacheChildProcess(t *testing.T) {
	if os.Getenv("REARM_TEST_TOKENCACHE_CHILD") != "1" {
		t.Skip("child of TestProcessesSharingOneTokenCache")
	}
	dir, id := os.Getenv("REARM_TEST_TOKENCACHE_DIR"), os.Getenv("REARM_TEST_TOKENCACHE_ID")
	cfgFile, rearmUri, apiClient = os.Getenv("REARM_TEST_TOKENCACHE_CFG"), os.Getenv("REARM_TEST_TOKENCACHE_URI"), nil
	apiKeyId, apiKey = os.Getenv("REARM_TEST_TOKENCACHE_KEYID"), os.Getenv("REARM_TEST_TOKENCACHE_SECRET")
	if !waitForFile(filepath.Join(dir, "go"), 60*time.Second) {
		t.Fatal("no go")
	}
	result := ""
	for i := 0; i < 3 && result == ""; i++ {
		if _, err := sendGraphQLRequest("query Ping { __typename }", nil); err != nil {
			result = err.Error()
		}
	}
	_ = os.WriteFile(filepath.Join(dir, "done-"+id), []byte(result), 0o600)
}

func TestProcessesSharingOneTokenCache(t *testing.T) { // K8
	if os.Getenv("REARM_TEST_TOKENCACHE_CHILD") == "1" {
		t.Skip("running as a child")
	}
	srv := withTokenCache(t)
	dir := t.TempDir()
	const children = 4
	var procs []*exec.Cmd
	for i := 0; i < children; i++ {
		cmd := exec.Command(os.Args[0], "-test.run=^TestTokenCacheChildProcess$", "-test.count=1")
		cmd.Env = append(os.Environ(), "REARM_TEST_TOKENCACHE_CHILD=1", "REARM_TEST_TOKENCACHE_DIR="+dir, "REARM_TEST_TOKENCACHE_ID="+strconv.Itoa(i),
			"REARM_TEST_TOKENCACHE_CFG="+cfgFile, "REARM_TEST_TOKENCACHE_URI="+srv.URL, "REARM_TEST_TOKENCACHE_KEYID="+tcKeyID, "REARM_TEST_TOKENCACHE_SECRET="+tcSecret)
		out, err := os.Create(filepath.Join(dir, "child-"+strconv.Itoa(i)+".log"))
		if err != nil {
			t.Fatal(err)
		}
		defer out.Close()
		cmd.Stdout, cmd.Stderr = out, out
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		procs = append(procs, cmd)
	}
	defer func() {
		for _, p := range procs {
			_ = p.Process.Kill()
		}
	}()
	if err := os.WriteFile(filepath.Join(dir, "go"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for i, p := range procs {
		err := p.Wait()
		log, _ := os.ReadFile(filepath.Join(dir, "child-"+strconv.Itoa(i)+".log"))
		if err != nil {
			t.Fatalf("child %d: %v\n%s", i, err, log)
		}
		if strings.Contains(strings.ToLower(string(log)), "warn") {
			t.Fatalf("child %d printed a warning:\n%s", i, log)
		}
		if res, err := os.ReadFile(filepath.Join(dir, "done-"+strconv.Itoa(i))); err != nil || len(res) > 0 {
			t.Fatalf("child %d did not succeed (%v): %s\n%s", i, err, res, log)
		}
	}
	procs = nil
	if srv.tokenPosts() != 1 {
		t.Fatalf("one POST for %d processes of one key, got %d", children, srv.tokenPosts())
	}
}

func TestLogoutRemovesTheTokenCache(t *testing.T) { // K9
	srv := withTokenCache(t)
	if err := writeCredentials(map[string]string{"URI": srv.URL, "APIKEYID": tcKeyID, "APIKEY": tcSecret}); err != nil {
		t.Fatal(err)
	}
	process(t)
	if !cacheExists() {
		t.Fatal("the cache file is written")
	}
	if err := logout(); err != nil {
		t.Fatal(err)
	}
	if cacheExists() {
		t.Fatal("logout removes the cache file")
	}
	raw, _ := os.ReadFile(cfgFile)
	if strings.Contains(string(raw), tcSecret) {
		t.Fatal("logout clears the key from the credentials file")
	}
	if err := logout(); err != nil {
		t.Fatalf("a logout with no cache file is not an error, got %v", err)
	}
}

func TestTokenCacheOffInACIJob(t *testing.T) { // K11
	off := func(t *testing.T, srv *tokenCacheServer) {
		t.Helper()
		process(t)
		process(t)
		if srv.tokenPosts() != 2 || cacheExists() {
			t.Fatalf("two POSTs and no file, got %d", srv.tokenPosts())
		}
	}
	on := func(t *testing.T, srv *tokenCacheServer) {
		t.Helper()
		process(t)
		process(t)
		if srv.tokenPosts() != 1 || !cacheExists() {
			t.Fatalf("one POST and the file, got %d", srv.tokenPosts())
		}
		if runtime.GOOS != "windows" {
			if m := modeOf(t, cachePath()); m != 0o600 {
				t.Fatalf("the cache file is mode 0600, got %#o", m)
			}
		}
	}
	t.Run("CI=true", func(t *testing.T) {
		srv := withTokenCache(t)
		t.Setenv("CI", "true")
		off(t, srv)
		_, errOut := processWithDebug(t)
		if !strings.Contains(errOut, "token cache off: CI job detected (CI)") || !strings.Contains(errOut, "REARM_TOKEN_CACHE") {
			t.Fatalf("the debug line names CI and REARM_TOKEN_CACHE, got %q", errOut)
		}
	})
	for _, v := range [][2]string{{"GITHUB_ACTIONS", "true"}, {"GITLAB_CI", "true"}, {"TF_BUILD", "True"}, {"JENKINS_URL", "http://jenkins.local/"}} {
		t.Run(v[0], func(t *testing.T) {
			srv := withTokenCache(t)
			t.Setenv(v[0], v[1])
			off(t, srv)
		})
	}
	t.Run("opt-in", func(t *testing.T) {
		srv := withTokenCache(t)
		t.Setenv("CI", "true")
		t.Setenv("REARM_TOKEN_CACHE", "true")
		on(t, srv)
	})
	t.Run("the opt-out wins over the opt-in", func(t *testing.T) {
		srv := withTokenCache(t)
		t.Setenv("CI", "true")
		t.Setenv("REARM_TOKEN_CACHE", "true")
		t.Setenv("REARM_NO_TOKEN_CACHE", "true")
		off(t, srv)
	})
	t.Run("CI=false", func(t *testing.T) {
		srv := withTokenCache(t)
		t.Setenv("CI", "false")
		on(t, srv)
	})
}

func TestTokenCacheTempFileIsExclusive(t *testing.T) { // K12
	t.Run("planted cache temp", func(t *testing.T) {
		srv := withTokenCache(t)
		planted := cachePath() + ".tmp"
		if err := os.WriteFile(planted, []byte("sentinel"), 0o644); err != nil {
			t.Fatal(err)
		}
		process(t)
		if srv.tokenPosts() != 1 {
			t.Fatalf("one POST, got %d", srv.tokenPosts())
		}
		if raw, _ := os.ReadFile(planted); string(raw) != "sentinel" {
			t.Fatalf("the planted temp is untouched, got %q", raw)
		}
		_, e := onlyEntry(t)
		if e.Token == "" {
			t.Fatal("the cache file holds the token")
		}
		if runtime.GOOS != "windows" {
			if m := modeOf(t, planted); m != 0o644 {
				t.Fatalf("the planted temp keeps its mode, got %#o", m)
			}
			if m := modeOf(t, cachePath()); m != 0o600 {
				t.Fatalf("the cache file is 0600, got %#o", m)
			}
		}
		if left, _ := filepath.Glob(cachePath() + ".*.tmp"); len(left) != 0 {
			t.Fatalf("no temp file of the helper is left, got %v", left)
		}
	})
	t.Run("planted credentials temp", func(t *testing.T) {
		withTokenCache(t)
		planted := cfgFile + ".tmp"
		if err := os.WriteFile(planted, []byte("sentinel"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := writeCredentials(map[string]string{"URI": rearmUri, "APIKEYID": tcKeyID, "APIKEY": tcSecret}); err != nil {
			t.Fatal(err)
		}
		if raw, _ := os.ReadFile(planted); string(raw) != "sentinel" {
			t.Fatalf("the planted temp never receives the credentials, got %q", raw)
		}
		if runtime.GOOS != "windows" {
			if m := modeOf(t, cfgFile); m != 0o600 {
				t.Fatalf("the credentials file is 0600, got %#o", m)
			}
		}
		if left, _ := filepath.Glob(cfgFile + ".*.tmp"); len(left) != 0 {
			t.Fatalf("no temp file of the helper is left, got %v", left)
		}
	})
	t.Run("failed rename", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("the pre-rename remove of a directory differs on Windows")
		}
		srv := withTokenCache(t)
		if err := os.Mkdir(cachePath(), 0o700); err != nil {
			t.Fatal(err)
		}
		process(t)
		if srv.tokenPosts() != 1 {
			t.Fatalf("no error surfaces and one POST, got %d", srv.tokenPosts())
		}
		if left, _ := filepath.Glob(cachePath() + ".*.tmp"); len(left) != 0 {
			t.Fatalf("a failed rename leaves no temp file, got %v", left)
		}
	})
}

func TestTokenCacheUntrustedFileIsNotAdopted(t *testing.T) { // K13
	skipUnlessUnixUser(t)
	srv := withTokenCache(t)
	process(t)
	_, first := onlyEntry(t)
	if err := os.Chmod(cachePath(), 0o644); err != nil {
		t.Fatal(err)
	}
	_, errOut := processWithDebug(t)
	if srv.tokenPosts() != 2 {
		t.Fatalf("a 0644 cache file is not adopted: one more POST, got %d in total", srv.tokenPosts())
	}
	if m := modeOf(t, cachePath()); m != 0o600 {
		t.Fatalf("the file is 0600 after the command, got %#o", m)
	}
	_, second := onlyEntry(t)
	if second.Token == first.Token {
		t.Fatal("the file holds the new token")
	}
	if !strings.Contains(errOut, "token cache ignored: "+cachePath()+" has mode 0644") {
		t.Fatalf("the debug line names the path and the mode, got %q", errOut)
	}
	noSecretOrToken(t, "stderr", errOut, first.Token)
	noSecretOrToken(t, "stderr", errOut, second.Token)

	cacheFileTrusted = func(os.FileInfo) (bool, string) { return false, "is not owned by this user" }
	process(t)
	cacheFileTrusted = fileTrustedForCache
	if srv.tokenPosts() != 3 {
		t.Fatalf("a file not owned by the caller is not adopted: one more POST, got %d in total", srv.tokenPosts())
	}
	if _, third := onlyEntry(t); third.Token == second.Token {
		t.Fatal("the file is rewritten with the new token")
	}
}
