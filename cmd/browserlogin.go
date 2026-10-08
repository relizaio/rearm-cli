/*
The MIT License (MIT)
Copyright (c) 2019 - 2026 Reliza Incorporated. https://reliza.io
*/

package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/mitchellh/go-homedir"
	rearm "github.com/relizaio/rearm-client-go"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// Browser login (RFC 8628 device authorization) and the token-based session it produces.
//
// `rearm login` with no key arguments starts a login request, opens the browser on the
// verification link and polls until the user approves it in ReARM. What comes back is a
// refresh token bound to the key the user chose; no key secret is ever stored. The client
// library then trades the refresh token for a one-hour access token on demand (when the
// cached one is expired or within a minute of it) and hands every new token set back through
// persistSessionTokens, so a CLI used at least once a month never asks to log in again; the
// server slides the session 30 days per refresh, capped at 90 days after approval. A key that
// bounds its sessions (sessionMaxMinutes) gives the session a hard end: login and whoami print
// it, the client does not refresh past it, and a session that ends inside its first access token
// comes with no refresh token at all -- the access token alone is the session.
// `rearm logout` revokes the session server side and clears the file.
//
// The credentials file is $HOME/.rearm.env (the file the key-based `rearm login` always
// wrote), now written with mode 0600 through os.WriteFile, which is portable: on Windows the
// mode bits are advisory and the file lives under the user's profile directory.

// Session-mode values loaded from the credentials file (see initConfig / loadSessionConfig).
var (
	sessionRefreshToken     string
	sessionAccessToken      string
	sessionAccessTokenExp   time.Time
	sessionExpiresAt        time.Time
	sessionHardExpiry       time.Time
	sessionKeyId            string
	sessionOrg              string
	sessionUri              string
	loginRequestedFromLabel string
)

// credentialsPath is $HOME/.rearm.env, or --config when given.
func credentialsPath() (string, error) {
	if cfgFile != "" {
		return cfgFile, nil
	}
	home, err := homedir.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, defaultConfigFilename+"."+configType), nil
}

