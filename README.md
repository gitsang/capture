# Capture

A command-line tool that scans directories for video files, extracts movie codes from filenames, fetches metadata, and organizes files into properly structured folders with NFO files and cover images.

## 1. Features

- **Recursive Directory Scanning**: Scans input directory recursively for video files
- **Video Format Support**: Supports mp4, mkv, wmv, avi video formats
- **Code Extraction**: Extracts movie codes case-insensitively, including FC2/FC2-PPV and multi-segment codes (e.g. `abf-004`, `fc2-ppv-1234567`, `T28-123`, `010120_001`)
- **Metadata Fetching**: Uses javdb API to fetch comprehensive movie metadata
- **File Organization**: Organizes into `video/`, `actor/`, `tag/` trees; actor and tag folders contain symlinks to the video folder
- **Caching**: Caches successful scrapes per code under `/tmp/capture` — metadata in `{CODE}.json`, images as raw files in `{CODE}/` (poster.jpg, extrafanart-N.jpg); validates on-disk NFO/JPG completeness and repairs from cache; `--force` bypasses the cache
- **Rate Limiting & Retry**: Configurable minimum request interval (default 10s) and exponential-backoff retry (default 10s base, 5 attempts)
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
go build -o capture ./cmd/capture
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

- `-i, --input`: Input directory to scan for video files (default: current directory). The output directory is always skipped during scanning.
- `-o, --output`: Output directory for organized files (default: ./output)
- `--cache-dir`: Directory for cached scrape results, one JSON file per code (default: /tmp/capture)
- `--force`: Ignore cached results and re-scrape
- `--proxy`: HTTP proxy URL for API and image requests (e.g. `http://127.0.0.1:7890`)
- `--insecure`: Skip TLS certificate verification (for MITM proxies)
- `--cookie`: Cookie header from a logged-in javdb browser session (e.g. `"_jdb_session=..."`), attached to javdb.com requests only. Some detail pages require login; copy the cookie from your browser's DevTools (Application → Cookies → javdb.com)
- `--interval`: Base interval between HTTP requests, e.g. `30s`, `2m` (default: 10s; `0` disables). Each wait is randomized ±50% (10s → 5–15s) to avoid a fixed request rhythm. Applies to all requests (API + images); the SDK's internal rate limit runs at interval/3 as a safety net only
- `--retry-base`: Initial retry backoff delay, doubles each retry (default: 10s)
- `--retry-attempts`: Maximum attempts per request (default: 5; `1` disables retries)

### 3.3 Example

```bash
# Organize videos from Downloads folder to Movies folder
./capture -i ~/Downloads -o ~/Movies
```

## 4. How It Works

1. **Scanning**: Recursively scans the input directory for video files with supported extensions
2. **Code Extraction**: Case-insensitive patterns for standard codes (`ABCD-180`), FC2 (`FC2-PPV-1234567`, `fc2-1234567`), multi-segment codes (`T28-123`, `1Pondo-010120-001`) and date-style ids (`010120_001`); results are normalized to uppercase
3. **Metadata Fetching**: Cache lookup first (unless `--force`); otherwise searches javdb for an exact code match and fetches the detail page, then stores metadata + cover bytes in the cache
4. **Integrity Checks**: Existing NFO/JPG files are validated (XML well-formedness, full image decode); incomplete files are repaired from cache or re-downloaded
5. **Folder Creation**: Creates `output/video/CODE/`
6. **NFO Creation**: Generates Emby/Jellyfin-style NFO files: CDATA `CODE-Title` titles, sorttitle, customrating/mpaa `JP-18+`, maker/label/num, premiered/releasedate/release, dual `<genre>`+`<tag>`, website link, CRLF line endings; maker, director and duration are filled when available
7. **Image Download**: Downloads the cover as poster.jpg and fanart.jpg
8. **File Moving**: Renames video files to match codes and moves them into `output/video/CODE/`
9. **Index Symlinks**: For every actor and tag, creates `output/actor/<name>/CODE` and `output/tag/<name>/CODE` symlinks pointing at the video folder

## 5. Output Structure

```
output/
├── video/
│   ├── ABCD-180/
│   │   ├── ABCD-180.mp4      # Renamed video file
│   │   ├── ABCD-180.nfo      # Movie metadata
│   │   ├── poster.jpg        # Cover image
│   │   ├── fanart.jpg        # Fanart image
│   │   ├── thumb.jpg         # Thumbnail (same cover)
│   │   └── extrafanart/      # Screenshots
│   │       ├── extrafanart-0.jpg
│   │       └── ...
│   └── FC2-PPV-1234567/
│       └── ...
├── actor/
│   └── 女優名/
│       └── ABCD-180 -> ../../video/ABCD-180   # relative symlink
└── tag/
    └── タグ名/
        └── ABCD-180 -> ../../video/ABCD-180   # relative symlink
```

## 6. NFO File Content

The generated NFO files include:

- CDATA `CODE-Title` as title/originaltitle/sorttitle
- customrating/mpaa `JP-18+`
- Plot/outline (title, plus score when available)
- Runtime (`Nminutes`), year, premiered/releasedate/release
- Studio/maker, director, label (series), num (lowercase code)
- Genres and tags (both from site tags)
- Actors
- Poster/thumb/fanart references, cover URL, javdatabase website link
- CRLF (DOS) line endings

## 7. Supported Video Extensions

- `.mp4`
- `.mkv`
- `.wmv`
- `.avi`

## 8. Code Pattern Recognition

Matching is case-insensitive; output folder names are uppercase. Patterns are tried in fallback order — Standard → FC2 → multi-segment → date-style — so a leading date in the filename (e.g. `2020-12-12 MIMK-079 ...`) never wins over the real code:

- Standard: `ABCD-180`, `ssni-123` → `SSNI-123`
- FC2: `fc2-ppv-1234567`, `FC2PPV-1234567`, `fc2-1234567` → `FC2-PPV-1234567`
- Multi-segment: `T28-123`, `3DSVR-123`, `1Pondo-010120-001`
- Date-style (1pondo/caribbean): `010120_001` → `010120-001`

## 9. Error Handling

- Skips files without recognizable codes
- Continues processing other files if one fails
- Provides detailed progress and error messages
- Retries transient failures with exponential backoff (honors `Retry-After`)
- Writes NFO/images/cache atomically; truncated files are detected and repaired on the next run

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

