package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	rearm "github.com/relizaio/rearm-client-go"
	"github.com/spf13/viper"
)

// A key's sessionMaxMinutes gives a device-login session a hard end (task RD3-7): login and whoami
// print it, the credentials file keeps it across runs, a session delivered without a refresh token
// is still a session, and the ended session says to log in again once.

// withSessionGlobals restores every session global the test touches.
func withSessionGlobals(t *testing.T) {
	t.Helper()
	saved := []interface{}{cfgFile, authMode, apiKey, apiKeyId, rearmUri, sessionRefreshToken, sessionAccessToken,
		sessionAccessTokenExp, sessionExpiresAt, sessionHardExpiry, sessionKeyId, sessionOrg}
	t.Cleanup(func() {
		cfgFile, authMode, apiKey, apiKeyId, rearmUri = saved[0].(string), saved[1].(string), saved[2].(string), saved[3].(string), saved[4].(string)
		sessionRefreshToken, sessionAccessToken = saved[5].(string), saved[6].(string)
		sessionAccessTokenExp, sessionExpiresAt, sessionHardExpiry = saved[7].(time.Time), saved[8].(time.Time), saved[9].(time.Time)
		sessionKeyId, sessionOrg = saved[10].(string), saved[11].(string)
	})
	cfgFile = filepath.Join(t.TempDir(), ".rearm.env")
	authMode, apiKey, apiKeyId, rearmUri = "", "", "", "https://rearm.example"
	sessionRefreshToken, sessionAccessToken = "", ""
	sessionAccessTokenExp, sessionExpiresAt, sessionHardExpiry = time.Time{}, time.Time{}, time.Time{}
	sessionKeyId, sessionOrg = "USER__u__ord__k", "o"
}

func reload(t *testing.T) {
	t.Helper()
	v := viper.New()
	v.SetConfigFile(cfgFile)
	v.SetConfigType(configType)
	if err := v.ReadInConfig(); err != nil {
		t.Fatal(err)
	}
	sessionRefreshToken, sessionAccessToken = "", ""
	sessionAccessTokenExp, sessionExpiresAt, sessionHardExpiry = time.Time{}, time.Time{}, time.Time{}
	loadSessionConfig(v)
}

func TestTheHardEndSurvivesTheCredentialsFileAndIsAbsentWithoutOne(t *testing.T) {
	withSessionGlobals(t)
	hard := time.Date(2026, 9, 28, 17, 30, 0, 0, time.UTC)
	sessionAccessToken, sessionAccessTokenExp, sessionExpiresAt, sessionHardExpiry = "at", hard, hard, hard
	if err := persistSession(); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(cfgFile)
	if !strings.Contains(string(raw), "SESSIONHARDEXPIRY=2026-09-28T17:30:00Z") || strings.Contains(string(raw), "REFRESHTOKEN") {
		t.Fatalf("the hard end is written and no refresh token is invented, got:\n%s", raw)
	}
	reload(t)
	if !sessionHardExpiry.Equal(hard) || sessionAccessToken != "at" {
		t.Fatalf("read back the hard end and the one token, got %v %q", sessionHardExpiry, sessionAccessToken)
	}

	sessionRefreshToken, sessionHardExpiry = "rt", time.Time{}
	if err := persistSession(); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(cfgFile)
	if strings.Contains(string(raw), "SESSIONHARDEXPIRY") {
		t.Fatalf("a session with no hard end writes none, got:\n%s", raw)
	}
	// a refresh that learns the hard end records it
	persistSessionTokens(rearm.SessionTokens{AccessToken: "at-2", AccessTokenExpiry: hard, SessionExpiry: hard, SessionHardExpiry: hard})
	reload(t)
	if !sessionHardExpiry.Equal(hard) || sessionRefreshToken != "rt" {
		t.Fatalf("persisted from the refresh, got %v %q", sessionHardExpiry, sessionRefreshToken)
	}
}

func TestASessionWithoutARefreshTokenIsStillASession(t *testing.T) {
	withSessionGlobals(t)
	sessionAccessToken = "at-only"
	if got := resolvedAuthMode(); got != authSession {
		t.Fatalf("an access token alone is a session on file, got %q", got)
	}
	sessionAccessToken = ""
	if got := resolvedAuthMode(); got == authSession {
		t.Fatal("nothing on file is not a session")
	}
}

func TestLoginSaysWhenTheSessionEnds(t *testing.T) {
	hard := time.Date(2026, 9, 28, 17, 30, 0, 0, time.UTC)
	bounded := loginMessage("USER__u__ord__k", "o", hard, hard, "/p")
	if !strings.Contains(bounded, "this one ends at "+hard.Local().Format(time.RFC1123)) || !strings.Contains(bounded, "no refresh after that") {
		t.Fatalf("a bounded session names its hard end, got %q", bounded)
	}
	sliding := loginMessage("USER__u__ord__k", "o", hard.AddDate(0, 0, 30), time.Time{}, "/p")
	if !strings.Contains(sliding, "Session valid until") || strings.Contains(sliding, "ends at") {
		t.Fatalf("an unbounded session keeps today's words, got %q", sliding)
	}
}

func TestWhoamiPrintsTheHardEnd(t *testing.T) {
	withSessionGlobals(t)
	hard := time.Date(2026, 9, 28, 17, 30, 0, 0, time.UTC)
	sessionAccessToken, sessionExpiresAt, sessionHardExpiry = "at", hard, hard
	out := captureStdout(t, func() { whoamiCmd.Run(whoamiCmd, nil) })
	var got map[string]interface{}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("whoami prints JSON, got %q: %v", out, err)
	}
	if got["mode"] != "browser-login session" || got["sessionHardExpiry"] != "2026-09-28T17:30:00Z" {
		t.Fatalf("whoami shows the session and its hard end, got %v", got)
	}
	sessionHardExpiry = time.Time{}
	out = captureStdout(t, func() { whoamiCmd.Run(whoamiCmd, nil) })
	if strings.Contains(out, "sessionHardExpiry") {
		t.Fatalf("no hard end, no field, got %s", out)
	}
}

func TestAnEndedSessionSaysSoOnce(t *testing.T) {
	ended := describeError(fmt.Errorf("wrapped: %w", &rearm.SessionError{Code: "invalid_grant",
		Description: rearm.SessionEndedDescription(time.Date(2026, 9, 28, 17, 30, 0, 0, time.UTC))}))
	if ended != "session ended at 2026-09-28T17:30:00Z; run rearm login" {
		t.Fatalf("the ended session's own words, got %q", ended)
	}
	other := describeError(&rearm.SessionError{Code: "invalid_grant", Description: "unknown, expired or revoked refresh token"})
	if !strings.HasPrefix(other, "session refresh failed (") {
		t.Fatalf("other refusals keep their framing, got %q", other)
	}
}

func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stdout
	os.Stdout = w
	f()
	os.Stdout = saved
	_ = w.Close()
	b, _ := io.ReadAll(r)
	return string(b)
}
