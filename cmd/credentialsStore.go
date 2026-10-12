/*
The MIT License (MIT)
Copyright (c) 2019 - 2026 Reliza Incorporated. https://reliza.io
*/

package cmd

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
	rearm "github.com/relizaio/rearm-client-go"
	"github.com/spf13/viper"
)

// Every rearm process under one browser login shares the credentials file, and the server binds
// each access token to the session's current refresh token, rotating it on every refresh. So the
// processes take turns through credentialsStore (task S401-1): the client renews only while holding
// the lock, re-reads the file first and adopts a set another process saved instead of refreshing,
// and saves a rotated token before giving the lock back. Login and logout write under the same lock.
//
// The lock is a separate file beside the credentials file (~/.rearm.env.lock): writeCredentials
// replaces the credentials file by rename, which would detach a lock held on the file itself. It is
// never deleted (removing it races another process's open descriptor), and the OS drops the lock
// when the process dies, so there is no stale-lock handling.

// credentialsStore is the credentials file as a rearm.SessionStore.
type credentialsStore struct{}

var _ rearm.SessionStore = credentialsStore{}

// lockRetry is how often a waiting process tries the lock again.
const lockRetry = 50 * time.Millisecond

func credentialsLockPath() (string, error) {
	path, err := credentialsPath()
	if err != nil {
		return "", err
	}
	return path + ".lock", nil
}

// Lock takes the exclusive lock on the lock file, waiting until ctx ends.
func (credentialsStore) Lock(ctx context.Context) (func(), error) {
	path, err := credentialsLockPath()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	fl := flock.New(path, flock.SetPermissions(0o600))
	ok, err := fl.TryLockContext(ctx, lockRetry)
	if err != nil || !ok {
		_ = fl.Close()
		if err == nil {
			err = ctx.Err()
		}
		return nil, err
	}
	return func() { _ = fl.Unlock() }, nil
}

// Load reads the session from the credentials file alone (no environment).
func (credentialsStore) Load() (rearm.SessionTokens, error) {
	path, err := credentialsPath()
	if err != nil {
		return rearm.SessionTokens{}, err
	}
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return rearm.SessionTokens{}, rearm.ErrNoSession
	}
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType(configType)
	if err := v.ReadInConfig(); err != nil {
		return rearm.SessionTokens{}, err
	}
	s := sessionValuesFrom(v)
	if s.refreshToken == "" && s.accessToken == "" {
		return rearm.SessionTokens{}, rearm.ErrNoSession
	}
	return rearm.SessionTokens{AccessToken: s.accessToken, AccessTokenExpiry: s.accessTokenExp, RefreshToken: s.refreshToken,
		SessionExpiry: s.expiresAt, SessionHardExpiry: s.hardExpiry}, nil
}

// Save writes the whole set to the file, keeping the session globals in step.
func (credentialsStore) Save(t rearm.SessionTokens) error {
	sessionRefreshToken = t.RefreshToken
	sessionAccessToken = t.AccessToken
	sessionAccessTokenExp = t.AccessTokenExpiry
	sessionExpiresAt = t.SessionExpiry
	sessionHardExpiry = t.SessionHardExpiry
	return persistSession()
}

// underCredentialsLock runs write while holding the credentials lock, waiting up to rearm.LockWait.
func underCredentialsLock(write func() error) error {
	ctx, cancel := context.WithTimeout(context.Background(), rearm.LockWait)
	defer cancel()
	release, err := credentialsStore{}.Lock(ctx)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("rearm: the credentials store has been locked by another process for %s", rearm.LockWait)
		}
		return err
	}
	defer release()
	return write()
}
