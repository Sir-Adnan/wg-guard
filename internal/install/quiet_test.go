package install

import (
	"bytes"
	"reflect"
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

func TestAptCommandsWaitForPackageManagerLock(t *testing.T) {
	input := []string{"apt-get", "install", "-y", "docker.io"}
	want := []string{"apt-get", "-o", "DPkg::Lock::Timeout=300", "install", "-y", "docker.io"}
	if got := withAptLockWait(input); !reflect.DeepEqual(got, want) {
		t.Fatalf("apt command = %v, want %v", got, want)
	}
	if !reflect.DeepEqual(input, []string{"apt-get", "install", "-y", "docker.io"}) {
		t.Fatalf("input command mutated: %v", input)
	}
	nonApt := []string{"docker", "info"}
	if got := withAptLockWait(nonApt); !reflect.DeepEqual(got, nonApt) {
		t.Fatalf("non-apt command changed: %v", got)
	}
}
