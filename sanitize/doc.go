// Package sanitize provides content sanitization functionality for removing sensitive
// strings and patterns before sending content to LLMs.
//
// Configuration is done via environment variables:
//   - REMOVE_FROM_CONTENT: Comma-separated list of literal strings to remove
//   - REMOVE_FROM_CONTENT_REGEX: Semicolon-separated list of regex patterns to remove
//
// Example usage:
//
//	if err := sanitize.Init(); err != nil {
//	    log.Fatal(err)
//	}
//	cleanContent := sanitize.Sanitize(dirtyContent)
package sanitize
