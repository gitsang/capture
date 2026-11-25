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
	"github.com/spf13/cobra"
)

// VideoFile represents a video file found during scanning
type VideoFile struct {
	Path     string
	Filename string
	Code     string
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
}

var (
	inputDir      string
	outputDir     string
	cacheDir      string
	force         bool
	proxyURL      string
	insecure      bool
	interval      time.Duration
	retryBase     time.Duration
	retryAttempts int

	videoExts = []string{".mp4", ".mkv", ".wmv", ".avi"}
)

var rootCmd = &cobra.Command{
	Use:   "capture",
	Short: "Capture - Organize video files with metadata",
	Long: `Capture is a command-line tool that scans directories for video files,
extracts movie codes from filenames, fetches metadata, and organizes
files into properly structured folders with NFO files and cover images.

Output layout:
  {output}/video/CODE/          video + CODE.nfo + poster.jpg + fanart.jpg
                                + thumb.jpg + extrafanart/extrafanart-N.jpg
  {output}/actor/<name>/CODE    symlink to the video folder
  {output}/tag/<name>/CODE      symlink to the video folder`,
	Run: runCapture,
}

func init() {
	rootCmd.Flags().StringVarP(&inputDir, "input", "i", ".", "Input directory to scan for video files")
	rootCmd.Flags().StringVarP(&outputDir, "output", "o", "./output", "Output directory for organized files")
	rootCmd.Flags().StringVar(&cacheDir, "cache-dir", "/tmp/capture", "Directory for cached scrape results (one file per code)")
	rootCmd.Flags().BoolVar(&force, "force", false, "Ignore cached results and re-scrape")
	rootCmd.Flags().StringVar(&proxyURL, "proxy", "", "HTTP proxy URL for API and image requests (e.g. http://127.0.0.1:7890)")
	rootCmd.Flags().BoolVar(&insecure, "insecure", false, "Skip TLS certificate verification (for MITM proxies)")
	rootCmd.Flags().DurationVar(&interval, "interval", time.Minute, "Minimum interval between HTTP requests (0 disables)")
	rootCmd.Flags().DurationVar(&retryBase, "retry-base", time.Minute, "Initial retry backoff delay (doubles each retry)")
	rootCmd.Flags().IntVar(&retryAttempts, "retry-attempts", 5, "Maximum attempts per request (1 disables retries)")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runCapture(cmd *cobra.Command, args []string) {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	fmt.Printf("Starting Capture\n")
	fmt.Printf("Input directory: %s\n", inputDir)
	fmt.Printf("Output directory: %s\n", outputDir)
	fmt.Printf("Cache directory: %s (force=%v)\n", cacheDir, force)
	fmt.Printf("Request interval: %s, retry: %d attempts from %s (exponential)\n\n", interval, retryAttempts, retryBase)

	if retryAttempts < 1 {
		retryAttempts = 1
	}

	// Create output directory if it doesn't exist
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create output directory: %v\n", err)
		return
	}
	absOutput, err := filepath.Abs(outputDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to resolve output directory: %v\n", err)
		return
	}

	// Scan for video files (output directory is skipped)
	fmt.Println("Scanning for video files...")
	videoFiles, err := scanVideoFiles(inputDir, absOutput)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to scan video files: %v\n", err)
		return
	}
	fmt.Printf("Found %d video files\n\n", len(videoFiles))

	client, err := NewClient(interval, retryBase, retryAttempts, proxyURL, insecure)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create client: %v\n", err)
		return
	}
	cache := NewCache(cacheDir)

	for i, video := range videoFiles {
		fmt.Printf("[%d/%d] Processing: %s\n", i+1, len(videoFiles), video.Filename)
		processVideo(ctx, client, cache, video)
		fmt.Println()
	}

	fmt.Println("Processing complete!")
}

