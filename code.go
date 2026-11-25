package main

import (
	"regexp"
	"strings"
)

// codePatterns are tried in order; the first match wins.
//
//   - FC2 forms first: fc2-ppv-1234567 / fc2ppv-1234567 / fc2-1234567
//     (with optional PPV-PPV / PPV_123 style separators).
//   - multi-segment codes such as T28-123, 3DSVR-123, 1Pondo-010120-001
//     (letters + digits prefix, one or more dash-separated numeric parts).
//   - plain codes such as ABC-123, HEYZO-1234 (covers 無碼/素人 series like
//     10musume / caribbean / 1pondo whose "numeric-only" ids are handled by
//     the multi-segment pattern when a date segment is present).
//
// All patterns are case-insensitive; the result is normalized to upper case
// with uniform "-" separators.
var codePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bFC2[-_ ]?(?:PPV[-_ ]?)?(\d{5,8})\b`),
	regexp.MustCompile(`(?i)\b([A-Z0-9]{2,10}(?:-\d+){1,3})\b`),
	regexp.MustCompile(`(?i)\b(\d{6}[-_]\d{2,3})\b`), // 1pondo/caribbean style: 010120_001
}

// extractCodeFromFilename pulls a movie code out of a file name.
// Returns "" when nothing recognizable is found.
func extractCodeFromFilename(filename string) string {
	// Strip extension so trailing numbers in ".mp4" etc. can't interfere.
	if i := strings.LastIndex(filename, "."); i > 0 {
		filename = filename[:i]
	}

	if m := codePatterns[0].FindStringSubmatch(filename); m != nil {
		return "FC2-PPV-" + m[1]
	}
	for _, re := range codePatterns[1:] {
		if m := re.FindStringSubmatch(filename); m != nil {
			code := strings.ToUpper(m[1])
			code = strings.ReplaceAll(code, "_", "-")
			return code
		}
	}
	return ""
}

// normalizeCode canonicalizes a code for comparison (upper case, "-" only).
func normalizeCode(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	code = strings.ReplaceAll(code, "_", "-")
	return code
}

// codesMatch reports whether two codes refer to the same movie.
// FC2-1234567 and FC2-PPV-1234567 are treated as equal.
func codesMatch(a, b string) bool {
	a, b = normalizeCode(a), normalizeCode(b)
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
