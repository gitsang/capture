package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gitsang/capture/pkg/cache"
	"github.com/gitsang/capture/pkg/client"
)

var (
	inputDir      string
	outputDir     string
	cacheDir      string
	noCache       bool
	fix           string
	proxyURL      string
	insecure      bool
	cookie        string
	noFallback    bool
	interval      time.Duration
	retryBase     time.Duration
	retryAttempts int
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
  {output}/tag/<name>/CODE      symlink to the video folder
  {output}/fullname/CODE Title  symlink to the video folder
  {output}/failed/<original name>   sources that could not be processed`,
	Run: runCapture,
}

func init() {
	rootCmd.Flags().StringVarP(&inputDir, "input", "i", ".", "Input directory to scan for video files")
	rootCmd.Flags().StringVarP(&outputDir, "output", "o", "./output", "Output directory for organized files")
	rootCmd.Flags().StringVar(&cacheDir, "cache-dir", "/tmp/capture", "Directory for cached scrape results (one file per code)")
	rootCmd.Flags().BoolVar(&noCache, "no-cache", false, "Ignore cached results and re-scrape (also applies to --fix)")
	rootCmd.Flags().StringVar(&fix, "fix", "", "Repair the output directory instead of scanning input. Optional comma list of targets to force regeneration: nfo,fanart,thumb,extrafanart,poster,symlink (empty/missing = only create what is absent or broken). 'failed' re-scrapes the files parked in {output}/failed, e.g. --fix failed,nfo")
	// Allow bare `--fix` (no value) to mean "missing".
	rootCmd.Flags().Lookup("fix").NoOptDefVal = "missing"
	rootCmd.Flags().StringVar(&proxyURL, "proxy", "", "HTTP proxy URL for API and image requests (e.g. http://127.0.0.1:7890)")
	rootCmd.Flags().BoolVar(&insecure, "insecure", false, "Skip TLS certificate verification (for MITM proxies)")
	rootCmd.Flags().StringVar(&cookie, "cookie", "", "Cookie header for javdb.com from a logged-in browser session (e.g. \"_jdb_session=...\")")
	rootCmd.Flags().BoolVar(&noFallback, "no-fallback", false, "Only use javdb; do not fall back to r18.dev when a code is missing")
	rootCmd.Flags().DurationVar(&interval, "interval", 10*time.Second, "Minimum interval between HTTP requests (0 disables)")
	rootCmd.Flags().DurationVar(&retryBase, "retry-base", 10*time.Second, "Initial retry backoff delay (doubles each retry)")
	rootCmd.Flags().IntVar(&retryAttempts, "retry-attempts", 5, "Maximum attempts per request (1 disables retries)")
}

// looksLikeFixTargets reports whether s resembles a --fix target list:
// comma-separated lowercase bare words. Unknown words are tolerated here
// so misspellings surface as proper errors from parseFixTargets instead of
// being silently ignored.
func looksLikeFixTargets(s string) bool {
	if s == "" || strings.ContainsAny(s, "/\\. ") {
		return false
	}
	for _, part := range strings.Split(s, ",") {
		part = strings.ToLower(strings.TrimSpace(part))
		if part == "" {
			return false
		}
		for _, r := range part {
			if !(r >= 'a' && r <= 'z') {
				return false
			}
		}
	}
	return true
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// clientConfig collects the flags shared by both run modes.
func clientConfig() client.Config {
	return client.Config{
		Interval:      interval,
		RetryBase:     retryBase,
		RetryAttempts: retryAttempts,
		ProxyURL:      proxyURL,
		Insecure:      insecure,
		Cookie:        cookie,
		NoFallback:    noFallback,
	}
}

func runCapture(cmd *cobra.Command, args []string) {
	// --fix: adopt positional target list (space form), then validate.
	if cmd.Flags().Changed("fix") {
		if fix == "missing" && len(args) > 0 && looksLikeFixTargets(args[0]) {
			fix = args[0]
		}
		if _, err := parseFixTargets(fix); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	fmt.Printf("Starting Capture\n")
	fmt.Printf("Input directory: %s\n", inputDir)
	fmt.Printf("Output directory: %s\n", outputDir)
	fmt.Printf("Cache directory: %s (no-cache=%v)\n", cacheDir, noCache)
	fmt.Printf("Request interval: %s, retry: %d attempts from %s (exponential)\n\n", interval, retryAttempts, retryBase)

	if retryAttempts < 1 {
		retryAttempts = 1
	}

	// Create output directory if it doesn't exist
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create output directory: %v\n", err)
		return
	}

	// --fix: repair the output tree, never scan input.
	if cmd.Flags().Changed("fix") {
		targets, err := parseFixTargets(fix)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return
		}
		api, err := client.New(clientConfig())
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create client: %v\n", err)
			return
		}
		switch {
		case targets.failed:
			fmt.Printf("Fix mode: retrying files in %s\n", filepath.Join(outputDir, "failed"))
		case targets.missingOnly():
			fmt.Println("Fix mode: repairing missing/broken artifacts only")
		default:
			fmt.Printf("Fix mode: forcing regeneration of %s\n", fix)
		}
		fmt.Println()
		fixOutput(ctx, api, cache.New(cacheDir), outputDir, targets, noCache)
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

	api, err := client.New(clientConfig())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create client: %v\n", err)
		return
	}
	c := cache.New(cacheDir)

	for i, video := range videoFiles {
		fmt.Printf("[%d/%d] Processing: %s\n", i+1, len(videoFiles), video.Filename)
		processVideo(ctx, api, c, outputDir, noCache, video, fixTargets{})
		fmt.Println()
	}

	fmt.Println("Processing complete!")
}
