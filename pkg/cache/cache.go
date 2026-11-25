package cache

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RPbro/javdbapi"

	"github.com/gitsang/capture/pkg/code"
)

// CacheEntry is one cached scrape result. Metadata lives in
// {cache-dir}/{CODE}.json; images are stored as raw files under
// {cache-dir}/{CODE}/ (poster.jpg, extrafanart-N.jpg) so they stay
// inspectable and individually repairable.
//
// Poster/ExtraFanart carry image bytes in memory. They are still serialized
// to JSON for backward compatibility with entries written by older versions
// (images embedded as base64); new writes keep the JSON metadata-only.
type CacheEntry struct {
	CachedAt    time.Time             `json:"cached_at"`
	Detail      *javdbapi.VideoDetail `json:"detail"`
	Poster      []byte                `json:"poster,omitempty"`
	ExtraFanart [][]byte              `json:"extra_fanart,omitempty"`
}

type Cache struct {
	dir string
}

func New(dir string) *Cache {
	return &Cache{dir: dir}
}

func (c *Cache) base(movieCode string) string {
	return filepath.Join(c.dir, SanitizeName(code.Normalize(movieCode)))
}

func (c *Cache) path(movieCode string) string { return c.base(movieCode) + ".json" }

func (c *Cache) imagePath(movieCode, name string) string {
	return filepath.Join(c.base(movieCode), name)
}

// Load reads a cached entry: metadata from JSON, images from raw files.
// Legacy entries with base64-embedded images are still honored.
// Missing/corrupt entries return nil so the caller falls back to a live scrape.
func (c *Cache) Load(movieCode string) *CacheEntry {
	data, err := os.ReadFile(c.path(movieCode))
	if err != nil {
		return nil
	}
	var entry CacheEntry
	if err := json.Unmarshal(data, &entry); err != nil || entry.Detail == nil {
		return nil
	}

	// Raw image files take precedence over legacy embedded bytes.
	if b, err := os.ReadFile(c.imagePath(movieCode, "poster.jpg")); err == nil {
		entry.Poster = b
	}
	for i := range entry.Detail.Screenshots {
		b, err := os.ReadFile(c.imagePath(movieCode, fmt.Sprintf("extrafanart-%d.jpg", i)))
		if err != nil {
			continue
		}
		for len(entry.ExtraFanart) <= i {
			entry.ExtraFanart = append(entry.ExtraFanart, nil)
		}
		entry.ExtraFanart[i] = b
	}
	return &entry
}

// Store writes metadata (JSON) and raw image files atomically.
// Cache failures are non-fatal.
func (c *Cache) Store(movieCode string, entry *CacheEntry) error {
	if err := os.MkdirAll(c.dir, 0755); err != nil {
		return err
	}

	// Images as raw files.
	if ValidImage(entry.Poster) || len(entry.ExtraFanart) > 0 {
		imgDir := c.base(movieCode)
		if err := os.MkdirAll(imgDir, 0755); err != nil {
			return err
		}
		if ValidImage(entry.Poster) {
			if err := atomicWriteFile(c.imagePath(movieCode, "poster.jpg"), entry.Poster); err != nil {
				return err
			}
		}
		for i, b := range entry.ExtraFanart {
			if !ValidImage(b) {
				continue
			}
			name := fmt.Sprintf("extrafanart-%d.jpg", i)
			if err := atomicWriteFile(c.imagePath(movieCode, name), b); err != nil {
				return err
			}
		}
	}

	// Metadata-only JSON (image bytes live in the files above).
	meta := CacheEntry{CachedAt: entry.CachedAt, Detail: entry.Detail}
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return atomicWriteFile(c.path(movieCode), data)
}

func atomicWriteFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".capture-cache-*")
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
	return os.Rename(tmpName, path)
}

// ValidImage reports whether data is a fully decodable image (guards against
// truncated downloads and HTML error pages saved as .jpg).
func ValidImage(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	_, _, err := image.Decode(bytes.NewReader(data))
	return err == nil
}

// ValidImageFile checks an on-disk image for completeness.
func ValidImageFile(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return ValidImage(data)
}

// ValidNFOFile checks the NFO exists, is non-empty and looks like our XML
// (starts with the XML declaration and closes the <movie> element).
func ValidNFOFile(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return false
	}
	s := strings.TrimSpace(string(data))
	return strings.HasPrefix(s, "<?xml") && strings.HasSuffix(s, "</movie>")
}

// SanitizeName makes an actor/tag/code safe to use as a single path segment.
func SanitizeName(name string) string {
	name = strings.TrimSpace(name)
	repl := strings.NewReplacer("/", "-", "\\", "-", ":", "-", "*", "-", "?", "-",
		"\"", "-", "<", "-", ">", "-", "|", "-", "\x00", "")
	name = repl.Replace(name)
	if name == "" || name == "." || name == ".." {
		return "_"
	}
	return name
}
