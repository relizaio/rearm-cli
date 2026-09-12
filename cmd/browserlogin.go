/*
The MIT License (MIT)
Copyright (c) 2019 - 2026 Reliza Incorporated. https://reliza.io
*/

package cmd

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/mitchellh/go-homedir"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// Browser login (RFC 8628 device authorization) and the token-based session it produces.
//
// `rearm login` with no key arguments starts a login request, opens the browser on the
// verification link and polls until the user approves it in ReARM. What comes back is a
// refresh token bound to the key the user chose; no key secret is ever stored. Every
// command then trades the refresh token for a one-hour access token on demand (when the
// cached one is expired or within a minute of it), so a CLI used at least once a month
// never asks to log in again; the server slides the session 30 days per refresh, capped at
// 90 days after approval. `rearm logout` revokes the session server side and clears the file.
//
// The credentials file is $HOME/.rearm.env (the file the key-based `rearm login` always
// wrote), now written with mode 0600 through os.WriteFile, which is portable: on Windows the
// mode bits are advisory and the file lives under the user's profile directory.

const (
	deviceCodeGrant     = "urn:ietf:params:oauth:grant-type:device_code"
	deviceCodePath      = "/api/programmatic/device/code"
	tokenPath           = "/api/programmatic/token"
	revokePath          = "/api/programmatic/revoke"
	programmaticGraphQL = "/api/programmatic/graphql"
	accessTokenSkew     = 60 * time.Second
)

