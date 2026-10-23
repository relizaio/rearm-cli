//go:build !unix

/*
The MIT License (MIT)
Copyright (c) 2019 - 2026 Reliza Incorporated. https://reliza.io
*/

package cmd

import "os"

// fileTrustedForCache: there are no unix owner and mode bits to check here (as in the S401-1 idiom).
func fileTrustedForCache(os.FileInfo) (bool, string) { return true, "" }