func processVideo(ctx context.Context, client *Client, cache *Cache, video VideoFile) {
	if video.Code == "" {
		fmt.Printf("  ⚠️  No code found in filename, skipping\n")
		return
	}
	fmt.Printf("  📋 Code: %s\n", video.Code)

	movieFolder := filepath.Join(outputDir, "video", video.Code)
	nfoPath := filepath.Join(movieFolder, video.Code+".nfo")
	posterPath := filepath.Join(movieFolder, "poster.jpg")
	fanartPath := filepath.Join(movieFolder, "fanart.jpg")
	newVideoPath := filepath.Join(movieFolder, video.Code+strings.ToLower(filepath.Ext(video.Path)))

	// 1. Get metadata: cache first (unless --force), then live scrape.
	var detail *javdbapi.VideoDetail
	var poster []byte
	var extraFanart [][]byte

	if !force {
		if entry := cache.Load(video.Code); entry != nil {
			detail = entry.Detail
			poster = entry.Poster
			extraFanart = entry.ExtraFanart
			fmt.Printf("  💾 Using cached result (cached at %s)\n", entry.CachedAt.Format(time.RFC3339))
		}
	}

	if detail == nil {
		d, err := client.SearchByCode(ctx, video.Code)
		if err != nil {
			fmt.Printf("  ❌ Failed to fetch movie data: %v\n", err)
			return
		}
		detail = d
		fmt.Printf("  ✅ Found movie data: %s\n", detail.Summary.Title)

		// Download cover into memory for caching (poster doubles as fanart/thumb).
		if url := detail.Summary.CoverURL; url != "" {
			p, err := client.DownloadImageBytes(ctx, url, retryBase, retryAttempts)
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
				b, err = client.DownloadImageBytes(ctx, shot.FullURL, retryBase, retryAttempts)
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

		entry := &CacheEntry{CachedAt: time.Now(), Detail: detail, Poster: poster, ExtraFanart: extraFanart}
		if err := cache.Store(video.Code, entry); err != nil {
			fmt.Printf("  ⚠️  Failed to write cache: %v\n", err)
		}
	}

	// 2. Create the movie folder.
	if err := os.MkdirAll(movieFolder, 0755); err != nil {
		fmt.Printf("  ❌ Failed to create movie folder: %v\n", err)
		return
	}

	// 3. NFO: regenerate when missing or incomplete.
	if validNFOFile(nfoPath) {
		fmt.Printf("  📄 NFO already complete\n")
	} else if err := createNFOFile(nfoPath, video.Code, detail); err != nil {
		fmt.Printf("  ⚠️  Failed to create NFO file: %v\n", err)
	} else {
		fmt.Printf("  📄 Created NFO file\n")
	}

	// 4. Images: verify on-disk completeness; re-materialize from cached
	//    bytes or re-download when truncated/corrupt/missing.
	for _, img := range []struct{ name, path string }{
		{"poster", posterPath},
		{"fanart", fanartPath},
		{"thumb", filepath.Join(movieFolder, "thumb.jpg")},
	} {
		if validImageFile(img.path) {
			fmt.Printf("  🖼️  %s.jpg already complete\n", img.name)
			continue
		}
		if validImage(poster) {
			if err := os.WriteFile(img.path, poster, 0644); err != nil {
				fmt.Printf("  ⚠️  Failed to write %s.jpg: %v\n", img.name, err)
			} else {
				fmt.Printf("  🖼️  Restored %s.jpg from cache\n", img.name)
			}
			continue
		}
		if url := detail.Summary.CoverURL; url != "" {
			if err := client.DownloadImage(ctx, url, img.path, retryBase, retryAttempts); err != nil {
				fmt.Printf("  ⚠️  Failed to download %s.jpg: %v\n", img.name, err)
			} else {
				fmt.Printf("  🖼️  Downloaded %s.jpg\n", img.name)
			}
		} else {
			fmt.Printf("  ⚠️  No cover URL for %s.jpg\n", img.name)
		}
	}

	// 4b. Extrafanart: one file per screenshot, verified/repaired like the rest.
	if len(detail.Screenshots) > 0 || len(extraFanart) > 0 {
		extraDir := filepath.Join(movieFolder, "extrafanart")
		if err := os.MkdirAll(extraDir, 0755); err != nil {
			fmt.Printf("  ⚠️  Failed to create extrafanart dir: %v\n", err)
		} else {
			restored, downloaded := 0, 0
			for i, shot := range detail.Screenshots {
				p := filepath.Join(extraDir, fmt.Sprintf("extrafanart-%d.jpg", i))
				if validImageFile(p) {
					continue
				}
				if i < len(extraFanart) && validImage(extraFanart[i]) {
					if err := os.WriteFile(p, extraFanart[i], 0644); err == nil {
						restored++
						continue
					}
				}
				if shot.FullURL == "" {
					continue
				}
				if err := client.DownloadImage(ctx, shot.FullURL, p, retryBase, retryAttempts); err != nil {
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

	// 5. Move the video file. When the target already exists (cached rerun,
	// duplicate source), remove the redundant source so it is not rescanned.
	if _, err := os.Stat(newVideoPath); err == nil {
		srcAbs, _ := filepath.Abs(video.Path)
		dstAbs, _ := filepath.Abs(newVideoPath)
		if srcAbs == dstAbs {
			fmt.Printf("  📁 Video already in place: %s\n", newVideoPath)
		} else if err := os.Remove(video.Path); err != nil {
			fmt.Printf("  ⚠️  Target exists, failed to remove duplicate source %s: %v\n", video.Path, err)
		} else {
			fmt.Printf("  📁 Video already in place, removed duplicate source: %s\n", video.Path)
		}
	} else if err := moveFile(video.Path, newVideoPath); err != nil {
		fmt.Printf("  ❌ Failed to move video file: %v\n", err)
	} else {
		fmt.Printf("  📁 Moved video file to: %s\n", newVideoPath)
	}

	// 6. Symlinks for every actor and tag.
	linkCount := 0
	for _, actor := range detail.Actors {
		if createIndexSymlink("actor", actor.Name, video.Code, movieFolder) {
			linkCount++
		}
	}
	for _, tag := range detail.Tags {
		if createIndexSymlink("tag", tag.Name, video.Code, movieFolder) {
			linkCount++
		}
	}
	fmt.Printf("  🔗 %d actor/tag symlinks created/updated\n", linkCount)
}

// createIndexSymlink links {output}/{kind}/{name}/{CODE} -> movieFolder.
// Returns true when a new link was created.
func createIndexSymlink(kind, name, code, movieFolder string) bool {
	name = sanitizeName(name)
	if name == "" {
		return false
	}
	dir := filepath.Join(outputDir, kind, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		fmt.Printf("  ⚠️  Failed to create %s dir %s: %v\n", kind, dir, err)
		return false
	}
	link := filepath.Join(dir, code)

	// Relative target: {output}/{kind}/{name}/CODE -> ../../video/CODE,
	// so the whole output tree can be moved/renamed without breaking links.
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

		ext := strings.ToLower(filepath.Ext(path))
		isVideo := false
		for _, videoExt := range videoExts {
			if ext == videoExt {
				isVideo = true
				break
			}
		}
		if !isVideo {
			return nil
		}

		filename := filepath.Base(path)
		videoFiles = append(videoFiles, VideoFile{
			Path:     path,
			Filename: filename,
			Code:     extractCodeFromFilename(filename),
		})
		return nil
	})

	return videoFiles, err
}

func createNFOFile(nfoPath, code string, detail *javdbapi.VideoDetail) error {
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

	fullTitle := fmt.Sprintf("<![CDATA[%s-%s]]>", code, s.Title)
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
		Num:           strings.ToLower(code),
		Premiered:     date,
		ReleaseDate:   date,
		Release:       date,
		Cover:         s.CoverURL,
		Website:       fmt.Sprintf("https://www.javdatabase.com/movies/%s", strings.ToLower(code)),
	}
	for _, tag := range detail.Tags {
		nfo.Genre = append(nfo.Genre, tag.Name)
		nfo.Tag = append(nfo.Tag, tag.Name)
	}
	for _, actor := range detail.Actors {
		nfo.Actor = append(nfo.Actor, NFOActor{Name: actor.Name, Role: ""})
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

	return atomicWrite(nfoPath, []byte(xmlContent), 0644)
}

func atomicWrite(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".capture-tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
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
