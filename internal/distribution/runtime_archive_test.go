package distribution

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"testing"
)

type archiveEntry struct {
	name string
	body []byte
}

func gzipTar(t *testing.T, entries []archiveEntry) []byte {
	t.Helper()
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		if err := tw.WriteHeader(&tar.Header{Name: e.name, Mode: 0644, Size: int64(len(e.body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(e.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func imageExport(config string, manifests int, alterManifest bool) ([]archiveEntry, string) {
	manifest := []byte(`{"schemaVersion":2,"config":{"digest":"` + config + `"},"layers":[]}`)
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(manifest))
	stored := manifest
	if alterManifest {
		stored = bytes.Replace(manifest, []byte(config), []byte("sha256:"+strings.Repeat("9", 64)), 1)
	}
	refs := make([]string, manifests)
	for i := range refs {
		refs[i] = `{"mediaType":"application/vnd.oci.image.manifest.v1+json","digest":"` + digest + `","size":1}`
	}
	return []archiveEntry{
		{"blobs/sha256/" + strings.Repeat("1", 64), bytes.Repeat([]byte{0}, 128<<10)}, // a layer: skipped, never buffered
		{"blobs/sha256/" + strings.TrimPrefix(digest, "sha256:"), stored},
		{"index.json", []byte(`{"schemaVersion":2,"manifests":[` + strings.Join(refs, ",") + `]}`)},
		{"manifest.json", []byte(`[{"Config":"blobs/sha256/` + strings.TrimPrefix(config, "sha256:") + `","Layers":[]}]`)},
	}, digest
}

func TestInspectRuntimeArchiveBindsBothDockerIdentities(t *testing.T) {
	config := "sha256:" + strings.Repeat("c", 64)
	entries, manifest := imageExport(config, 1, false)
	body := gzipTar(t, entries)
	id, err := InspectRuntimeArchive(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	if id.ConfigDigest != config || id.ManifestDigest != manifest || id.ArchiveSize != int64(len(body)) ||
		id.ArchiveSHA256 != fmt.Sprintf("%x", sha256.Sum256(body)) {
		t.Fatalf("identity = %+v", id)
	}
}

func TestInspectRuntimeArchiveAcceptsLegacyExportWithoutIndex(t *testing.T) {
	config := "sha256:" + strings.Repeat("c", 64)
	body := gzipTar(t, []archiveEntry{{"manifest.json", []byte(`[{"Config":"` + strings.Repeat("c", 64) + `.json","Layers":[]}]`)}})
	id, err := InspectRuntimeArchive(bytes.NewReader(body), int64(len(body)))
	if err != nil || id.ConfigDigest != config || id.ManifestDigest != "" {
		t.Fatalf("legacy identity = %+v, %v", id, err)
	}
}

func TestInspectRuntimeArchiveRefusesAmbiguousOrAlteredExports(t *testing.T) {
	config := "sha256:" + strings.Repeat("c", 64)
	two, _ := imageExport(config, 2, false)
	altered, _ := imageExport(config, 1, true)
	valid, _ := imageExport(config, 1, false)
	for name, body := range map[string][]byte{
		"two images":       gzipTar(t, two),
		"altered manifest": gzipTar(t, altered),
		"no legacy index":  gzipTar(t, valid[:3]),
		"not gzip":         []byte("synthetic runtime archive"),
	} {
		if _, err := InspectRuntimeArchive(bytes.NewReader(body), int64(len(body))); err == nil {
			t.Errorf("%s: archive admitted", name)
		}
	}
	body := gzipTar(t, valid)
	if _, err := InspectRuntimeArchive(bytes.NewReader(body), int64(len(body))-1); err == nil {
		t.Error("archive above its size limit admitted")
	}
}

// WGG_RUNTIME_ARCHIVE points at a downloaded public runtime archive and
// WGG_RUNTIME_IDS at "config manifest" digests to check a real export.
func TestInspectPublishedRuntimeArchive(t *testing.T) {
	path, want := os.Getenv("WGG_RUNTIME_ARCHIVE"), strings.Fields(os.Getenv("WGG_RUNTIME_IDS"))
	if path == "" || len(want) != 2 {
		t.Skip("set WGG_RUNTIME_ARCHIVE and WGG_RUNTIME_IDS to inspect a published archive")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	id, err := InspectRuntimeArchive(f, RuntimeArchiveLimit)
	if err != nil || id.ConfigDigest != want[0] || id.ManifestDigest != want[1] {
		t.Fatalf("published identity = %+v, %v", id, err)
	}
}
