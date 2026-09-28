package install

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/network"
)

var ownedInterfaceName = regexp.MustCompile(`^awg[0-9]+$`)

type runtimeLink struct {
	Name string `json:"ifname"`
	Info struct {
		Kind string `json:"info_kind"`
	} `json:"linkinfo"`
}

type ownedInterface struct {
	name string
	mode string
}

// Stopping the service does not remove kernel links. Read the durable
// ownership record before deleting data, and never delete a different link
// type that happens to reuse an awgN name.
func removeOwnedRuntimeInterfaces(ctx context.Context, h Host, dataDir string) error {
	path := filepath.Join(dataDir, "wg-guard.db")
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return refuseUnattributedLinks(ctx, h)
	} else if err != nil {
		return err
	}
	db, err := database.Open(path, database.Options{ReadOnly: true})
	if err != nil {
		return refuseUnattributedLinks(ctx, h)
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, "SELECT name, backend_mode FROM tunnel_interfaces ORDER BY name")
	if err != nil {
		return refuseUnattributedLinks(ctx, h)
	}
	var owned []ownedInterface
	for rows.Next() {
		var item ownedInterface
		if err := rows.Scan(&item.name, &item.mode); err != nil {
			rows.Close()
			return err
		}
		owned = append(owned, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, item := range owned {
		if !ownedInterfaceName.MatchString(item.name) || len(item.name) > 15 {
			return fmt.Errorf("invalid recorded tunnel interface name")
		}
		link, exists, err := inspectRuntimeLink(ctx, h, item.name)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		if item.mode != "kernel" || link.Info.Kind != "amneziawg" {
			return fmt.Errorf("%s is still live but cannot be safely removed as an owned kernel link", item.name)
		}
		if err := h.Run(ctx, []string{"ip", "link", "del", "dev", item.name}, 15*time.Second); err != nil {
			return fmt.Errorf("delete %s: %w", item.name, err)
		}
		if _, exists, err := inspectRuntimeLink(ctx, h, item.name); err != nil {
			return fmt.Errorf("verify removal of %s: %w", item.name, err)
		} else if exists {
			return fmt.Errorf("verify removal of %s: link is still live", item.name)
		}
	}
	// A stale tunnel that is not in the ownership record must not silently
	// survive a successful uninstall, nor may we guess that it is ours.
	return refuseUnattributedLinks(ctx, h)
}

func inspectRuntimeLink(ctx context.Context, h Host, name string) (runtimeLink, bool, error) {
	var link runtimeLink
	out, err := h.Output(ctx, []string{"ip", "-j", "-d", "link", "show", "dev", name}, 15*time.Second)
	if err != nil {
		if network.LinkMissing(err, err.Error()) {
			return link, false, nil
		}
		return link, false, fmt.Errorf("inspect interface %s: %w", name, err)
	}
	var links []runtimeLink
	if err := json.Unmarshal([]byte(out), &links); err != nil || len(links) != 1 || links[0].Name != name {
		return link, false, fmt.Errorf("inspect interface %s: unexpected link metadata", name)
	}
	return links[0], true, nil
}

// If the database is missing or corrupt, a reset can proceed only after
// proving no awgN tunnel link remains. Never guess which live peer is owned.
func refuseUnattributedLinks(ctx context.Context, h Host) error {
	out, err := h.Output(ctx, []string{"ip", "-j", "-d", "link", "show"}, 15*time.Second)
	if err != nil {
		return fmt.Errorf("cannot inspect live links without the ownership database: %w", err)
	}
	var links []runtimeLink
	if err := json.Unmarshal([]byte(out), &links); err != nil {
		return fmt.Errorf("cannot parse live links without the ownership database: %w", err)
	}
	for _, link := range links {
		if ownedInterfaceName.MatchString(link.Name) && (link.Info.Kind == "amneziawg" || link.Info.Kind == "tun") {
			return fmt.Errorf("live %s tunnel has no readable ownership record; restore the database or remove it explicitly before uninstall", link.Name)
		}
	}
	return nil
}
