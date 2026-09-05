// Command qrcheck independently decodes a pristine WG-Guard QR PNG and
// compares it with a canonical client configuration without printing either
// secret-bearing payload. It is a verification helper, never a production
// runtime dependency.
package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"

	"github.com/Sir-Adnan/wg-guard/internal/testutil/qrdecode"
)

const (
	maxPNGBytes    = 4 << 20
	maxConfigBytes = 4 << 10
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: qrcheck QR.png CONFIG.conf")
		os.Exit(2)
	}
	pngData, err := readBounded(os.Args[1], maxPNGBytes)
	if err == nil {
		var configData []byte
		configData, err = readBounded(os.Args[2], maxConfigBytes)
		if err == nil {
			err = compareQR(pngData, configData, os.Stdout)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "qrcheck:", err)
		os.Exit(1)
	}
}

func readBounded(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s exceeds the %d-byte verification limit", path, limit)
	}
	return data, nil
}

func compareQR(pngData, configData []byte, out io.Writer) error {
	decoded, err := qrdecode.PNG(pngData)
	if err != nil {
		return err
	}
	decodedBytes := []byte(decoded)
	decodedHash := sha256.Sum256(decodedBytes)
	configHash := sha256.Sum256(configData)
	if !bytes.Equal(decodedBytes, configData) {
		return fmt.Errorf(
			"QR content mismatch: decoded bytes=%d sha256=%x; config bytes=%d sha256=%x",
			len(decodedBytes), decodedHash, len(configData), configHash,
		)
	}
	_, err = fmt.Fprintf(out, "qr match bytes=%d sha256=%x\n", len(configData), configHash)
	return err
}
