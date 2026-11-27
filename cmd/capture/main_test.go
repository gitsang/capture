package main

import (
	"strings"
	"testing"
)

// TestHelpNoTrailingWhitespace guards the multi-line --fix usage: without the
// captureTrimLines template func, pflag pads blank lines out to the usage
// column and they end up as whitespace-only lines.
func TestHelpNoTrailingWhitespace(t *testing.T) {
	var buf strings.Builder
	rootCmd.SetOut(&buf)
	defer rootCmd.SetOut(nil)
	if err := rootCmd.Help(); err != nil {
		t.Fatal(err)
	}
	help := buf.String()
	if !strings.Contains(help, "re-scrape the files parked") {
		t.Fatalf("multi-line --fix usage missing from help:\n%s", help)
	}
	for i, line := range strings.Split(help, "\n") {
		if line != strings.TrimRight(line, " \t") {
			t.Errorf("line %d has trailing whitespace: %q", i+1, line)
		}
	}
}

// TestFlagsRegistered guards against init() edits silently dropping flags.
func TestFlagsRegistered(t *testing.T) {
	for _, name := range []string{
		"input", "output", "cache-dir", "no-cache", "fix",
		"proxy", "insecure", "cookie",
		"no-fallback",
		"interval", "retry-base", "retry-attempts",
	} {
		f := rootCmd.Flags().Lookup(name)
		if f == nil {
			t.Errorf("flag --%s not registered", name)
			continue
		}
		if f.DefValue == "" && f.Value.Type() == "bool" && name != "fix" {
			t.Errorf("flag --%s has no default", name)
		}
	}
}
