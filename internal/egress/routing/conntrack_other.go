//go:build !linux

package routing

// CountConnections is unavailable outside Linux.
func CountConnections(uint32) (int64, error) { return 0, ErrCountUnavailable }
