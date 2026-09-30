package terminal

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// Task shows one truthful operation stage without inventing a percentage or
// exposing command arguments. On a TTY its heartbeat replaces one line; when
// redirected it emits a bounded, plain-text update once per minute.
type Task struct {
	mu          sync.Mutex
	ui          *UI
	label       string
	started     time.Time
	lastPrinted time.Duration
	finished    bool
}

func (u *UI) BeginTask(label string) *Task {
	t := &Task{ui: u, label: Clean(label), started: time.Now()}
	t.render("RUN", 0, false)
	return t
}

func (t *Task) Tick(elapsed time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.finished || !t.ui.outputTTY && elapsed-t.lastPrinted < time.Minute {
		return
	}
	t.lastPrinted = elapsed
	t.render("RUN", elapsed, false)
}

func (t *Task) Done(err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.finished {
		return
	}
	t.finished = true
	state := "OK"
	if err != nil {
		state = "FAIL"
	}
	t.render(state, time.Since(t.started).Round(time.Second), true)
}

func (t *Task) render(state string, elapsed time.Duration, final bool) {
	line := "[" + state + "] " + t.label
	if elapsed > 0 {
		line += " · " + elapsed.Round(time.Second).String()
	}
	width := t.ui.width
	if width > 72 {
		width = 72
	}
	runes := []rune(line)
	if len(runes) > width {
		line = string(runes[:width-1]) + "…"
	}
	if t.ui.outputTTY {
		fmt.Fprint(t.ui.Out, "\r\x1b[2K")
		if t.ui.color {
			tone := colorInfo
			if state == "OK" {
				tone = colorSuccess
			} else if state == "FAIL" {
				tone = colorFailure
			}
			fmt.Fprint(t.ui.Out, tone)
		}
		fmt.Fprint(t.ui.Out, line)
		if t.ui.color {
			fmt.Fprint(t.ui.Out, colorReset)
		}
		if final {
			fmt.Fprintln(t.ui.Out)
		}
		return
	}
	fmt.Fprintln(t.ui.Out, strings.TrimSpace(line))
}
