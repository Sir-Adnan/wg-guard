package backup

import "testing"

func TestFailedMigrationPromotionPreservesSharedAdmission(t *testing.T) {
	s, _ := newService(t)
	first, err := s.OpenData(false)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := s.OpenData(false)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if err := first.RequireExclusive(); err == nil {
		t.Fatal("promotion evicted a live reader")
	}
	third, err := s.OpenData(false)
	if err != nil {
		t.Fatal("failed promotion left admission locked")
	}
	third.Close()
	second.Close()
	if err := first.RequireExclusive(); err != nil {
		t.Fatal(err)
	}
	if other, err := s.OpenData(false); err == nil {
		other.Close()
		t.Fatal("reader entered the exclusive migration window")
	}
	if err := first.Share(); err != nil {
		t.Fatal(err)
	}
	other, err := s.OpenData(false)
	if err != nil {
		t.Fatal(err)
	}
	other.Close()
}
