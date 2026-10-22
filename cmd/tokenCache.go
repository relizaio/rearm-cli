/*
The MIT License (MIT)
Copyright (c) 2019 - 2026 Reliza Incorporated. https://reliza.io
*/

package cmd

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	rearm "github.com/relizaio/rearm-client-go"
)

// In API-key mode every rearm process used to exchange the key for a one-hour access token and throw
// it away at exit, so a caller that runs one process per call (rearm-cd: every 15 s) made one
// exchange per call (task TPF-67). The token now lives in a cache file beside the credentials file,
// ~/.rearm.env.tokens (or <--config>.tokens), mode 0600, under the S401-1 lock (~/.rearm.env.lock),
// and rearm-client-go reads and writes it through rearm.WithTokenStore: it adopts a stored token until
// a minute before its expiry, exchanges under the lock otherwise (so parallel processes of one key
// make one exchange), and saves what it got. One file holds the entries of every (server, key id,
// secret) the user runs, each under hex(HMAC-SHA256(secret, uri + "\n" + key id)): the secret is the
// HMAC key and is never written, a rotated secret is a different entry. Any failure of the cache
// (no home, read-only home, a bad file) only means one exchange per process, as before.
//
// The cache is off when REARM_NO_TOKEN_CACHE is true, and in a detected CI job unless
// REARM_TOKEN_CACHE is true: a persistent runner's home is shared by every job on it, and an entry
// written during a job must not be left for later jobs unless the operator asks.

// tokenCacheVersion is the file format: {"v":1,"entries":{"<hex>":{"token":"<jwt>","expiry":"<RFC3339>"}}}.
const tokenCacheVersion = 1

type tokenCacheEntry struct {
	Token  string    `json:"token"`
	Expiry time.Time `json:"expiry"`
}

type tokenCacheFile struct {
	V       int                        `json:"v"`
	Entries map[string]tokenCacheEntry `json:"entries"`
}

// tokenCache is the cache file as the rearm.TokenStore of one (server, key id, secret).
type tokenCache struct {
	uri, keyID, secret string
}

var _ rearm.TokenStore = tokenCache{}

// cacheFileTrusted says whether a cache file opened for reading may be adopted, and if not why (for
// the debug line). The real check is fileTrustedForCache (tokenCache_unix.go; always trusted
// elsewhere); a variable so a test can reach the owner branch without privileges.
var cacheFileTrusted = fileTrustedForCache

// ciVariables are the environment variables whose truth marks a CI job; JENKINS_URL marks one by
// being set at all.
var ciVariables = []string{"CI", "GITHUB_ACTIONS", "GITLAB_CI", "TF_BUILD"}

// envTrue: the variable is "true" or "1", case-insensitive after trimming; empty is unset.
func envTrue(name string) bool {
	v := strings.TrimSpace(os.Getenv(name))
	return v == "1" || strings.EqualFold(v, "true")
}

// ciJobVariable is the first variable that marks a CI job, or "" outside one.
func ciJobVariable() string {
	for _, name := range ciVariables {
		if envTrue(name) {
			return name
		}
	}
	if strings.TrimSpace(os.Getenv("JENKINS_URL")) != "" {
		return "JENKINS_URL"
	}
	return ""
}

func tokenCacheDebug(format string, args ...interface{}) {
	if debug == "true" {
		fmt.Fprintf(os.Stderr, format+"\n", args...)
	}
}

func tokenCachePath() (string, error) {
	path, err := credentialsPath()
	if err != nil {
		return "", err
	}
	return path + ".tokens", nil
}

// tokenCacheStore is the store a key client gets, or none: the opt-out wins over everything, a CI job
// has none unless it opts in, and without a home directory or a secret there is nothing to key by.
func tokenCacheStore() (rearm.TokenStore, bool) {
	if envTrue("REARM_NO_TOKEN_CACHE") {
		tokenCacheDebug("token cache off: REARM_NO_TOKEN_CACHE")
		return nil, false
	}
	if name := ciJobVariable(); name != "" && !envTrue("REARM_TOKEN_CACHE") {
		tokenCacheDebug("token cache off: CI job detected (%s); set REARM_TOKEN_CACHE=true to keep the token across commands of this job", name)
		return nil, false
	}
	if _, err := tokenCachePath(); err != nil {
		tokenCacheDebug("token cache unavailable: %v", err)
		return nil, false
	}
	if apiKey == "" {
		return nil, false
	}
	return tokenCache{uri: rearmUri, keyID: apiKeyId, secret: apiKey}, true
}

