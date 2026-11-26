package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/RPbro/javdbapi"

	"github.com/gitsang/capture/pkg/code"
	"github.com/gitsang/capture/pkg/r18"
)

// errNoResult marks a definitive "javdb does not have this code" - as
// opposed to a network failure, rate limit or parse error. Only this error
// triggers the r18.dev fallback: a transient javdb problem must not be
// silently papered over with lower-quality foreign metadata that then gets
// cached forever.
var errNoResult = errors.New("not found")

// NoResult reports whether err means "the code is definitively absent from
// javdb" (safe to retry against another source), as opposed to a transport
// or parsing failure.
func NoResult(err error) bool { return errors.Is(err, errNoResult) }

// SearchByCode finds the movie with the exact code and fetches its detail page.
//
// javdb is authoritative: FC2 releases and Japanese titles only exist there.
// When javdb definitively lacks the code, r18.dev (DMM's index) is asked as a
// fallback - it carries older DMM numbering that javdb never indexed. FC2 is
// absent from r18, so those miss both and stay failures.
func (c *Client) SearchByCode(ctx context.Context, movieCode string) (*javdbapi.VideoDetail, error) {
	detail, err := c.searchJavdb(ctx, movieCode)
	if err == nil {
		return detail, nil
	}
	if !NoResult(err) || c.r18api == nil || isFC2(movieCode) {
		return nil, err
	}

	fallback, r18Err := c.searchR18(ctx, movieCode)
	if r18Err != nil {
		// Keep the original javdb verdict as the primary error; the fallback
		// failure is context, not the cause.
		return nil, fmt.Errorf("%w (r18.dev fallback also failed: %v)", err, r18Err)
	}
	return fallback, nil
}

// searchJavdb searches javdb once, then compares the hits against every
// zero-padding variant of the code. javdb spells older DMM codes with fixed
// width (ECR-0084) while filenames often drop the zeros (ecr-084); the site
// search already surfaces the right item for the unpadded query, so matching
// against the variants fixes it without extra requests.
//
// Padding is only ever ADDED here, never stripped. Equating MBD-016 with
// MBD-16 would be wrong (different movies, and both appear in the same result
// set), so a "same number when zeros are ignored" comparison is deliberately
// not used.
func (c *Client) searchJavdb(ctx context.Context, movieCode string) (*javdbapi.VideoDetail, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, err
	}
	page, err := c.api.Search(ctx, javdbapi.SearchQuery{Keyword: movieCode})
	if err != nil {
		if errors.Is(err, javdbapi.ErrEmptyResult) || errors.Is(err, javdbapi.ErrNotFound) {
			return nil, fmt.Errorf("%w: no results for %s", errNoResult, movieCode)
		}
		return nil, err
	}

	candidates := code.ZeroPadCandidates(movieCode)
	var summary *javdbapi.VideoSummary
	for _, want := range candidates {
		for i := range page.Items {
			if code.Match(page.Items[i].Code, want) {
				summary = &page.Items[i]
				break
			}
		}
		if summary != nil {
			break
		}
	}
	if summary == nil {
		return nil, fmt.Errorf("%w: no exact match for %s in %d results", errNoResult, movieCode, len(page.Items))
	}

	if err := c.limiter.Wait(ctx); err != nil {
		return nil, err
	}
	return c.api.Detail(ctx, summary.ID)
}

// searchR18 looks the code up on r18.dev, trying zero-padded spellings in
// order (r18 indexes ECR-0084 but not ECR-084).
func (c *Client) searchR18(ctx context.Context, movieCode string) (*javdbapi.VideoDetail, error) {
	var lastErr error = r18.ErrNotFound
	for _, candidate := range code.ZeroPadCandidates(movieCode) {
		if err := c.limiter.Wait(ctx); err != nil {
			return nil, err
		}
		movie, err := c.r18api.Lookup(ctx, candidate)
		if err != nil {
			if errors.Is(err, r18.ErrNotFound) {
				lastErr = err
				continue
			}
			return nil, err
		}
		fmt.Printf("  🌐 Found on r18.dev: %s\n", candidate)
		return c.detailFromR18(ctx, movieCode, movie)
	}
	return nil, lastErr
}

