package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/gitsang/capture/pkg/cache"
	"github.com/gitsang/capture/pkg/client"
)

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
	rootCmd.Flags().DurationVar(&interval, "interval", 10*time.Second, "Minimum interval between HTTP requests (0 disables)")
	rootCmd.Flags().DurationVar(&retryBase, "retry-base", 10*time.Second, "Initial retry backoff delay (doubles each retry)")
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

	api, err := client.New(interval, retryBase, retryAttempts, proxyURL, insecure)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create client: %v\n", err)
		return
	}
	c := cache.New(cacheDir)

	for i, video := range videoFiles {
		fmt.Printf("[%d/%d] Processing: %s\n", i+1, len(videoFiles), video.Filename)
		processVideo(ctx, api, c, outputDir, force, video)
		fmt.Println()
	}

	fmt.Println("Processing complete!")
}
