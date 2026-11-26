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
		// heyzo underscore forms
		{"heyzo_hd_0474_full.mp4", "HEYZO-0474"},
		{"HEYZO_0521.mp4", "HEYZO-0521"},
		// long DMM-style prefixes and dot-separated letter suffix
		{"yrnkmtndvaj-642.mkv", "YRNKMTNDVAJ-642"},
		{"YRNKMTNDVAJ-647A.2025.1080p.DMM.WEB-DL.AAC2.0.H.264-MTeam.mkv", "YRNKMTNDVAJ-647"},
		{"YRNKNKJDVAJ-640A.2024.1080p.DMM.WEB-DL.AAC2.0.H.264-MTeam.mkv", "YRNKNKJDVAJ-640"},
		// site-prefix noise
		{"hhd800.com@FC2-PPV-4573738.mp4", "FC2-PPV-4573738"},
	}
	for _, c := range cases {
		if got := Extract(c.filename); got != c.want {
			t.Errorf("Extract(%q) = %q, want %q", c.filename, got, c.want)
		}
	}
}

func TestMatch(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"ABC-001", "ABC-001", true},
		{"abc-001", "ABC-001", true},
		{" abc_001 ", "ABC-001", true}, // trim + underscore normalization
		// FC2 equivalence: javdb mixes both spellings for the same movie.
		{"FC2-PPV-1234567", "FC2-1234567", true},
		{"FC2-1234567", "FC2-PPV-1234567", true},
		{"FC2-PPV-1234567", "FC2-PPV-7654321", false},
		{"ABC-001", "ABC-002", false},
		{"ABC-001", "ABCD-001", false},
		{"", "", true},
	}
	for _, c := range cases {
		if got := Match(c.a, c.b); got != c.want {
			t.Errorf("Match(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// TestZeroPadCandidates covers the older-DMM numbering fix: filenames drop
// leading zeros (ecr-084) while the sites spell codes at a fixed width
// (ECR-0084, 015ecr0084r). Padding only ADDS zeros and never strips them, so
// MBD-016 must never become MBD-16 - that is a different movie which javdb
// carries alongside MBD-016 in the same result set.
func TestZeroPadCandidates(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"ECR-084", []string{"ECR-084", "ECR-0084", "ECR-00084"}},
		{"ecr-084", []string{"ECR-084", "ECR-0084", "ECR-00084"}},
		// already at max width (5 digits): no further variants
		{"MBD-00123", []string{"MBD-00123"}},
		// padding must never shorten a code
		{"MBD-016", []string{"MBD-016", "MBD-0016", "MBD-00016"}},
		// FC2 numbers are never zero-padded (and 7 digits exceeds the cap)
		{"FC2-PPV-4573738", []string{"FC2-PPV-4573738"}},
		{"HEYZO", []string{"HEYZO"}},
		{"", []string{""}},
	}
	for _, c := range cases {
		got := ZeroPadCandidates(c.in)
		if len(got) != len(c.want) {
			t.Errorf("ZeroPadCandidates(%q) = %v, want %v", c.in, got, c.want)
			continue
		}
		for i := range c.want {
			if got[i] != c.want[i] {
				t.Errorf("ZeroPadCandidates(%q) = %v, want %v", c.in, got, c.want)
				break
			}
		}
	}
}
