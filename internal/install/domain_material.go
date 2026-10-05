package install

import (
	"errors"
	"io/fs"
	"path"
	"path/filepath"
	"regexp"

	"github.com/Sir-Adnan/wg-guard/internal/domainqueue"
	"github.com/Sir-Adnan/wg-guard/internal/domaintls"
	"github.com/Sir-Adnan/wg-guard/internal/layout"
)

var archivedCertbotMaterial = regexp.MustCompile(`\A(fullchain|privkey)[1-9][0-9]*\.pem\z`)

// Certbot's live files are symlinks. Resolve only the corresponding recorded
// archive directory, never a caller-selected root file. All other imports refuse
// symlinks, including ancestor-directory substitutions.
func resolveDomainMaterial(file, lineage string) (string, error) {
	if lineage == "" {
		return file, safeHostPath(file)
	}
	if filepath.Dir(file) != CertbotLivePath(lineage) {
		return "", domainqueue.ErrInvalid
	}
	return resolveCertbotMaterial(file, filepath.Join("/etc/letsencrypt/archive", lineage))
}

func resolveCertbotMaterial(file, archiveDir string) (string, error) {
	resolved, err := filepath.EvalSymlinks(file)
	if err != nil {
		return "", domainqueue.ErrInvalid
	}
	if filepath.Dir(resolved) != archiveDir || !archivedCertbotMaterial.MatchString(filepath.Base(resolved)) {
		return "", domainqueue.ErrInvalid
	}
	if err := safeHostPath(resolved); err != nil {
		return "", domainqueue.ErrInvalid
	}
	return resolved, nil
}

func readDomainPairFiles(h Host, certPath, keyPath, lineage string) ([]byte, []byte, error) {
	paths := []string{certPath, keyPath}
	for i, file := range paths {
		if _, real := h.(realHost); real {
			resolved, err := resolveDomainMaterial(file, lineage)
			if err != nil {
				return nil, nil, domainqueue.ErrInvalid
			}
			paths[i] = resolved
		}
		info, err := h.Stat(paths[i])
		if err != nil || !info.Mode().IsRegular() || i == 1 && info.Mode().Perm()&0077 != 0 {
			return nil, nil, domainqueue.ErrInvalid
		}
	}
	cert, err := readBoundedFile(h, paths[0], domaintls.MaxMaterialBytes)
	if err != nil {
		return nil, nil, domainqueue.ErrInvalid
	}
	key, err := readBoundedFile(h, paths[1], domaintls.MaxMaterialBytes)
	if err != nil {
		clear(cert)
		return nil, nil, domainqueue.ErrInvalid
	}
	return cert, key, nil
}

// Only unreferenced immutable pairs beneath the owned directory are removed.
// Pending recovery keeps every pair referenced by the backed-up active policy.
func pruneDomainPairs(h Host) error {
	keep := map[string]bool{}
	add := func(p domaintls.Policy) {
		for _, site := range p.Sites {
			keep[site.CertificateID] = true
		}
	}
	if p, err := readDomainPolicy(h); err == nil {
		add(p)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if j, err := LoadJournal(h); err != nil {
		return err
	} else if j != nil && !j.terminal() {
		if raw, err := readBoundedFile(h, filepath.Join(exposureBackupDir(j.ID), "domain-policy"), domaintls.MaxPolicyBytes); err == nil {
			if p, err := domaintls.ReadPolicy(raw); err == nil {
				add(p)
			} else {
				return err
			}
		}
	}
	base := path.Join(layout.DomainDir, "certificates")
	entries, err := h.ReadDir(base)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() || !domaintls.IDPattern.MatchString(entry.Name()) || keep[entry.Name()] {
			continue
		}
		dir := path.Join(base, entry.Name())
		files, err := h.ReadDir(dir)
		if err != nil {
			return err
		}
		owned := len(files) > 0 && len(files) <= 2
		for _, file := range files {
			owned = owned && file.Type().IsRegular() && (file.Name() == "fullchain.pem" || file.Name() == "privkey.pem")
		}
		if owned {
			if err := h.RemoveAll(dir); err != nil {
				return err
			}
		}
	}
	return nil
}
