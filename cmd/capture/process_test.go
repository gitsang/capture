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
		// ActressesResolved: these entries stand in for current-format cache
		// entries, so the cached path makes no extra detail-page request.
		if err := c.Store(movieCode, &cache.CacheEntry{
			CachedAt: time.Now(), Detail: testDetail(), Poster: png, ExtraFanart: [][]byte{png, png},
			Actresses: []string{"女優A", "女優B"}, ActressesResolved: true,
		}); err != nil {
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

	// No api (offline): the actress lookup is skipped, so every actor keeps
	// its symlink and the repair loop never touches the network.
	fixOutput(context.Background(), nil, c, out, fixTargets{}, false)

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
	fixOutput(context.Background(), nil, c, out, fixTargets{poster: true}, false)
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

// TestQuarantineFailed checks failed sources are moved to {output}/failed
// with the original name, collisions get a suffix, and rescans skip them.
func TestQuarantineFailed(t *testing.T) {
	base := t.TempDir()
	in := filepath.Join(base, "in")
	out := filepath.Join(base, "out")
	os.MkdirAll(in, 0755)

	src := filepath.Join(in, "AIFOCS-004.mp4")
	os.WriteFile(src, []byte("v"), 0644)
	quarantineFailed(out, VideoFile{Path: src, Filename: "AIFOCS-004.mp4", Code: "AIFOCS-004"})

	dst := filepath.Join(out, "failed", "AIFOCS-004.mp4")
	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("failed file missing: %v", err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatal("source should be moved away")
	}

	// collision: second file with same name gets -1 suffix
	os.WriteFile(src, []byte("v2"), 0644)
	quarantineFailed(out, VideoFile{Path: src, Filename: "AIFOCS-004.mp4", Code: "AIFOCS-004"})
	if _, err := os.Stat(filepath.Join(out, "failed", "AIFOCS-004-1.mp4")); err != nil {
		t.Fatalf("collision suffix missing: %v", err)
	}

	// scanning the whole base dir skips the output tree incl. failed/
	files, err := scanVideoFiles(base, out)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Errorf("rescan found %d files, want 0 (failed/ must be skipped)", len(files))
	}
}

// TestCastToLink covers actress-only symlinking: with a resolved actress set
// only actresses are linked (male co-stars must not create actor entries);
// with no resolution every actor is linked (pre-gender behavior).
func TestCastToLink(t *testing.T) {
	actors := []javdbapi.Actor{
		{Name: "河合あすな"}, {Name: "野々浦暖"}, // actresses
		{Name: "小田切ジュン"}, {Name: "セツネヒデユキ"}, // male co-stars
	}
	got := castToLink(actors, map[string]bool{"河合あすな": true, "野々浦暖": true})
	want := []string{"河合あすな", "野々浦暖"}
	if len(got) != len(want) {
		t.Fatalf("castToLink = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("castToLink = %v, want %v", got, want)
		}
	}

	// unresolved (nil set) links everyone
	if got := castToLink(actors, nil); len(got) != len(actors) {
		t.Errorf("unresolved castToLink = %v, want all %d actors", got, len(actors))
	}
	// resolved but no match links nobody (unknown cast)
	if got := castToLink(actors, map[string]bool{}); len(got) != 0 {
		t.Errorf("castToLink with empty set = %v, want none", got)
	}
}

// TestNFOActorType covers the <type> element: female/male when resolved,
// omitted entirely when the actress list was not resolved.
func TestNFOActorType(t *testing.T) {
	if got := actorType("河合あすな", map[string]bool{"河合あすな": true}); got != "female" {
		t.Errorf("actress type = %q, want female", got)
	}
	if got := actorType("小田切ジュン", map[string]bool{"河合あすな": true}); got != "male" {
		t.Errorf("male type = %q, want male", got)
	}
	if got := actorType("河合あすな", nil); got != "" {
		t.Errorf("unresolved type = %q, want empty", got)
	}

	// end-to-end: the element lands in the NFO for resolved entries and is
	// absent for unresolved ones.
	dir := t.TempDir()
	d := testDetail()
	mixed := map[string]bool{"女優A": true}
	nfoPath := filepath.Join(dir, "ACTRESS-001.nfo")
	if err := createNFOFile(nfoPath, "ACTRESS-001", d, mixed); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(nfoPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "<type>female</type>") {
		t.Error("resolved NFO missing <type>female</type>")
	}
	if !strings.Contains(string(data), "<type>male</type>") {
		t.Error("resolved NFO missing <type>male</type> for co-star")
	}

	if err := createNFOFile(nfoPath, "ACTRESS-001", d, nil); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(nfoPath)
	if strings.Contains(string(data), "<type>") {
		t.Error("unresolved NFO must omit <type>")
	}
}
