package code

import (
	"regexp"
	"strings"
)

// Patterns are tried in fallback order; the first tier that matches wins:
//
//  1. Standard: pure-letter prefix + number, e.g. ABC-123, MIMK-079, HEYZO-1234.
//     Tried first so a leading date like "2020-12-12" never beats the real code.
//  2. FC2: fc2-ppv-1234567 / fc2ppv-1234567 / fc2-1234567 (dash/space/underscore
//     separators, optional PPV).
//  3. Multi-segment: alphanumeric prefix with digits, e.g. T28-123, 3DSVR-123,
//     1Pondo-010120-001.
//  4. Glued: letters+digits without separator, e.g. hunter003, START281.
//  5. Date-style: 1pondo/caribbean numeric ids like 010120_001.
//
// All patterns are case-insensitive; results are normalized to upper case
// with uniform "-" separators.
var (
	// Trailing letter suffixes are tolerated: START-281ch, ABP-123-C, SSIS-001C.
	standardPattern = regexp.MustCompile(`(?i)\b([A-Z]{2,10}-\d{2,6})(?:-?[A-Z]{1,2})?\b`)
	fc2Pattern      = regexp.MustCompile(`(?i)\bFC2[-_ ]?(?:PPV[-_ ]?)?(\d{5,8})\b`)
	// Prefix must contain at least one letter, otherwise a bare date
	// (2020-12-12) would masquerade as a multi-segment code.
	multiSegmentPattern = regexp.MustCompile(`(?i)\b([A-Z0-9]*[A-Z][A-Z0-9]*(?:-\d+){1,3})\b`)
	// Glued form: >=2 letters immediately followed by >=2 digits. Leading
	// digits are allowed (476MLA095 -> MLA-095) but a leading digit run
	// right before the letters usually is a date fragment, so only strip
	// it when the remainder still starts with letters.
	gluedPattern     = regexp.MustCompile(`(?i)\b(\d{0,3})([A-Z]{2,10})(\d{2,6})\b`)
	dateStylePattern = regexp.MustCompile(`(?i)\b(\d{6}[-_]\d{2,3})\b`)
)

// Extract pulls a movie code out of a file name.
// Returns "" when nothing recognizable is found.
func Extract(filename string) string {
	// Strip extension so trailing numbers in ".mp4" etc. can't interfere.
	if i := strings.LastIndex(filename, "."); i > 0 {
		filename = filename[:i]
	}

	if m := standardPattern.FindStringSubmatch(filename); m != nil {
		return strings.ToUpper(m[1])
	}
	if m := fc2Pattern.FindStringSubmatch(filename); m != nil {
		return "FC2-PPV-" + m[1]
	}
	if m := gluedPattern.FindStringSubmatch(filename); m != nil {
		// hunter003 -> HUNTER-003, start-281ch handled by standard first;
		// 476MLA095 -> MLA-095 (drop the numeric prefix).
		return strings.ToUpper(m[2] + "-" + m[3])
	}
	for _, re := range []*regexp.Regexp{multiSegmentPattern, dateStylePattern} {
		if m := re.FindStringSubmatch(filename); m != nil {
			c := strings.ToUpper(m[1])
			c = strings.ReplaceAll(c, "_", "-")
			return c
		}
	}
	return ""
}

// Normalize canonicalizes a code for comparison (upper case, "-" only).
func Normalize(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	code = strings.ReplaceAll(code, "_", "-")
	return code
}

// Match reports whether two codes refer to the same movie.
// FC2-1234567 and FC2-PPV-1234567 are treated as equal.
func Match(a, b string) bool {
	a, b = Normalize(a), Normalize(b)
	if a == b {
		return true
	}
	strip := func(s string) string {
		if strings.HasPrefix(s, "FC2-") {
			return "FC2-" + strings.TrimPrefix(strings.TrimPrefix(s, "FC2-"), "PPV-")
		}
		return s
	}
	return strip(a) == strip(b)
}
