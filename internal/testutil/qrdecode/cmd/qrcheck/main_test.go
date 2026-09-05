package main

import (
	"bytes"
	"image/png"
	"strings"
	"testing"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"
)

func TestCompareQRMatchReportsOnlyMetadata(t *testing.T) {
	const secret = "PrivateKey = do-not-print-this\n"
	pngData := encodeQR(t, secret)
	var out bytes.Buffer
	if err := compareQR(pngData, []byte(secret), &out); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "qr match bytes=31 sha256=") {
		t.Fatalf("safe result = %q", got)
	}
	if strings.Contains(got, secret) || strings.Contains(got, "do-not-print") {
		t.Fatal("secret config leaked into match output")
	}
}

func TestCompareQRMismatchReportsOnlyMetadata(t *testing.T) {
	const secret = "PrivateKey = do-not-print-this\n"
	pngData := encodeQR(t, secret)
	var out bytes.Buffer
	err := compareQR(pngData, []byte("different secret config\n"), &out)
	if err == nil {
		t.Fatal("mismatched QR accepted")
	}
	got := err.Error()
	if !strings.Contains(got, "QR content mismatch") || !strings.Contains(got, "sha256=") {
		t.Fatalf("safe error = %q", got)
	}
	for _, forbidden := range []string{secret, "do-not-print", "different secret"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("secret config leaked into mismatch output: %q", got)
		}
	}
}

func encodeQR(t *testing.T, value string) []byte {
	t.Helper()
	matrix, err := qrcode.NewQRCodeWriter().EncodeWithoutHint(
		value, gozxing.BarcodeFormat_QR_CODE, 256, 256,
	)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := png.Encode(&out, matrix); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
