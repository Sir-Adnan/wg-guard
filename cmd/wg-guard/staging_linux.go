//go:build linux

package main

import (
	"fmt"
	"os"
	"syscall"
)

func checkStagingOwner(info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("acquisition: staging directory has another owner")
	}
	return nil
}
