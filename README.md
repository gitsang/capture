# Capture

A command-line tool that scans directories for video files, extracts movie codes from filenames, fetches metadata, and organizes files into properly structured folders with NFO files and cover images.

## 1. Features

- **Recursive Directory Scanning**: Scans input directory recursively for video files
- **Video Format Support**: Supports mp4, mkv, wmv, avi video formats
- **Code Extraction**: Automatically extracts movie codes (like ABCD-180) from filenames using regex
- **Metadata Fetching**: Uses javdb API to fetch comprehensive movie metadata
- **File Organization**: Creates organized folder structure with uppercase code names
- **NFO Generation**: Creates Kodi/Plex compatible NFO files with movie metadata
- **Cover Download**: Downloads movie cover images as poster.jpg and fanart.jpg
- **File Management**: Renames and moves video files to organized folders

## 2. Installation

### 2.1 Prerequisites

- Internet connection for fetching metadata and cover images

### 2.2 Download Pre-built Binaries

Download the latest release for your platform from the [Releases page](https://github.com/gitsang/capture/releases):

- **Linux AMD64**: `capture-linux-amd64.tar.gz`
- **Linux ARM64**: `capture-linux-arm64.tar.gz`
- **macOS AMD64**: `capture-darwin-amd64.tar.gz`
- **macOS ARM64**: `capture-darwin-arm64.tar.gz` (Apple Silicon)
- **Windows AMD64**: `capture-windows-amd64.zip`
- **Windows ARM64**: `capture-windows-arm64.zip`

Extract the archive and run the binary:

```bash
# Linux/macOS
tar -xzf capture-linux-amd64.tar.gz
chmod +x capture-linux-amd64
./capture-linux-amd64

# Windows
# Extract capture-windows-amd64.zip and run capture-windows-amd64.exe
```

### 2.3 Build from Source

If you prefer to build from source:

- Go 1.24.2 or later required

```bash
git clone https://github.com/gitsang/capture.git
cd capture
go mod tidy
go build -o capture
```

### 2.4 Verify Downloads (Optional)

Each release includes SHA256 checksums for verification:

```bash
# Download the checksum file
wget https://github.com/gitsang/capture/releases/download/v1.0.0/capture-linux-amd64.tar.gz.sha256

# Verify the download
sha256sum -c capture-linux-amd64.tar.gz.sha256
```

## 3. Usage

### 3.1 Basic Usage

```bash
# Scan current directory and organize to ./output
./capture

# Specify input and output directories
./capture -i /path/to/videos -o /path/to/organized

# Show help
./capture --help
```

### 3.2 Command Line Options

- `-i, --input`: Input directory to scan for video files (default: current directory)
- `-o, --output`: Output directory for organized files (default: ./output)

### 3.3 Example

```bash
# Organize videos from Downloads folder to Movies folder
./capture -i ~/Downloads -o ~/Movies
```

## 4. How It Works

1. **Scanning**: Recursively scans the input directory for video files with supported extensions
2. **Code Extraction**: Uses regex pattern `([A-Z]+-\d+)` to extract codes like "ABCD-180" from filenames
3. **Metadata Fetching**: Calls `client.SearchByCode()` to get movie data from javdb
4. **Folder Creation**: Creates folders named after the codes (uppercase, e.g., `ABCD-180/`)
5. **NFO Creation**: Generates XML NFO files with movie metadata
6. **Image Download**: Downloads cover images as poster.jpg and fanart.jpg
7. **File Moving**: Renames video files to match codes and moves them to respective folders

## 5. Output Structure

```
output/
├── ABCD-180/
│   ├── ABCD-180.mp4          # Renamed video file
│   ├── ABCD-180.nfo          # Movie metadata
│   ├── poster.jpg            # Cover image
│   └── fanart.jpg            # Fanart image
└── ANOTHER-123/
    ├── ANOTHER-123.mkv
    ├── ANOTHER-123.nfo
    ├── poster.jpg
    └── fanart.jpg
```

## 6. NFO File Content

The generated NFO files include:

- Title and original title
- Plot description with code and score
- Year (extracted from publication date)
- Genres (from tags)
- Actresses (as actors)
- Unique ID (movie code)
- Poster and fanart references

## 7. Supported Video Extensions

- `.mp4`
- `.mkv`
- `.wmv`
- `.avi`

## 8. Code Pattern Recognition

The tool recognizes movie codes in the following format:

- Pattern: `[A-Z]+-\d+`
- Examples: `ABCD-180`, `HHHHHH-123`, `XYZ-456`
- Case insensitive in filename, but output folders are uppercase

## 9. Error Handling

- Skips files without recognizable codes
- Continues processing other files if one fails
- Provides detailed progress and error messages
- Handles network timeouts for image downloads

## 10. Example Output

```
Starting capture - Video Capture
Input directory: /home/user/videos
Output directory: /home/user/organized

Scanning for video files...
Found 3 video files

[1/3] Processing: HHHHH-180.mp4
  📋 Code: HHHHH-180
  ✅ Found movie data: Movie Title
  📄 Created NFO file
  🖼️  Downloaded cover image
  📁 Moved video file to: /home/user/organized/HHHHH-180/HHHHH-180.mp4

[2/3] Processing: random_video.mp4
  ⚠️  No code found in filename, skipping

[3/3] Processing: ABCD-123.mkv
  📋 Code: ABCD-123
  ✅ Found movie data: Another Movie Title
  📄 Created NFO file
  🖼️  Downloaded cover image
  📁 Moved video file to: /home/user/organized/ABCD-123/SSIS-123.mkv

Processing complete!
```

