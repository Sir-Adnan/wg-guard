//go:build linux

package main

import (
	"os"
	"syscall"
	"testing"
)

type stagingOwnerInfo struct {
	os.FileInfo
	stat syscall.Stat_t
}

func (i stagingOwnerInfo) Sys() any { return &i.stat }

func TestStagingRejectsAnotherOwner(t *testing.T) {
	info, err := os.Stat(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if checkStagingOwner(stagingOwnerInfo{FileInfo: info, stat: syscall.Stat_t{Uid: uint32(os.Geteuid() + 1)}}) == nil {
		t.Fatal("another owner's directory admitted")
	}
}
