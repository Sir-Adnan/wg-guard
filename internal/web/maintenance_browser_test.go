package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/distribution"
	"github.com/Sir-Adnan/wg-guard/internal/updatequeue"
)

func TestBrowserMaintenance(t *testing.T) {
	node := os.Getenv("WG_TEST_BROWSER_NODE")
	if node == "" {
		t.Skip("opt-in browser fixture")
	}
	e := newEnv(t)
	e.seedOwner()
	q := wireUpdateQueue(t, e)
	e.srv.Version = "v0.1.6"
	var releases []distribution.Release
	for n := 9; n >= 0; n-- {
		releases = append(releases, distribution.Release{Tag: fmt.Sprintf("v0.1.%d", n), PublishedAt: "2026-10-01T10:00:00Z", Body: "Verified release details.\nتوضیحات انتشار و جزئیات تغییرات برای مدیر سرور."})
	}
	e.srv.UpdateCatalog = fixedReleaseCatalog{releases: releases}
	i := updatequeue.Inventory{Schema: 1, ObservedAt: time.Now().UTC(), Mode: "docker", PanelVersion: "v0.1.6", PanelCommit: strings.Repeat("a", 40), ManagerVersion: "v0.1.6", ManagerCommit: strings.Repeat("a", 40), OS: "ubuntu", OSVersion: "24.04", Architecture: "amd64", Kernel: "6.8.0-generic", Bundle: "awg-2026-09", ToolsVersion: "v3.1.20260812", ToolsLocation: "container", ModuleLoaded: true, ModuleIdentity: "matches-disk", Userspace: "verified", Bridge: "active", DiskFreeBytes: 4 << 30}
	if err := q.PublishInventory(i); err != nil {
		t.Fatal(err)
	}
	cookie := e.login("owner")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_qa/finish" {
			req, err := q.Claim(r.Context())
			if err != nil {
				http.Error(w, "fixture claim", 500)
				return
			}
			_ = q.Record(req, "host", "preflight", updatequeue.StateSucceeded)
			input := req.Input
			input.Operation, input.Target = input.Target, ""
			i.Preflight = &updatequeue.Preflight{Input: input, CheckedAt: time.Now().UTC(), Commit: strings.Repeat("b", 40), Ready: true, Checks: []updatequeue.Check{{Code: "platform", State: "pass"}, {Code: "backup", State: "warn"}}}
			if err := q.PublishInventory(i); err != nil {
				http.Error(w, "fixture inventory", 500)
				return
			}
			if err := q.Finish(r.Context(), req, nil); err != nil {
				http.Error(w, "fixture finish", 500)
				return
			}
			w.WriteHeader(204)
			return
		}
		e.handler.ServeHTTP(w, r)
	}))
	defer server.Close()
	payload, _ := json.Marshal(map[string]string{"url": server.URL, "session": cookie.Value, "csrf": deriveCSRF(cookie.Value)})
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, filepath.Join("..", "..", "scripts", "test-web-maintenance.cjs"))
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stdout
	if err := cmd.Run(); err != nil {
		t.Fatalf("maintenance browser: %v", err)
	}
}