// writeCredentials rewrites the credentials file with exactly these values, mode 0600.
func writeCredentials(values map[string]string) error {
	path, err := credentialsPath()
	if err != nil {
		return err
	}
	keys := []string{"URI", "APIKEYID", "APIKEY", "ORG", "REFRESHTOKEN", "ACCESSTOKEN", "ACCESSTOKENEXPIRY", "SESSIONEXPIRY", "SESSIONHARDEXPIRY"}
	var sb strings.Builder
	for _, k := range keys {
		if v, ok := values[k]; ok && v != "" {
			sb.WriteString(k + "=" + v + "\n")
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	// write-then-rename so a crash never leaves a half-written file; 0600 from the first byte
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(sb.String()), 0o600); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		_ = os.Remove(path) // rename does not replace on Windows
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

// sessionValues is a browser login as the credentials file (or the REARM_* environment) holds it.
type sessionValues struct {
	refreshToken, accessToken, keyID, org, uri string
	accessTokenExp, expiresAt, hardExpiry      time.Time
}

func sessionValuesFrom(v *viper.Viper) sessionValues {
	s := sessionValues{refreshToken: v.GetString("refreshtoken"), accessToken: v.GetString("accesstoken"),
		keyID: v.GetString("apikeyid"), org: v.GetString("org"), uri: v.GetString("uri")}
	s.accessTokenExp, _ = time.Parse(time.RFC3339, v.GetString("accesstokenexpiry"))
	s.expiresAt, _ = time.Parse(time.RFC3339, v.GetString("sessionexpiry"))
	s.hardExpiry, _ = time.Parse(time.RFC3339, v.GetString("sessionhardexpiry"))
	return s
}

// sessionFromFile: the session in use came from the credentials file (not from REARM_REFRESHTOKEN /
// REARM_ACCESSTOKEN), so the client renews through the file's lock (credentialsStore).
var sessionFromFile bool

// loadSessionConfig picks the session values out of the viper config (file or REARM_* env).
func loadSessionConfig(v *viper.Viper) {
	s := sessionValuesFrom(v)
	sessionRefreshToken = s.refreshToken
	sessionAccessToken = s.accessToken
	sessionKeyId = s.keyID
	sessionOrg = s.org
	sessionUri = s.uri
	if !s.accessTokenExp.IsZero() {
		sessionAccessTokenExp = s.accessTokenExp
	}
	if !s.expiresAt.IsZero() {
		sessionExpiresAt = s.expiresAt
	}
	if !s.hardExpiry.IsZero() {
		sessionHardExpiry = s.hardExpiry
	}
	sessionFromFile = sessionOnFile() && isCredentialsFile(v.ConfigFileUsed()) &&
		os.Getenv("REARM_REFRESHTOKEN") == "" && os.Getenv("REARM_ACCESSTOKEN") == ""
}

// isCredentialsFile says whether used is the credentials file the CLI writes.
func isCredentialsFile(used string) bool {
	if used == "" {
		return false
	}
	path, err := credentialsPath()
	if err != nil {
		return false
	}
	a, errA := filepath.Abs(used)
	b, errB := filepath.Abs(path)
	return errA == nil && errB == nil && a == b
}

// sessionOnFile: a browser login is stored -- its refresh token, or, for a session that ends inside
// its first access token, that token alone.
func sessionOnFile() bool {
	return sessionRefreshToken != "" || sessionAccessToken != ""
}

// inSessionMode: a browser login is on file and no explicit key secret was given.
func inSessionMode() bool {
	return resolvedAuthMode() == authSession
}

const (
	authKey        = "key"
	authSession    = "session"
	authGitHubOIDC = "github-oidc"
)

// resolvedAuthMode: --auth / REARM_AUTH when given; otherwise the credentials present decide,
// an explicit key secret winning over a stored session, and a GitHub Actions job with
// id-token: write and no other credentials using its identity token.
func resolvedAuthMode() string {
	switch strings.ToLower(strings.TrimSpace(authMode)) {
	case authKey, authSession, authGitHubOIDC:
		return strings.ToLower(strings.TrimSpace(authMode))
	case "":
	default:
		fmt.Println("Error: --auth must be key, session or github-oidc")
		os.Exit(1)
	}
	if apiKey != "" {
		return authKey
	}
	if sessionOnFile() {
		return authSession
	}
	if os.Getenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN") != "" && apiKeyId == "" {
		return authGitHubOIDC
	}
	return authKey
}

// oidcOrg: the organization to name in the exchange, when several trust the same identity.
func oidcOrg() string {
	if orgFlag != "" {
		return orgFlag
	}
	return sessionOrg
}

// sessionStoreInUse: this process's session client was built with credentialsStore (set by
// rearmClient), so the store's Save writes the file. It differs from sessionFromFile, which only says
// where the session was read from: nothing renews through the store until a client is built with it.
var sessionStoreInUse bool

// persistSessionTokens receives every token set the client refreshes or adopts. With the store in use
// the file is already written (under its lock) and only the globals follow. Without it (a session from
// the environment) the file is written here, as before: the server rotates the refresh token on every
// refresh, so the rotated one replaces the stored one before anything else happens, because the
// previous token is retired (a short grace window on the server covers a crash between here and the
// write; reuse after it revokes the session).
func persistSessionTokens(t rearm.SessionTokens) {
	sessionAccessToken = t.AccessToken
	sessionAccessTokenExp = t.AccessTokenExpiry
	if !t.SessionExpiry.IsZero() {
		sessionExpiresAt = t.SessionExpiry
	}
	if !t.SessionHardExpiry.IsZero() {
		sessionHardExpiry = t.SessionHardExpiry
	}
	if t.RefreshToken != "" {
		sessionRefreshToken = t.RefreshToken
	}
	if sessionStoreInUse {
		return
	}
	if err := persistSession(); err != nil {
		fmt.Fprintln(os.Stderr, "Warning: could not write the credentials file; the session may need `rearm login` again:", err)
	}
}

func persistSession() error {
	return writeCredentials(map[string]string{
		"URI": rearmUri, "APIKEYID": sessionKeyId, "ORG": sessionOrg,
		"REFRESHTOKEN": sessionRefreshToken, "ACCESSTOKEN": sessionAccessToken,
		"ACCESSTOKENEXPIRY": sessionAccessTokenExp.UTC().Format(time.RFC3339),
		"SESSIONEXPIRY":     sessionExpiresAt.UTC().Format(time.RFC3339),
		"SESSIONHARDEXPIRY": rfc3339OrEmpty(sessionHardExpiry),
	})
}

func rfc3339OrEmpty(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// loginMessage is what a delivered login prints: the key, and when the session ends -- its hard
// end when the key bounds its sessions, the sliding expiry otherwise.
func loginMessage(keyID, org string, sessionExpiry, hardExpiry time.Time, path string) string {
	if !hardExpiry.IsZero() {
		return fmt.Sprintf("Signed in as key %s (org %s). The key bounds its sessions: this one ends at %s, with no refresh after that; run `rearm login` again then. Credentials written to %s",
			keyID, org, hardExpiry.Local().Format(time.RFC1123), path)
	}
	return fmt.Sprintf("Signed in as key %s (org %s). Session valid until %s. Credentials written to %s",
		keyID, org, sessionExpiry.Local().Format(time.RFC1123), path)
}

// openBrowser is best effort; the link is always printed too.
func openBrowser(url string) {
	var c *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		c = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		c = exec.Command("open", url)
	default:
		c = exec.Command("xdg-open", url)
	}
	_ = c.Start()
}

// localTimeZone: the UTC offset and abbreviation of the local clock, e.g. "UTC+02:00 CEST", with the
// IANA name appended when the environment names one. Cheap, portable, and a mismatch with the
// approver's own clock is a useful smell.
func localTimeZone() string {
	abbr, offset := time.Now().Zone()
	sign := "+"
	if offset < 0 {
		sign = "-"
		offset = -offset
	}
	tz := fmt.Sprintf("UTC%s%02d:%02d", sign, offset/3600, (offset%3600)/60)
	if abbr != "" && !strings.HasPrefix(abbr, "+") && !strings.HasPrefix(abbr, "-") {
		tz += " " + abbr
	}
	if name := os.Getenv("TZ"); name != "" && strings.Contains(name, "/") {
		tz += " (" + name + ")"
	}
	return tz
}

func hostLabel() string {
	if loginRequestedFromLabel != "" {
		return loginRequestedFromLabel
	}
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "rearm cli"
	}
	return h
}

