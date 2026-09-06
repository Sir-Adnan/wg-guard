//go:build !linux && !windows

package backup

import "os"

func openLeaseFile(string) (*os.File, error) { return nil, os.ErrPermission }
func leaseLock(*os.File, int64, bool) error  { return os.ErrPermission }
func leaseUnlock(*os.File, int64) error      { return os.ErrPermission }
