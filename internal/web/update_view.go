package web

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/auth"
	"github.com/Sir-Adnan/wg-guard/internal/distribution"
	"github.com/Sir-Adnan/wg-guard/internal/install"
	"github.com/Sir-Adnan/wg-guard/internal/updatequeue"
)

type updatesData struct {
	Available                         bool
	ReleasesKnown                     bool
	Active                            bool
	Error                             string
	Releases                          []distribution.Release
	Cores                             []install.CoreBundle
	Status                            updatequeue.Status
	PanelVersion                      string
	ToolsVersion                      string
	Enhanced                          bool
	Inventory                         updatequeue.Inventory
	InventoryKnown                    bool
	InventoryStale                    bool
	History                           []updatequeue.Status
	CatalogAt                         time.Time
	CatalogStale                      bool
	Tab                               string
	VersionTab                        string
	Page                              int
	PageSize                          int
	HasNext                           bool
	PreviousURL, NextURL              string
	Search                            string
	NewerOnly                         bool
	Rows                              []releaseRow
	Selected                          *releaseRow
	SelectedCore                      *install.CoreBundle
	NewVersion                        bool
	KernelProfiles, UserspaceProfiles int
	CanManage                         bool
	Scope                             string
	Ready                             bool
	HistoryPage                       int
	HistoryPrevious, HistoryNext      string
	CatalogRateLimited                bool
	RecoveryAllowed                   bool
}

type releaseRow struct {
	Tag, Notes, URL, SelectURL  string
	Published                   time.Time
	Installed, Newer, Downgrade bool
}

func (s *Server) updatesData(r *http.Request) updatesData {
	d := s.updateRuntimeData()
	d.CanManage = maintenanceCan(r, auth.ScopeUpdateManage)
	d.Tab = r.URL.Query().Get("tab")
	switch d.Tab {
	case "versions", "operations", "recovery":
	default:
		d.Tab = "overview"
	}
	d.VersionTab = r.URL.Query().Get("component")
	if d.VersionTab != "core" {
		d.VersionTab = "panel"
	}
	d.Page, _ = strconv.Atoi(r.URL.Query().Get("page"))
	if d.Page < 1 || d.Page > 100 {
		d.Page = 1
	}
	d.PageSize, _ = strconv.Atoi(r.URL.Query().Get("size"))
	if d.PageSize != 20 && d.PageSize != 30 {
		d.PageSize = 10
	}
	d.Search = strings.TrimSpace(r.URL.Query().Get("q"))
	if len(d.Search) > 128 {
		d.Search = d.Search[:128]
	}
	d.NewerOnly = r.URL.Query().Get("newer") == "1"
	d.Scope = r.URL.Query().Get("scope")
	if d.Scope != "all" {
		d.Scope = "panel"
	}
	if d.VersionTab == "core" {
		d.Scope = "core"
	}
	s.loadUpdateCatalog(r, &d)
	s.updateSelection(r, &d)
	if p := d.Inventory.Preflight; p != nil && p.Ready && time.Since(p.CheckedAt) < 10*time.Minute {
		expected := updatequeue.Input{Operation: updatequeue.Operation(d.Scope)}
		if d.Scope != "core" && d.Selected != nil {
			expected.Channel = "release"
			expected.Ref = d.Selected.Tag
		}
		if d.Scope == "all" {
			expected.Core = d.Cores[0].ID
		}
		if d.Scope == "core" && d.SelectedCore != nil {
			expected.Core = d.SelectedCore.ID
		}
		d.Ready = p.Input == expected
	}
	d.HistoryPage, _ = strconv.Atoi(r.URL.Query().Get("history_page"))
	if d.HistoryPage < 1 || d.HistoryPage > 10 {
		d.HistoryPage = 1
	}
	start := (d.HistoryPage - 1) * 10
	if d.HistoryPage > 1 {
		d.HistoryPrevious = "/updates?tab=operations&history_page=" + strconv.Itoa(d.HistoryPage-1)
	}
	if start+10 < len(d.History) {
		d.HistoryNext = "/updates?tab=operations&history_page=" + strconv.Itoa(d.HistoryPage+1)
	}
	if start < len(d.History) {
		d.History = d.History[start:min(start+10, len(d.History))]
	} else {
		d.History = nil
	}
	if s.DB != nil {
		_ = s.DB.QueryRow(`SELECT COUNT(*) FILTER (WHERE backend_mode='kernel'), COUNT(*) FILTER (WHERE backend_mode='userspace') FROM tunnel_interfaces`).Scan(&d.KernelProfiles, &d.UserspaceProfiles)
	}
	return d
}

