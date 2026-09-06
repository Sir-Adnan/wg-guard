package terminal

import (
	"context"
	"golang.org/x/sys/unix"
	"io"
	"os"
)

// Poll avoids blocked terminal reads after SIGINT cancels the operation. There
// is no reader goroutine to leak; raw secret Ctrl-C is handled as a byte.
func waitInput(ctx context.Context, in io.Reader) error {
	f, ok := in.(*os.File)
	if !ok {
		return nil
	}
	p := []unix.PollFd{{Fd: int32(f.Fd()), Events: unix.POLLIN}}
	for {
		if ctx.Err() != nil {
			return ErrCanceled
		}
		n, err := unix.Poll(p, 100)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return err
		}
		if n > 0 {
			return nil
		}
	}
}
