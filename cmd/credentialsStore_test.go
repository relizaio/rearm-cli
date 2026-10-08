package cmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	rearm "github.com/relizaio/rearm-client-go"
	"github.com/relizaio/rearm-client-go/sessiontest"
	"github.com/spf13/viper"
)

// S401-1: every rearm process of one browser login renews through the credentials file's lock
// (credentialsStore), so a refresh by one never kills another's tokens.

// withSessionFile isolates the test (temp HOME, temp credentials file, globals restored), starts a
// fake server with the real session semantics and writes its login to the file, then loads the
// globals from the file as initConfig would.
func withSessionFile(t *testing.T, o sessiontest.Options) *sessiontest.Server {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("REARM_REFRESHTOKEN", "")
	t.Setenv("REARM_ACCESSTOKEN", "")
	withSessionGlobals(t)
	savedClient, savedFromFile, savedInUse := apiClient, sessionFromFile, sessionStoreInUse
	t.Cleanup(func() { apiClient, sessionFromFile, sessionStoreInUse = savedClient, savedFromFile, savedInUse })
	apiClient, sessionFromFile, sessionStoreInUse = nil, false, false
	srv := sessiontest.NewServer(t, o)
	_, set := srv.Login()
	rearmUri = srv.URL
	if err := (credentialsStore{}).Save(set); err != nil {
		t.Fatal(err)
	}
	reload(t)
	if !sessionFromFile {
		t.Fatal("the session should read as coming from the credentials file")
	}
	return srv
}

