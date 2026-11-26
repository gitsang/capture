package r18

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestLookupParsesMovie pins the payload fields the NFO builder consumes.
func TestLookupParsesMovie(t *testing.T) {
	const payload = `{
		"content_id": "yrnkmtndvaj00647",
		"title": "Some Title",
		"release_date": "2024-06-14",
		"runtime_minutes": 79,
		"director": "Sagi Kanda",
		"maker": {"name": "Alice JAPAN"},
		"label": {"name": "More Cum"},
		"series": null,
		"actresses": [{"name": "Sakura Kurumi"}],
		"categories": [{"name": "Big Tits"}],
		"images": {"jacket_image": {
			"large": " ",
			"large2": "https://pics.dmm.co.jp/digital/video/x/xpl.jpg"
		}}
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("x") != "" {
			t.Errorf("unexpected query: %s", r.URL.RawQuery)
		}
		w.Write([]byte(payload))
	}))
	defer srv.Close()

	c, err := NewWithBase("", false, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	m, err := c.Lookup(context.Background(), "YRNKMTNDVAJ-647")
	if err != nil {
		t.Fatal(err)
	}
	if m.ContentID != "yrnkmtndvaj00647" || m.Title != "Some Title" {
		t.Errorf("unexpected identity: %+v", m)
	}
	if m.RuntimeMins == nil || *m.RuntimeMins != 79 {
		t.Errorf("runtime not parsed: %v", m.RuntimeMins)
	}
	if m.Maker == nil || m.Maker.Name != "Alice JAPAN" {
		t.Errorf("maker not parsed: %+v", m.Maker)
	}
	if len(m.Actresses) != 1 || m.Actresses[0].Name != "Sakura Kurumi" {
		t.Errorf("actresses not parsed: %+v", m.Actresses)
	}
	// large2 wins; large is a single space on most entries
	if got := m.CoverURL(); got != "https://pics.dmm.co.jp/digital/video/x/xpl.jpg" {
		t.Errorf("CoverURL = %q", got)
	}
}

// TestCoverURLFallsBack covers the placeholder "large" field: an entry with
// only large2 usable, and one with nothing usable at all.
func TestCoverURLFallsBack(t *testing.T) {
	var m Movie
	m.Images.JacketImage.Large = " "
	m.Images.JacketImage.Large2 = "https://example.com/a.jpg"
	if got := m.CoverURL(); got != "https://example.com/a.jpg" {
		t.Errorf("CoverURL = %q", got)
	}

	var empty Movie
	empty.Images.JacketImage.Large = " "
	if got := empty.CoverURL(); got != "" {
		t.Errorf("CoverURL on empty = %q, want empty", got)
	}
}

// TestLookupErrors covers the two miss shapes plus a server error.
func TestLookupErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Lookup builds /videos/vod/movies/detail/-/dvd_id=<id>/json
		id := r.URL.Path
		id = id[strings.LastIndex(id, "dvd_id=")+len("dvd_id="):]
		id = strings.TrimSuffix(id, "/json")
		switch {
		case id == "missing":
			w.WriteHeader(http.StatusNotFound)
		case id == "empty":
			w.Write([]byte(`{}`))
		case id == "boom":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.Write([]byte(`{"title":"ok"}`))
		}
	}))
	defer srv.Close()

	c, err := NewWithBase("", false, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	for _, id := range []string{"missing", "empty"} {
		if _, err := c.Lookup(ctx, id); !errors.Is(err, ErrNotFound) {
			t.Errorf("Lookup(%q) = %v, want ErrNotFound", id, err)
		}
	}
	if _, err := c.Lookup(ctx, "boom"); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("Lookup(boom) = %v, want a non-ErrNotFound error", err)
	}
	if _, err := c.Lookup(ctx, "  "); err == nil {
		t.Error("empty id should error")
	}
}
