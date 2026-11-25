package cache

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/RPbro/javdbapi"
)

const pngB64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

func TestStoreRawAndLegacyLoad(t *testing.T) {
	dir := t.TempDir()
	c := New(dir)
	png, _ := base64.StdEncoding.DecodeString(pngB64)

	d := javdbapi.NewVideoDetail(javdbapi.VideoSummary{ID: "x1", Code: "ABF-004", Title: "t"})
	d.Screenshots = []javdbapi.Screenshot{{FullURL: "u0"}, {FullURL: "u1"}}

	// new-style store: JSON metadata-only + raw image files
	e := &CacheEntry{CachedAt: time.Now(), Detail: &d, Poster: png, ExtraFanart: [][]byte{png, nil}}
	if err := c.Store("ABF-004", e); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "ABF-004", "poster.jpg")); err != nil {
		t.Fatal("poster.jpg missing")
	}
	if _, err := os.Stat(filepath.Join(dir, "ABF-004", "extrafanart-0.jpg")); err != nil {
		t.Fatal("extrafanart-0 missing")
	}
	if _, err := os.Stat(filepath.Join(dir, "ABF-004", "extrafanart-1.jpg")); err == nil {
		t.Fatal("nil slot should not be written")
	}
	j, _ := os.ReadFile(filepath.Join(dir, "ABF-004", "ABF-004.json"))
	if len(j) > 4096 {
		t.Fatalf("JSON should be metadata-only, got %d bytes", len(j))
	}
	got := c.Load("ABF-004")
	if got == nil || !ValidImage(got.Poster) || len(got.ExtraFanart) < 1 || !ValidImage(got.ExtraFanart[0]) {
		t.Fatal("new-style load failed")
	}

	// legacy-style: flat {CODE}.json with images embedded, no raw files
	legacy := &CacheEntry{CachedAt: time.Now(), Detail: &d, Poster: png, ExtraFanart: [][]byte{png, png}}
	lj, _ := json.Marshal(legacy)
	os.WriteFile(filepath.Join(dir, "OLD-001.json"), lj, 0644)
	got2 := c.Load("OLD-001")
	if got2 == nil || !ValidImage(got2.Poster) || len(got2.ExtraFanart) != 2 || !ValidImage(got2.ExtraFanart[1]) {
		t.Fatal("legacy load failed")
	}
}
