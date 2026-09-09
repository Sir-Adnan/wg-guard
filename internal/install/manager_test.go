package install

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/distribution"
)

func TestUpdateManagerCachesVerifiedBuildWithoutReplacingServiceBinary(t *testing.T) {
	h := newMemHost()
	h.files[BinPath] = memFile{data: []byte("active service binary"), perm: 0o755}
	digest := sha256.Sum256(h.files["/src/wg-guard"].data)
	b := distribution.Build{
		Channel: "commit", Ref: "0123456789abcdef0123456789abcdef01234567",
		Commit: "0123456789abcdef0123456789abcdef01234567", Version: "0.0.0-dev.0123456789ab",
		SHA256: hex.EncodeToString(digest[:]), BinaryPath: "/src/wg-guard",
	}

	if err := UpdateManager(context.Background(), h, ManagerUpdateOptions{Build: b, Stdout: io.Discard}); err != nil {
		t.Fatal(err)
	}
	if got := string(h.files[BinPath].data); got != "active service binary" {
		t.Fatalf("manager update replaced active service binary: %q", got)
	}
	if got := string(h.files[ManagerBinaryPath].data); got != "/src/wg-guard" || h.files[ManagerBinaryPath].perm != 0o755 {
		t.Fatalf("cached manager = %q mode %o", got, h.files[ManagerBinaryPath].perm)
	}
	if h.files[ManagerBuildPath].perm != 0o600 {
		t.Fatalf("manager receipt mode = %o", h.files[ManagerBuildPath].perm)
	}
	var receipt distribution.Build
	if err := json.Unmarshal(h.files[ManagerBuildPath].data, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.BinaryPath != ManagerBinaryPath || receipt.Commit != b.Commit || receipt.SHA256 != b.SHA256 {
		t.Fatalf("manager receipt = %+v", receipt)
	}
	loaded, err := LoadManagerBuild(context.Background(), h)
	if err != nil || loaded != receipt {
		t.Fatalf("loaded manager = %+v, err %v", loaded, err)
	}
}

func TestLoadManagerBuildRejectsTamperedCache(t *testing.T) {
	h := newMemHost()
	digest := sha256.Sum256(h.files["/src/wg-guard"].data)
	b := distribution.Build{
		Channel: "release", Ref: "v1.2.3", Commit: "0123456789abcdef0123456789abcdef01234567",
		Version: "v1.2.3", SHA256: hex.EncodeToString(digest[:]), BinaryPath: "/src/wg-guard",
	}
	if err := UpdateManager(context.Background(), h, ManagerUpdateOptions{Build: b}); err != nil {
		t.Fatal(err)
	}
	h.files[ManagerBinaryPath] = memFile{data: []byte("tampered"), perm: 0o755}
	if _, err := LoadManagerBuild(context.Background(), h); err == nil {
		t.Fatal("tampered cached manager was trusted")
	}
}
