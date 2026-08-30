package api

import (
	"bytes"
	"image/png"

	"rsc.io/qr"
)

// qrPNG renders text as a PNG QR code. rsc.io/qr is the only QR option with
// zero transitive dependencies (justified in THIRD_PARTY.md: ~100 KB binary
// impact, BSD license, no network or cgo). Client configs are small
// (≤ ~2 KB) but hard-bounded here: oversized payloads are a client error,
// not a panic in the encoder.
func qrPNG(text string) ([]byte, error) {
	const maxQRBytes = 2600 // QR version 40 byte-mode capacity is 2953
	if len(text) > maxQRBytes {
		return nil, invalidRequestErr("%s", "device configuration too large for a QR code")
	}
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return nil, invalidRequestErr("qr encode: %s", err.Error())
	}
	img := code.Image() // 6 px module border-included scale
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, invalidRequestErr("qr png: %s", err.Error())
	}
	return buf.Bytes(), nil
}
