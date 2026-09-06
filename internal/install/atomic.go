package install

import (
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// AtomicWrite syncs the private file and containing directory before success.
func (realHost) AtomicWrite(p string, b []byte, mode fs.FileMode) error {
	if err := safeHostPath(p); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(p), ".wg-guard-write-")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(tmp, p); err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(p))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
func atomicWrite(h Host, p string, b []byte, mode fs.FileMode) error {
	if a, ok := h.(interface {
		AtomicWrite(string, []byte, fs.FileMode) error
	}); ok {
		return a.AtomicWrite(p, b, mode)
	}
	if err := h.WriteFile(p+".tmp", b, mode); err != nil {
		return err
	}
	return h.Rename(p+".tmp", p)
}
func writeJSON(h Host, p string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(h, p, append(b, '\n'), 0600)
}

func readRecord(h Host, p string) ([]byte, error) {
	f, err := h.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 256<<10+1))
	if err != nil {
		return nil, err
	}
	if len(b) > 256<<10 {
		return nil, terminalError("install.error.state")
	}
	return b, nil
}
func safeHostPath(p string) error {
	for cur := filepath.Clean(p); ; cur = filepath.Dir(cur) {
		info, err := os.Lstat(cur)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return terminalError("install.error.state")
		}
		if filepath.Dir(cur) == cur {
			break
		}
	}
	return nil
}
