package distribution

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

// RuntimeArchiveRawLimit bounds the uncompressed `docker save` stream, matching
// the release builder's export budget.
const RuntimeArchiveRawLimit int64 = 4 << 30

const (
	maxArchiveMetadata  = 64 << 10
	maxArchiveSmallBlob = 64 << 10
	maxArchiveSmallSet  = 1 << 20
)

// RuntimeArchiveIdentity is what a verified `docker save` archive proves about
// the image Docker will create from it. Docker's classic image store names the
// loaded image by ConfigDigest; the containerd image store names it by the OCI
// ManifestDigest. Both are bound to the archive bytes by ArchiveSHA256.
type RuntimeArchiveIdentity struct {
	ArchiveSHA256  string
	ArchiveSize    int64
	ConfigDigest   string
	ManifestDigest string // "" for a legacy archive without an OCI index
}

// InspectRuntimeArchive hashes the compressed archive and reads its image
// identity in one bounded pass, so the digest and the parsed identity come from
// the same bytes. Layer contents are skipped, never buffered.
func InspectRuntimeArchive(r io.Reader, limit int64) (RuntimeArchiveIdentity, error) {
	var id RuntimeArchiveIdentity
	hash := sha256.New()
	counted := &countingReader{r: io.LimitReader(r, limit+1)}
	raw := io.TeeReader(counted, hash)
	gz, err := gzip.NewReader(raw)
	if err != nil {
		return id, fmt.Errorf("distribution: runtime archive is not gzip")
	}
	defer gz.Close()
	stream := io.LimitReader(gz, RuntimeArchiveRawLimit+1)
	tr := tar.NewReader(stream)
	var index, legacy []byte
	blobs := map[string][]byte{}
	stored := 0
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return id, fmt.Errorf("distribution: runtime archive is not a valid image export")
		}
		name := path.Clean(strings.TrimPrefix(h.Name, "./"))
		switch {
		case h.Typeflag != tar.TypeReg:
			continue
		case name == "index.json":
			if index, err = readBounded(tr, h.Size, maxArchiveMetadata); err != nil {
				return id, err
			}
		case name == "manifest.json":
			if legacy, err = readBounded(tr, h.Size, maxArchiveMetadata); err != nil {
				return id, err
			}
		case strings.HasPrefix(name, "blobs/sha256/") && h.Size <= maxArchiveSmallBlob && stored+int(h.Size) <= maxArchiveSmallSet:
			body, err := readBounded(tr, h.Size, maxArchiveSmallBlob)
			if err != nil {
				return id, err
			}
			blobs[strings.TrimPrefix(name, "blobs/sha256/")] = body
			stored += len(body)
		}
	}
	if _, err := io.Copy(io.Discard, stream); err != nil {
		return id, fmt.Errorf("distribution: runtime archive is not a valid image export")
	}
	if _, err := io.Copy(io.Discard, raw); err != nil {
		return id, err
	}
	if counted.n > limit {
		return id, fmt.Errorf("distribution: runtime archive exceeds its size limit")
	}
	id.ArchiveSHA256 = hex.EncodeToString(hash.Sum(nil))
	id.ArchiveSize = counted.n
	if id.ConfigDigest, err = legacyConfigDigest(legacy); err != nil {
		return id, err
	}
	if index == nil {
		return id, nil
	}
	manifestDigest, err := singleIndexManifest(index)
	if err != nil {
		return id, err
	}
	body, ok := blobs[strings.TrimPrefix(manifestDigest, "sha256:")]
	if !ok || "sha256:"+fmt.Sprintf("%x", sha256.Sum256(body)) != manifestDigest {
		return id, fmt.Errorf("distribution: runtime archive manifest is missing or altered")
	}
	var manifest struct {
		Config struct {
			Digest string `json:"digest"`
		} `json:"config"`
	}
	if json.Unmarshal(body, &manifest) != nil || manifest.Config.Digest != id.ConfigDigest {
		return id, fmt.Errorf("distribution: runtime archive manifest does not reference its image config")
	}
	id.ManifestDigest = manifestDigest
	return id, nil
}

func singleIndexManifest(raw []byte) (string, error) {
	var index struct {
		SchemaVersion int `json:"schemaVersion"`
		Manifests     []struct {
			MediaType string `json:"mediaType"`
			Digest    string `json:"digest"`
		} `json:"manifests"`
	}
	if json.Unmarshal(raw, &index) != nil || index.SchemaVersion != 2 || len(index.Manifests) != 1 {
		return "", fmt.Errorf("distribution: runtime archive must contain exactly one image")
	}
	m := index.Manifests[0]
	if (m.MediaType != "application/vnd.oci.image.manifest.v1+json" && m.MediaType != "application/vnd.docker.distribution.manifest.v2+json") || !imageDigest(m.Digest) {
		return "", fmt.Errorf("distribution: runtime archive has an unsupported image manifest")
	}
	return m.Digest, nil
}

func legacyConfigDigest(raw []byte) (string, error) {
	var entries []struct {
		Config string `json:"Config"`
	}
	if json.Unmarshal(raw, &entries) != nil || len(entries) != 1 {
		return "", fmt.Errorf("distribution: runtime archive must contain exactly one image")
	}
	name := path.Base(entries[0].Config)
	digest := "sha256:" + strings.TrimSuffix(name, ".json")
	if !imageDigest(digest) {
		return "", fmt.Errorf("distribution: runtime archive has an invalid image config")
	}
	return digest, nil
}

func imageDigest(s string) bool {
	return len(s) == 71 && strings.HasPrefix(s, "sha256:") && digestSHA.MatchString(s[7:])
}

func readBounded(r io.Reader, size, limit int64) ([]byte, error) {
	if size < 0 || size > limit {
		return nil, fmt.Errorf("distribution: runtime archive metadata exceeds its limit")
	}
	var b bytes.Buffer
	if _, err := io.CopyN(&b, r, size); err != nil {
		return nil, fmt.Errorf("distribution: runtime archive is truncated")
	}
	return b.Bytes(), nil
}

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}
