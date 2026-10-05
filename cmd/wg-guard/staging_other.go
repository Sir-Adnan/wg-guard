//go:build !linux

package main

import "os"

// Production acquisition is Linux-only. Other hosts retain the path/permission
// checks for isolated fixtures without claiming Linux ownership verification.
func checkStagingOwner(os.FileInfo) error { return nil }
