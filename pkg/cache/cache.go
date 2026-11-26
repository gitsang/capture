package cache

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RPbro/javdbapi"

	"github.com/gitsang/capture/pkg/code"
	"github.com/gitsang/capture/pkg/imageutil"
)

// CacheEntry is one cached scrape result. Everything for one code lives in
// a single folder: {cache-dir}/{CODE}/{CODE}.json (metadata) plus raw image
// files (poster.jpg, extrafanart-N.jpg) so they stay inspectable and
// individually repairable.
//
// Poster/ExtraFanart carry image bytes in memory. They are still serialized
// to JSON for backward compatibility with entries written by older versions
// (images embedded as base64); new writes keep the JSON metadata-only.
type CacheEntry struct {
	CachedAt    time.Time             `json:"cached_at"`
	Detail      *javdbapi.VideoDetail `json:"detail"`
	Poster      []byte                `json:"poster,omitempty"`
	ExtraFanart [][]byte              `json:"extra_fanart,omitempty"`
	// Actresses holds the actor names javdb flags female on the detail page
	// (see client.ActressNames). Empty means "not resolved yet" - entries
	// written by older versions and codes whose scrape failed. A set with
	// actresses but no male co-stars is still a valid, complete result.
	Actresses []string `json:"actresses,omitempty"`
	// ActressesResolved distinguishes "no actress on this page" from "not
	// looked up yet", so a resolved empty list is not re-fetched.
	ActressesResolved bool `json:"actresses_resolved,omitempty"`
}

// ActressSet returns the cached actress names as a lookup set, plus whether
// the entry carries a resolved actress list (false = needs a re-fetch).
func (e *CacheEntry) ActressSet() (map[string]bool, bool) {
	if e == nil || !e.ActressesResolved {
		return nil, false
	}
	set := make(map[string]bool, len(e.Actresses))
	for _, n := range e.Actresses {
		set[n] = true
	}
	return set, true
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

func (c *Cache) path(movieCode string) string {
	name := SanitizeName(code.Normalize(movieCode))
	return filepath.Join(c.base(movieCode), name+".json")
}

// legacyPath is the pre-folder layout: {cache-dir}/{CODE}.json.
func (c *Cache) legacyPath(movieCode string) string {
	return c.base(movieCode) + ".json"
}

func (c *Cache) imagePath(movieCode, name string) string {
	return filepath.Join(c.base(movieCode), name)
}

// Load reads a cached entry: metadata from JSON, images from raw files.
// Legacy entries with base64-embedded images are still honored.
// Missing/corrupt entries return nil so the caller falls back to a live scrape.
func (c *Cache) Load(movieCode string) *CacheEntry {
	data, err := os.ReadFile(c.path(movieCode))
	if err != nil {
		// Fall back to the legacy flat layout ({cache-dir}/{CODE}.json,
		// possibly with base64-embedded images).
		data, err = os.ReadFile(c.legacyPath(movieCode))
		if err != nil {
			return nil
		}
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

	// Everything under one per-code folder.
	if err := os.MkdirAll(c.base(movieCode), 0755); err != nil {
		return err
	}

	// Images as raw files.
	if ValidImage(entry.Poster) || len(entry.ExtraFanart) > 0 {
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
	meta := CacheEntry{
		CachedAt:          entry.CachedAt,
		Detail:            entry.Detail,
		Actresses:         entry.Actresses,
		ActressesResolved: entry.ActressesResolved,
	}
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

// ValidImage reports whether data is a fully decodable image.
// Deprecated alias kept for callers; prefer imageutil.Valid.
func ValidImage(data []byte) bool { return imageutil.Valid(data) }

// ValidImageFile checks an on-disk image for completeness.
func ValidImageFile(path string) bool { return imageutil.ValidFile(path) }

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
