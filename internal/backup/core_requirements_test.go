package backup

import (
	"context"
	"testing"
)

func TestOfflineVerificationPreservesReviewedBackendRequirements(t *testing.T) {
	s, _ := newService(t)
	seedMigrationData(t, s)
	if _, err := s.DB.Exec(`UPDATE tunnel_interfaces SET backend_mode='userspace' WHERE id='awg1'`); err != nil {
		t.Fatal(err)
	}
	archive, err := s.Create(context.Background(), CreateOpts{})
	if err != nil {
		t.Fatal(err)
	}
	report, err := VerifyArchive(context.Background(), archive.Path, "")
	if err != nil || report.Inventory.KernelInterfaces != 1 || report.Inventory.UserspaceInterfaces != 1 {
		t.Fatal("offline verification lost supported backend requirements", err)
	}
	// An unclassified engine is not admitted merely because its keys decrypt.
	conn, err := s.DB.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(context.Background(), `PRAGMA ignore_check_constraints=ON`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(context.Background(), `UPDATE tunnel_interfaces SET backend_mode='unreviewed-engine' WHERE id='awg1'`); err != nil {
		t.Fatal(err)
	}
	conn.Close()
	if _, err := s.Create(context.Background(), CreateOpts{}); err == nil {
		t.Fatal("unreviewed backend was admitted as portable data")
	}
}
