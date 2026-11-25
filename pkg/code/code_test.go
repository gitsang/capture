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
		{"random_video.mp4", ""},
		{"movie.mp4", ""},
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
		{"FC2-PPV-1234567", "FC2-1234567", true},
		{"FC2-1234567", "FC2-PPV-1234567", true},
		{"FC2-PPV-1234567", "FC2-PPV-7654321", false},
		{"ABC-001", "ABC-002", false},
	}
	for _, c := range cases {
		if got := Match(c.a, c.b); got != c.want {
			t.Errorf("Match(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