// Session-mode values loaded from the credentials file (see initConfig / loadSessionConfig).
var (
	sessionRefreshToken     string
	sessionAccessToken      string
	sessionAccessTokenExp   time.Time
	sessionExpiresAt        time.Time
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
	keys := []string{"URI", "APIKEYID", "APIKEY", "ORG", "REFRESHTOKEN", "ACCESSTOKEN", "ACCESSTOKENEXPIRY", "SESSIONEXPIRY"}
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

// loadSessionConfig picks the session values out of the viper config (file or REARM_* env).
func loadSessionConfig(v *viper.Viper) {
	sessionRefreshToken = v.GetString("refreshtoken")
	sessionAccessToken = v.GetString("accesstoken")
	sessionKeyId = v.GetString("apikeyid")
	sessionOrg = v.GetString("org")
	sessionUri = v.GetString("uri")
	if s := v.GetString("accesstokenexpiry"); s != "" {
		sessionAccessTokenExp, _ = time.Parse(time.RFC3339, s)
	}
	if s := v.GetString("sessionexpiry"); s != "" {
		sessionExpiresAt, _ = time.Parse(time.RFC3339, s)
	}
}

// inSessionMode: a browser login is on file and no explicit key secret was given.
func inSessionMode() bool {
	return sessionRefreshToken != "" && apiKey == ""
}

// graphqlPath: session tokens are only honoured on the programmatic endpoint; key
// credentials keep the historic /graphql path (with its CSRF handshake) unchanged.
func graphqlPath() string {
	if inSessionMode() {
		return programmaticGraphQL
	}
	return "/graphql"
}

// authorizationHeader is what every request sends: Basic for a key secret, Bearer for a session.
func authorizationHeader() string {
	if inSessionMode() {
		tok, err := ensureAccessToken()
		if err != nil {
			fmt.Println("Error:", err)
			os.Exit(1)
		}
		return "Bearer " + tok
	}
	if len(apiKeyId) > 0 && len(apiKey) > 0 {
		return "Basic " + base64.StdEncoding.EncodeToString([]byte(apiKeyId+":"+apiKey))
	}
	return ""
}

// ensureAccessToken returns a usable access token, refreshing (and persisting) when the cached
// one is missing, expired, or within a minute of expiring.
func ensureAccessToken() (string, error) {
	if sessionAccessToken != "" && time.Now().Add(accessTokenSkew).Before(sessionAccessTokenExp) {
		return sessionAccessToken, nil
	}
	var out map[string]interface{}
	resp, err := resty.New().R().
		SetHeader("User-Agent", "ReARM CLI").
		SetFormData(map[string]string{"grant_type": "refresh_token", "refresh_token": sessionRefreshToken}).
		SetResult(&out).SetError(&out).
		Post(rearmUri + tokenPath)
	if err != nil {
		return "", err
	}
	if e, _ := out["error"].(string); e != "" || resp.IsError() {
		return "", fmt.Errorf("session refresh failed (%s): run `rearm login` again", errorDescription(out))
	}
	tok, _ := out["access_token"].(string)
	expiresIn, _ := out["expires_in"].(float64)
	if tok == "" {
		return "", errors.New("token endpoint returned no access token")
	}
	sessionAccessToken = tok
	sessionAccessTokenExp = time.Now().Add(time.Duration(expiresIn) * time.Second)
	if s, ok := out["session_expires_at"].(string); ok {
		sessionExpiresAt, _ = time.Parse(time.RFC3339, s)
	}
	_ = persistSession()
	return tok, nil
}

func persistSession() error {
	return writeCredentials(map[string]string{
		"URI": rearmUri, "APIKEYID": sessionKeyId, "ORG": sessionOrg,
		"REFRESHTOKEN": sessionRefreshToken, "ACCESSTOKEN": sessionAccessToken,
		"ACCESSTOKENEXPIRY": sessionAccessTokenExp.UTC().Format(time.RFC3339),
		"SESSIONEXPIRY":     sessionExpiresAt.UTC().Format(time.RFC3339),
	})
}

func errorDescription(m map[string]interface{}) string {
	if d, ok := m["error_description"].(string); ok && d != "" {
		return d
	}
	if e, ok := m["error"].(string); ok {
		return e
	}
	return "unknown error"
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
	var start map[string]interface{}
	resp, err := resty.New().R().SetHeader("User-Agent", "ReARM CLI").
		SetFormData(map[string]string{"requested_from": hostLabel()}).
		SetResult(&start).SetError(&start).Post(rearmUri + deviceCodePath)
	if err != nil {
		return err
	}
	if resp.IsError() {
		return fmt.Errorf("could not start a login: %s", errorDescription(start))
	}
	deviceCode, _ := start["device_code"].(string)
	userCode, _ := start["user_code"].(string)
	link, _ := start["verification_uri_complete"].(string)
	interval := 5.0
	if i, ok := start["interval"].(float64); ok && i > 0 {
		interval = i
	}
	expiresIn := 600.0
	if e, ok := start["expires_in"].(float64); ok && e > 0 {
		expiresIn = e
	}
	fmt.Println("Open this link in your browser and approve the sign-in:")
	fmt.Println("  " + link)
	fmt.Println("Code: " + userCode)
	openBrowser(link)
	deadline := time.Now().Add(time.Duration(expiresIn) * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(time.Duration(interval) * time.Second)
		var out map[string]interface{}
		r, err := resty.New().R().SetHeader("User-Agent", "ReARM CLI").
			SetFormData(map[string]string{"grant_type": deviceCodeGrant, "device_code": deviceCode}).
			SetResult(&out).SetError(&out).Post(rearmUri + tokenPath)
		if err != nil {
			return err
		}
		// outcomes arrive as 200 with an error member (the ingress in front of ReARM rewrites 4xx bodies), so read the member first
		if e, _ := out["error"].(string); e != "" || r.IsError() {
			switch out["error"] {
			case "authorization_pending":
				continue
			case "slow_down":
				interval += 5
				continue
			case "access_denied":
				return errors.New("the sign-in was denied in the browser")
			case "expired_token":
				return errors.New("the sign-in request expired; run `rearm login` again")
			default:
				return fmt.Errorf("sign-in failed: %s", errorDescription(out))
			}
		}
		sessionRefreshToken, _ = out["refresh_token"].(string)
		sessionAccessToken, _ = out["access_token"].(string)
		expiresIn, _ := out["expires_in"].(float64)
		sessionAccessTokenExp = time.Now().Add(time.Duration(expiresIn) * time.Second)
		sessionKeyId, _ = out["api_key_id"].(string)
		sessionOrg, _ = out["org"].(string)
		if s, ok := out["session_expires_at"].(string); ok {
			sessionExpiresAt, _ = time.Parse(time.RFC3339, s)
		}
		sessionUri = rearmUri
		if err := persistSession(); err != nil {
			return err
		}
		path, _ := credentialsPath()
		fmt.Printf("Signed in as key %s (org %s). Session valid until %s. Credentials written to %s\n",
			sessionKeyId, sessionOrg, sessionExpiresAt.Local().Format(time.RFC1123), path)
		return nil
	}
	return errors.New("the sign-in request expired; run `rearm login` again")
}

var logoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Sign the CLI out of ReARM",
	Long:  "Revokes the browser-login session on the server (a key created for it is deleted) and clears the credentials file.",
	Run: func(cmd *cobra.Command, args []string) {
		if sessionRefreshToken != "" {
			_, err := resty.New().R().SetHeader("User-Agent", "ReARM CLI").
				SetFormData(map[string]string{"token": sessionRefreshToken}).Post(rearmUri + revokePath)
			if err != nil {
				fmt.Println("Warning: could not reach ReARM to revoke the session:", err)
			}
		}
		if err := writeCredentials(map[string]string{"URI": rearmUri}); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		fmt.Println("Signed out.")
	},
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
