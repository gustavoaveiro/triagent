package classifier

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

func encodedSolidPNG(t *testing.T, w, h int, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}

func TestPreprocess_SolidColorNormalization(t *testing.T) {
	// A uniform-color source image is unaffected by resizing, so every
	// output value should match normalizeChannel exactly regardless of the
	// resampling algorithm used.
	tests := []struct {
		name string
		c    color.RGBA
	}{
		{"black", color.RGBA{0, 0, 0, 255}},
		{"white", color.RGBA{255, 255, 255, 255}},
		{"mid-gray", color.RGBA{128, 128, 128, 255}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data := encodedSolidPNG(t, 10, 10, tc.c)
			dst := make([]float32, 3*InputSize*InputSize)
			if err := preprocess(bytes.NewReader(data), dst); err != nil {
				t.Fatalf("preprocess: %v", err)
			}

			wantR := normalizeChannel(tc.c.R)
			wantG := normalizeChannel(tc.c.G)
			wantB := normalizeChannel(tc.c.B)
			plane := InputSize * InputSize

			const tol = 0.01 // PNG->RGBA->resize round-trip tolerance
			check := func(label string, got, want float32) {
				t.Helper()
				if diff := got - want; diff > tol || diff < -tol {
					t.Errorf("%s = %v, want ~%v", label, got, want)
				}
			}
			check("R[0]", dst[0*plane], wantR)
			check("G[0]", dst[1*plane], wantG)
			check("B[0]", dst[2*plane], wantB)
			check("R[last]", dst[0*plane+plane-1], wantR)
			check("B[last]", dst[2*plane+plane-1], wantB)
		})
	}
}

func TestPreprocess_OutputShape(t *testing.T) {
	data := encodedSolidPNG(t, 50, 30, color.RGBA{10, 20, 30, 255})
	dst := make([]float32, 3*InputSize*InputSize)
	if err := preprocess(bytes.NewReader(data), dst); err != nil {
		t.Fatalf("preprocess: %v", err)
	}
	// Just confirms it ran without panicking on a non-square source and
	// filled the whole buffer (spot-check a middle index).
	mid := len(dst) / 2
	if dst[mid] == 0 && dst[0] == 0 && dst[len(dst)-1] == 0 {
		t.Error("dst appears entirely unfilled")
	}
}

func TestPreprocess_InvalidImageDataFails(t *testing.T) {
	dst := make([]float32, 3*InputSize*InputSize)
	err := preprocess(strings.NewReader("not an image"), dst)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
}

func TestNormalizeChannel(t *testing.T) {
	tests := []struct {
		in   uint8
		want float32
	}{
		{0, -1.0},
		{255, 1.0},
		{128, 0.003921628}, // (128/255 - 0.5) / 0.5
	}
	for _, tc := range tests {
		got := normalizeChannel(tc.in)
		if diff := got - tc.want; diff > 1e-6 || diff < -1e-6 {
			t.Errorf("normalizeChannel(%d) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