// browserLogin runs the device flow end to end and writes the session to the credentials file.
func browserLogin() error {
	if rearmUri == "" {
		return errors.New("--uri (or REARM_URI) is required to log in")
	}
	rearmUri = strings.TrimRight(rearmUri, "/")
	ctx := context.Background()
	hc := &http.Client{Timeout: 30 * time.Second}
	// what this CLI knows about the device; the approval page shows it as reported by the requester, apart from the address the server sees
	start, err := rearm.StartDeviceLoginWithDetails(ctx, hc, rearmUri, rearm.DeviceDetails{
		Hostname: hostLabel(),
		OS:       runtime.GOOS + "/" + runtime.GOARCH,
		TimeZone: localTimeZone(),
		Client:   "rearm-cli " + strings.TrimSpace(strings.TrimPrefix(Version, "v")),
	})
	if err != nil {
		return fmt.Errorf("could not start a login: %w", err)
	}
	interval := time.Duration(start.Interval) * time.Second
	expiresIn := time.Duration(start.ExpiresIn) * time.Second
	if expiresIn <= 0 {
		expiresIn = 10 * time.Minute
	}
	fmt.Println("Open this link in your browser and approve the sign-in:")
	fmt.Println("  " + start.VerificationURIComplete)
	fmt.Println("Code: " + start.UserCode)
	openBrowser(start.VerificationURIComplete)
	deadline := time.Now().Add(expiresIn)
	for time.Now().Before(deadline) {
		time.Sleep(interval)
		login, err := rearm.PollDeviceLogin(ctx, hc, rearmUri, start.DeviceCode)
		if err != nil {
			return err
		}
		switch login.Status {
		case rearm.DevicePending:
			continue
		case rearm.DeviceSlowDown:
			interval += 5 * time.Second
			continue
		case rearm.DeviceDenied:
			return errors.New("the sign-in was denied in the browser")
		case rearm.DeviceExpired:
			return errors.New("the sign-in request expired; run `rearm login` again")
		case rearm.DeviceDelivered:
			sessionRefreshToken = login.RefreshToken
			sessionAccessToken = login.Tokens.AccessToken
			sessionAccessTokenExp = login.Tokens.AccessTokenExpiry
			sessionExpiresAt = login.Tokens.SessionExpiry
			sessionHardExpiry = login.Tokens.SessionHardExpiry
			sessionKeyId = login.APIKeyID
			sessionOrg = login.Org
			sessionUri = rearmUri
			// under the lock, so a login never interleaves with another process's refresh write
			if err := underCredentialsLock(persistSession); err != nil {
				return err
			}
			path, _ := credentialsPath()
			fmt.Println(loginMessage(sessionKeyId, sessionOrg, sessionExpiresAt, sessionHardExpiry, path))
			return nil
		default:
			desc := login.Description
			if desc == "" {
				desc = string(login.Status)
			}
			return fmt.Errorf("sign-in failed: %s", desc)
		}
	}
	return errors.New("the sign-in request expired; run `rearm login` again")
}

var logoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Sign the CLI out of ReARM",
	Long:  "Revokes the browser-login session on the server (a key created for it is deleted) and clears the credentials file.",
	Run: func(cmd *cobra.Command, args []string) {
		if err := logout(); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		fmt.Println("Signed out.")
	},
}

// logout revokes the session and clears the credentials file, all under the credentials lock: the
// refresh token revoked is the one on file at that moment (another process may have rotated the one
// this process loaded), and no refresh write can land after the clear. A running process's next
// renewal then finds no session and says to log in again.
func logout() error {
	return underCredentialsLock(func() error {
		refreshToken := sessionRefreshToken
		if on, err := (credentialsStore{}).Load(); err == nil && on.RefreshToken != "" {
			refreshToken = on.RefreshToken
		}
		if refreshToken != "" {
			c, err := rearm.NewWithSession(rearmUri, refreshToken, rearm.SessionTokens{}, nil, rearm.WithUserAgent("ReARM CLI"),
				rearm.WithHTTPClient(&http.Client{Timeout: 20 * time.Second}))
			if err == nil {
				err = c.Revoke(context.Background())
			}
			if err != nil {
				fmt.Println("Warning: could not reach ReARM to revoke the session:", err)
			}
		}
		return writeCredentials(map[string]string{"URI": rearmUri})
	})
}

var whoamiCmd = &cobra.Command{
	Use:   "whoami",
	Short: "Show which key the CLI acts as",
	Run: func(cmd *cobra.Command, args []string) {
		out := map[string]interface{}{"uri": rearmUri}
		if inSessionMode() {
			out["mode"] = "browser-login session"
			out["apiKeyId"] = sessionKeyId
			out["org"] = sessionOrg
			if !sessionExpiresAt.IsZero() {
				out["sessionExpiresAt"] = sessionExpiresAt.UTC().Format(time.RFC3339)
			}
			if !sessionHardExpiry.IsZero() {
				out["sessionHardExpiry"] = sessionHardExpiry.UTC().Format(time.RFC3339)
			}
		} else if resolvedAuthMode() == authGitHubOIDC {
			out["mode"] = "github-oidc"
			if o := oidcOrg(); o != "" {
				out["org"] = o
			}
			// the identity is only known after an exchange; do one so whoami says who the job acts as
			c := rearmClient()
			if _, err := rearm.Raw(context.Background(), c, "Whoami", "query Whoami { __typename }", nil); err != nil {
				out["error"] = describeError(err)
			} else {
				id := c.Identity()
				out["apiKeyId"] = id.APIKeyID
				out["org"] = id.Org
				out["identity"] = id.Repository
			}
		} else if apiKeyId != "" {
			out["mode"] = "api key"
			out["apiKeyId"] = apiKeyId
		} else {
			out["mode"] = "not logged in"
		}
		b, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(b))
	},
}

func init() {
	rootCmd.AddCommand(logoutCmd)
	rootCmd.AddCommand(whoamiCmd)
}
