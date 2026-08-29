//go:build linux

package subprocess

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSystemRunTimeout(t *testing.T) {
	r := &System{Timeout: 150 * time.Millisecond}
	_, err := r.Run(context.Background(), []string{"sleep", "5"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want DeadlineExceeded, got %v", err)
	}
}
