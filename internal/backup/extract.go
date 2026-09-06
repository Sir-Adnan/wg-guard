package backup

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
)

const (
	maxDatabaseBytes int64 = 1 << 30
	maxConfigBytes   int64 = 1 << 20
	maxMetadataBytes int64 = 64 << 10
	maxExpandedBytes int64 = maxDatabaseBytes + maxConfigBytes + maxMetadataBytes + (1 << 20)
)

func memberLimit(name string) int64 {
	switch name {
	case DBMember:
		return maxDatabaseBytes
	case ConfigMember:
		return maxConfigBytes
	case ManifestName:
		return maxMetadataBytes
	case KeyMember:
		return 32
	}
	return 0
}

func readSmall(path string, limit int64) ([]byte, error) {
	st, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() || st.Size() > limit {
		return nil, safetyError("metadata_unsafe", nil)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, safetyError("metadata_limit", nil)
	}
	return b, nil
}

type restoreReader struct {
	ctx context.Context
	r   io.Reader
}

func (r restoreReader) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(b)
}

// extractArchive bounds every decompressed byte, including ignored padding.
// Files stream directly to a private stage; only small metadata enters memory.
func extractArchive(ctx context.Context, r io.Reader, password, dir string) (*Manifest, bool, error) {
	kind, br, err := sniffContainer(restoreReader{ctx, r})
	if err != nil {
		return nil, false, err
	}
	encrypted := kind == "age"
	var src io.Reader = br
	if encrypted {
		src, err = ageDecrypt(br, password)
		if err != nil {
			return nil, true, err
		}
	}
	tr, err := newTarReader(src)
	if err != nil {
		return nil, encrypted, err
	}
	defer tr.gz.Close()
	expanded := &io.LimitedReader{R: tr.gz, N: maxExpandedBytes + 1}
	reader := tar.NewReader(expanded)
	hashes := map[string]string{}
	for {
		hdr, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, encrypted, safetyError("truncated", err)
		}
		limit := memberLimit(hdr.Name)
		if limit == 0 || hdr.Typeflag != tar.TypeReg || hdr.Size < 0 || hdr.Size > limit || hashes[hdr.Name] != "" {
			return nil, encrypted, safetyError("member_unsafe", nil)
		}
		f, err := os.OpenFile(filepath.Join(dir, hdr.Name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return nil, encrypted, err
		}
		h := sha256.New()
		n, err := io.Copy(io.MultiWriter(f, h), reader)
		if err == nil {
			err = f.Sync()
		}
		closeErr := f.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil || n != hdr.Size {
			return nil, encrypted, safetyError("member_incomplete", err)
		}
		if hdr.Name == KeyMember && n != 32 {
			return nil, encrypted, safetyError("key_size", nil)
		}
		hashes[hdr.Name] = hex.EncodeToString(h.Sum(nil))
	}
	if _, err := io.Copy(io.Discard, expanded); err != nil {
		return nil, encrypted, safetyError("container_checksum", err)
	}
	if expanded.N <= 0 {
		return nil, encrypted, safetyError("expanded_limit", nil)
	}
	raw, err := readSmall(filepath.Join(dir, ManifestName), maxMetadataBytes)
	if err != nil {
		return nil, encrypted, safetyError("manifest_missing", nil)
	}
	var manifest Manifest
	if json.Unmarshal(raw, &manifest) == nil && manifest.Schema > SchemaVersion {
		return nil, encrypted, safetyError("schema_newer", nil)
	}
	if json.Unmarshal(raw, &manifest) != nil || manifest.Schema != SchemaVersion || len(manifest.Files) != len(hashes)-1 || manifest.Files[DBMember] == "" {
		return nil, encrypted, safetyError("manifest_invalid", nil)
	}
	for name, want := range manifest.Files {
		if name == ManifestName || !validHash(want) || hashes[name] != want {
			return nil, encrypted, safetyError("archive_checksum", nil)
		}
	}
	return &manifest, encrypted, nil
}

func validHash(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == 32 && hex.EncodeToString(b) == s
}
