package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RPbro/javdbapi"

	"github.com/gitsang/capture/pkg/cache"
	"github.com/gitsang/capture/pkg/client"
	"github.com/gitsang/capture/pkg/imageutil"
)

// tiny valid 1x1 PNG
const pngB64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

func testDetail() *javdbapi.VideoDetail {
	d := javdbapi.NewVideoDetail(javdbapi.VideoSummary{
		ID:          "abc123",
		Code:        "ABF-004",
		Title:       "テストタイトル",
		CoverURL:    "https://example.invalid/cover.jpg",
		PublishedAt: time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC),
	})
	d.Actors = []javdbapi.Actor{{Name: "女優A"}, {Name: "女優B"}}
	d.Tags = []javdbapi.Tag{{Name: "タグX"}, {Name: "タグ/Y"}} // slash must be sanitized
	d.Screenshots = []javdbapi.Screenshot{
		{FullURL: "https://example.invalid/s0.jpg"},
		{FullURL: "https://example.invalid/s1.jpg"},
	}
	mins := 120
	d.DurationMinutes = &mins
	return &d
}

// TestProcessVideoCacheFlow exercises the cached path end to end without any
// network: the Client has nil internals, so any HTTP attempt would panic.
func TestProcessVideoCacheFlow(t *testing.T) {
	base := t.TempDir()
	in := filepath.Join(base, "in")
	out := filepath.Join(base, "out")
	cacheDir := filepath.Join(base, "cache")
	if err := os.MkdirAll(filepath.Join(in, "nested"), 0755); err != nil {
		t.Fatal(err)
	}

	png, _ := base64.StdEncoding.DecodeString(pngB64)

	for name, content := range map[string]string{
		"fc2-ppv-1234567.mp4":                  "video1",
		filepath.Join("nested", "abf-004.mkv"): "video2",
		"junk.mp4":                             "junk",
	} {
		if err := os.WriteFile(filepath.Join(in, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	// pre-existing output content must be ignored by the scan
	if err := os.MkdirAll(filepath.Join(out, "video", "OLD-001"), 0755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(out, "video", "OLD-001", "OLD-001.mp4"), []byte("old"), 0644)

	api := &client.Client{}
	c := cache.New(cacheDir)
	for _, movieCode := range []string{"FC2-PPV-1234567", "ABF-004"} {
		if err := c.Store(movieCode, &cache.CacheEntry{CachedAt: time.Now(), Detail: testDetail(), Poster: png, ExtraFanart: [][]byte{png, png}}); err != nil {
			t.Fatal(err)
		}
	}

	ctx := context.Background()

	files, err := scanVideoFiles(in, out)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 {
		t.Fatalf("scanned %d files, want 3", len(files))
	}
	for _, f := range files {
		processVideo(ctx, api, c, out, false, f)
	}

	checks := []string{
		filepath.Join(out, "video", "FC2-PPV-1234567", "FC2-PPV-1234567.mp4"),
		filepath.Join(out, "video", "FC2-PPV-1234567", "FC2-PPV-1234567.nfo"),
		filepath.Join(out, "video", "FC2-PPV-1234567", "poster.jpg"),
		filepath.Join(out, "video", "FC2-PPV-1234567", "fanart.jpg"),
		filepath.Join(out, "video", "FC2-PPV-1234567", "thumb.jpg"),
		filepath.Join(out, "video", "FC2-PPV-1234567", "extrafanart", "extrafanart-0.jpg"),
		filepath.Join(out, "video", "FC2-PPV-1234567", "extrafanart", "extrafanart-1.jpg"),
		filepath.Join(out, "video", "ABF-004", "ABF-004.mkv"),
		filepath.Join(out, "actor", "女優A", "FC2-PPV-1234567"),
		filepath.Join(out, "actor", "女優B", "ABF-004"),
		filepath.Join(out, "tag", "タグX", "FC2-PPV-1234567"),
		filepath.Join(out, "tag", "タグ-Y", "ABF-004"), // sanitized slash
	}
	for _, p := range checks {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("missing: %s (%v)", p, err)
		}
	}
	// NFO: Emby-style CDATA title + CRLF line endings
	nfoData, err := os.ReadFile(filepath.Join(out, "video", "ABF-004", "ABF-004.nfo"))
	if err != nil {
		t.Fatal(err)
	}
	nfoStr := string(nfoData)
	if !strings.Contains(nfoStr, "<![CDATA[ABF-004-\u30c6\u30b9\u30c8\u30bf\u30a4\u30c8\u30eb]]>") {
		t.Error("NFO missing CDATA title")
	}
	if !strings.Contains(nfoStr, "\r\n") || strings.Contains(strings.ReplaceAll(nfoStr, "\r\n", ""), "\n") {
		t.Error("NFO not fully CRLF")
	}
	if !strings.Contains(nfoStr, "<runtime>120minutes</runtime>") {
		t.Error("NFO runtime wrong")
	}
	if !strings.Contains(nfoStr, "<tag>\u30bf\u30b0X</tag>") || !strings.Contains(nfoStr, "<genre>\u30bf\u30b0X</genre>") {
		t.Error("NFO missing dual genre/tag")
	}

	tgt, err := os.Readlink(filepath.Join(out, "actor", "女優A", "FC2-PPV-1234567"))
	if err != nil {
		t.Fatal(err)
	}
	if tgt != filepath.Join("..", "..", "video", "FC2-PPV-1234567") {
		t.Errorf("symlink target = %q, want relative ../../video/FC2-PPV-1234567", tgt)
	}

	// fullname index: {output}/fullname/{CODE} {title} -> ../video/CODE
	fnLink := filepath.Join(out, "fullname", "ABF-004 \u30c6\u30b9\u30c8\u30bf\u30a4\u30c8\u30eb")
	ftgt, err := os.Readlink(fnLink)
	if err != nil {
		t.Fatalf("fullname symlink missing: %v", err)
	}
	if ftgt != filepath.Join("..", "video", "ABF-004") {
		t.Errorf("fullname target = %q, want ../video/ABF-004", ftgt)
	}
	// title change: old fullname link replaced, exactly one link for the code
	if createFullnameSymlink(out, "ABF-004", "\u65b0\u30bf\u30a4\u30c8\u30eb", filepath.Join(out, "video", "ABF-004")) != true {
		t.Error("title-change link not created")
	}
	if _, err := os.Lstat(fnLink); !os.IsNotExist(err) {
		t.Error("stale fullname link not removed")
	}
	entries, _ := os.ReadDir(filepath.Join(out, "fullname"))
	count := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "ABF-004") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("fullname links for ABF-004 = %d, want 1", count)
	}

	// corrupt poster + truncate NFO, rerun: both must be repaired from cache
	posterPath := filepath.Join(out, "video", "ABF-004", "poster.jpg")
	os.WriteFile(posterPath, png[:10], 0644)
	nfoPath := filepath.Join(out, "video", "ABF-004", "ABF-004.nfo")
	os.WriteFile(nfoPath, []byte("<?xml truncated"), 0644)

	// a new source file with the same code appears (videos were moved away)
	os.WriteFile(filepath.Join(in, "abf-004.mkv"), []byte("video2-again"), 0644)
	processVideo(ctx, api, c, out, false, VideoFile{
		Path:     filepath.Join(in, "abf-004.mkv"),
		Filename: "abf-004.mkv",
		Code:     "ABF-004",
	})
	if !cache.ValidImageFile(posterPath) {
		t.Error("truncated poster was not restored from cache")
	}
	if !cache.ValidNFOFile(nfoPath) {
		t.Error("truncated NFO was not regenerated")
	}
	// corrupt one extrafanart -> repaired from cache
	efPath := filepath.Join(out, "video", "ABF-004", "extrafanart", "extrafanart-1.jpg")
	os.WriteFile(efPath, png[:8], 0644)
	processVideo(ctx, api, c, out, false, VideoFile{
		Path:     filepath.Join(in, "abf-004.mkv"),
		Filename: "abf-004.mkv",
		Code:     "ABF-004",
	})
	if !cache.ValidImageFile(efPath) {
		t.Error("truncated extrafanart was not restored from cache")
	}

	// rescanning the whole base dir must skip the output tree
	files3, _ := scanVideoFiles(base, out)
	if len(files3) != 1 || files3[0].Filename != "junk.mp4" {
		t.Errorf("rescan found %d files, want only junk.mp4", len(files3))
	}
}

