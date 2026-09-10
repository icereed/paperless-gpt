package sanitize

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
)

var (
	literalPatterns []string
	regexPatterns   []*regexp.Regexp
	initOnce        sync.Once
	initErr         error
)

// Init initializes sanitization patterns from environment variables
func Init() error {
	initOnce.Do(func() {
		// Parse literal patterns
		if literals := os.Getenv("REMOVE_FROM_CONTENT"); literals != "" {
			literalPatterns = parseCommaSeparated(literals)
		}

		// Parse regex patterns
		if regexStr := os.Getenv("REMOVE_FROM_CONTENT_REGEX"); regexStr != "" {
			patterns := parseSemicolonSeparated(regexStr)
			for _, pattern := range patterns {
				if pattern == "" {
					continue
				}
				re, err := regexp.Compile(pattern)
				if err != nil {
					initErr = fmt.Errorf("invalid regex pattern %q: %w", pattern, err)
					return
				}
				regexPatterns = append(regexPatterns, re)
			}
		}
	})
	return initErr
}

// parseCommaSeparated splits by comma and trims whitespace
func parseCommaSeparated(s string) []string {
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// parseSemicolonSeparated splits by semicolon and trims whitespace
func parseSemicolonSeparated(s string) []string {
	parts := strings.Split(s, ";")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// Sanitize removes configured patterns from content
func Sanitize(content string) string {
	if content == "" {
		return content
	}

	if err := Init(); err != nil {
		return ""
	}

	result := content

	// Remove literal patterns
	for _, pattern := range literalPatterns {
		result = strings.ReplaceAll(result, pattern, "")
	}

	// Remove regex patterns
	for _, re := range regexPatterns {
		result = re.ReplaceAllString(result, "")
	}

	return result
}
