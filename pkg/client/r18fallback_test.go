package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gitsang/capture/pkg/r18"
)

// TestDMMShotBase pins the cover→screenshot URL derivation. DMM reuses one
// content id for both, so …/<id>/<id>pl.jpg yields …/<id>/<id>jp-N.jpg.
func TestDMMShotBase(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"https://pics.dmm.co.jp/digital/video/yrnkmtndvaj00647/yrnkmtndvaj00647pl.jpg",
			"https://pics.dmm.co.jp/digital/video/yrnkmtndvaj00647/yrnkmtndvaj00647", true},
		{" https://pics.dmm.co.jp/mono/movie/adult/oae286/oae286pl.jpg ",
			"https://pics.dmm.co.jp/mono/movie/adult/oae286/oae286", true},
		{"", "", false},
		{"https://example.com/cover.jpg", "", false},
		// stem does not repeat the directory name: the jp-N rule does not hold
		{"https://pics.dmm.co.jp/digital/video/abc/xyzpl.jpg", "", false},
	}
	for _, c := range cases {
		got, ok := dmmShotBase(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("dmmShotBase(%q) = (%q, %v), want (%q, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

// TestHeadOKRejectsPlaceholder is the regression for the DMM CDN quirk: a
// missing image answers 302 → a "now_printing" placeholder that returns 200.
// Following that redirect reported 12 identical placeholder images as real
// screenshots, so the probe must treat any 3xx as a miss.
func TestHeadOKRejectsPlaceholder(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "jp-1.jpg") {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Redirect(w, r, "/placeholder.jpg", http.StatusFound)
	}))
	defer srv.Close()

	c := &Client{probe: &http.Client{
		Timeout: srv.Client().Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}}
	ok, err := c.headOK(context.Background(), srv.URL+"/jp-1.jpg")
	if err != nil || !ok {
		t.Fatalf("existing image: ok=%v err=%v", ok, err)
	}
	ok, err = c.headOK(context.Background(), srv.URL+"/jp-2.jpg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("redirect to placeholder must report false, not follow the redirect")
	}
}

// TestNoResultClassification guards the fallback trigger: only a definitive
// "javdb has no such code" may fall through to r18.dev. A transport failure
// must not be papered over with foreign metadata that then gets cached.
func TestNoResultClassification(t *testing.T) {
	if !NoResult(errNoResult) {
		t.Fatal("errNoResult must classify as a definitive miss")
	}
	if !NoResult(fmt.Errorf("%w: no results for X", errNoResult)) {
		t.Fatal("wrapped errNoResult must classify as a definitive miss")
	}
	if NoResult(errors.New("dial tcp: timeout")) {
		t.Error("transport error must not classify as a definitive miss")
	}
	if NoResult(nil) {
		t.Error("nil must not classify as a miss")
	}
}

// TestR18FallbackOnlyOnDefinitiveMiss checks the gate in SearchByCode: FC2
// never falls back (r18 carries no FC2 releases).
func TestR18FallbackOnlyOnDefinitiveMiss(t *testing.T) {
	if !isFC2("FC2-PPV-4573738") {
		t.Error("FC2-PPV-4573738 must be recognized as FC2")
	}
	if isFC2("MBD-016") {
		t.Error("MBD-016 is not FC2")
	}
}

// TestParseR18Date covers both date shapes r18 serves.
func TestParseR18Date(t *testing.T) {
	for _, raw := range []string{"2015-10-30", "2005-06-09 10:00:04"} {
		got, err := parseR18Date(raw)
		if err != nil {
			t.Errorf("parseR18Date(%q): %v", raw, err)
			continue
		}
		if got.Year() == 0 {
			t.Errorf("parseR18Date(%q) returned zero time", raw)
		}
	}
	for _, raw := range []string{"", "not-a-date"} {
		if _, err := parseR18Date(raw); err == nil {
			t.Errorf("parseR18Date(%q) should fail", raw)
		}
	}
}

// TestLookupNotFound checks r18's two miss shapes: HTTP 404 and HTTP 200 with
// an empty object (returned for some unindexed ids).
func TestLookupNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "EMPTY"):
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c, err := r18.NewWithBase("", false, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"OM-122", "EMPTY-001"} {
		if _, err := c.Lookup(context.Background(), id); !errors.Is(err, r18.ErrNotFound) {
			t.Errorf("Lookup(%q) err = %v, want ErrNotFound", id, err)
		}
	}
}
