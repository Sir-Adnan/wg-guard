package install

import (
	"bytes"
	"testing"
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
