//go:build !linux

package terminal

import (
	"context"
	"io"
)

// Host lifecycle mutation is Linux-only. Scripted terminal tests remain portable.
func waitInput(ctx context.Context, in io.Reader) error {
	if ctx.Err() != nil {
		return ErrCanceled
	}
	return nil
}
