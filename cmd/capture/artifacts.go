package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RPbro/javdbapi"

	"github.com/gitsang/capture/pkg/cache"
	"github.com/gitsang/capture/pkg/client"
	"github.com/gitsang/capture/pkg/imageutil"
)

// materializeArtifacts writes/repairs everything inside {output}/video/CODE
// plus the index symlinks: NFO, poster (portrait crop), fanart, thumb,
// extrafanart, fullname/actor/tag links. Existing complete files are kept;
// broken/missing ones are restored from cached bytes or re-downloaded via
// api (which may be nil in offline --fix runs).
//
// allowDownload=false skips all network fallbacks (cache-only repair).
func materializeArtifacts(ctx context.Context, api *client.Client, outputDir, movieCode string,
	detail *javdbapi.VideoDetail, cover []byte, extraFanart [][]byte, allowDownload bool) {

	movieFolder := filepath.Join(outputDir, "video", movieCode)
	nfoPath := filepath.Join(movieFolder, movieCode+".nfo")
	posterPath := filepath.Join(movieFolder, "poster.jpg")
	fanartPath := filepath.Join(movieFolder, "fanart.jpg")
	thumbPath := filepath.Join(movieFolder, "thumb.jpg")

	if err := os.MkdirAll(movieFolder, 0755); err != nil {
		fmt.Printf("  ❌ Failed to create movie folder: %v\n", err)
		return
	}

	// NFO: regenerate when missing or incomplete.
	if cache.ValidNFOFile(nfoPath) {
		fmt.Printf("  📄 NFO already complete\n")
	} else if err := createNFOFile(nfoPath, movieCode, detail); err != nil {
		fmt.Printf("  ⚠️  Failed to create NFO file: %v\n", err)
	} else {
		fmt.Printf("  📄 Created NFO file\n")
	}

	// fanart/thumb: full landscape cover.
	for _, img := range []struct{ name, path string }{
		{"fanart", fanartPath},
		{"thumb", thumbPath},
	} {
		if imageutil.ValidFile(img.path) {
			fmt.Printf("  🖼️  %s.jpg already complete\n", img.name)
			continue
		}
		if imageutil.Valid(cover) {
			if err := client.AtomicWrite(img.path, cover, 0644); err != nil {
				fmt.Printf("  ⚠️  Failed to write %s.jpg: %v\n", img.name, err)
			} else {
				fmt.Printf("  🖼️  Restored %s.jpg from cache\n", img.name)
			}
			continue
		}
		if allowDownload && api != nil && detail.Summary.CoverURL != "" {
			if err := api.DownloadImage(ctx, detail.Summary.CoverURL, img.path, retryBase, retryAttempts); err != nil {
				fmt.Printf("  ⚠️  Failed to download %s.jpg: %v\n", img.name, err)
			} else {
				fmt.Printf("  🖼️  Downloaded %s.jpg\n", img.name)
			}
		} else {
			fmt.Printf("  ⚠️  %s.jpg missing and no cached cover to restore\n", img.name)
		}
	}

	// poster: portrait crop of the cover's right half. Repair when missing,
	// corrupt, or still landscape (legacy full-cover poster).
	posterOK := false
	if imageutil.ValidFile(posterPath) {
		if portrait, err := imageutil.IsPortraitFile(posterPath); err == nil && portrait {
			posterOK = true
			fmt.Printf("  🖼️  poster.jpg already complete (portrait)\n")
		}
	}
	if !posterOK {
		var src []byte
		switch {
		case imageutil.Valid(cover):
			src = cover
		case imageutil.ValidFile(fanartPath):
			src, _ = os.ReadFile(fanartPath)
		case allowDownload && api != nil && detail.Summary.CoverURL != "":
			if b, err := api.DownloadImageBytes(ctx, detail.Summary.CoverURL, retryBase, retryAttempts); err == nil {
				src = b
			} else {
				fmt.Printf("  ⚠️  Failed to download cover for poster: %v\n", err)
			}
		}
		if imageutil.Valid(src) {
			if cropped, err := imageutil.CropPoster(src); err != nil {
				fmt.Printf("  ⚠️  Failed to crop poster: %v\n", err)
			} else if err := client.AtomicWrite(posterPath, cropped, 0644); err != nil {
				fmt.Printf("  ⚠️  Failed to write poster.jpg: %v\n", err)
			} else {
				fmt.Printf("  🖼️  poster.jpg written (portrait crop)\n")
			}
		} else {
			fmt.Printf("  ⚠️  poster.jpg missing and no cover available to crop\n")
		}
	}

	// Extrafanart: one file per screenshot, verified/repaired like the rest.
	if len(detail.Screenshots) > 0 || len(extraFanart) > 0 {
		extraDir := filepath.Join(movieFolder, "extrafanart")
		if err := os.MkdirAll(extraDir, 0755); err != nil {
			fmt.Printf("  ⚠️  Failed to create extrafanart dir: %v\n", err)
		} else {
			restored, downloaded := 0, 0
			for i, shot := range detail.Screenshots {
				p := filepath.Join(extraDir, fmt.Sprintf("extrafanart-%d.jpg", i))
				if imageutil.ValidFile(p) {
					continue
				}
				if i < len(extraFanart) && imageutil.Valid(extraFanart[i]) {
					if err := client.AtomicWrite(p, extraFanart[i], 0644); err == nil {
						restored++
						continue
					}
				}
				if !allowDownload || api == nil || shot.FullURL == "" {
					continue
				}
				if err := api.DownloadImage(ctx, shot.FullURL, p, retryBase, retryAttempts); err != nil {
					fmt.Printf("  ⚠️  Failed to download extrafanart-%d: %v\n", i, err)
				} else {
					downloaded++
				}
			}
			if restored+downloaded > 0 {
				fmt.Printf("  🖼️  extrafanart: %d restored from cache, %d downloaded\n", restored, downloaded)
			}
		}
	}

	// Fullname index: {output}/fullname/{CODE} {title} -> ../video/CODE.
	if createFullnameSymlink(outputDir, movieCode, detail.Summary.Title, movieFolder) {
		fmt.Printf("  🔗 fullname symlink created: %s %s\n", movieCode, detail.Summary.Title)
	}

	// Symlinks for every actor and tag.
	linkCount := 0
	for _, actor := range detail.Actors {
		if createIndexSymlink(outputDir, "actor", actor.Name, movieCode, movieFolder) {
			linkCount++
		}
	}
	for _, tag := range detail.Tags {
		if createIndexSymlink(outputDir, "tag", tag.Name, movieCode, movieFolder) {
			linkCount++
		}
	}
	fmt.Printf("  🔗 %d actor/tag symlinks created/updated\n", linkCount)
}

