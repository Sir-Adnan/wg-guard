package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func TestUpdateAcquisitionHeartbeatKeepsQuietSSHSessionAlive(t *testing.T) {
	previous := updateHeartbeatInterval
	updateHeartbeatInterval = time.Millisecond
	t.Cleanup(func() { updateHeartbeatInterval = previous })
	var out bytes.Buffer
	stop := startUpdateHeartbeat(context.Background(), &out, "Acquiring verified build")
	time.Sleep(4 * time.Millisecond)
	stop()
	if text := out.String(); !strings.Contains(text, "Acquiring verified build") || !strings.Contains(text, "Still working") || !strings.Contains(text, "elapsed") {
		t.Fatalf("bounded acquisition progress missing: %q", text)
	}
}
