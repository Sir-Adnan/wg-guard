package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/i18n"
	"github.com/Sir-Adnan/wg-guard/internal/terminal"
)

var updateHeartbeatInterval = 15 * time.Second

// startUpdateHeartbeat provides bounded progress around distribution source
// acquisition, whose compiler output is intentionally private and quiet.
func startUpdateHeartbeat(ctx context.Context, out io.Writer, label string) func() {
	u := terminal.New(os.Stdin, out, terminal.Detect(os.Stdin, out, i18n.En))
	u.Info(label)
	stop := make(chan struct{})
	done := make(chan struct{})
	started := time.Now()
	go func() {
		defer close(done)
		ticker := time.NewTicker(updateHeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-stop:
				return
			case <-ticker.C:
				u.Info(fmt.Sprintf("Still working · %s elapsed", time.Since(started).Round(time.Second)))
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			close(stop)
			<-done
		})
	}
}
