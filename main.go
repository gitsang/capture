package main

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
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

// NFOData represents the structure for NFO files
type NFOData struct {
	XMLName       xml.Name `xml:"movie"`
	Title         string   `xml:"title"`
	OriginalTitle string   `xml:"originaltitle"`
	Plot          string   `xml:"plot"`
	Runtime       string   `xml:"runtime"`
	Year          string   `xml:"year"`
	Studio        string   `xml:"studio"`
	Director      string   `xml:"director"`
	Genre         []string `xml:"genre"`
	Actor         []Actor  `xml:"actor"`
	Poster        string   `xml:"art>poster"`
	Fanart        string   `xml:"art>fanart"`
	Code          string   `xml:"uniqueid"`
}

type Actor struct {
	Name string `xml:"name"`
	Role string `xml:"role"`
}

var (
	inputDir  string
	outputDir string
	videoExts = []string{".mp4", ".mkv", ".wmv", ".avi"}
	codeRegex = regexp.MustCompile(`([A-Z]+-\d+)`)
)

var rootCmd = &cobra.Command{
	Use:   "capture",
	Short: "Capture - Organize video files with metadata",
	Long: `Capture is a command-line tool that scans directories for video files,
extracts movie codes from filenames, fetches metadata, and organizes
files into properly structured folders with NFO files and cover images.`,
	Run: runCapture,
}

func init() {
	rootCmd.Flags().StringVarP(&inputDir, "input", "i", ".", "Input directory to scan for video files")
	rootCmd.Flags().StringVarP(&outputDir, "output", "o", "./output", "Output directory for organized files")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runCapture(cmd *cobra.Command, args []string) {
	fmt.Printf("Starting Capture\n")
	fmt.Printf("Input directory: %s\n", inputDir)
	fmt.Printf("Output directory: %s\n", outputDir)
	fmt.Println()

	// Create output directory if it doesn't exist
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create output directory: %v\n", err)
		return
	}

	// Scan for video files
	fmt.Println("Scanning for video files...")
	videoFiles, err := scanVideoFiles(inputDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to scan video files: %v\n", err)
		return
	}

	fmt.Printf("Found %d video files\n\n", len(videoFiles))

	// Initialize client
	client := NewClient()

	// Process each video file
	for i, video := range videoFiles {
		fmt.Printf("[%d/%d] Processing: %s\n", i+1, len(videoFiles), video.Filename)

		if video.Code == "" {
			fmt.Printf("  ⚠️  No code found in filename, skipping\n\n")
			continue
		}

		fmt.Printf("  📋 Code: %s\n", video.Code)

		// Search for movie data
		movieData, err := client.SearchByCode(video.Code)
		if err != nil {
			fmt.Printf("  ❌ Failed to fetch movie data: %v\n\n", err)
			continue
		}

		fmt.Printf("  ✅ Found movie data: %s\n", movieData.Title)

		// Create folder for the movie
		movieFolder := filepath.Join(outputDir, strings.ToUpper(video.Code))
		if err := os.MkdirAll(movieFolder, 0755); err != nil {
			fmt.Printf("  ❌ Failed to create movie folder: %v\n\n", err)
			continue
		}

		// Generate NFO file
		if err := createNFOFile(movieFolder, video.Code, movieData); err != nil {
			fmt.Printf("  ⚠️  Failed to create NFO file: %v\n", err)
		} else {
			fmt.Printf("  📄 Created NFO file\n")
		}

		// Download cover image
		if err := downloadCoverImage(movieFolder, video.Code, movieData); err != nil {
			fmt.Printf("  ⚠️  Failed to download cover image: %v\n", err)
		} else {
			fmt.Printf("  🖼️  Downloaded cover image\n")
		}

		// Move and rename video file
		newVideoPath := filepath.Join(movieFolder, strings.ToUpper(video.Code)+filepath.Ext(video.Path))
		if err := moveFile(video.Path, newVideoPath); err != nil {
			fmt.Printf("  ❌ Failed to move video file: %v\n", err)
		} else {
			fmt.Printf("  📁 Moved video file to: %s\n", newVideoPath)
		}

		fmt.Println()
	}

	fmt.Println("Processing complete!")
}

