package cache

import (
	"bytes"
	"encoding/json"
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

// CacheEntry is one cached scrape result, stored as {cache-dir}/{CODE}.json.
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

func (c *Cache) path(movieCode string) string {
	return filepath.Join(c.dir, SanitizeName(code.Normalize(movieCode))+".json")
}

// Load reads a cached entry. Missing/corrupt entries return (nil, nil) so
// the caller falls back to a live scrape.
func (c *Cache) Load(movieCode string) *CacheEntry {
	data, err := os.ReadFile(c.path(movieCode))
	if err != nil {
		return nil
	}
	var entry CacheEntry
	if err := json.Unmarshal(data, &entry); err != nil || entry.Detail == nil {
		return nil
	}
	return &entry
}

// Store writes the entry atomically. Cache failures are non-fatal.
func (c *Cache) Store(movieCode string, entry *CacheEntry) error {
	if err := os.MkdirAll(c.dir, 0755); err != nil {
		return err
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(c.dir, ".capture-cache-*")
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
	return os.Rename(tmpName, c.path(movieCode))
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
