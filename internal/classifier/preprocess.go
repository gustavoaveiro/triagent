// Package classifier scores images for explicit/nsfw content using a
// locally loaded ONNX vision model - no network calls, nothing leaves the
// machine.
package classifier

import (
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"

	"golang.org/x/image/draw"
)

// InputSize is the model's expected square input resolution in pixels.
const InputSize = 384

// preprocess decodes an image from r and writes it into dst as a
// normalized NCHW tensor: resized to InputSize x InputSize, each channel
// scaled to [-1, 1] via (pixel/255 - 0.5) / 0.5, matching the model's
// published preprocessor config (image_mean=0.5, image_std=0.5).
// dst must have length 3*InputSize*InputSize.
func preprocess(r io.Reader, dst []float32) error {
	img, _, err := image.Decode(r)
	if err != nil {
		return err
	}
	writeTensor(img, dst)
	return nil
}

func writeTensor(img image.Image, dst []float32) {
	resized := image.NewRGBA(image.Rect(0, 0, InputSize, InputSize))
	draw.CatmullRom.Scale(resized, resized.Bounds(), img, img.Bounds(), draw.Over, nil)

	plane := InputSize * InputSize
	for y := 0; y < InputSize; y++ {
		for x := 0; x < InputSize; x++ {
			r, g, b, _ := resized.At(x, y).RGBA()
			idx := y*InputSize + x
			dst[0*plane+idx] = normalizeChannel(uint8(r >> 8))
			dst[1*plane+idx] = normalizeChannel(uint8(g >> 8))
			dst[2*plane+idx] = normalizeChannel(uint8(b >> 8))
		}
	}
}

func normalizeChannel(c uint8) float32 {
	return (float32(c)/255.0 - 0.5) / 0.5
}
