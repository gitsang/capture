package main

import "testing"

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