func scanVideoFiles(dir string) ([]VideoFile, error) {
	var videoFiles []VideoFile

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		// Check if file has video extension
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
		code := extractCodeFromFilename(filename)

		videoFiles = append(videoFiles, VideoFile{
			Path:     path,
			Filename: filename,
			Code:     code,
		})

		return nil
	})

	return videoFiles, err
}

func extractCodeFromFilename(filename string) string {
	matches := codeRegex.FindStringSubmatch(filename)
	if len(matches) > 1 {
		return strings.ToUpper(matches[1])
	}
	return ""
}

func createNFOFile(movieFolder, code string, movieData *javdbapi.JavDB) error {
	nfoPath := filepath.Join(movieFolder, code+".nfo")

	// Extract year from pub_date
	year := ""
	if !movieData.PubDate.IsZero() {
		year = movieData.PubDate.Format("2006")
	}

	// Create NFO data structure
	nfo := NFOData{
		Title:         movieData.Title,
		OriginalTitle: movieData.Title,
		Plot:          fmt.Sprintf("Movie code: %s\nScore: %.2f (%d votes)", code, movieData.Score, movieData.ScoreCount),
		Runtime:       "240", // Default runtime in minutes
		Year:          year,
		Studio:        "Unknown Studio",
		Director:      "Unknown Director",
		Code:          code,
		Poster:        "poster.jpg",
		Fanart:        "fanart.jpg",
	}

	// Add tags as genres
	for _, tag := range movieData.Tags {
		nfo.Genre = append(nfo.Genre, tag)
	}

	// Add actresses as actors
	for _, actress := range movieData.Actresses {
		nfo.Actor = append(nfo.Actor, Actor{
			Name: actress,
			Role: "Actress",
		})
	}

	// Marshal to XML
	xmlData, err := xml.MarshalIndent(nfo, "", "    ")
	if err != nil {
		return err
	}

	// Add XML header
	xmlContent := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
%s`, string(xmlData))

	return os.WriteFile(nfoPath, []byte(xmlContent), 0644)
}

func downloadCoverImage(movieFolder, code string, movieData *javdbapi.JavDB) error {
	if movieData.Cover == "" {
		return fmt.Errorf("no cover image URL available")
	}

	// Download poster
	posterPath := filepath.Join(movieFolder, "poster.jpg")
	if err := downloadImage(movieData.Cover, posterPath); err != nil {
		return fmt.Errorf("failed to download poster: %v", err)
	}

	// Download fanart (use cover as fanart if no separate fanart available)
	fanartPath := filepath.Join(movieFolder, "fanart.jpg")
	if err := downloadImage(movieData.Cover, fanartPath); err != nil {
		return fmt.Errorf("failed to download fanart: %v", err)
	}

	return nil
}

func downloadImage(url, filepath string) error {
	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Make HTTP request
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP error: %d", resp.StatusCode)
	}

	// Create file
	file, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer file.Close()

	// Copy image data to file
	_, err = io.Copy(file, resp.Body)
	return err
}

func moveFile(src, dst string) error {
	// First try to rename (move) the file
	if err := os.Rename(src, dst); err == nil {
		return nil
	}

	// If rename fails (e.g., cross-device), copy and delete
	return copyAndDelete(src, dst)
}

func copyAndDelete(src, dst string) error {
	// Open source file
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	// Create destination file
	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	// Copy content
	_, err = io.Copy(dstFile, srcFile)
	if err != nil {
		return err
	}

	// Sync to ensure data is written
	err = dstFile.Sync()
	if err != nil {
		return err
	}

	// Remove source file
	return os.Remove(src)
}
