//go:build !linux

package routing

import "errors"

// CountConnections is unavailable outside Linux.
func CountConnections(uint32) (int64, error) { return 0, ErrCountUnavailable }

// FlushConnections is unavailable outside Linux.
func FlushConnections(uint32) error { return errors.New("routing: connection drain unavailable") }
