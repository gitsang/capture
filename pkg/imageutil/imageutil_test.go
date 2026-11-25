package imageutil

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

func makeJPEG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if x < w/2 {
				img.Set(x, y, color.RGBA{0, 0, 255, 255}) // left half blue
			} else {
				img.Set(x, y, color.RGBA{255, 0, 0, 255}) // right half red
			}
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func TestCropPosterLandscape(t *testing.T) {
	// javdb-like cover: 800x538 landscape
	src := makeJPEG(800, 538)
	out, err := CropPoster(src)
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	w := 800
	wantW := int(float64(w) * posterWidthRatio)
	if cfg.Width != wantW || cfg.Height != 538 {
		t.Fatalf("cropped = %dx%d, want %dx538", cfg.Width, cfg.Height, wantW)
	}
	if cfg.Height <= cfg.Width {
		t.Fatal("cropped poster should be portrait")
	}
	// right half was red; cropped image must be red-dominant
	img, _, _ := image.Decode(bytes.NewReader(out))
	r, _, b, _ := img.At(cfg.Width/2, cfg.Height/2).RGBA()
	if r < b {
		t.Fatal("crop kept the wrong (left) half")
	}
}

func TestCropPosterPortraitPassthrough(t *testing.T) {
	src := makeJPEG(300, 450)
	out, err := CropPoster(src)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(src, out) {
		t.Fatal("portrait image should pass through unchanged")
	}
}

func TestCropPosterInvalid(t *testing.T) {
	if _, err := CropPoster([]byte("not an image")); err == nil {
		t.Fatal("expected error for invalid data")
	}
}
