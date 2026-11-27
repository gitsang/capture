package main

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RPbro/javdbapi"

	"github.com/gitsang/capture/pkg/cache"
	"github.com/gitsang/capture/pkg/client"
	"github.com/gitsang/capture/pkg/code"
)

var videoExts = []string{".mp4", ".mkv", ".wmv", ".avi"}

// VideoFile represents a video file found during scanning
type VideoFile struct {
	Path     string
	Filename string
	Code     string
	// Part is the split-movie disc marker (KAVR-316_A -> "A"), empty for
	// single-file movies. It only affects the on-disk filename: a movie's
	// parts are separate files, not separate entries.
	Part string
}

// NFOData is the Emby/Jellyfin-style NFO layout (ported from the debug
// branch): flat art fields, CDATA titles, dual genre/tag, release dates.
type NFOData struct {
	XMLName       xml.Name   `xml:"movie"`
	Title         string     `xml:"title"`
	OriginalTitle string     `xml:"originaltitle"`
	SortTitle     string     `xml:"sorttitle"`
	CustomRating  string     `xml:"customrating"`
	MPAA          string     `xml:"mpaa"`
	Set           string     `xml:"set"`
	Plot          string     `xml:"plot"`
	Outline       string     `xml:"outline"`
	Runtime       string     `xml:"runtime"`
	Year          string     `xml:"year"`
	Studio        string     `xml:"studio"`
	Director      string     `xml:"director"`
	Poster        string     `xml:"poster"`
	Thumb         string     `xml:"thumb"`
	Fanart        string     `xml:"fanart"`
	Genre         []string   `xml:"genre"`
	Tag           []string   `xml:"tag"`
	Actor         []NFOActor `xml:"actor"`
	Maker         string     `xml:"maker"`
	Label         string     `xml:"label"`
	Num           string     `xml:"num"`
	Premiered     string     `xml:"premiered"`
	ReleaseDate   string     `xml:"releasedate"`
	Release       string     `xml:"release"`
	Cover         string     `xml:"cover"`
	Website       string     `xml:"website"`
}

type NFOActor struct {
	Name string `xml:"name"`
	Role string `xml:"role"`
	// Type is female/male when javdb's female flags were resolved, empty
	// otherwise. Kodi/tinyMediaManager read it; Emby/Jellyfin ignore it.
	Type string `xml:"type,omitempty"`
}

// actorType maps an actor to the NFO <type> value. Unknown when the
// actress list was not resolved (nil): better silent than wrong.
func actorType(name string, actresses map[string]bool) string {
	if actresses == nil {
		return ""
	}
	if actresses[name] {
		return "female"
	}
	return "male"
}

