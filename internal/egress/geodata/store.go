package geodata

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/egress"
)

// Kind names one of the two data files.
type Kind string

const (
	KindGeosite Kind = "geosite"
	KindGeoIP   Kind = "geoip"
)

// MaxFileBytes bounds one downloaded or uploaded data file.
const MaxFileBytes = 48 << 20

// Default sources: the v2fly community lists published as release assets.
// They are used only when an operator presses "download"; nothing is fetched
// automatically (docs/product/requirements.md: no automatic updates).
const (
	DefaultGeositeURL = "https://github.com/v2fly/domain-list-community/releases/latest/download/dlc.dat"
	DefaultGeoIPURL   = "https://github.com/v2fly/geoip/releases/latest/download/geoip.dat"
)

// Info describes one installed file.
type Info struct {
	Kind       Kind
	Present    bool
	Size       int64
	ModTime    time.Time
	Categories int
}

// Store keeps the two files under one directory of the data volume and
// caches the last materialization (one entry per kind) keyed by file
// identity and the requested categories.
type Store struct {
	Dir    string
	Client *http.Client

	mu    sync.Mutex
	cache map[Kind]cacheEntry
	names map[Kind]namesEntry
}

type fileID struct {
	size int64
	mod  time.Time
}

type cacheEntry struct {
	id      fileID
	wanted  string
	domains map[string][]egress.Matcher
	ips     map[string][]netip.Prefix
}

type namesEntry struct {
	id    fileID
	names []string
}

// NewStore returns a store rooted at dir.
func NewStore(dir string) *Store {
	return &Store{Dir: dir, cache: map[Kind]cacheEntry{}, names: map[Kind]namesEntry{}}
}

func (s *Store) path(k Kind) string { return filepath.Join(s.Dir, string(k)+".dat") }

func (s *Store) stat(k Kind) (fileID, bool, error) {
	st, err := os.Stat(s.path(k))
	if errors.Is(err, os.ErrNotExist) {
		return fileID{}, false, nil
	}
	if err != nil {
		return fileID{}, false, err
	}
	return fileID{size: st.Size(), mod: st.ModTime()}, true, nil
}

func (s *Store) read(k Kind) ([]byte, error) {
	f, err := os.Open(s.path(k))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, MaxFileBytes+1))
}

// Info reports both files.
func (s *Store) Info() []Info {
	var out []Info
	for _, k := range []Kind{KindGeosite, KindGeoIP} {
		info := Info{Kind: k}
		if id, ok, err := s.stat(k); err == nil && ok {
			info.Present, info.Size, info.ModTime = true, id.size, id.mod
			if names, err := s.Categories(k); err == nil {
				info.Categories = len(names)
			}
		}
		out = append(out, info)
	}
	return out
}

// Categories lists the categories of one installed file (cached by identity).
func (s *Store) Categories(k Kind) ([]string, error) {
	id, ok, err := s.stat(k)
	if err != nil || !ok {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, hit := s.names[k]; hit && e.id == id {
		return e.names, nil
	}
	data, err := s.read(k)
	if err != nil {
		return nil, err
	}
	names, err := Categories(data)
	if err != nil {
		return nil, err
	}
	s.names[k] = namesEntry{id: id, names: names}
	return names, nil
}

func wantedKey(wanted []string) string {
	w := append([]string(nil), wanted...)
	sort.Strings(w)
	return strings.Join(w, ",")
}

// Domains materializes geosite categories. A missing file yields an empty map.
func (s *Store) Domains(wanted []string) (map[string][]egress.Matcher, error) {
	if len(wanted) == 0 {
		return nil, nil
	}
	id, ok, err := s.stat(KindGeosite)
	if err != nil || !ok {
		return nil, err
	}
	key := wantedKey(wanted)
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, hit := s.cache[KindGeosite]; hit && e.id == id && e.wanted == key {
		return e.domains, nil
	}
	data, err := s.read(KindGeosite)
	if err != nil {
		return nil, err
	}
	out, err := Geosite(data, wanted)
	if err != nil {
		return nil, err
	}
	s.cache[KindGeosite] = cacheEntry{id: id, wanted: key, domains: out}
	return out, nil
}

// Prefixes materializes geoip categories. A missing file yields an empty map.
func (s *Store) Prefixes(wanted []string) (map[string][]netip.Prefix, error) {
	if len(wanted) == 0 {
		return nil, nil
	}
	id, ok, err := s.stat(KindGeoIP)
	if err != nil || !ok {
		return nil, err
	}
	key := wantedKey(wanted)
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, hit := s.cache[KindGeoIP]; hit && e.id == id && e.wanted == key {
		return e.ips, nil
	}
	data, err := s.read(KindGeoIP)
	if err != nil {
		return nil, err
	}
	out, err := GeoIP(data, wanted)
	if err != nil {
		return nil, err
	}
	s.cache[KindGeoIP] = cacheEntry{id: id, wanted: key, ips: out}
	return out, nil
}

// Install validates data and atomically replaces one file.
func (s *Store) Install(k Kind, data []byte) (int, error) {
	if k != KindGeosite && k != KindGeoIP {
		return 0, fmt.Errorf("geodata: unknown kind")
	}
	if len(data) == 0 || len(data) > MaxFileBytes {
		return 0, ErrMalformed
	}
	names, err := Categories(data)
	if err != nil || len(names) == 0 {
		return 0, ErrMalformed
	}
	if err := os.MkdirAll(s.Dir, 0o750); err != nil {
		return 0, err
	}
	tmp, err := os.CreateTemp(s.Dir, "."+string(k)+"-*.tmp")
	if err != nil {
		return 0, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return 0, err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return 0, err
	}
	if err := tmp.Close(); err != nil {
		return 0, err
	}
	if err := os.Chmod(tmpName, 0o640); err != nil {
		return 0, err
	}
	if err := os.Rename(tmpName, s.path(k)); err != nil {
		return 0, err
	}
	s.mu.Lock()
	delete(s.cache, k)
	delete(s.names, k)
	s.mu.Unlock()
	return len(names), nil
}

// Remove deletes one installed file.
func (s *Store) Remove(k Kind) error {
	err := os.Remove(s.path(k))
	if errors.Is(err, os.ErrNotExist) {
		err = nil
	}
	s.mu.Lock()
	delete(s.cache, k)
	delete(s.names, k)
	s.mu.Unlock()
	return err
}

// ErrDownload reports a failed fetch; the panel shows a fixed message.
var ErrDownload = errors.New("geodata: download failed")

// Download fetches an HTTPS URL (HTTPS redirects only, bounded size and time)
// and installs it.
func (s *Store) Download(ctx context.Context, k Kind, rawURL string) (int, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return 0, ErrDownload
	}
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}
	c := *client
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || req.URL.Scheme != "https" {
			return ErrDownload
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return 0, ErrDownload
	}
	resp, err := c.Do(req)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrDownload, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("%w: HTTP %d", ErrDownload, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxFileBytes+1))
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrDownload, err)
	}
	if len(data) > MaxFileBytes {
		return 0, ErrMalformed
	}
	return s.Install(k, data)
}
