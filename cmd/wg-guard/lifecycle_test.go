package main

import (
	"github.com/Sir-Adnan/wg-guard/internal/distribution"
	"testing"
)

func TestParseLifecycleSources(t *testing.T) {
	for _, args := range [][]string{{"--release", "v1", "--yes"}, {"--commit", "main", "--yes"}} {
		o, err := parseInstallOptions(args)
		if err != nil {
			t.Fatal(err)
		}
		if o.Selection.Channel == "" {
			t.Fatal("explicit source lost")
		}
	}
	if _, err := parseInstallOptions([]string{"--release", "v1", "--commit", "main", "--yes"}); err == nil {
		t.Fatal("ambiguous source accepted")
	}
}
func TestUpdateSelectionIsFreshAndExplicit(t *testing.T) {
	o, err := parseUpdateOptions(nil)
	if err != nil {
		t.Fatal(err)
	}
	if o.Selection != (distribution.Selection{Channel: "release", Ref: "latest"}) {
		t.Fatalf("default source %+v", o.Selection)
	}
	o, err = parseUpdateOptions([]string{"--binary", "/tmp/candidate", "--image", "local:test", "--local-image"})
	if err != nil {
		t.Fatal(err)
	}
	if !o.LocalImage || o.Selection.Channel != "" {
		t.Fatal("local path lost")
	}
	if _, err = parseUpdateOptions([]string{"--rollback", "--commit", "main"}); err == nil {
		t.Fatal("rollback mixed with acquisition")
	}
}