func storeClient(t *testing.T, srv *sessiontest.Server) *rearm.Client {
	t.Helper()
	on, err := credentialsStore{}.Load()
	if err != nil {
		t.Fatal(err)
	}
	c, err := rearm.NewWithSession(srv.URL, on.RefreshToken, on, nil, rearm.WithSessionStore(credentialsStore{}))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func ping(c *rearm.Client) error {
	_, err := rearm.Raw(context.Background(), c, "Ping", "query Ping { __typename }", nil)
	return err
}

func noSessionHarm(t *testing.T, srv *sessiontest.Server) {
	t.Helper()
	n := srv.Counts()
	if n.GraceRerotations != 0 || n.Reused != 0 || n.Revocations != 0 || n.InvalidGrants != 0 || srv.Revoked() {
		t.Fatalf("expected no grace re-rotation, no reuse, no revocation, no invalid_grant, got %+v", n)
	}
}

func shortLockWait(t *testing.T, d time.Duration) {
	saved := rearm.LockWait
	rearm.LockWait = d
	t.Cleanup(func() { rearm.LockWait = saved })
}

func TestTheCredentialsStoreLocksLoadsAndSaves(t *testing.T) {
	withSessionGlobals(t)
	t.Setenv("HOME", t.TempDir())
	s := credentialsStore{}
	if _, err := s.Load(); !errors.Is(err, rearm.ErrNoSession) {
		t.Fatalf("an absent file holds no session, got %v", err)
	}
	release, err := s.Lock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(cfgFile + ".lock")
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("the lock file is created 0600 beside the credentials file, got %v %v", st, err)
	}
	exp := time.Date(2026, 10, 8, 19, 0, 0, 0, time.UTC)
	set := rearm.SessionTokens{AccessToken: "at", AccessTokenExpiry: exp, RefreshToken: "rt", SessionExpiry: exp.Add(time.Hour), SessionHardExpiry: exp.Add(2 * time.Hour)}
	if err := s.Save(set); err != nil {
		t.Fatal(err)
	}
	// a second descriptor on the same lock file waits, and gives up with its context
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	start := time.Now()
	if _, err := s.Lock(ctx); err == nil {
		t.Fatal("a second Lock must wait while the first is held")
	}
	cancel()
	if time.Since(start) < 100*time.Millisecond {
		t.Fatal("the second Lock gave up before its context ended")
	}
	release()
	st, _ = os.Stat(cfgFile)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("the credentials file is 0600, got %v", st.Mode())
	}
	raw, _ := os.ReadFile(cfgFile)
	for _, k := range []string{"URI=", "APIKEYID=", "ORG=", "REFRESHTOKEN=rt", "ACCESSTOKEN=at", "ACCESSTOKENEXPIRY=", "SESSIONEXPIRY=", "SESSIONHARDEXPIRY="} {
		if !strings.Contains(string(raw), k) {
			t.Fatalf("%s missing from the file:\n%s", k, raw)
		}
	}
	got, err := s.Load()
	if err != nil || got.RefreshToken != "rt" || got.AccessToken != "at" || !got.AccessTokenExpiry.Equal(exp) ||
		!got.SessionExpiry.Equal(set.SessionExpiry) || !got.SessionHardExpiry.Equal(set.SessionHardExpiry) {
		t.Fatalf("Load round-trips the set, got %+v %v", got, err)
	}
	// released: the lock is free again, and the lock file stays
	release2, err := s.Lock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	release2()
	if _, err := os.Stat(cfgFile + ".lock"); err != nil {
		t.Fatal("the lock file is never deleted")
	}
	if err := writeCredentials(map[string]string{"URI": "https://rearm.example"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load(); !errors.Is(err, rearm.ErrNoSession) {
		t.Fatalf("a file holding URI only holds no session, got %v", err)
	}
	// a lock never given back is an error within LockWait, not a hang
	shortLockWait(t, 100*time.Millisecond)
	hold, _ := s.Lock(context.Background())
	defer hold()
	if err := underCredentialsLock(func() error { return nil }); err == nil || !strings.Contains(err.Error(), "rearm: the credentials store has been locked by another process for 100ms") {
		t.Fatalf("expected the lock timeout, got %v", err)
	}
}

func TestTwoClientsOnOneCredentialsFile(t *testing.T) {
	srv := withSessionFile(t, sessiontest.Options{})
	a, b := storeClient(t, srv), storeClient(t, srv)
	for _, c := range []*rearm.Client{a, b} {
		if err := ping(c); err != nil {
			t.Fatal(err)
		}
	}
	for cycle := 1; cycle <= 3; cycle++ {
		srv.Advance(time.Hour + time.Minute) // every token on file and in memory expires server-side
		before := srv.Counts().Refreshes
		for _, c := range []*rearm.Client{b, a, a, b, b, a} {
			if err := ping(c); err != nil {
				t.Fatalf("cycle %d: %v", cycle, err)
			}
		}
		if got := srv.Counts().Refreshes - before; got != 1 {
			t.Fatalf("cycle %d: one refresh per expiry, got %d", cycle, got)
		}
	}
	noSessionHarm(t, srv)
	on, _ := credentialsStore{}.Load()
	if !srv.IsCurrent(on.RefreshToken) || a.Tokens().RefreshToken != on.RefreshToken || b.Tokens().RefreshToken != on.RefreshToken {
		t.Fatal("the file and both clients end with the server's current refresh token")
	}
	if _, err := os.Stat(cfgFile + ".tmp"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("no .tmp file is left behind")
	}
}

func TestLoginAndLogoutWriteUnderTheLock(t *testing.T) {
	srv := withSessionFile(t, sessiontest.Options{})
	running := storeClient(t, srv) // a wait running in another process
	if err := ping(running); err != nil {
		t.Fatal(err)
	}
	hold, err := credentialsStore{}.Lock(context.Background()) // another process is mid-refresh
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- logout() }()
	select {
	case err := <-done:
		t.Fatalf("logout must wait for the lock, returned %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	hold()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := (credentialsStore{}).Load(); !errors.Is(err, rearm.ErrNoSession) {
		t.Fatalf("logout clears the session from the file, got %v", err)
	}
	if !srv.Revoked() {
		t.Fatal("logout revokes the session with the refresh token on file")
	}
	srv.Advance(2 * time.Hour)
	err = ping(running)
	var se *rearm.SessionError
	if !errors.As(err, &se) || se.Description != "no browser-login session on file; run rearm login" {
		t.Fatalf("the running process's renewal reports the logout, got %v", err)
	}
	if got := describeError(err); got != "session refresh failed (no browser-login session on file; run rearm login): run `rearm login` again" {
		t.Fatalf("unexpected wording %q", got)
	}
}

func TestAnEnvOnlySessionHasNoStore(t *testing.T) {
	withSessionGlobals(t)
	t.Setenv("HOME", t.TempDir())
	savedClient, savedFromFile, savedInUse := apiClient, sessionFromFile, sessionStoreInUse
	t.Cleanup(func() { apiClient, sessionFromFile, sessionStoreInUse = savedClient, savedFromFile, savedInUse })
	srv := sessiontest.NewServer(t, sessiontest.Options{})
	rt, _ := srv.Login()
	t.Setenv("REARM_REFRESHTOKEN", rt)
	apiClient, sessionFromFile, sessionStoreInUse = nil, false, false
	// as initConfig reads it: no file, the refresh token from the environment
	v := viper.New()
	v.SetConfigFile(cfgFile)
	_ = v.ReadInConfig()
	_ = v.BindEnv("refreshtoken", "REARM_REFRESHTOKEN")
	loadSessionConfig(v)
	rearmUri = srv.URL
	if sessionFromFile {
		t.Fatal("a session from the environment does not come from the file")
	}
	if _, err := sendGraphQLRequest("query Ping { __typename }", nil); err != nil {
		t.Fatal(err)
	}
	if sessionStoreInUse {
		t.Fatal("rearmClient builds an environment session without the store")
	}
	if _, err := os.Stat(cfgFile + ".lock"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("no lock is taken without the store")
	}
	// the refresh went through persistSessionTokens, which writes the file as before
	on, err := credentialsStore{}.Load()
	if err != nil || !srv.IsCurrent(on.RefreshToken) || srv.Counts().Refreshes != 1 {
		t.Fatalf("the rotated token is written by persistSessionTokens, got %+v %v %+v", on, err, srv.Counts())
	}
}

func TestTheWordingOfRenewalFailures(t *testing.T) {
	cases := map[string]error{
		"session refresh failed (no browser-login session on file; run rearm login): run `rearm login` again": &rearm.SessionError{Code: "invalid_grant", Description: rearm.NoSessionDescription},
		"rearm: session refreshed but could not be stored: read-only file system":                             errors.New("rearm: session refreshed but could not be stored: read-only file system"),
		"rearm: the credentials store has been locked by another process for 30s":                             errors.New("rearm: the credentials store has been locked by another process for 30s"),
	}
	for want, err := range cases {
		if got := describeError(err); got != want {
			t.Fatalf("describeError: want %q, got %q", want, got)
		}
	}
	// a 401 that survives the retry keeps today's message
	srv := withSessionFile(t, sessiontest.Options{HTML401: true})
	srv.RefuseAll(true)
	_, err := sendGraphQLRequest("query Ping { __typename }", nil)
	if err == nil || !strings.HasPrefix(describeError(err), "rearm: request failed with status 401") {
		t.Fatalf("expected the 401 message, got %v", err)
	}
	if n := srv.Counts(); n.Rejected401 != 2 || n.Refreshes != 1 {
		t.Fatalf("one retry after one renewal, got %+v", n)
	}
}

func TestTheCredentialsFileIsRecognised(t *testing.T) {
	withSessionGlobals(t)
	if !isCredentialsFile(cfgFile) || isCredentialsFile("") || isCredentialsFile(filepath.Join(t.TempDir(), "other.env")) {
		t.Fatal("only the credentials file the CLI writes counts")
	}
}
