package main

import "testing"

// TestFlagsRegistered guards against init() edits silently dropping flags.
func TestFlagsRegistered(t *testing.T) {
	for _, name := range []string{
		"input", "output", "cache-dir", "no-cache", "fix",
		"proxy", "insecure", "cookie",
		"interval", "retry-base", "retry-attempts",
	} {
		if rootCmd.Flags().Lookup(name) == nil {
			t.Errorf("flag --%s not registered", name)
		}
	}
}