// entryKey is hex(HMAC-SHA256(key = secret, message = uri + "\n" + key id)).
func (c tokenCache) entryKey() string {
	m := hmac.New(sha256.New, []byte(c.secret))
	m.Write([]byte(strings.TrimRight(c.uri, "/") + "\n" + c.keyID))
	return hex.EncodeToString(m.Sum(nil))
}

// Lock is the S401-1 credentials lock, shared with session mode.
func (tokenCache) Lock(ctx context.Context) (func(), error) {
	release, err := credentialsStore{}.Lock(ctx)
	if err != nil {
		tokenCacheDebug("token cache unavailable: %v", err)
	}
	return release, err
}

// readTokenCache reads the file, adopting it only when cacheFileTrusted says so, checked on the open
// descriptor before a byte is read. A missing, untrusted, unreadable or unparsable file, or one of
// another version, is reported as an error and counts as empty.
func readTokenCache(path string) (tokenCacheFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return tokenCacheFile{}, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return tokenCacheFile{}, err
	}
	if ok, why := cacheFileTrusted(fi); !ok {
		return tokenCacheFile{}, untrustedTokenCache{why: why}
	}
	raw, err := io.ReadAll(io.LimitReader(f, 1<<20))
	if err != nil {
		return tokenCacheFile{}, err
	}
	var doc tokenCacheFile
	if err := json.Unmarshal(raw, &doc); err != nil || doc.V != tokenCacheVersion {
		return tokenCacheFile{}, errUnreadableTokenCache
	}
	return doc, nil
}

var errUnreadableTokenCache = errors.New("the token cache file is not a v1 cache")

// untrustedTokenCache is a cache file cacheFileTrusted refused; why says what was wrong with it.
type untrustedTokenCache struct{ why string }

func (e untrustedTokenCache) Error() string { return "the token cache file " + e.why }

// Load returns this key's entry, or rearm.ErrNoToken when there is none it can use.
func (c tokenCache) Load() (rearm.SessionTokens, error) {
	path, err := tokenCachePath()
	if err != nil {
		tokenCacheDebug("token cache unavailable: %v", err)
		return rearm.SessionTokens{}, rearm.ErrNoToken
	}
	doc, err := readTokenCache(path)
	if err != nil {
		var untrusted untrustedTokenCache
		switch {
		case errors.Is(err, fs.ErrNotExist):
		case errors.As(err, &untrusted):
			tokenCacheDebug("token cache ignored: %s %s", path, untrusted.why)
		default:
			tokenCacheDebug("token cache unavailable: %v", err)
		}
		return rearm.SessionTokens{}, rearm.ErrNoToken
	}
	e, ok := doc.Entries[c.entryKey()]
	if !ok || e.Token == "" || !e.Expiry.After(time.Now()) {
		return rearm.SessionTokens{}, rearm.ErrNoToken
	}
	return rearm.SessionTokens{AccessToken: e.Token, AccessTokenExpiry: e.Expiry}, nil
}

// Save sets (or, for an empty token, deletes) this key's entry, drops every expired one and rewrites
// the file. Only ever called under the lock, so the read-modify-write is atomic across processes. A
// file that cannot be read or trusted is replaced by a fresh one holding only this entry.
func (c tokenCache) Save(t rearm.SessionTokens) error {
	path, err := tokenCachePath()
	if err != nil {
		tokenCacheDebug("token cache unavailable: %v", err)
		return err
	}
	doc, err := readTokenCache(path)
	if err != nil {
		doc = tokenCacheFile{}
	}
	doc.V = tokenCacheVersion
	if doc.Entries == nil {
		doc.Entries = map[string]tokenCacheEntry{}
	}
	key := c.entryKey()
	if t.AccessToken == "" {
		delete(doc.Entries, key)
	} else {
		doc.Entries[key] = tokenCacheEntry{Token: t.AccessToken, Expiry: t.AccessTokenExpiry.UTC().Truncate(time.Second)}
	}
	now := time.Now()
	for k, e := range doc.Entries {
		if !e.Expiry.After(now) {
			delete(doc.Entries, k)
		}
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	if err := writeFile0600(path, raw); err != nil {
		tokenCacheDebug("token cache unavailable: %v", err)
		return err
	}
	return nil
}

// writeFile0600 replaces path with data, mode 0600 from the first byte: the bytes go to a temp file
// created exclusively under an unpredictable name in the same directory (so a file or symlink
// planted at a guessable name is never opened), which is then renamed over path; a crash never
// leaves a half-written file, and on any error the temp file is removed.
func writeFile0600(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmp, renamed := f.Name(), false
	defer func() {
		if !renamed {
			_ = os.Remove(tmp)
		}
	}()
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		_ = os.Remove(path) // rename does not replace on Windows
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	renamed = true
	return os.Chmod(path, 0o600)
}