func (s *Server) updateRuntimeData() updatesData {
	d := updatesData{PanelVersion: s.Version, ToolsVersion: s.ToolsVersion}
	if s.UpdateQueue != nil {
		d.Available = s.UpdateQueue.Available()
		d.Enhanced = s.UpdateQueue.Enhanced()
		if inventory, err := s.UpdateQueue.Inventory(); err == nil && !inventory.ObservedAt.IsZero() {
			d.Inventory = inventory
			d.InventoryKnown = true
			d.InventoryStale = time.Since(inventory.ObservedAt) > 15*time.Minute
			d.RecoveryAllowed = (inventory.RecoveryOperation == "update" || inventory.RecoveryOperation == "rollback") && inventory.Recovery != "restore-required" && inventory.Recovery != "unreadable"
		}
		d.History, _ = s.UpdateQueue.History()
		if status, err := s.UpdateQueue.Status(); err == nil {
			d.Status = status
			d.Active = status.State == updatequeue.StateQueued || status.State == updatequeue.StateRunning || status.State == updatequeue.StateScheduled
		}
	}
	d.Cores = install.ReviewedCoreBundles()
	return d
}

func (s *Server) loadUpdateCatalog(r *http.Request, d *updatesData) {
	if s.UpdateCatalog != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		page, size := d.Page, d.PageSize
		if page == 0 {
			page = 1
		}
		if size == 0 {
			size = 10
		}
		result, at, stale, err := s.updateCache.page(ctx, s.UpdateCatalog, page, size, r.URL.Query().Get("refresh") == "1")
		var httpErr *distribution.HTTPStatusError
		d.CatalogRateLimited = errors.As(err, &httpErr) && (httpErr.RateLimited || httpErr.Code == 429)
		d.Releases = result.Releases
		d.CatalogAt = at
		d.CatalogStale = stale
		d.ReleasesKnown = err == nil || stale
		d.HasNext = result.HasNext
		for _, release := range result.Releases {
			if d.Search != "" && !strings.Contains(strings.ToLower(release.Tag), strings.ToLower(d.Search)) {
				continue
			}
			row := releaseView(release, s.Version)
			if d.NewerOnly && !row.Newer {
				continue
			}
			selectionQuery := r.URL.Query()
			selectionQuery.Del("refresh")
			selectionQuery.Set("tab", "versions")
			selectionQuery.Set("version", row.Tag)
			row.SelectURL = "/updates?" + selectionQuery.Encode()
			d.Rows = append(d.Rows, row)
		}
		latest := s.updateCache.latestTag()
		comparison, known := compareRelease(latest, s.Version)
		d.NewVersion = known && comparison > 0
		query := r.URL.Query()
		query.Del("refresh")
		query.Set("tab", "versions")
		query.Set("size", strconv.Itoa(size))
		if page > 1 {
			query.Set("page", strconv.Itoa(page-1))
			d.PreviousURL = "/updates?" + query.Encode()
		}
		if d.HasNext && page < 100 {
			query.Set("page", strconv.Itoa(page+1))
			d.NextURL = "/updates?" + query.Encode()
		}
	}
}

func releaseView(release distribution.Release, current string) releaseRow {
	if len(release.Body) > 8192 {
		release.Body = strings.ToValidUTF8(release.Body[:8192], "")
	}
	comparison, known := compareRelease(release.Tag, current)
	published, _ := time.Parse(time.RFC3339, release.PublishedAt)
	return releaseRow{Tag: release.Tag, Notes: release.Body, URL: "https://github.com/Sir-Adnan/wg-guard/releases/tag/" + url.PathEscape(release.Tag), Published: published, Installed: release.Tag == current, Newer: known && comparison > 0, Downgrade: known && comparison < 0}
}

func (s *Server) updateSelection(r *http.Request, d *updatesData) {
	if core := r.URL.Query().Get("core"); core != "" {
		if b, err := install.SelectCore(core); err == nil && b.ID == core {
			d.SelectedCore = &b
		}
	}
	tag := r.URL.Query().Get("version")
	if tag == "" && len(d.Releases) > 0 {
		tag = d.Releases[0].Tag
	}
	for _, release := range d.Releases {
		if release.Tag == tag {
			row := releaseView(release, s.Version)
			d.Selected = &row
			return
		}
	}
	if source, ok := s.UpdateCatalog.(exactReleaseCatalog); ok && len(tag) <= 128 && tag != "" && tag != "latest" {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		if release, err := source.ReleaseByTag(ctx, tag); err == nil {
			row := releaseView(release, s.Version)
			d.Selected = &row
		}
	}
}
