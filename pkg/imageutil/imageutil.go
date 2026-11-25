// Package imageutil provides artwork helpers: completeness checks and
// javdb cover cropping (the right ~52.7% of the full DVD cover is the
// portrait movie poster).
package imageutil

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"os"
)

// Valid reports whether data is a fully decodable image (guards against
// truncated downloads and HTML error pages saved as .jpg).
func Valid(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	_, _, err := image.Decode(bytes.NewReader(data))
	return err == nil
}

// ValidFile checks an on-disk image for completeness.
func ValidFile(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return Valid(data)
}

// IsPortraitFile reports whether the on-disk image is taller than wide.
func IsPortraitFile(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return false, err
	}
	return cfg.Height > cfg.Width, nil
}

// javdb full covers are ~1.47:1 landscape; the front (portrait) poster
// occupies the right 52.7% of the width.
const posterWidthRatio = 0.527

// CropPoster extracts the portrait front poster from a javdb full cover.
// Landscape covers are cropped to the right posterWidthRatio of the width
// and re-encoded as JPEG. Already-portrait images are returned unchanged.
func CropPoster(data []byte) ([]byte, error) {
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if h >= w {
		return data, nil // already portrait, nothing to crop
	}

	cropW := int(float64(w) * posterWidthRatio)
	if cropW < 1 || cropW > w {
		cropW = w
	}
	rect := image.Rect(b.Max.X-cropW, b.Min.Y, b.Max.X, b.Max.Y)

	type subImager interface {
		SubImage(image.Rectangle) image.Image
	}
	var cropped image.Image
	if si, ok := src.(subImager); ok {
		cropped = si.SubImage(rect)
	} else {
		dst := image.NewRGBA(image.Rect(0, 0, cropW, h))
		for y := 0; y < h; y++ {
			for x := 0; x < cropW; x++ {
				dst.Set(x, y, src.At(rect.Min.X+x, rect.Min.Y+y))
			}
		}
		cropped = dst
	}

	var out bytes.Buffer
	if err := jpeg.Encode(&out, cropped, &jpeg.Options{Quality: 95}); err != nil {
		return nil, fmt.Errorf("encode cropped poster: %w", err)
	}
	return out.Bytes(), nil
}