// fixOutput repairs an existing output tree without scanning input:
// for every {output}/video/CODE folder it reloads the cached metadata and
// re-materializes missing/broken artifacts and symlinks. With --fix-online
// missing cache entries are re-scraped; otherwise they are reported only.
func fixOutput(ctx context.Context, api *client.Client, c *cache.Cache, outputDir string, online bool) {
	videoRoot := filepath.Join(outputDir, "video")
	entries, err := os.ReadDir(videoRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to read %s: %v\n", videoRoot, err)
		return
	}

	var codes []string
	for _, e := range entries {
		if e.IsDir() {
			codes = append(codes, e.Name())
		}
	}
	fmt.Printf("Found %d movie folders under %s\n\n", len(codes), videoRoot)

	fixed, skipped := 0, 0
	for i, movieCode := range codes {
		fmt.Printf("[%d/%d] Fixing: %s\n", i+1, len(codes), movieCode)

		entry := c.Load(movieCode)
		if entry == nil && online {
			fmt.Printf("  🌐 No cache, re-scraping...\n")
			d, err := api.SearchByCode(ctx, movieCode)
			if err != nil {
				fmt.Printf("  ❌ Failed to re-scrape: %v\n\n", err)
				skipped++
				continue
			}
			var cover []byte
			if u := d.Summary.CoverURL; u != "" {
				cover, _ = api.DownloadImageBytes(ctx, u, retryBase, retryAttempts)
			}
			var ef [][]byte
			for _, shot := range d.Screenshots {
				var b []byte
				if shot.FullURL != "" {
					b, _ = api.DownloadImageBytes(ctx, shot.FullURL, retryBase, retryAttempts)
				}
				ef = append(ef, b)
			}
			entry = &cache.CacheEntry{CachedAt: time.Now(), Detail: d, Poster: cover, ExtraFanart: ef}
			if err := c.Store(movieCode, entry); err != nil {
				fmt.Printf("  ⚠️  Failed to write cache: %v\n", err)
			}
		}
		if entry == nil {
			fmt.Printf("  ⚠️  No cached metadata; run with --fix-online to re-scrape. Skipping.\n\n")
			skipped++
			continue
		}

		materializeArtifacts(ctx, api, outputDir, movieCode, entry.Detail, entry.Poster, entry.ExtraFanart, online)
		fixed++
		fmt.Println()
	}

	// Legacy flat folders ({output}/CODE from very old versions) are reported.
	if rootEntries, err := os.ReadDir(outputDir); err == nil {
		for _, e := range rootEntries {
			if !e.IsDir() {
				continue
			}
			switch e.Name() {
			case "video", "actor", "tag", "fullname":
				continue
			}
			if strings.Contains(e.Name(), "-") || strings.Contains(e.Name(), "FC2") {
				fmt.Printf("⚠️  Legacy flat movie folder found (not managed by --fix): %s\n", filepath.Join(outputDir, e.Name()))
			}
		}
	}

	fmt.Printf("Fix complete: %d repaired/verified, %d skipped\n", fixed, skipped)
}
