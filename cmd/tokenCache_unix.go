//go:build unix

/*
The MIT License (MIT)
Copyright (c) 2019 - 2026 Reliza Incorporated. https://reliza.io
*/

package cmd

import (
	"fmt"
	"os"
	"syscall"
)

// fileTrustedForCache: a token cache file is adopted only when it is a regular file owned by the
// effective uid with no group or other permission bit, so a file planted, copied or restored into the
// directory (--config may point into one the user does not control alone) is a miss, which the next
// write replaces with a fresh 0600 file of the caller's.
func fileTrustedForCache(fi os.FileInfo) (bool, string) {
	if !fi.Mode().IsRegular() {
		return false, "is not a regular file"
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); !ok || st.Uid != uint32(os.Geteuid()) {
		return false, "is not owned by this user"
	}
	if perm := fi.Mode().Perm(); perm&0o077 != 0 {
		return false, fmt.Sprintf("has mode %#o", perm)
	}
	return true, ""
}
