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

// fixTargets selects which artifacts --fix regenerates.
type fixTargets struct {
	nfo         bool
	fanart      bool
	thumb       bool
	extrafanart bool
	poster      bool
	symlink     bool
}

// missingOnly: only create/repair absent or broken artifacts (default).
// A named target (e.g. --fix poster) forces regeneration of that artifact
// even when it already exists and is valid.
func (t fixTargets) missingOnly() bool {
	return !t.nfo && !t.fanart && !t.thumb && !t.extrafanart && !t.poster && !t.symlink
}

// parseFixTargets parses the --fix value: empty/"missing" = repair only
// missing/broken; otherwise a comma list of nfo,fanart,thumb,extrafanart,
// poster,symlink (each forces regeneration; implies missing repair too).
func parseFixTargets(spec string) (fixTargets, error) {
	var t fixTargets
	for _, part := range strings.Split(spec, ",") {
		switch strings.ToLower(strings.TrimSpace(part)) {
		case "", "missing":
			// default behavior, nothing forced
		case "nfo":
			t.nfo = true
		case "fanart":
			t.fanart = true
		case "thumb":
			t.thumb = true
		case "extrafanart":
			t.extrafanart = true
		case "poster":
			t.poster = true
		case "symlink":
			t.symlink = true
		default:
			return t, fmt.Errorf("unknown --fix target %q (valid: nfo,fanart,thumb,extrafanart,poster,symlink,missing)", part)
		}
	}
	return t, nil
}