// t selects forcibly regenerated artifacts; the normal scan passes the zero
// value (repair only what is missing/broken).
func processVideo(ctx context.Context, api *client.Client, c *cache.Cache, outputDir string, noCache bool, video VideoFile, t fixTargets) {
	if video.Code == "" {
		fmt.Printf("  ⚠️  No code found in filename, skipping\n")
		return
	}
	if video.Part != "" {
		fmt.Printf("  📋 Code: %s (part %s)\n", video.Code, video.Part)
	} else {
		fmt.Printf("  📋 Code: %s\n", video.Code)
	}

	movieFolder := filepath.Join(outputDir, "video", video.Code)
	newVideoPath := filepath.Join(movieFolder, videoFileName(video))

	// 1. Get metadata: cache first (unless --no-cache), then live scrape.
	var detail *javdbapi.VideoDetail
	var poster []byte
	var extraFanart [][]byte
	var actresses actressSet

	if !noCache {
		if entry := c.Load(video.Code); entry != nil {
			detail = entry.Detail
			poster = entry.Poster
			extraFanart = entry.ExtraFanart
			if set, ok := entry.ActressSet(); ok {
				actresses = set
			} else if set, ok := backfillActresses(ctx, api, c, video.Code, entry); ok {
				// Cache predates the actress list: resolve once, persist it.
				actresses = set
			}
			fmt.Printf("  💾 Using cached result (cached at %s)\n", entry.CachedAt.Format(time.RFC3339))
		}
	}

	if detail == nil {
		d, err := api.SearchByCode(ctx, video.Code)
		if err != nil {
			fmt.Printf("  ❌ Failed to fetch movie data: %v\n", err)
			quarantineFailed(outputDir, video)
			return
		}
		detail = d
		fmt.Printf("  ✅ Found movie data: %s\n", detail.Summary.Title)

		// Download cover into memory for caching (poster doubles as fanart/thumb).
		if url := detail.Summary.CoverURL; url != "" {
			p, err := api.DownloadImageBytes(ctx, url, retryBase, retryAttempts)
			if err != nil {
				fmt.Printf("  ⚠️  Failed to download cover: %v\n", err)
			} else {
				poster = p
			}
		}

		// Download screenshots (extrafanart) into memory for caching.
		// Bytes stay index-aligned with detail.Screenshots; a failed download
		// leaves a nil slot so it can be retried on a later run.
		fetched := 0
		for i, shot := range detail.Screenshots {
			var b []byte
			if shot.FullURL != "" {
				var err error
				b, err = api.DownloadImageBytes(ctx, shot.FullURL, retryBase, retryAttempts)
				if err != nil {
					fmt.Printf("  ⚠️  Failed to download extrafanart-%d: %v\n", i, err)
					b = nil
				} else {
					fetched++
				}
			}
			extraFanart = append(extraFanart, b)
		}
		if fetched > 0 {
			fmt.Printf("  🖼️  Fetched %d extrafanart images\n", fetched)
		}

		names, resolved := resolveActresses(ctx, api, detail)
		if resolved {
			actresses = names
		}

		entry := &cache.CacheEntry{
			CachedAt:          time.Now(),
			Detail:            detail,
			Poster:            poster,
			ExtraFanart:       extraFanart,
			Actresses:         actresses.names(),
			ActressesResolved: resolved,
		}
		if err := c.Store(video.Code, entry); err != nil {
			fmt.Printf("  ⚠️  Failed to write cache: %v\n", err)
		}
	}

	// 2-4b. Materialize NFO + artwork (shared with --fix).
	materializeArtifacts(ctx, api, outputDir, video.Code, detail, poster, extraFanart, actresses, t)

	// 5. Move the video file.
	//
	// Only an already-moved source (same path, or the same file reached
	// through a different route) may be deleted. Anything else that occupies
	// the target is NOT removed: silently discarding a source whose target
	// name happens to collide would destroy user data, and a collision here
	// is precisely what a mismatched part suffix would cause. Keep the
	// existing file and quarantine the newcomer so nothing is lost.
	if _, err := os.Stat(newVideoPath); err == nil {
		if sameFile(video.Path, newVideoPath) {
			fmt.Printf("  📁 Video already in place: %s\n", newVideoPath)
		} else {
			fmt.Printf("  ⚠️  Target already exists, not overwriting: %s\n", newVideoPath)
			quarantineFailed(outputDir, video)
		}
	} else if err := moveFile(video.Path, newVideoPath); err != nil {
		fmt.Printf("  ❌ Failed to move video file: %v\n", err)
		quarantineFailed(outputDir, video)
	} else {
		fmt.Printf("  📁 Moved video file to: %s\n", newVideoPath)
	}
}

// backfillActresses resolves and persists the actress list for a cache entry
// written before the field existed, so the extra detail-page fetch happens
// once per code instead of on every run.
func backfillActresses(ctx context.Context, api *client.Client, c *cache.Cache, movieCode string, entry *cache.CacheEntry) (actressSet, bool) {
	names, ok := resolveActresses(ctx, api, entry.Detail)
	if !ok {
		return nil, false
	}
	entry.Actresses = names.names()
	entry.ActressesResolved = true
	if err := c.Store(movieCode, entry); err != nil {
		fmt.Printf("  ⚠️  Failed to update cache with actress list: %v\n", err)
	}
	return names, true
}

// quarantineFailed moves an unprocessable source file to {output}/failed/
// (original filename kept) so later runs do not retry it. The output tree
// is skipped by the scanner, so failed files stay out of the work list.
// To retry them later, move them back into the input directory.
func quarantineFailed(outputDir string, video VideoFile) {
	failedDir := filepath.Join(outputDir, "failed")
	if err := os.MkdirAll(failedDir, 0755); err != nil {
		fmt.Printf("  ⚠️  Failed to create failed dir: %v\n", err)
		return
	}
	// Already parked (retried by --fix failed and failed again): leave it, so
	// repeated retries do not accumulate -1, -2, -3 suffixed copies.
	if filepath.Dir(video.Path) == failedDir {
		fmt.Printf("  🚫 Still failing, kept in failed dir: %s\n", video.Path)
		return
	}
	dst := filepath.Join(failedDir, video.Filename)
	if _, err := os.Stat(dst); err == nil {
		// Same name already quarantined: keep both by suffixing.
		ext := filepath.Ext(video.Filename)
		base := strings.TrimSuffix(video.Filename, ext)
		for i := 1; ; i++ {
			dst = filepath.Join(failedDir, fmt.Sprintf("%s-%d%s", base, i, ext))
			if _, err := os.Stat(dst); err != nil {
				break
			}
		}
	}
	if err := moveFile(video.Path, dst); err != nil {
		fmt.Printf("  ⚠️  Failed to move to failed dir: %v\n", err)
		return
	}
	fmt.Printf("  🚫 Moved to failed dir (will be skipped on future runs): %s\n", dst)
}

