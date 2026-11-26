package code

import "testing"

func TestExtract(t *testing.T) {
	cases := []struct {
		filename string
		want     string
	}{
		{"ABF-004.mp4", "ABF-004"},
		{"abf-004.mp4", "ABF-004"},
		{"Abf-004.mkv", "ABF-004"},
		{"[Thz.la]ssni-123-C.avi", "SSNI-123"},
		{"fc2-ppv-1234567.mp4", "FC2-PPV-1234567"},
		{"FC2PPV-1234567.mp4", "FC2-PPV-1234567"},
		{"fc2-1234567.mp4", "FC2-PPV-1234567"},
		{"FC2_PPV_1234567.wmv", "FC2-PPV-1234567"},
		{"T28-123.mp4", "T28-123"},
		{"3DSVR-123.mp4", "3DSVR-123"},
		{"heyzo-1234.mp4", "HEYZO-1234"},
		{"010120_001-1pon.mp4", "010120-001"},
		{"010120-001-carib.mp4", "010120-001"},
		{"1Pondo-010120-001.mp4", "1PONDO-010120-001"},
		{"2020-12-12 MIMK-079 \u7121\u53e3\u306a\u56f3\u66f8\u59d4\u54e1\u3068\u3001\u30bb\u30c3\u30af\u30b9\u6f2c\u3051\u3002 \u4e03\u6ca2\u307f\u3042.mp4", "MIMK-079"},
		{"2020-12-12 fc2-ppv-1234567.mp4", "FC2-PPV-1234567"},
		{"[2021-01-01] abf-004.mp4", "ABF-004"},
		{"2020-12-12.mp4", ""}, // bare date is not a code
		{"random_video.mp4", ""},
		{"movie.mp4", ""},
		// tier 1: trailing letter suffixes
		{"start-281ch.mp4", "START-281"},
		{"SSIS-001C.mp4", "SSIS-001"},
		{"ABP-123-C.mp4", "ABP-123"},
		{"abc-123-cd1.mp4", "ABC-123"},
		{"720P-ABP-123.mp4", "ABP-123"},
		// tier 2: more FC2 separator variants
		{"fc2 ppv 1234567.mp4", "FC2-PPV-1234567"},
		{"FC2_1234567.mp4", "FC2-PPV-1234567"},
		{"fc2-123456.mp4", "FC2-PPV-123456"},
		// tier 3: glued forms
		{"hunter003.mp4", "HUNTER-003"},
		{"SSNI497.mp4", "SSNI-497"},
		{"abp123.mp4", "ABP-123"},
		{"476MLA095.mp4", "MLA-095"},
		// noise that must NOT be a code
		{"1080p.mp4", ""},
		{"720P.mp4", ""},
		{"x264.mp4", ""},
		{"h264.mp4", ""},
		{"HEVC.mp4", ""},
		{"1080.mp4", ""},
		// extension must not leak digits
		{"ABF-004.1080p.mp4", "ABF-004"},
		{"ABF-004", "ABF-004"}, // no extension at all
		{"movie2.mp4", ""},     // glued needs >=2 digits
		{"hunter3.mp4", ""},
		{"ABF-004-CD2.mp4", "ABF-004"},
		// known looseness of the multi-segment/glued fallback tiers:
		// extraction only proposes a candidate; the exact match against
		// search results filters bogus ones out.
		{"AB-1.mp4", "AB-1"},                       // 1 digit
		{"A-123.mp4", "A-123"},                     // 1-letter prefix
		{"ABCDEFGHIJK-123.mp4", "ABCDEFGHIJK-123"}, // 11-letter prefix
		{"ABF-123456789.mp4", "ABF-123456789"},     // 9-digit run
		{"fc2-1234.mp4", "FC2-1234"},               // below FC2 minimum, multi-segment picks it up
		{"movie2023.mp4", "MOVIE-2023"},            // year-like word
	}
	for _, c := range cases {
		if got := Extract(c.filename); got != c.want {
			t.Errorf("Extract(%q) = %q, want %q", c.filename, got, c.want)
		}
	}
}
