package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/RPbro/javdbapi"
)

// Client wraps the javdbapi SDK plus a shared limiter used for image
// downloads, so every outbound request (API + images) honors --interval.
type Client struct {
	api     *javdbapi.Client
	limiter *Limiter
	http    *http.Client
}

// NewClient builds the SDK client. Retry/backoff and API rate limiting are
// configured inside the SDK (shared budget across all its requests).
func NewClient(interval, retryBase time.Duration, retryAttempts int) (*Client, error) {
	limiter := NewLimiter(interval)

	rps := 1.0
	if interval > 0 {
		rps = float64(time.Second) / float64(interval)
	}

	api, err := javdbapi.NewClient(javdbapi.ClientConfig{
		BaseURL: "https://javdb.com",
		Retry: javdbapi.RetryPolicy{
			MaxAttempts: retryAttempts,
			BaseDelay:   retryBase,
			MaxDelay:    retryBase << (retryAttempts - 1),
		},
		RateLimit: javdbapi.RateLimitPolicy{
			Disabled:          interval <= 0,
			RequestsPerSecond: rps,
			Burst:             1,
		},
	})
	if err != nil {
		return nil, err
	}

	return &Client{
		api:     api,
		limiter: limiter,
		http:    &http.Client{Timeout: 60 * time.Second},
	}, nil
}

// SearchByCode finds the movie with the exact code and fetches its detail page.
func (c *Client) SearchByCode(ctx context.Context, code string) (*javdbapi.VideoDetail, error) {
	page, err := c.api.Search(ctx, javdbapi.SearchQuery{Keyword: code})
	if err != nil {
		if errors.Is(err, javdbapi.ErrEmptyResult) || errors.Is(err, javdbapi.ErrNotFound) {
			return nil, fmt.Errorf("no results for %s", code)
		}
		return nil, err
	}

	var summary *javdbapi.VideoSummary
	for i := range page.Items {
		if codesMatch(page.Items[i].Code, code) {
			summary = &page.Items[i]
			break
		}
	}
	if summary == nil {
		return nil, fmt.Errorf("no exact match for %s in %d results", code, len(page.Items))
	}

	return c.api.Detail(ctx, summary.ID)
}

// fetchBytes GETs url with rate limiting and exponential-backoff retry.
func (c *Client) fetchBytes(ctx context.Context, url string, retryBase time.Duration, retryAttempts int) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < retryAttempts; attempt++ {
		if attempt > 0 {
			delay := retryBase << (attempt - 1)
			fmt.Printf("  ⏳ retry %d/%d in %s: %v\n", attempt, retryAttempts-1, delay, lastErr)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}
		if err := c.limiter.Wait(ctx); err != nil {
			return nil, err
		}
		var data []byte
		data, lastErr = c.getBytesOnce(ctx, url)
		if lastErr == nil {
			return data, nil
		}
	}
	return nil, lastErr
}

// DownloadImageBytes fetches an image and verifies it decodes fully.
func (c *Client) DownloadImageBytes(ctx context.Context, url string, retryBase time.Duration, retryAttempts int) ([]byte, error) {
	data, err := c.fetchBytes(ctx, url, retryBase, retryAttempts)
	if err != nil {
		return nil, err
	}
	if !validImage(data) {
		return nil, fmt.Errorf("downloaded data is not a complete image: %s", url)
	}
	return data, nil
}

// DownloadImage downloads url to path atomically (temp file + rename) so a
// partial download never leaves a truncated image at path.
func (c *Client) DownloadImage(ctx context.Context, url, path string, retryBase time.Duration, retryAttempts int) error {
	data, err := c.DownloadImageBytes(ctx, url, retryBase, retryAttempts)
	if err != nil {
		return err
	}
	return atomicWrite(path, data, 0644)
}

func (c *Client) getBytesOnce(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/139.0.0.0 Safari/537.36")
	req.Header.Set("Referer", "https://javdb.com/")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d for %s", resp.StatusCode, url)
	}
	return io.ReadAll(resp.Body)
}