// materializeArtifacts writes/repairs everything inside {output}/video/CODE
// plus the index symlinks. Existing complete files are kept unless the
// corresponding target is forced; broken/missing ones are restored from
// cached bytes or re-downloaded via api (nil api = offline).
func materializeArtifacts(ctx context.Context, api *client.Client, outputDir, movieCode string,
	detail *javdbapi.VideoDetail, cover []byte, extraFanart [][]byte,
	actresses map[string]bool, t fixTargets) {

	movieFolder := filepath.Join(outputDir, "video", movieCode)
	nfoPath := filepath.Join(movieFolder, movieCode+".nfo")
	posterPath := filepath.Join(movieFolder, "poster.jpg")
	fanartPath := filepath.Join(movieFolder, "fanart.jpg")
	thumbPath := filepath.Join(movieFolder, "thumb.jpg")

	if err := os.MkdirAll(movieFolder, 0755); err != nil {
		fmt.Printf("  ❌ Failed to create movie folder: %v\n", err)
		return
	}

	// NFO.
	if !t.nfo && cache.ValidNFOFile(nfoPath) {
		fmt.Printf("  📄 NFO already complete\n")
	} else if err := createNFOFile(nfoPath, movieCode, detail, actresses); err != nil {
		fmt.Printf("  ⚠️  Failed to create NFO file: %v\n", err)
	} else {
		fmt.Printf("  📄 Created NFO file\n")
	}

	// fanart: full landscape cover.
	if !t.fanart && imageutil.ValidFile(fanartPath) {
		fmt.Printf("  🖼️  fanart.jpg already complete\n")
	} else if imageutil.Valid(cover) {
		if err := client.AtomicWrite(fanartPath, cover, 0644); err != nil {
			fmt.Printf("  ⚠️  Failed to write fanart.jpg: %v\n", err)
		} else {
			fmt.Printf("  🖼️  fanart.jpg written from cache\n")
		}
	} else if api != nil && detail.Summary.CoverURL != "" {
		if err := api.DownloadImage(ctx, detail.Summary.CoverURL, fanartPath, retryBase, retryAttempts); err != nil {
			fmt.Printf("  ⚠️  Failed to download fanart.jpg: %v\n", err)
		} else {
			fmt.Printf("  🖼️  Downloaded fanart.jpg\n")
		}
	} else {
		fmt.Printf("  ⚠️  fanart.jpg missing and no cached cover to restore\n")
	}

	// thumb: copy of fanart.
	if !t.thumb && imageutil.ValidFile(thumbPath) {
		fmt.Printf("  🖼️  thumb.jpg already complete\n")
	} else {
		var src []byte
		if imageutil.ValidFile(fanartPath) {
			src, _ = os.ReadFile(fanartPath)
		} else if imageutil.Valid(cover) {
			src = cover
		}
		if imageutil.Valid(src) {
			if err := client.AtomicWrite(thumbPath, src, 0644); err != nil {
				fmt.Printf("  ⚠️  Failed to write thumb.jpg: %v\n", err)
			} else {
				fmt.Printf("  🖼️  thumb.jpg copied from fanart\n")
			}
		} else {
			fmt.Printf("  ⚠️  thumb.jpg: no fanart available to copy\n")
		}
	}

	// poster: portrait crop of fanart (full cover). Repair when missing,
	// corrupt, or still landscape (legacy full-cover poster).
	posterOK := false
	if !t.poster && imageutil.ValidFile(posterPath) {
		if portrait, err := imageutil.IsPortraitFile(posterPath); err == nil && portrait {
			posterOK = true
			fmt.Printf("  🖼️  poster.jpg already complete (portrait)\n")
		}
	}
	if !posterOK {
		var src []byte
		switch {
		case imageutil.ValidFile(fanartPath):
			src, _ = os.ReadFile(fanartPath)
		case imageutil.Valid(cover):
			src = cover
		case api != nil && detail.Summary.CoverURL != "":
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
				if !t.extrafanart && imageutil.ValidFile(p) {
					continue
				}
				if i < len(extraFanart) && imageutil.Valid(extraFanart[i]) {
					if err := client.AtomicWrite(p, extraFanart[i], 0644); err == nil {
						restored++
						continue
					}
				}
				if api == nil || shot.FullURL == "" {
					continue
				}
				if err := api.DownloadImage(ctx, shot.FullURL, p, retryBase, retryAttempts); err != nil {
					fmt.Printf("  ⚠️  Failed to download extrafanart-%d: %v\n", i, err)
				} else {
					downloaded++
				}
			}
			if restored+downloaded > 0 {
				fmt.Printf("  🖼️  extrafanart: %d written from cache, %d downloaded\n", restored, downloaded)
			}
		}
	}

	// Symlinks: fullname + actor/tag. Idempotent — correct links are kept,
	// stale ones replaced. t.symlink forces re-creation of every link.
	movieFolderAbs, _ := filepath.Abs(movieFolder)
	if t.symlink {
		removeIndexSymlinks(outputDir, movieCode, movieFolderAbs)
	}
	if createFullnameSymlink(outputDir, movieCode, detail.Summary.Title, movieFolder) {
		fmt.Printf("  🔗 fullname symlink created: %s %s\n", movieCode, detail.Summary.Title)
	}
	// Only actresses get actor/ symlinks: male co-stars would otherwise
	// create bogus actor entries (a work with 1 actress and 5 actors would
	// fan out into 6 links). castToLink picks the actresses when javdb's
	// female flags were resolved, and falls back to everyone otherwise so an
	// unresolved entry never loses links.
	linkCount := 0
	for _, name := range castToLink(detail.Actors, actresses) {
		if createIndexSymlink(outputDir, "actor", name, movieCode, movieFolder) {
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

// castToLink returns the actor names that should get an actor/ symlink.
// When actresses is nil (gender not resolved) every actor is linked, which
// is the pre-gender behavior; when resolved, only flagged actresses are.
func castToLink(actors []javdbapi.Actor, actresses map[string]bool) []string {
	if actresses == nil {
		names := make([]string, 0, len(actors))
		for _, a := range actors {
			names = append(names, a.Name)
		}
		return names
	}
	var names []string
	for _, a := range actors {
		if actresses[a.Name] {
			names = append(names, a.Name)
		}
	}
	return names
}

// removeIndexSymlinks deletes all actor/tag/fullname symlinks pointing at
// this movie folder (used by --fix symlink before re-creating them).
func removeIndexSymlinks(outputDir, movieCode, movieFolderAbs string) {
	for _, kind := range []string{"actor", "tag", "fullname"} {
		root := filepath.Join(outputDir, kind)
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			sub := filepath.Join(root, e.Name())
			links, err := os.ReadDir(sub)
			if err != nil {
				continue
			}
			for _, l := range links {
				p := filepath.Join(sub, l.Name())
				target, err := os.Readlink(p)
				if err != nil {
					continue // not a symlink
				}
				abs := target
				if !filepath.IsAbs(abs) {
					abs = filepath.Join(sub, target)
				}
				if absClean, err := filepath.Abs(abs); err == nil && absClean == movieFolderAbs {
					os.Remove(p)
				}
			}
			// Drop the index folder once its last link is gone, so stale
			// actor entries (e.g. male co-stars linked by older versions)
			// disappear from the tree instead of lingering empty.
			if remaining, err := os.ReadDir(sub); err == nil && len(remaining) == 0 {
				os.Remove(sub)
			}
		}
	}
}

// fixOutput repairs an existing output tree without scanning input.
// Cache is used when present; missing entries (or noCache=true) are
// re-scraped from the network. Targets select forced regeneration.
func fixOutput(ctx context.Context, api *client.Client, c *cache.Cache, outputDir string, t fixTargets, noCache bool) {
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

		var entry *cache.CacheEntry
		if !noCache {
			entry = c.Load(movieCode)
		}
		if entry == nil {
			fmt.Printf("  🌐 No usable cache, scraping...\n")
			d, err := api.SearchByCode(ctx, movieCode)
			if err != nil {
				fmt.Printf("  ❌ Failed to scrape: %v\n\n", err)
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
			names, resolved := resolveActresses(ctx, api, d)
			entry = &cache.CacheEntry{
				CachedAt:          time.Now(),
				Detail:            d,
				Poster:            cover,
				ExtraFanart:       ef,
				Actresses:         names.names(),
				ActressesResolved: resolved,
			}
			if err := c.Store(movieCode, entry); err != nil {
				fmt.Printf("  ⚠️  Failed to write cache: %v\n", err)
			}
		} else {
			fmt.Printf("  💾 Using cached result (cached at %s)\n", entry.CachedAt.Format(time.RFC3339))
			// Older cache entries predate the actress list; resolve it now
			// and persist so the fetch happens once per code, ever.
			if _, resolved := entry.ActressSet(); !resolved {
				backfillActresses(ctx, api, c, movieCode, entry)
			}
		}

		actresses, _ := entry.ActressSet()
		materializeArtifacts(ctx, api, outputDir, movieCode, entry.Detail, entry.Poster, entry.ExtraFanart, actresses, t)
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
			case "video", "actor", "tag", "fullname", "failed":
				continue
			}
			if strings.Contains(e.Name(), "-") || strings.Contains(e.Name(), "FC2") {
				fmt.Printf("⚠️  Legacy flat movie folder found (not managed by --fix): %s\n", filepath.Join(outputDir, e.Name()))
			}
		}
	}

	fmt.Printf("Fix complete: %d repaired/verified, %d skipped\n", fixed, skipped)
}

// actressSet is the resolved set of actress names (nil = unresolved).
type actressSet map[string]bool

// names flattens the set for JSON storage (nil when unresolved).
func (a actressSet) names() []string {
	if a == nil {
		return nil
	}
	out := make([]string, 0, len(a))
	for n := range a {
		out = append(out, n)
	}
	return out
}

// resolveActresses asks javdb which actors on this video's page are female.
// resolved=false means the lookup failed (offline, no api, network error):
// callers must keep the previous behavior instead of guessing.
func resolveActresses(ctx context.Context, api *client.Client, detail *javdbapi.VideoDetail) (actressSet, bool) {
	if api == nil || detail == nil || detail.Summary.ID == "" {
		return nil, false
	}
	names, err := api.ActressNames(ctx, string(detail.Summary.ID))
	if err != nil {
		fmt.Printf("  ⚠️  Failed to resolve actresses: %v (all actors will be linked)\n", err)
		return nil, false
	}
	return actressSet(names), true
}
