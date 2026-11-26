# Capture

A command-line tool that scans directories for video files, extracts movie codes from filenames, fetches metadata, and organizes files into properly structured folders with NFO files and cover images.

## 1. Features

- **Recursive Directory Scanning**: Scans input directory recursively for video files
- **Video Format Support**: Supports mp4, mkv, wmv, avi video formats
- **Code Extraction**: Extracts movie codes case-insensitively, including FC2/FC2-PPV and multi-segment codes (e.g. `abf-004`, `fc2-ppv-1234567`, `T28-123`, `010120_001`)
- **Metadata Fetching**: Uses javdb API to fetch comprehensive movie metadata
- **File Organization**: Organizes into `video/`, `actor/`, `tag/` trees; actor and tag folders contain symlinks to the video folder. Only **actresses** get `actor/` links — javdb lists male co-stars in the same cast panel, and linking them would create bogus actor entries
- **Caching**: Caches successful scrapes per code under `/tmp/capture` — everything per code in one folder `{CODE}/` ({CODE}.json metadata + raw poster.jpg/extrafanart-N.jpg); validates on-disk NFO/JPG completeness and repairs from cache; `--no-cache` bypasses the cache
- **Rate Limiting & Retry**: Configurable minimum request interval (default 10s) and exponential-backoff retry (default 10s base, 5 attempts)
- **NFO Generation**: Creates Kodi/Plex compatible NFO files with movie metadata
- **Cover Download**: Downloads movie cover images as poster.jpg and fanart.jpg
- **File Management**: Renames and moves video files to organized folders; sources that cannot be scraped (no exact match, network error, move failure) are parked in `{output}/failed/` so later runs skip them

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
- `--no-cache`: Ignore cached results and re-scrape (applies to both normal runs and `--fix`)
- `--fix [targets]`: Repair the output directory instead of scanning input. Without targets (or `missing`), only absent/broken artifacts are created: missing actor/tag/fullname symlinks, broken NFO/images restored from cache, landscape posters re-cropped. Named targets force regeneration even when files exist:
  - `nfo`: regenerate the NFO from metadata
  - `fanart`: re-write fanart from the cached cover (or re-download)
  - `thumb`: re-copy thumb from fanart
  - `extrafanart`: re-write all screenshots
  - `poster`: re-crop the portrait poster from fanart
  - `symlink`: delete and re-create all actor/tag/fullname symlinks
  - e.g. `capture --fix poster,symlink`; cache is used when present, codes missing from cache are scraped from the network (unless the scrape fails they are skipped)
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
3. **Metadata Fetching**: Cache lookup first (unless `--no-cache`); otherwise searches javdb for an exact code match and fetches the detail page, then stores metadata + cover bytes in the cache
4. **Integrity Checks**: Existing NFO/JPG files are validated (XML well-formedness, full image decode); incomplete files are repaired from cache or re-downloaded
5. **Folder Creation**: Creates `output/video/CODE/`
6. **NFO Creation**: Generates Emby/Jellyfin-style NFO files: CDATA `CODE-Title` titles, sorttitle, customrating/mpaa `JP-18+`, maker/label/num, premiered/releasedate/release, dual `<genre>`+`<tag>`, website link, CRLF line endings; maker, director and duration are filled when available
7. **Image Download**: Downloads the cover as poster.jpg and fanart.jpg
8. **File Moving**: Renames video files to match codes and moves them into `output/video/CODE/`
9. **Actress Resolution**: javdb marks actresses with `class="actor-female"` on the detail page (every actress in multi-actress works; male co-stars carry no class — its SDK `Actor.Gender` field is always `unknown` because the SDK reads a `symbol` element the page no longer has). The detail page HTML is re-read once per code to recover the list, cached as `actresses`/`actresses_resolved`, so later runs and `--fix` make no extra request
10. **Index Symlinks**: For every actress and tag, creates `output/actor/<name>/CODE` and `output/tag/<name>/CODE` symlinks pointing at the video folder. All actresses of a multi-actress work get links. Codes whose actress list could not be resolved fall back to linking every cast member