func TestValidImage(t *testing.T) {
	png, _ := base64.StdEncoding.DecodeString(pngB64)
	if !cache.ValidImage(png) {
		t.Error("valid PNG rejected")
	}
	if cache.ValidImage(png[:10]) {
		t.Error("truncated PNG accepted")
	}
	if cache.ValidImage([]byte("<html>error</html>")) {
		t.Error("HTML accepted as image")
	}
	if cache.ValidImage(nil) {
		t.Error("nil accepted as image")
	}
}

// TestFixOutput repairs a damaged output tree offline from cache:
// missing symlinks, truncated NFO/images, landscape poster.
func TestFixOutput(t *testing.T) {
	base := t.TempDir()
	out := filepath.Join(base, "out")
	cacheDir := filepath.Join(base, "cache")

	// landscape 100x60 JPEG as the "full cover"
	cover := landscapeJPEG(t, 100, 60)

	c := cache.New(cacheDir)
	d := testDetail()
	if err := c.Store("ABF-004", &cache.CacheEntry{CachedAt: time.Now(), Detail: d, Poster: cover, ExtraFanart: [][]byte{cover, cover}}); err != nil {
		t.Fatal(err)
	}

	// damaged output: video present, everything else missing/broken
	movieFolder := filepath.Join(out, "video", "ABF-004")
	os.MkdirAll(movieFolder, 0755)
	os.WriteFile(filepath.Join(movieFolder, "ABF-004.mp4"), []byte("v"), 0644)
	os.WriteFile(filepath.Join(movieFolder, "ABF-004.nfo"), []byte("<?xml broken"), 0644)
	os.WriteFile(filepath.Join(movieFolder, "poster.jpg"), cover, 0644)      // landscape poster (legacy)
	os.WriteFile(filepath.Join(movieFolder, "fanart.jpg"), cover[:12], 0644) // truncated

	api := &client.Client{} // nil internals: any network attempt would panic
	fixOutput(context.Background(), api, c, out, fixTargets{}, false)

	// NFO repaired
	if !cache.ValidNFOFile(filepath.Join(movieFolder, "ABF-004.nfo")) {
		t.Error("NFO not repaired")
	}
	// fanart restored, poster now portrait
	if !imageutil.ValidFile(filepath.Join(movieFolder, "fanart.jpg")) {
		t.Error("fanart not restored")
	}
	if portrait, err := imageutil.IsPortraitFile(filepath.Join(movieFolder, "poster.jpg")); err != nil || !portrait {
		t.Errorf("poster not cropped to portrait (err=%v)", err)
	}
	// symlinks rebuilt
	for _, p := range []string{
		filepath.Join(out, "actor", "女優A", "ABF-004"),
		filepath.Join(out, "tag", "タグX", "ABF-004"),
		filepath.Join(out, "fullname", "ABF-004 テストタイトル"),
		filepath.Join(movieFolder, "extrafanart", "extrafanart-0.jpg"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("missing after fix: %s (%v)", p, err)
		}
	}
	// --fix poster forces re-crop even when the poster is already portrait
	if err := os.WriteFile(filepath.Join(movieFolder, "poster.jpg"), portraitJPEG(t, 40, 60), 0644); err != nil {
		t.Fatal(err)
	}
	fixOutput(context.Background(), api, c, out, fixTargets{poster: true}, false)
	if portrait, _ := imageutil.IsPortraitFile(filepath.Join(movieFolder, "poster.jpg")); !portrait {
		t.Error("forced poster re-crop failed")
	}
	// forced crop must differ from the pre-existing 40x60 placeholder
	cfgW := func() int {
		f, _ := os.Open(filepath.Join(movieFolder, "poster.jpg"))
		defer f.Close()
		cfg, _, _ := image.DecodeConfig(f)
		return cfg.Width
	}()
	if cfgW == 40 {
		t.Error("poster was not regenerated by --fix poster")
	}
}

// landscapeJPEG builds a solid-color landscape JPEG for tests.
func landscapeJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// portraitJPEG builds a solid-color portrait JPEG for tests.
func portraitJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestParseFixTargets(t *testing.T) {
	tt, err := parseFixTargets("missing")
	if err != nil || !tt.missingOnly() {
		t.Fatal("missing should select nothing forced")
	}
	tt, err = parseFixTargets("poster,symlink, nfo")
	if err != nil || !tt.poster || !tt.symlink || !tt.nfo || tt.fanart || tt.missingOnly() {
		t.Fatalf("bad parse: %+v err=%v", tt, err)
	}
	if _, err := parseFixTargets("bogus"); err == nil {
		t.Fatal("unknown target must error")
	}
}

// TestFlagsRegistered guards against init() edits silently dropping flags.
func TestFlagsRegistered(t *testing.T) {
	for _, name := range []string{
		"input", "output", "cache-dir", "no-cache", "fix",
		"proxy", "insecure", "cookie",
		"interval", "retry-base", "retry-attempts",
	} {
		if rootCmd.Flags().Lookup(name) == nil {
			t.Errorf("flag --%s not registered", name)
		}
	}
}
