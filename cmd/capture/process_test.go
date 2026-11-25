package main

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RPbro/javdbapi"

	"github.com/gitsang/capture/pkg/cache"
	"github.com/gitsang/capture/pkg/client"
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
