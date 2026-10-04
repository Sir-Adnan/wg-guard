package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInterruptedVerificationIsIncompleteAndNotCorrupt(t *testing.T) {
	s, _ := newService(t)
	archive, err := s.Create(context.Background(), CreateOpts{})
	if err != nil {
		t.Fatal(err)
	}
	for _, expired := range []bool{false, true} {
		var ctx context.Context
		var cancel context.CancelFunc
		cause := error(context.Canceled)
		if expired {
			ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			cause = context.DeadlineExceeded
		} else {
			ctx, cancel = context.WithCancel(context.Background())
			cancel()
		}
		for _, original := range []bool{false, true} {
			var preview *PendingRestore
			if original {
				preview, _, err = s.StageOriginal(ctx, archive.Path, "")
			} else {
				preview, _, err = s.Stage(ctx, archive.Path, "")
			}
			var message Message
			if preview != nil || !errors.Is(err, cause) || !errors.As(err, &message) || message.Key != "verification_incomplete" {
				t.Fatal("interrupted verification published a preview or reported corrupt data")
			}
		}
		cancel()
	}
	broken := filepath.Join(t.TempDir(), "broken.wgg")
	if err := os.WriteFile(broken, []byte("synthetic-invalid-archive"), 0600); err != nil {
		t.Fatal(err)
	}
	_, _, err = s.Stage(context.Background(), broken, "")
	var message Message
	if err == nil || !errors.As(err, &message) || message.Key == "verification_incomplete" {
		t.Fatal("invalid archive was treated as an incomplete check")
	}
}
