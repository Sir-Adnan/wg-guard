package backup

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"filippo.io/age"
)

// ageMagic is the first line of every age v1 file (age-encryption.org/v1).
const ageMagic = "age-en"

// ageMinPassword mirrors the product rule: setting or overriding the backup
// password with fewer than 8 characters is rejected.
const ageMinPassword = 8
const maxScryptWorkFactor = 18

// ValidatePassword checks an explicit archive password; an unset stored
// password remains the separately documented plaintext choice in Create.
func ValidatePassword(password string) error {
	if len(password) < ageMinPassword {
		return safetyError("password_short", nil, ageMinPassword)
	}
	return nil
}

// ageEncrypt wraps w in an age v1 stream using the scrypt passphrase
// recipient (ADR-0008: a standard format, no custom cryptography).
func ageEncrypt(w io.Writer, password string) (io.WriteCloser, error) {
	if err := ValidatePassword(password); err != nil {
		return nil, err
	}
	recipient, err := age.NewScryptRecipient(password)
	if err != nil {
		return nil, safetyError("encrypt_failed", err)
	}
	writer, err := age.Encrypt(w, recipient)
	if err != nil {
		return nil, safetyError("encrypt_failed", err)
	}
	return encryptedWriter{writer}, nil
}

// ageDecrypt wraps r in an age v1 reader using the passphrase identity.
func ageDecrypt(r io.Reader, password string) (io.Reader, error) {
	if password == "" {
		return nil, safetyError("password_required", errPasswordRequired)
	}
	id, err := age.NewScryptIdentity(password)
	if err != nil {
		return nil, safetyError("decrypt_failed", err)
	}
	// Match our pinned writer's default without allowing attacker-selected
	// factors up to age's default maximum (22, approximately 4 GiB).
	id.SetMaxWorkFactor(maxScryptWorkFactor)
	reader, err := age.Decrypt(r, cappedScryptIdentity{id})
	if errors.Is(err, errScryptWorkFactor) {
		return nil, safetyError("scrypt_limit", err, maxScryptWorkFactor)
	}
	if err != nil {
		return nil, safetyError("decrypt_failed", err)
	}
	return decryptedReader{reader}, nil
}

var errScryptWorkFactor = errors.New("scrypt work factor too large")

// age exposes parsed recipient stanzas but no typed work-factor error. Check
// only the declared bound here to preserve a specific refusal without matching
// library error strings. age remains responsible for all format/crypto validation.
type cappedScryptIdentity struct{ identity *age.ScryptIdentity }

func (i cappedScryptIdentity) Unwrap(stanzas []*age.Stanza) ([]byte, error) {
	if len(stanzas) == 1 && stanzas[0].Type == "scrypt" && len(stanzas[0].Args) == 2 {
		if n, err := strconv.ParseUint(stanzas[0].Args[1], 10, 64); err == nil && n > maxScryptWorkFactor {
			return nil, errScryptWorkFactor
		}
	}
	return i.identity.Unwrap(stanzas)
}

type decryptedReader struct{ io.Reader }

func (r decryptedReader) Read(b []byte) (int, error) {
	n, err := r.Reader.Read(b)
	if err != nil && err != io.EOF {
		err = safetyError("decrypt_failed", err)
	}
	return n, err
}

type encryptedWriter struct{ io.WriteCloser }

func (w encryptedWriter) Write(b []byte) (int, error) {
	n, err := w.WriteCloser.Write(b)
	if err != nil {
		err = safetyError("encrypt_failed", err)
	}
	return n, err
}
func (w encryptedWriter) Close() error {
	if err := w.WriteCloser.Close(); err != nil {
		return safetyError("encrypt_failed", err)
	}
	return nil
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
		return "", nil, safetyError("archive_invalid", err)
	}
	switch {
	case string(head) == ageMagic:
		return "age", br, nil
	case head[0] == 0x1f && head[1] == 0x8b:
		return "gzip", br, nil
	default:
		return "", nil, safetyError("archive_invalid", nil)
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
