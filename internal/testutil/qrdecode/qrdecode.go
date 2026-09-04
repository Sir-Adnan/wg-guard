// Package qrdecode provides an independent QR decoder for tests.
//
// Production code must not import this package. It intentionally uses a
// different implementation from the rsc.io/qr encoder so a shared encoder
// defect cannot make a round-trip test pass by construction.
package qrdecode

import (
	"bytes"
	"fmt"
	"image/png"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"
)

// PNG decodes one QR symbol from a PNG and returns its UTF-8 text.
func PNG(data []byte) (string, error) {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("decode QR PNG: %w", err)
	}
	bitmap, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		return "", fmt.Errorf("prepare QR bitmap: %w", err)
	}
	result, err := qrcode.NewQRCodeReader().Decode(bitmap, map[gozxing.DecodeHintType]interface{}{
		gozxing.DecodeHintType_CHARACTER_SET: "UTF-8",
		// HTTP responses are pristine, axis-aligned symbols. Pure mode tests
		// their exact QR structure and bytes without the camera-oriented
		// finder detector becoming payload-sensitive. Real presentation and
		// camera detection are a separate VPS/client verification gate.
		gozxing.DecodeHintType_PURE_BARCODE: true,
	})
	if err != nil {
		return "", fmt.Errorf("decode QR symbol: %w", err)
	}
	return result.GetText(), nil
}