// createIndexSymlink links {output}/{kind}/{name}/{CODE} -> movieFolder.
// Returns true when a new link was created.
func createIndexSymlink(outputDir, kind, name, movieCode, movieFolder string) bool {
	name = cache.SanitizeName(name)
	if name == "" {
		return false
	}
	dir := filepath.Join(outputDir, kind, name)
	return linkDir(dir, movieCode, movieFolder, kind)
}

// createFullnameSymlink links {output}/fullname/{CODE} {title} -> ../video/CODE.
// When the title changes, stale links for the same code are replaced.
func createFullnameSymlink(outputDir, movieCode, title, movieFolder string) bool {
	title = cache.SanitizeName(title)
	if title == "" {
		return false
	}
	dir := filepath.Join(outputDir, "fullname")
	name := fullnameEntryName(movieCode, title)

	// Remove stale "{CODE} ..." links whose title changed (or old plain-CODE link).
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			old := e.Name()
			if old == name {
				continue
			}
			if old == movieCode || strings.HasPrefix(old, movieCode+" ") {
				p := filepath.Join(dir, old)
				if _, err := os.Readlink(p); err == nil {
					os.Remove(p)
				}
			}
		}
	}
	return linkDir(dir, name, movieFolder, "fullname")
}

// fullnameEntryName builds "{CODE} {title}", truncating the title so the
// whole entry stays within the 255-byte filesystem limit (Japanese titles
// are 3 bytes per rune in UTF-8 and can exceed it).
func fullnameEntryName(movieCode, title string) string {
	const maxBytes = 255
	name := movieCode + " " + title
	if len(name) <= maxBytes {
		return name
	}
	budget := maxBytes - len(movieCode) - 1 // space
	if budget <= 0 {
		return movieCode
	}
	// Cut on rune boundaries so we never split a UTF-8 sequence.
	runes := []rune(title)
	for len(string(runes)) > budget {
		runes = runes[:len(runes)-1]
	}
	return movieCode + " " + string(runes)
}

// linkDir creates {dir}/{name} as a relative symlink to movieFolder.
// Returns true when a new link was created.
func linkDir(dir, name, movieFolder, kind string) bool {
	if err := os.MkdirAll(dir, 0755); err != nil {
		fmt.Printf("  ⚠️  Failed to create %s dir %s: %v\n", kind, dir, err)
		return false
	}
	link := filepath.Join(dir, name)

	// Relative target (e.g. ../../video/CODE or ../video/CODE), so the whole
	// output tree can be moved/renamed without breaking links.
	relTarget, err := filepath.Rel(dir, movieFolder)
	if err != nil {
		relTarget = movieFolder
	}

	if target, err := os.Readlink(link); err == nil {
		if target == relTarget {
			return false // already correct
		}
		os.Remove(link) // stale (or old absolute) link, replace
	} else if _, err := os.Lstat(link); err == nil {
		fmt.Printf("  ⚠️  %s exists and is not a symlink, skipping: %s\n", kind, link)
		return false
	}

	if err := os.Symlink(relTarget, link); err != nil {
		fmt.Printf("  ⚠️  Failed to create symlink %s: %v\n", link, err)
		return false
	}
	return true
}

// scanVideoFiles walks dir collecting video files, skipping the output
// directory (by absolute path) and never following symlinked directories.
func scanVideoFiles(dir, absOutput string) ([]VideoFile, error) {
	var videoFiles []VideoFile

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			abs, aerr := filepath.Abs(path)
			if aerr == nil && abs == absOutput {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip symlinks entirely: actor/tag index links point at video dirs.
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}

		if !isVideoExt(path) {
			return nil
		}

		filename := filepath.Base(path)
		movieCode, part := code.ExtractWithPart(filename)
		videoFiles = append(videoFiles, VideoFile{
			Path:     path,
			Filename: filename,
			Code:     movieCode,
			Part:     part,
		})
		return nil
	})

	return videoFiles, err
}

