package install

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/distribution"
)

func TestUpdateAllCachesManagerThenUpdatesPanelAndCompatibleCore(t *testing.T) {
	base := installedFixture(t, ModeNative)
	contractFixture(base)
	body := "age-encryption.org/v1\nfull update backup"
	h := archiveHost{base, body, "created wg-guard-test.wgg (1 KiB, age-encrypted)\n"}
	digest := sha256.Sum256([]byte("candidate"))
	b := distribution.Build{
		Channel: "commit", Ref: "0123456789abcdef0123456789abcdef01234567",
		Commit: "0123456789abcdef0123456789abcdef01234567", Version: "0.0.0-dev.0123456789ab",
		SHA256: fmt.Sprintf("%x", digest), BinaryPath: "/tmp/candidate",
	}

	if err := UpdateAll(context.Background(), h, FullUpdateOptions{Build: b, Core: "recommended", Stdout: io.Discard}); err != nil {
		t.Fatal(err)
	}
	if string(base.files[ManagerBinaryPath].data) != "candidate" || string(base.files[BinPath].data) != "candidate" {
		t.Fatal("full update did not synchronize manager and panel binaries")
	}
	st, err := LoadState(h)
	if err != nil || st.Version != b.Version || st.Core.Requested.ID != "awg-2026-09" {
		t.Fatalf("full update state = %+v, err %v", st, err)
	}
	j, err := LoadJournal(h)
	if err != nil || j.Operation != "core" || j.Stage != "complete" {
		t.Fatalf("full update did not finish core gate: %+v, err %v", j, err)
	}
}

func TestUpdateAllStopsBeforePanelWhenManagerCandidateIsInvalid(t *testing.T) {
	h := installedFixture(t, ModeNative)
	before := string(h.files[BinPath].data)
	h.files["/tmp/candidate"] = memFile{data: []byte("candidate"), perm: 0o755}
	b := distribution.Build{
		Channel: "commit", Ref: "0123456789abcdef0123456789abcdef01234567",
		Commit: "0123456789abcdef0123456789abcdef01234567", Version: "0.0.0-dev.0123456789ab",
		SHA256: "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", BinaryPath: "/tmp/candidate",
	}
	if err := UpdateAll(context.Background(), h, FullUpdateOptions{Build: b, Core: "recommended", Stdout: io.Discard}); err == nil {
		t.Fatal("invalid manager candidate was accepted")
	}
	if string(h.files[BinPath].data) != before {
		t.Fatal("panel changed after manager admission failed")
	}
}
