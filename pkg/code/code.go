package code

import (
	"regexp"
	"strconv"
	"strings"
)

// Patterns are tried in fallback order; the first tier that matches wins:
//
//  1. Standard: pure-letter prefix + number, e.g. ABC-123, MIMK-079, HEYZO-1234.
//     Tried first so a leading date like "2020-12-12" never beats the real code.
//  2. FC2: fc2-ppv-1234567 / fc2ppv-1234567 / fc2-1234567 (dash/space/underscore
//     separators, optional PPV).
//  3. Glued: letters+digits without separator, e.g. hunter003, SSNI497;
//     also underscore-joined heyzo_hd_0474_full -> HEYZO-0474.
//  4. Multi-segment: alphanumeric prefix with digits, e.g. T28-123, 3DSVR-123,
//     1Pondo-010120-001.
//  5. Date-style: 1pondo/caribbean numeric ids like 010120_001.
//
// All patterns are case-insensitive; results are normalized to upper case
// with uniform "-" separators.
var (
	// Trailing letter suffixes are tolerated: START-281ch, ABP-123-C,
	// SSIS-001C, YRNKMTNDVAJ-647A (suffix may be dot-separated too).
	standardPattern = regexp.MustCompile(`(?i)\b([A-Z]{2,12}-\d{2,6})(?:[-.]?[A-Z]{1,2})?\b`)
	fc2Pattern      = regexp.MustCompile(`(?i)\bFC2[-_ ]?(?:PPV[-_ ]?)?(\d{5,8})\b`)
	// Prefix must contain at least one letter, otherwise a bare date
	// (2020-12-12) would masquerade as a multi-segment code.
	multiSegmentPattern = regexp.MustCompile(`(?i)\b([A-Z0-9]*[A-Z][A-Z0-9]*(?:-\d+){1,3})\b`)
	// Glued form: >=2 letters immediately followed by >=2 digits. Leading
	// digits are allowed (476MLA095 -> MLA-095) but a leading digit run
	// right before the letters usually is a date fragment, so only strip
	// it when the remainder still starts with letters.
	gluedPattern = regexp.MustCompile(`(?i)\b(\d{0,3})([A-Z]{2,12})(\d{2,6})\b`)
	// heyzo_hd_0474_full: label + underscore-joined segments + number.
	// Leading  only: the number is usually followed by "_full", where
	// a trailing  would fail (underscore is a word character).
	heyzoPattern     = regexp.MustCompile(`(?i)\bHEYZO(?:[_-][A-Z]+)*[_-](\d{3,4})`)
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
	if m := heyzoPattern.FindStringSubmatch(filename); m != nil {
		return "HEYZO-" + m[1]
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

// ZeroPadCandidates returns the code plus its zero-padded variants, most
// plausible first: ECR-084 -> [ECR-084 ECR-0084 ECR-00084]. Older DMM
// releases are numbered to a fixed width (ECR-0084, 015ecr0084r), so a
// filename that dropped the leading zeros never matches the site's spelling.
//
// Padding only ADDS zeros, never strips them, so this can never turn a real
// code into a different real one: MBD-016 yields MBD-0016/MBD-00016 and
// never MBD-16, which is an unrelated movie on javdb.
//
// Only the number part is padded; the prefix must stay byte-identical.
func ZeroPadCandidates(code string) []string {
	code = Normalize(code)
	i := strings.LastIndex(code, "-")
	if i <= 0 || i == len(code)-1 {
		return []string{code}
	}
	prefix, num := code[:i], code[i+1:]
	if _, err := strconv.Atoi(num); err != nil {
		return []string{code} // non-numeric tail (FC2-PPV, HEYZO-XXX)
	}
	out := []string{code}
	for width := len(num) + 1; width <= maxPaddedDigits; width++ {
		out = append(out, prefix+"-"+strings.Repeat("0", width-len(num))+num)
	}
	return out
}

// maxPaddedDigits bounds zero padding. DMM's widest observed numbering is
// 5 digits (118mbd00016); going further only costs requests.
const maxPaddedDigits = 5

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
