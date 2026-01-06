package main

import (
	"crypto/tls"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/gitsang/capture/pkg/javdbapi"
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
	SortTitle     string   `xml:"sorttitle"`
	CustomRating  string   `xml:"customrating"`
	MPAA          string   `xml:"mpaa"`
	Set           string   `xml:"set"`
	Plot          string   `xml:"plot"`
	Outline       string   `xml:"outline"`
	Runtime       string   `xml:"runtime"`
	Year          string   `xml:"year"`
	Studio        string   `xml:"studio"`
	Director      string   `xml:"director"`
	Poster        string   `xml:"poster"`
	Thumb         string   `xml:"thumb"`
	Fanart        string   `xml:"fanart"`
	Genre         []string `xml:"genre"`
	Tag           []string `xml:"tag"`
	Actor         []Actor  `xml:"actor"`
	Maker         string   `xml:"maker"`
	Label         string   `xml:"label"`
	Num           string   `xml:"num"`
	Premiered     string   `xml:"premiered"`
	ReleaseDate   string   `xml:"releasedate"`
	Release       string   `xml:"release"`
	Cover         string   `xml:"cover"`
	Website       string   `xml:"website"`
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
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
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
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for i, video := range videoFiles {
		<-ticker.C
		log.Printf("Sleeping for 5 seconds before processing next file...")
		time.Sleep(5 * time.Second)

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
		if err := os.MkdirAll(movieFolder, 0o755); err != nil {
			fmt.Printf("  ❌ Failed to create movie folder: %v\n\n", err)
			continue
		}

		// Generate NFO file
		if err := createNFOFile(movieFolder, video.Code, movieData); err != nil {
			fmt.Printf("  ⚠️  Failed to create NFO file: %v\n", err)
			continue
		} else {
			fmt.Printf("  📄 Created NFO file\n")
		}

		// Download cover image
		if err := downloadCoverImage(movieFolder, movieData); err != nil {
			fmt.Printf("  ⚠️  Failed to download cover image: %v\n", err)
			continue
		} else {
			fmt.Printf("  🖼️  Downloaded cover image\n")
		}

		// Move and rename video file
		newVideoPath := filepath.Join(movieFolder, strings.ToUpper(video.Code)+filepath.Ext(video.Path))
		if err := moveFile(video.Path, newVideoPath); err != nil {
			fmt.Printf("  ❌ Failed to move video file: %v\n", err)
			_ = os.RemoveAll(movieFolder)
		} else {
			fmt.Printf("  📁 Moved video file to: %s\n", newVideoPath)
		}

		fmt.Println()
	}

	fmt.Println("Processing complete!")
}

func scanVideoFiles(dir string) ([]VideoFile, error) {
	var videoFiles []VideoFile

	// Get absolute path for output directory comparison
	outputAbs, _ := filepath.Abs(outputDir)

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip output directory and its subdirectories
		if info.IsDir() {
			dirAbs, _ := filepath.Abs(path)
			if dirAbs == outputAbs {
				return filepath.SkipDir
			}
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

func createNFOFile(movieFolder, code string, movieData *javdbapi.Item) error {
	nfoPath := filepath.Join(movieFolder, code+".nfo")

	// Create NFO data structure
	nfo := NFOData{
		Title:         fmt.Sprintf("<![CDATA[%s-%s]]>", code, movieData.Title),
		OriginalTitle: fmt.Sprintf("<![CDATA[%s-%s]]>", code, movieData.Title),
		SortTitle:     fmt.Sprintf("<![CDATA[%s-%s]]>", code, movieData.Title),
		CustomRating:  "JP-18+",
		MPAA:          "JP-18+",
		Set:           "꞉",
		Plot:          fmt.Sprintf("<![CDATA[%s]]>", movieData.Title),
		Outline:       fmt.Sprintf("<![CDATA[%s]]>", movieData.Title),
		Runtime:       "119minutes",
		Year:          movieData.PubDate.Format("2006"),
		Studio:        "",
		Director:      "",
		Poster:        "poster.jpg",
		Thumb:         "thumb.jpg",
		Fanart:        "fanart.jpg",
		Maker:         "",
		Label:         "",
		Num:           strings.ToLower(code),
		Premiered:     movieData.PubDate.Format("2006-01-02"),
		ReleaseDate:   movieData.PubDate.Format("2006-01-02"),
		Release:       movieData.PubDate.Format("2006-01-02"),
		Cover:         movieData.Cover,
		Website:       fmt.Sprintf("https://www.javdatabase.com/movies/%s", strings.ToLower(code)),
	}

	// Add tags as both genres and tags
	for _, tag := range movieData.Tags {
		nfo.Genre = append(nfo.Genre, tag)
		nfo.Tag = append(nfo.Tag, tag)
	}

	// Add actresses as actors
	for _, actress := range movieData.Actors {
		nfo.Actor = append(nfo.Actor, Actor{
			Name: actress,
			Role: "",
		})
	}

	// Marshal to XML
	xmlData, err := xml.MarshalIndent(nfo, "", "  ")
	if err != nil {
		return err
	}

	// Add XML header and remove CDATA duplication
	xmlContent := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" ?>
%s`, string(xmlData))
	// Fix CDATA formatting
	xmlContent = strings.ReplaceAll(xmlContent, "&lt;![CDATA[", "<![CDATA[")
	xmlContent = strings.ReplaceAll(xmlContent, "]]&gt;", "]]")

	// Convert to DOS file format (\r\n)
	xmlContent = strings.ReplaceAll(xmlContent, "\n", "\r\n")

	return os.WriteFile(nfoPath, []byte(xmlContent), 0o644)
}

func downloadCoverImage(movieFolder string, movieData *javdbapi.Item) error {
	if movieData.Cover == "" {
		return fmt.Errorf("no cover image URL available")
	}

	// Download poster
	posterPath := filepath.Join(movieFolder, "poster.jpg")
	if err := downloadImage(movieData.Cover, posterPath); err != nil {
		return fmt.Errorf("failed to download poster: %v", err)
	}

	// Download fanart
	fanartPath := filepath.Join(movieFolder, "fanart.jpg")
	if err := downloadImage(movieData.Cover, fanartPath); err != nil {
		return fmt.Errorf("failed to download fanart: %v", err)
	}

	// Download thumb
	thumbPath := filepath.Join(movieFolder, "thumb.jpg")
	if err := downloadImage(movieData.Cover, thumbPath); err != nil {
		return fmt.Errorf("failed to download thumb: %v", err)
	}

	// Download extra fanart
	extrafanartFolder := filepath.Join(movieFolder, "extrafanart")
	err := os.MkdirAll(extrafanartFolder, 0o755)
	if err != nil {
		return err
	}
	for idx, url := range movieData.Pics {
		extrafanartPath := filepath.Join(extrafanartFolder, fmt.Sprintf("extrafanart-%d.jpg", idx))
		if err := downloadImage(url, extrafanartPath); err != nil {
			continue
		}
	}

	return nil
}

func downloadImage(url, filepath string) error {
	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
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
