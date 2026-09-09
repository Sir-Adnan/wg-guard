package install

import (
	"bytes"
	"testing"
	"time"
)

func TestInstallerLogWriterIsHardBounded(t *testing.T) {
	var dst bytes.Buffer
	w := &boundedLogWriter{Writer: &dst, remaining: 5}
	for _, payload := range []string{"123456789", "more"} {
		n, err := w.Write([]byte(payload))
		if err != nil || n != len(payload) {
			t.Fatalf("write %q = %d, %v", payload, n, err)
		}
	}
	if got := dst.String(); got != "12345" {
		t.Fatalf("bounded log = %q", got)
	}
}

func TestQuietCommandReportsProgressWhileWaiting(t *testing.T) {
	done := make(chan error, 1)
	go func() {
		time.Sleep(25 * time.Millisecond)
		done <- nil
	}()
	heartbeats := 0
	if err := waitQuietCommand(done, 5*time.Millisecond, func(time.Duration) { heartbeats++ }); err != nil {
		t.Fatal(err)
	}
	if heartbeats == 0 {
		t.Fatal("long quiet command produced no progress heartbeat")
	}
}
