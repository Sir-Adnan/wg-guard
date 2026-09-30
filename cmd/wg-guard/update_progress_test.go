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
	stage, stop := startUpdateHeartbeat(context.Background(), &out, "Acquiring verified build")
	stage("Downloading release binary")
	time.Sleep(4 * time.Millisecond)
	stop(nil)
	if text := out.String(); !strings.Contains(text, "[RUN] Acquiring verified build") || !strings.Contains(text, "[OK] Acquiring verified build") || !strings.Contains(text, "[RUN] Downloading release binary") || strings.Contains(text, "Still working") {
		t.Fatalf("bounded acquisition progress missing: %q", text)
	}
}
