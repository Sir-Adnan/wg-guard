package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/install"
)

func TestCoreCommandCatalogAndInvalidSelection(t *testing.T) {
	for _, args := range [][]string{{"recommended"}, {"latest-compatible"}, {"exact", "awg-2026-08"}} {
		var out bytes.Buffer
		if err := runCoreWithHost(args, nil, &out); err != nil {
			t.Fatal(err)
		}
		var b install.CoreBundle
		if err := json.Unmarshal(out.Bytes(), &b); err != nil || b.ID != "awg-2026-08" {
			t.Fatal("invalid catalog response")
		}
	}
	for _, args := range [][]string{{"exact"}, {"exact", "upstream-head"}, {"recommended", "extra"}} {
		if err := runCoreWithHost(args, nil, &bytes.Buffer{}); err == nil {
			t.Fatalf("invalid core args accepted: %v", args)
		}
	}
}

func TestInstallerExplicitPortFlagsAndInvalidValues(t *testing.T) {
	o, err := parseInstallOptions([]string{"--yes", "--domain", "panel.example.com", "--panel-port", "8080", "--public-ip", "8.8.8.8", "--prerequisites", "check", "--core", "recommended"})
	if err != nil {
		t.Fatal(err)
	}
	p, err := o.Plan.Resolve()
	if err != nil || p.PanelPort != 8080 || !p.PanelPortExplicit || o.Prerequisites != install.PrerequisitesCheck {
		t.Fatalf("explicit flags lost: %+v %v", p, err)
	}
	for _, args := range [][]string{{"--panel-port"}, {"--public-ip"}, {"--core"}, {"--prerequisites", "unsupported"}, {"--panel-port", "0"}} {
		o, err := parseInstallOptions(args)
		if err == nil {
			_, err = o.Plan.Resolve()
		}
		if err == nil {
			t.Fatalf("invalid flag accepted: %s", strings.Join(args, " "))
		}
	}
}