// detailFromR18 adapts an r18.dev movie to the javdb detail shape the rest of
// the pipeline consumes.
//
// Two things cannot be recovered: javdb's video id (so actress gender is never
// "resolved" and the NFO omits <type>) and the Japanese title (r18 only serves
// English). r18's cast is female-only, so mapping actresses to Actors is
// enough - the unresolved fallback already links every actor, which here means
// every actress, with no male co-stars to pollute actor/.
func (c *Client) detailFromR18(ctx context.Context, movieCode string, m *r18.Movie) (*javdbapi.VideoDetail, error) {
	detail := javdbapi.NewVideoDetail(javdbapi.VideoSummary{
		Code:     code.Normalize(movieCode),
		Title:    strings.TrimSpace(m.Title),
		CoverURL: m.CoverURL(),
	})
	if released, err := parseR18Date(m.ReleaseDate); err == nil {
		detail.Summary.PublishedAt = released
	}
	if m.RuntimeMins != nil && *m.RuntimeMins > 0 {
		mins := *m.RuntimeMins
		detail.DurationMinutes = &mins
	}
	if m.Maker != nil && strings.TrimSpace(m.Maker.Name) != "" {
		detail.Maker = &javdbapi.NamedRef[javdbapi.MakerID]{Name: strings.TrimSpace(m.Maker.Name)}
	}
	if d := strings.TrimSpace(m.Director); d != "" {
		detail.Director = &javdbapi.NamedRef[javdbapi.DirectorID]{Name: d}
	}
	// NFO's <label> is rendered from Series, so prefer the series name and
	// fall back to r18's label field when there is no series.
	if m.Series != nil && strings.TrimSpace(m.Series.Name) != "" {
		detail.Series = &javdbapi.NamedRef[javdbapi.SeriesID]{Name: strings.TrimSpace(m.Series.Name)}
	} else if m.Label != nil && strings.TrimSpace(m.Label.Name) != "" {
		detail.Series = &javdbapi.NamedRef[javdbapi.SeriesID]{Name: strings.TrimSpace(m.Label.Name)}
	}
	for _, a := range m.Actresses {
		if name := strings.TrimSpace(a.Name); name != "" && name != "----" {
			detail.Actors = append(detail.Actors, javdbapi.Actor{Name: name})
		}
	}
	for _, cat := range m.Categories {
		if name := strings.TrimSpace(cat.Name); name != "" {
			detail.Tags = append(detail.Tags, javdbapi.Tag{Name: name})
		}
	}
	detail.Screenshots = c.r18Screenshots(ctx, m)
	return &detail, nil
}

// maxR18Shots bounds the screenshot probe. DMM galleries top out around 11-12
// images; the probe stops at the first miss, so a movie with 6 screenshots
// costs 7 requests, not 12.
const maxR18Shots = 12

// r18Screenshots derives the DMM sample-image URLs from the cover path and
// probes them, because r18.dev's JSON carries no gallery list.
//
// The jacket URL (…/<contentID>/<contentID>pl.jpg) shares its directory and
// prefix with the shots (…/<contentID>/<contentID>jp-N.jpg), which held for
// every entry probed. Probing is the only option, so the count is discovered
// one request at a time and capped; a failed probe means "no screenshots"
// rather than an error - extrafanart is optional artwork, not metadata.
func (c *Client) r18Screenshots(ctx context.Context, m *r18.Movie) []javdbapi.Screenshot {
	base, ok := dmmShotBase(m.CoverURL())
	if !ok {
		return nil
	}
	var shots []javdbapi.Screenshot
	for i := 1; i <= maxR18Shots; i++ {
		target := fmt.Sprintf("%sjp-%d.jpg", base, i)
		if err := c.limiter.Wait(ctx); err != nil {
			return shots
		}
		ok, err := c.headOK(ctx, target)
		if err != nil || !ok {
			return shots
		}
		shots = append(shots, javdbapi.Screenshot{FullURL: target})
	}
	return shots
}

// dmmShotBase turns …/<id>/<id>pl.jpg into …/<id>/<id>. DMM reuses one content
// id for cover and sample images; anything else is not worth guessing at.
func dmmShotBase(coverURL string) (string, bool) {
	trimmed := strings.TrimSpace(coverURL)
	if !strings.HasPrefix(trimmed, "http") || !strings.HasSuffix(trimmed, "pl.jpg") {
		return "", false
	}
	base := strings.TrimSuffix(trimmed, "pl.jpg")
	last := base[strings.LastIndex(base, "/")+1:]
	dir := base[:strings.LastIndex(base, "/")]
	// The file stem must repeat the directory name, otherwise the jp-N
	// convention does not hold and probing would just 404 twelve times.
	if last == "" || !strings.HasSuffix(dir, "/"+last) {
		return "", false
	}
	return base, true
}

// headOK probes a URL for existence.
//
// The DMM CDN answers 302 for an image that does not exist, redirecting to a
// "now_printing" placeholder that itself returns 200. Redirects must
// therefore NOT be followed (the default client would follow and report a
// placeholder as a hit); only a direct 200 means the image is real.
func (c *Client) headOK(ctx context.Context, target string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, target, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("User-Agent", probeUserAgent)
	resp, err := c.probe.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK, nil
}

const probeUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/139.0.0.0 Safari/537.36"

func parseR18Date(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, fmt.Errorf("empty date")
	}
	for _, layout := range []string{"2006-01-02", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized date %q", raw)
}

// isFC2 reports whether a code is an FC2 release, which r18.dev does not carry.
func isFC2(movieCode string) bool {
	return strings.HasPrefix(code.Normalize(movieCode), "FC2-")
}