## 5. Output Structure

```
output/
├── video/
│   ├── ABCD-180/
│   │   ├── ABCD-180.mp4      # Renamed video file
│   │   ├── ABCD-180.nfo      # Movie metadata
│   │   ├── poster.jpg        # Portrait poster (right 47.4% crop of cover)
│   │   ├── fanart.jpg        # Full landscape cover
│   │   ├── thumb.jpg         # Thumbnail (same full cover)
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
- Actors, each with `<type>female|male</type>` when the actress list was resolved (Kodi/tinyMediaManager read it; Emby/Jellyfin ignore it). The element is omitted entirely when unresolved, rather than guessing
- Poster/thumb/fanart references, cover URL, javdatabase website link
- CRLF (DOS) line endings

## 7. Supported Video Extensions

- `.mp4`
- `.mkv`
- `.wmv`
- `.avi`

## 8. Code Pattern Recognition

The code is extracted from the **filename only** (the extension is stripped first, so numbers in `.mp4` etc. cannot interfere). Matching is case-insensitive; extracted codes are normalized to **upper case with `-` separators** and used as folder names.

### 8.1 Recognition tiers (first match wins)

Patterns are tried in a fixed fallback order. Higher tiers win so that noise in the filename (leading dates, resolution tags, part suffixes) never beats the real code:

| # | Tier | Pattern | Examples → extracted |
|---|------|---------|---------------------|
| 1 | **Standard** | 2–12 letters + `-` + 2–6 digits; optionally followed by 1–2 trailing letters, `-`/`.`-separated or glued (dropped) | `ABF-004` → `ABF-004`; `start-281ch` → `START-281`; `SSIS-001C` → `SSIS-001`; `ABP-123-C` → `ABP-123`; `YRNKMTNDVAJ-647A.2025.1080p...` → `YRNKMTNDVAJ-647` |
| 2 | **FC2** | `FC2` + optional `PPV` + 5–8 digits; separators `-`/`_`/space or none | `fc2-ppv-1234567`, `FC2PPV-1234567`, `fc2-1234567` → `FC2-PPV-1234567` |
| 3 | **Glued / HEYZO** | 2–12 letters immediately followed by 2–6 digits (optional 1–3 digit prefix dropped); or `HEYZO` + `_`/`-`-joined segments + number | `hunter003` → `HUNTER-003`; `476MLA095` → `MLA-095`; `heyzo_hd_0474_full` → `HEYZO-0474` |
| 4 | **Multi-segment** | alphanumeric prefix (must contain ≥1 letter) + 1–3 `-number` groups | `T28-123`, `3DSVR-123`, `1Pondo-010120-001` |
| 5 | **Date-style** | 6 digits + `_`/`-` + 2–3 digits (1pondo/caribbean ids) | `010120_001` → `010120-001`; `123119-001-carib` → `123119-001` |

### 8.2 Noise tolerance

- **Leading dates**: `2020-12-12 MIMK-079 ...` → `MIMK-079` (Standard wins; a bare date matches no tier — Multi-segment requires a letter in its prefix)
- **Resolution/codec tags**: `1080p`, `720P`, `x264`, `h264`, `HEVC` are not codes (fewer than 2 letters or no digits)
- **Prefix noise**: `720P-ABP-123` → `ABP-123`; `hhd800.com@FC2-PPV-4573738` → `FC2-PPV-4573738`
- **Part suffixes**: `abc-123-cd1` → `ABC-123` (the `cd1` tail is outside the Standard match)
- **Word boundaries**: all patterns anchor on `\b`, so codes embedded in longer words are not partially matched

### 8.3 Exact matching against search results

Extraction only proposes a candidate; the search result must **exactly match** it after normalization (upper case, `_`→`-`). `FC2-1234567` and `FC2-PPV-1234567` are treated as equal. If nothing matches (`no exact match`), the file is parked in `{output}/failed/` — see §4.

### 8.4 Files without a recognizable code

Left untouched in the input directory and skipped (not quarantined), so renaming the file is enough to have it picked up on the next run.

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

