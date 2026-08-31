package backup

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"filippo.io/age"
)

// ageMagic is the first line of every age v1 file (age-encryption.org/v1).
const ageMagic = "age-en"

// ageMinPassword mirrors the product rule: setting or overriding the backup
// password with fewer than 8 characters is rejected.
const ageMinPassword = 8

// ageEncrypt wraps w in an age v1 stream using the scrypt passphrase
// recipient (ADR-0008: a standard format, no custom cryptography).
func ageEncrypt(w io.Writer, password string) (io.WriteCloser, error) {
	if len(password) < ageMinPassword {
		return nil, fmt.Errorf("backup: password must be at least %d characters", ageMinPassword)
	}
	recipient, err := age.NewScryptRecipient(password)
	if err != nil {
		return nil, err
	}
	return age.Encrypt(w, recipient)
}

// ageDecrypt wraps r in an age v1 reader using the passphrase identity.
func ageDecrypt(r io.Reader, password string) (io.Reader, error) {
	if password == "" {
		return nil, errPasswordRequired
	}
	id, err := age.NewScryptIdentity(password)
	if err != nil {
		return nil, err
	}
	return age.Decrypt(r, id)
}

// errPasswordRequired is returned when an age-encrypted archive is opened
// without a password.
var errPasswordRequired = fmt.Errorf("backup: archive is password-protected; a password is required")

// HTTPDoer is the transport used for sink HTTP calls (injectable in tests).
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

var _ HTTPDoer = (*http.Client)(nil)

// sniffContainer classifies the archive: age-encrypted, plain gzip, unknown.
func sniffContainer(r io.Reader) (kind string, br *bufio.Reader, err error) {
	br = bufio.NewReaderSize(r, 64)
	head, err := br.Peek(len(ageMagic))
	if err != nil {
		return "", nil, fmt.Errorf("backup: read archive header: %w", err)
	}
	switch {
	case string(head) == ageMagic:
		return "age", br, nil
	case head[0] == 0x1f && head[1] == 0x8b:
		return "gzip", br, nil
	default:
		return "", nil, fmt.Errorf("backup: unrecognized archive format")
	}
}

// openArchive returns the raw tar reader after transparently unwrapping the
// age layer (password required for encrypted archives).
func openArchive(r io.Reader, password string) (*tarReader, error) {
	kind, br, err := sniffContainer(r)
	if err != nil {
		return nil, err
	}
	src := io.Reader(br)
	if kind == "age" {
		src, err = ageDecrypt(br, password)
		if err != nil {
			return nil, err
		}
	}
	return newTarReader(src)
}

// fileEncrypted reports whether the file at path starts with the age header.
func fileEncrypted(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var magic [len(ageMagic)]byte
	if _, err := io.ReadFull(f, magic[:]); err != nil {
		return false
	}
	return string(magic[:]) == ageMagic
}

// trimEcho bounds sink error bodies so upstream responses can never dump
// unbounded (or sensitive) text into warnings and logs.
func trimEcho(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		s = s[:n] + "…"
	}
	return s
}

// httpTimeout is the sink delivery budget (large uploads over real networks).
const httpTimeout = 120 * time.Second
