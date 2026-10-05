package backup

import (
	"container/heap"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type ArchivePage struct {
	Items      []ArchiveInfo
	NextCursor string
}

type archiveCursor struct {
	Time time.Time `json:"time"`
	Name string    `json:"name"`
}

// ListPage scans the filesystem in fixed batches and retains only limit+1
// candidates. The (mtime,name) cursor tolerates deletion/newer creation between
// pages and avoids retaining an unbounded list for the web panel.
func (s *Service) ListPage(limit int, cursor string) (ArchivePage, error) {
	if limit != 25 && limit != 50 && limit != 100 {
		limit = 25
	}
	var after archiveCursor
	if cursor != "" {
		if len(cursor) > 512 {
			return ArchivePage{}, safetyError("archive_cursor", nil)
		}
		raw, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil || json.Unmarshal(raw, &after) != nil || after.Time.IsZero() || !validArchiveName(after.Name) {
			return ArchivePage{}, safetyError("archive_cursor", nil)
		}
	}
	dir, err := os.Open(s.localDir())
	if os.IsNotExist(err) {
		return ArchivePage{}, nil
	}
	if err != nil {
		return ArchivePage{}, err
	}
	defer dir.Close()
	items := archiveHeap{}
	for {
		entries, err := dir.ReadDir(64)
		for _, entry := range entries {
			if !validArchiveName(entry.Name()) || !entry.Type().IsRegular() {
				continue
			}
			stat, err := entry.Info()
			if err != nil {
				continue
			}
			item := ArchiveInfo{Name: entry.Name(), Path: filepath.Join(s.localDir(), entry.Name()), Size: stat.Size(), ModTime: stat.ModTime()}
			if cursor != "" && !archiveNewer(ArchiveInfo{Name: after.Name, ModTime: after.Time}, item) {
				continue
			}
			heap.Push(&items, item)
			if len(items) > limit+1 {
				heap.Pop(&items)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return ArchivePage{}, err
		}
	}
	sort.Slice(items, func(i, j int) bool { return archiveNewer(items[i], items[j]) })
	page := ArchivePage{Items: items}
	if len(items) > limit {
		page.Items = items[:limit]
		last := page.Items[limit-1]
		raw, _ := json.Marshal(archiveCursor{Time: last.ModTime, Name: last.Name})
		page.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	for i := range page.Items {
		page.Items[i].Encrypted = fileEncrypted(page.Items[i].Path)
	}
	return page, nil
}

func archiveNewer(a, b ArchiveInfo) bool {
	if a.ModTime.Equal(b.ModTime) {
		return a.Name > b.Name
	}
	return a.ModTime.After(b.ModTime)
}

// The oldest candidate is the heap root and is evicted first.
type archiveHeap []ArchiveInfo

func (h archiveHeap) Len() int           { return len(h) }
func (h archiveHeap) Less(i, j int) bool { return archiveNewer(h[j], h[i]) }
func (h archiveHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *archiveHeap) Push(v any)        { *h = append(*h, v.(ArchiveInfo)) }
func (h *archiveHeap) Pop() any          { n := len(*h) - 1; v := (*h)[n]; *h = (*h)[:n]; return v }
