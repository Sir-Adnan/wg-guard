package clientconf

import (
	"bytes"
	"image/png"
	"testing"
)

// Regression: the rasterized QR must fill its canvas — rsc.io/qr's
// code.Image() draws the modules unscaled in the corner of a scaled canvas.
func TestQRFillsCanvas(t *testing.T) {
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
	// The QR finder patterns sit near the corners: the top-left tenth of
	// the canvas must contain dark modules (with a scaled raster it does;
	// with the library's corner-drawn output it would be all white).
	darkInFinder := 0
	for y := 0; y < b.Dy()/10; y++ {
		for x := 0; x < b.Dx()/10; x++ {
			if r, g, _, _ := img.At(x, y).RGBA(); r+g < 30000 {
				darkInFinder++
			}
		}
	}
	if darkInFinder == 0 {
		t.Fatal("top-left finder area is empty — QR not filling canvas")
	}
}
