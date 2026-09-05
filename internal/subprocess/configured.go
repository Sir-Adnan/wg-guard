package subprocess

import (
	"bytes"
	"context"
)

// RunConfigured runs with an explicit working directory and environment. Build
// callers use this instead of changing process-global environment or cwd.
func (s *System) RunConfigured(ctx context.Context, argv []string, dir string, env []string) (Result, error) {
	return s.run(ctx, argv, dir, env)
}

type boundedOutput struct {
	bytes.Buffer
	limit int
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	n := len(p)
	if b.limit == 0 {
		return b.Buffer.Write(p)
	}
	remaining := b.limit - b.Len()
	if remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		_, _ = b.Buffer.Write(p)
	}
	return n, nil
}
