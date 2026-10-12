package cmd

import (
	"context"
	"testing"
	"time"

	rearm "github.com/relizaio/rearm-client-go"
	"github.com/relizaio/rearm-client-go/sessiontest"
)

// S401-1 round 2: the writes to the credentials file that no round-1 test pinned (test report
// run 1, T-14, T-15 and T-17).

// T-14: with the store in use the client has already saved under the lock; persistSessionTokens runs
// after the lock is given back, so writing the file there could put back a set another process has
// replaced since. It only moves the globals.
func TestWithTheStoreInUsePersistLeavesTheFileAlone(t *testing.T) {
	withSessionFile(t, sessiontest.Options{})
	onFile, err := credentialsStore{}.Load()
	if err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	reported := rearm.SessionTokens{AccessToken: "at-reported", AccessTokenExpiry: later, RefreshToken: "rt-reported",
		SessionExpiry: later.Add(24 * time.Hour)}
	sessionStoreInUse = true
	persistSessionTokens(reported)
	if sessionAccessToken != "at-reported" || sessionRefreshToken != "rt-reported" || !sessionAccessTokenExp.Equal(later) {
		t.Fatal("the globals follow the reported set")
	}
	after, err := credentialsStore{}.Load()
	if err != nil {
		t.Fatal(err)
	}
	if after.RefreshToken != onFile.RefreshToken || after.AccessToken != onFile.AccessToken || !after.AccessTokenExpiry.Equal(onFile.AccessTokenExpiry) {
		t.Fatal("with the store in use the file keeps what the store saved")
	}
	// without the store (a session from the environment) the same report is written, as before
	sessionStoreInUse = false
	persistSessionTokens(reported)
	if written, err := (credentialsStore{}).Load(); err != nil || written.RefreshToken != "rt-reported" || written.AccessToken != "at-reported" {
		t.Fatalf("without the store persistSessionTokens writes the file, got %+v %v", written, err)
	}
}

// T-15: logout revokes the refresh token on file, not the one this process loaded: once another
// process has rotated past the grace, the loaded token revokes nothing, and the user would be told
// Signed out with the session still live.
func TestLogoutRevokesTheTokenOnFileAfterAnotherProcessRotated(t *testing.T) {
	srv := withSessionFile(t, sessiontest.Options{})
	loaded := sessionRefreshToken
	other := storeClient(t, srv) // another process of the same login
	srv.Advance(time.Hour + time.Minute)
	if err := ping(other); err != nil {
		t.Fatal(err)
	}
	// the other client runs in this test's process, and the store's Save moved the globals too; in its
	// own process it would not have: this process still holds the token it loaded
	sessionRefreshToken = loaded
	srv.Advance(3 * time.Minute) // past the grace of the token this process loaded
	onFile, err := credentialsStore{}.Load()
	if err != nil {
		t.Fatal(err)
	}
	if onFile.RefreshToken == loaded || !srv.IsCurrent(onFile.RefreshToken) {
		t.Fatal("the case needs the file rotated away from the token this process loaded")
	}
	if err := logout(); err != nil {
		t.Fatal(err)
	}
	if !srv.Revoked() {
		t.Fatal("logout revokes the session with the refresh token on file")
	}
	if n := srv.Counts(); n.Revocations != 1 || n.Reused != 0 {
		t.Fatalf("one revocation through the revoke endpoint, got %+v", n)
	}
}

// T-17: the session a login delivers is written under the credentials lock, so a refresh write
// already in flight in another process cannot land after it.
func TestLoginWritesTheDeliveredSessionUnderTheLock(t *testing.T) {
	withSessionFile(t, sessiontest.Options{})
	savedUri := sessionUri
	t.Cleanup(func() { sessionUri = savedUri })
	before, err := credentialsStore{}.Load()
	if err != nil {
		t.Fatal(err)
	}
	hold, err := credentialsStore{}.Lock(context.Background()) // another process is mid-refresh
	if err != nil {
		t.Fatal(err)
	}
	exp := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	login := &rearm.DeviceLogin{Status: rearm.DeviceDelivered, RefreshToken: "rt-new-login", APIKeyID: "USER__u__ord__new", Org: "o-new",
		Tokens: rearm.SessionTokens{AccessToken: "at-new-login", AccessTokenExpiry: exp, SessionExpiry: exp.Add(30 * 24 * time.Hour)}}
	done := make(chan error, 1)
	go func() { done <- storeDeliveredLogin(login) }()
	select {
	case err := <-done:
		hold()
		t.Fatalf("the login must wait for the lock, returned %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	if during, err := (credentialsStore{}).Load(); err != nil || during.RefreshToken != before.RefreshToken {
		hold()
		t.Fatalf("nothing is written while another process holds the lock, got %+v %v", during, err)
	}
	hold()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	after, err := credentialsStore{}.Load()
	if err != nil || after.RefreshToken != "rt-new-login" || after.AccessToken != "at-new-login" || !after.AccessTokenExpiry.Equal(exp) {
		t.Fatalf("the delivered session is on file once the lock is free, got %+v %v", after, err)
	}
}
