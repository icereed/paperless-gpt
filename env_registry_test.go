package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// envReadPattern matches literal os.Getenv("X") / os.LookupEnv("X") calls.
// Dynamically-composed keys (e.g. os.Getenv(prefix+"MAX_RETRIES")) can't be
// caught this way; those are covered by the registry entries directly.
var envReadPattern = regexp.MustCompile(`os\.(?:Getenv|LookupEnv)\("([A-Z0-9_]+)"\)`)

// TestEnvRegistryCoversAllReads is the drift guard: every environment variable
// the code reads with a literal key must be documented in envRegistry. Adding a
// new os.Getenv("NEW_VAR") without a registry entry fails this test, so the
// /api/config diagnostics view can never silently miss a setting.
func TestEnvRegistryCoversAllReads(t *testing.T) {
	registered := make(map[string]bool, len(envRegistry))
	for _, e := range envRegistry {
		registered[e.Name] = true
	}

	readInCode := map[string][]string{} // var -> files that read it

	roots := []string{".", "ocr", "sanitize", "internal"}
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "node_modules" || d.Name() == "web-app" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, m := range envReadPattern.FindAllStringSubmatch(string(data), -1) {
				readInCode[m[1]] = append(readInCode[m[1]], path)
			}
			return nil
		})
		require.NoError(t, err)
	}

	require.NotEmpty(t, readInCode, "expected to find env reads in the source tree")

	var missing []string
	for name, files := range readInCode {
		if !registered[name] {
			missing = append(missing, name+" (read in "+strings.Join(files, ", ")+")")
		}
	}
	assert.Empty(t, missing, "these env vars are read in code but missing from envRegistry — add them so the /config view documents them:\n%s", strings.Join(missing, "\n"))
}

// TestEnvRegistryWellFormed checks the registry's own invariants.
func TestEnvRegistryWellFormed(t *testing.T) {
	validCategory := make(map[string]bool, len(envCategoryOrder))
	for _, c := range envCategoryOrder {
		validCategory[c] = true
	}
	seen := make(map[string]bool, len(envRegistry))
	for _, e := range envRegistry {
		assert.NotEmpty(t, e.Name, "registry entry with empty name")
		assert.Falsef(t, seen[e.Name], "duplicate registry entry: %s", e.Name)
		seen[e.Name] = true
		assert.Truef(t, validCategory[e.Category], "%s has unknown category %q", e.Name, e.Category)
		assert.NotEmptyf(t, e.Description, "%s has no description", e.Name)
	}
}
