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
// server slides the session 30 days per refresh, capped at 90 days after approval.
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

// persistSessionTokens receives every token set the client refreshes and writes it to the file.
func persistSessionTokens(t rearm.SessionTokens) {
	sessionAccessToken = t.AccessToken
	sessionAccessTokenExp = t.AccessTokenExpiry
	if !t.SessionExpiry.IsZero() {
		sessionExpiresAt = t.SessionExpiry
	}
	_ = persistSession()
}

func persistSession() error {
	return writeCredentials(map[string]string{
		"URI": rearmUri, "APIKEYID": sessionKeyId, "ORG": sessionOrg,
		"REFRESHTOKEN": sessionRefreshToken, "ACCESSTOKEN": sessionAccessToken,
		"ACCESSTOKENEXPIRY": sessionAccessTokenExp.UTC().Format(time.RFC3339),
		"SESSIONEXPIRY":     sessionExpiresAt.UTC().Format(time.RFC3339),
	})
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
	ctx := context.Background()
	hc := &http.Client{Timeout: 30 * time.Second}
	start, err := rearm.StartDeviceLogin(ctx, hc, rearmUri, hostLabel())
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
			sessionKeyId = login.APIKeyID
			sessionOrg = login.Org
			sessionUri = rearmUri
			if err := persistSession(); err != nil {
				return err
			}
			path, _ := credentialsPath()
			fmt.Printf("Signed in as key %s (org %s). Session valid until %s. Credentials written to %s\n",
				sessionKeyId, sessionOrg, sessionExpiresAt.Local().Format(time.RFC1123), path)
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
		if sessionRefreshToken != "" {
			c, err := rearm.NewWithSession(rearmUri, sessionRefreshToken, rearm.SessionTokens{}, nil, rearm.WithUserAgent("ReARM CLI"))
			if err == nil {
				err = c.Revoke(context.Background())
			}
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
