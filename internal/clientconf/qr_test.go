package clientconf

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/testutil/qrdecode"
)

func TestQRDecodesExactConfiguration(t *testing.T) {
	fixture := newFullConfigFixture(t)
	config, err := fixture.renderer.Render(t.Context(), fixture.deviceID)
	if err != nil {
		t.Fatal(err)
	}
	pngBytes, err := QR(config)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := qrdecode.PNG(pngBytes)
	if err != nil {
		t.Fatalf("independent QR decode failed for %d-byte configuration: %v", len(config), err)
	}
	if decoded != config {
		gotSum, wantSum := sha256.Sum256([]byte(decoded)), sha256.Sum256([]byte(config))
		t.Fatalf("decoded QR differs: got len=%d sha256=%x, want len=%d sha256=%x, first difference=%d",
			len(decoded), gotSum[:8], len(config), wantSum[:8], firstDifference(decoded, config))
	}
}

func TestQRRoundTripsRepresentativePayloads(t *testing.T) {
	fixture := newFullConfigFixture(t)
	fullConfig, err := fixture.renderer.Render(t.Context(), fixture.deviceID)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		text string
	}{
		{name: "empty", text: ""},
		{name: "multilingual UTF-8", text: "WG-Guard / فارسی / English\nخط دوم\n"},
		{name: "representative full config", text: fullConfig},
		{name: "near medium-EC capacity", text: strings.Repeat("x", 2300)},
	} {
		t.Run(test.name, func(t *testing.T) {
			pngBytes, err := QR(test.text)
			if err != nil {
				t.Fatalf("encode %d-byte payload: %v", len(test.text), err)
			}
			decoded, err := qrdecode.PNG(pngBytes)
			if err != nil {
				t.Fatalf("decode %d-byte payload: %v", len(test.text), err)
			}
			assertQRTextEqual(t, decoded, test.text)
		})
	}
}

func TestQRRasterGeometryAndColors(t *testing.T) {
	pngBytes, err := QR("hello-wg-guard-qr-regression-test")
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		t.Fatal(err)
	}
	b := img.Bounds()
	if b.Dx() < 200 || b.Dy() != b.Dx() {
		t.Fatalf("canvas %v too small or not square", b)
	}
	const (
		modulePixels = 6
		quietModules = 4
	)
	if b.Dx()%modulePixels != 0 {
		t.Fatalf("canvas width %d is not module-aligned", b.Dx())
	}
	quietEdge := quietModules*modulePixels - 1
	for _, point := range [][2]int{{0, 0}, {quietEdge, quietEdge}, {b.Dx() - 1, b.Dy() - 1}} {
		if got := color.GrayModel.Convert(img.At(point[0], point[1])).(color.Gray).Y; got != 0xff {
			t.Fatalf("quiet-zone pixel (%d,%d) = %#02x, want white", point[0], point[1], got)
		}
	}
	finderStart := quietModules * modulePixels
	if got := color.GrayModel.Convert(img.At(finderStart, finderStart)).(color.Gray).Y; got != 0x00 {
		t.Fatalf("top-left finder module = %#02x, want black", got)
	}
}

func TestQRRejectsOversizedPayload(t *testing.T) {
	for _, size := range []int{2400, 2601} {
		t.Run(fmt.Sprintf("%d_bytes", size), func(t *testing.T) {
			if _, err := QR(strings.Repeat("x", size)); domain.CodeOf(err) != domain.CodeInvalidRequest {
				t.Fatalf("oversized QR error code = %q, want %q", domain.CodeOf(err), domain.CodeInvalidRequest)
			}
		})
	}
}

func TestQRIsDeterministic(t *testing.T) {
	const text = "same input must produce the same PNG bytes"
	first, err := QR(text)
	if err != nil {
		t.Fatal(err)
	}
	second, err := QR(text)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		firstSum, secondSum := sha256.Sum256(first), sha256.Sum256(second)
		t.Fatalf("QR PNG is not deterministic: first len=%d sha256=%x, second len=%d sha256=%x",
			len(first), firstSum[:8], len(second), secondSum[:8])
	}
}

func assertQRTextEqual(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		gotSum, wantSum := sha256.Sum256([]byte(got)), sha256.Sum256([]byte(want))
		t.Fatalf("decoded QR differs: got len=%d sha256=%x, want len=%d sha256=%x, first difference=%d",
			len(got), gotSum[:8], len(want), wantSum[:8], firstDifference(got, want))
	}
}
