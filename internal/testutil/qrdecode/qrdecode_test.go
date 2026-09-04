package qrdecode

import (
	"bytes"
	"image/png"
	"testing"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"
)

func TestPNG(t *testing.T) {
	const want = "independent decoder: فارسی / English"
	matrix, err := qrcode.NewQRCodeWriter().EncodeWithoutHint(
		want, gozxing.BarcodeFormat_QR_CODE, 256, 256,
	)
	if err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, matrix); err != nil {
		t.Fatal(err)
	}
	got, err := PNG(encoded.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("decoded text differs: got %d bytes, want %d", len(got), len(want))
	}
}

func TestPNGRejectsInvalidImage(t *testing.T) {
	if _, err := PNG([]byte("not a PNG")); err == nil {
		t.Fatal("invalid PNG decoded without an error")
	}
}
