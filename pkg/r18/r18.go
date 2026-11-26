// Package r18 talks to r18.dev, DMM's public metadata index, as a fallback
// source for movies javdb does not carry (older DMM releases, and titles
// whose numbering javdb spells with zero padding).
//
// Coverage differs from javdb: r18 has the DMM catalogue including titles
// javdb never indexed (YRNKMTNDVAJ-*, MBD-016, OAE-286), but it carries no
// FC2 releases at all, and its titles are English rather than Japanese.
package r18

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Movie is the subset of r18.dev's movie payload that maps onto our NFO.
type Movie struct {
	ContentID   string  `json:"content_id"`
	Title       string  `json:"title"`
	ReleaseDate string  `json:"release_date"`
	RuntimeMins *int    `json:"runtime_minutes"`
	Director    string  `json:"director"`
	Maker       *Named  `json:"maker"`
	Label       *Named  `json:"label"`
	Series      *Named  `json:"series"`
	Actresses   []Named `json:"actresses"`
	Categories  []Named `json:"categories"`
	Images      Images  `json:"images"`
}

// Named is r18's {name} object shape (maker, label, actress, category).
type Named struct {
	Name string `json:"name"`
}

// Images holds cover artwork. jacket_image.large2 is the full DVD cover
// (back cover + spine + front poster), the same shape javdb serves, so the
// existing poster crop applies unchanged. large is often a single space.
type Images struct {
	JacketImage struct {
		Large  string `json:"large"`
		Large2 string `json:"large2"`
	} `json:"jacket_image"`
}

// CoverURL returns the usable cover URL (large2 preferred; large is a
// placeholder on many entries).
func (m *Movie) CoverURL() string {
	for _, u := range []string{m.Images.JacketImage.Large2, m.Images.JacketImage.Large} {
		if strings.HasPrefix(strings.TrimSpace(u), "http") {
			return strings.TrimSpace(u)
		}
	}
	return ""
}

// Client fetches r18.dev movie details. The zero value is not usable; call
// New. Requests are cheap enough that the caller paces them with the shared
// limiter.
type Client struct {
	http *http.Client
	base string
}

// New builds a client. proxyURL/insecure mirror the javdb client so both
// sources go through the same egress; base is overridable for tests.
func New(proxyURL string, insecure bool) (*Client, error) {
	return NewWithBase(proxyURL, insecure, "https://r18.dev")
}

// NewWithBase is New with an explicit base URL (tests use a stub server).
func NewWithBase(proxyURL string, insecure bool, base string) (*Client, error) {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if insecure {
		tr.TLSClientConfig = insecureTLS
	}
	if proxyURL != "" {
		pu, err := url.Parse(proxyURL)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy %q: %w", proxyURL, err)
		}
		tr.Proxy = http.ProxyURL(pu)
	}
	return &Client{
		http: &http.Client{Timeout: 30 * time.Second, Transport: tr},
		base: strings.TrimSuffix(base, "/"),
	}, nil
}

// ErrNotFound means r18.dev has no entry for the code.
var ErrNotFound = fmt.Errorf("r18: not found")

// Lookup fetches one movie by DVD id. r18 accepts both the padded form
// (ECR-0084) and the bare one (oae286); callers try candidates in order.
func (c *Client) Lookup(ctx context.Context, dvdID string) (*Movie, error) {
	if strings.TrimSpace(dvdID) == "" {
		return nil, fmt.Errorf("r18: empty dvd id")
	}
	target := c.base + "/videos/vod/movies/detail/-/dvd_id=" + url.PathEscape(strings.TrimSpace(dvdID)) + "/json"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, ErrNotFound
	default:
		return nil, fmt.Errorf("r18: %s returned HTTP %d", target, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	var m Movie
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("r18: decode %s: %w", target, err)
	}
	if strings.TrimSpace(m.ContentID) == "" && strings.TrimSpace(m.Title) == "" {
		// r18 answers 200 with an empty object for some unindexed ids.
		return nil, ErrNotFound
	}
	return &m, nil
}

const userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/139.0.0.0 Safari/537.36"
