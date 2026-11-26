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

// TestActressRoundTrip checks the actress list survives Store/Load, and that
// "resolved but empty" stays distinguishable from "not looked up yet".
func TestActressRoundTrip(t *testing.T) {
	dir := t.TempDir()
	c := New(dir)
	d := &javdbapi.VideoDetail{Summary: javdbapi.VideoSummary{ID: "abc", Code: "ABC-001", Title: "t"}}

	if err := c.Store("ABC-001", &CacheEntry{CachedAt: time.Now(), Detail: d,
		Actresses: []string{"女優A", "女優B"}, ActressesResolved: true}); err != nil {
		t.Fatal(err)
	}
	entry := c.Load("ABC-001")
	if entry == nil {
		t.Fatal("cache entry missing")
	}
	set, resolved := entry.ActressSet()
	if !resolved || !set["女優A"] || !set["女優B"] || len(set) != 2 {
		t.Fatalf("actress set = %v resolved=%v", set, resolved)
	}

	// resolved-but-empty must not be re-fetched
	if err := c.Store("ABC-001", &CacheEntry{CachedAt: time.Now(), Detail: d, ActressesResolved: true}); err != nil {
		t.Fatal(err)
	}
	entry = c.Load("ABC-001")
	if _, resolved := entry.ActressSet(); !resolved {
		t.Error("resolved empty list reported as unresolved")
	}

	// legacy entry without the field is unresolved
	if err := c.Store("ABC-001", &CacheEntry{CachedAt: time.Now(), Detail: d}); err != nil {
		t.Fatal(err)
	}
	entry = c.Load("ABC-001")
	if _, resolved := entry.ActressSet(); resolved {
		t.Error("entry without actress data reported as resolved")
	}
}