// websiteFor links to the javdb page when the movie came from javdb. Entries
// resolved through the r18.dev fallback have no javdb id, and a javdatabase
// URL for a movie javdb does not carry would just 404.
func websiteFor(movieCode, videoID string) string {
	if videoID == "" {
		return ""
	}
	return fmt.Sprintf("https://javdb.com/v/%s", videoID)
}

func createNFOFile(nfoPath, movieCode string, detail *javdbapi.VideoDetail, actresses map[string]bool) error {
	s := detail.Summary

	year, date := "", ""
	if !s.PublishedAt.IsZero() {
		year = s.PublishedAt.Format("2006")
		date = s.PublishedAt.Format("2006-01-02")
	}

	runtime := "119minutes" // debug-branch default when unknown
	if detail.DurationMinutes != nil && *detail.DurationMinutes > 0 {
		runtime = fmt.Sprintf("%dminutes", *detail.DurationMinutes)
	}
	maker := ""
	if detail.Maker != nil {
		maker = detail.Maker.Name
	}
	director := ""
	if detail.Director != nil {
		director = detail.Director.Name
	}
	label := ""
	if detail.Series != nil {
		label = detail.Series.Name
	}

	fullTitle := fmt.Sprintf("<![CDATA[%s-%s]]>", movieCode, s.Title)
	plot := fmt.Sprintf("<![CDATA[%s]]>", s.Title)
	if s.Score != nil {
		plot = fmt.Sprintf("<![CDATA[%s\nScore: %.2f (%d votes)]]>", s.Title, s.Score.Value, s.Score.Count)
	}

	nfo := NFOData{
		Title:         fullTitle,
		OriginalTitle: fullTitle,
		SortTitle:     fullTitle,
		CustomRating:  "JP-18+",
		MPAA:          "JP-18+",
		Set:           "\uA789", // modifier-letter colon, debug-branch convention
		Plot:          plot,
		Outline:       fmt.Sprintf("<![CDATA[%s]]>", s.Title),
		Runtime:       runtime,
		Year:          year,
		Studio:        maker,
		Director:      director,
		Poster:        "poster.jpg",
		Thumb:         "thumb.jpg",
		Fanart:        "fanart.jpg",
		Maker:         maker,
		Label:         label,
		Num:           strings.ToLower(movieCode),
		Premiered:     date,
		ReleaseDate:   date,
		Release:       date,
		Cover:         s.CoverURL,
		Website:       websiteFor(movieCode, string(s.ID)),
	}
	for _, tag := range detail.Tags {
		nfo.Genre = append(nfo.Genre, tag.Name)
		nfo.Tag = append(nfo.Tag, tag.Name)
	}
	for _, actor := range detail.Actors {
		nfo.Actor = append(nfo.Actor, NFOActor{
			Name: actor.Name,
			Role: "",
			Type: actorType(actor.Name, actresses),
		})
	}

	xmlData, err := xml.MarshalIndent(nfo, "", "  ")
	if err != nil {
		return err
	}
	xmlContent := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" ?>
%s`, string(xmlData))
	// encoding/xml escapes the CDATA markers we injected; restore them.
	xmlContent = strings.ReplaceAll(xmlContent, "&lt;![CDATA[", "<![CDATA[")
	xmlContent = strings.ReplaceAll(xmlContent, "]]&gt;", "]]>")
	// DOS line endings, as expected by some media servers (debug branch).
	xmlContent = strings.ReplaceAll(xmlContent, "\n", "\r\n")

	return client.AtomicWrite(nfoPath, []byte(xmlContent), 0644)
}

// isVideoExt reports whether path has one of the video extensions we manage.
func isVideoExt(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	for _, videoExt := range videoExts {
		if ext == videoExt {
			return true
		}
	}
	return false
}

// videoFileName is the name a source takes inside the movie folder. The disc
// marker is appended so KAVR-316_A/B never collide: without it every part
// resolves to KAVR-316.mp4 and the parts overwrite (or, previously, deleted)
// each other.
func videoFileName(v VideoFile) string {
	name := v.Code
	if v.Part != "" {
		name += "_" + v.Part
	}
	return name + strings.ToLower(filepath.Ext(v.Path))
}

// sameFile reports whether two paths are the same file on disk, so an
// already-moved source can be recognized without deleting a real duplicate.
func sameFile(a, b string) bool {
	ai, err := os.Stat(a)
	if err != nil {
		return false
	}
	bi, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(ai, bi)
}

func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	return copyAndDelete(src, dst)
}

func copyAndDelete(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return err
	}
	if err := dstFile.Sync(); err != nil {
		return err
	}
	return os.Remove(src)
}
