package main

import (
	"context"
	"io"
	"os"
	"sync"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/i18n"
	"github.com/Sir-Adnan/wg-guard/internal/terminal"
)

var updateHeartbeatInterval = 15 * time.Second

// startUpdateHeartbeat keeps the current acquisition stage visible while
// verbose compiler output remains private and quiet.
func startUpdateHeartbeat(ctx context.Context, out io.Writer, label string) (func(string), func(error)) {
	u := terminal.New(os.Stdin, out, terminal.Detect(os.Stdin, out, i18n.En))
	task := u.BeginTask(label)
	stageStarted := time.Now()
	var taskMu sync.Mutex
	stage := func(label string) {
		taskMu.Lock()
		defer taskMu.Unlock()
		task.Done(nil)
		task = u.BeginTask(label)
		stageStarted = time.Now()
	}
	stop := make(chan struct{})
	done := make(chan struct{})
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
				taskMu.Lock()
				task.Tick(time.Since(stageStarted))
				taskMu.Unlock()
			}
		}
	}()
	var once sync.Once
	return stage, func(err error) {
		once.Do(func() {
			close(stop)
			<-done
			taskMu.Lock()
			task.Done(err)
			taskMu.Unlock()
		})
	}
}
