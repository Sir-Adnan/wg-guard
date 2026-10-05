package backup

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestArchiveCursorHasStableTiesAndToleratesConcurrentChanges(t *testing.T) {
	s, _ := newService(t)
	if err := os.MkdirAll(s.localDir(), 0700); err != nil {
		t.Fatal(err)
	}
	timestamp := time.Now().UTC().Add(-time.Hour)
	for i := 0; i < 127; i++ {
		path := filepath.Join(s.localDir(), fmt.Sprintf("wg-guard-fixture-%03d.wgg", i))
		if err := os.WriteFile(path, []byte("synthetic-envelope"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, timestamp, timestamp); err != nil {
			t.Fatal(err)
		}
	}
	page, err := s.ListPage(25, "")
	if err != nil || len(page.Items) != 25 || page.NextCursor == "" || page.Items[0].Name != "wg-guard-fixture-126.wgg" {
		t.Fatal("first page order/bound", err)
	}
	seen := map[string]bool{}
	for _, item := range page.Items {
		seen[item.Name] = true
	}
	// Delete an unseen record and add a newer one, as a concurrent retention/run would.
	if err := os.Remove(filepath.Join(s.localDir(), "wg-guard-fixture-000.wgg")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.localDir(), "wg-guard-newer.wgg"), []byte("synthetic-new"), 0600); err != nil {
		t.Fatal(err)
	}
	for page.NextCursor != "" {
		page, err = s.ListPage(25, page.NextCursor)
		if err != nil || len(page.Items) > 25 {
			t.Fatal("page bound", err)
		}
		for _, item := range page.Items {
			if seen[item.Name] || item.Name == "wg-guard-newer.wgg" {
				t.Fatal("cursor duplicated or shifted after concurrent changes")
			}
			seen[item.Name] = true
		}
	}
	if len(seen) != 126 {
		t.Fatalf("lost original records: %d", len(seen))
	}
	if _, err := s.ListPage(25, "invalid-cursor"); err == nil {
		t.Fatal("invalid cursor accepted")
	}
}
