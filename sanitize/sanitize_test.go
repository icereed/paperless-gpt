package sanitize

import (
	"os"
	"sync"
	"testing"
)

func TestSanitize(t *testing.T) {
	tests := []struct {
		name            string
		literals        string
		regexes         string
		input           string
		expected        string
		expectInitError bool
	}{
		{
			name:     "no patterns",
			input:    "Hello World",
			expected: "Hello World",
		},
		{
			name:     "literal removal",
			literals: "World",
			input:    "Hello World",
			expected: "Hello ",
		},
		{
			name:     "multiple literals",
			literals: "foo,bar",
			input:    "foobar baz",
			expected: " baz",
		},
		{
			name:     "literals with spaces",
			literals: " foo , bar ",
			input:    "foo bar baz",
			expected: "  baz",
		},
		{
			name:     "regex iban",
			regexes:  `DE\d{20}`,
			input:    "IBAN: DE56123341212312312312 end",
			expected: "IBAN:  end",
		},
		{
			name:     "regex email",
			regexes:  `[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`,
			input:    "Contact: john@example.com or jane@test.org",
			expected: "Contact:  or ",
		},
		{
			name:     "literal and regex combined",
			literals: "CONFIDENTIAL",
			regexes:  `\b\d{4}-\d{4}-\d{4}-\d{4}\b`,
			input:    "CONFIDENTIAL card: 1234-5678-9012-3456",
			expected: " card: ",
		},
		{
			name:            "invalid regex",
			regexes:         `[invalid`,
			input:           "test",
			expectInitError: true,
		},
		{
			name:     "empty content",
			literals: "test",
			input:    "",
			expected: "",
		},
		{
			name:     "regex with semicolon separator",
			regexes:  `[A-Z]{2}\d{2}[A-Z0-9]+;test@example\.com`,
			input:    "IBAN: DE561233412123123 and email: test@example.com",
			expected: "IBAN:  and email: ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset state
			literalPatterns = nil
			regexPatterns = nil
			initOnce = sync.Once{}

			// Set env vars
			if tt.literals != "" {
				os.Setenv("REMOVE_FROM_CONTENT", tt.literals)
				defer os.Unsetenv("REMOVE_FROM_CONTENT")
			}
			if tt.regexes != "" {
				os.Setenv("REMOVE_FROM_CONTENT_REGEX", tt.regexes)
				defer os.Unsetenv("REMOVE_FROM_CONTENT_REGEX")
			}

			// Initialize
			err := Init()
			if tt.expectInitError {
				if err == nil {
					t.Errorf("expected init error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected init error: %v", err)
			}

			// Test sanitization
			result := Sanitize(tt.input)
			if result != tt.expected {
				t.Errorf("Sanitize() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestParseCommaSeparated(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"a,b,c", []string{"a", "b", "c"}},
		{"a, b, c", []string{"a", "b", "c"}},
		{"  a  ,  b  ", []string{"a", "b"}},
		{"a,,b", []string{"a", "b"}},
		{"a", []string{"a"}},
		{"", []string{}},
	}

	for _, tt := range tests {
		result := parseCommaSeparated(tt.input)
		if len(result) != len(tt.expected) {
			t.Errorf("parseCommaSeparated(%q) = %v, want %v", tt.input, result, tt.expected)
			continue
		}
		for i := range result {
			if result[i] != tt.expected[i] {
				t.Errorf("parseCommaSeparated(%q)[%d] = %q, want %q", tt.input, i, result[i], tt.expected[i])
			}
		}
	}
}

func TestParseSemicolonSeparated(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{`a;b;c`, []string{"a", "b", "c"}},
		{`a; b; c`, []string{"a", "b", "c"}},
		{`DE\d{20};[a-z]+`, []string{`DE\d{20}`, `[a-z]+`}},
		{``, []string{}},
	}

	for _, tt := range tests {
		result := parseSemicolonSeparated(tt.input)
		if len(result) != len(tt.expected) {
			t.Errorf("parseSemicolonSeparated(%q) = %v, want %v", tt.input, result, tt.expected)
			continue
		}
		for i := range result {
			if result[i] != tt.expected[i] {
				t.Errorf("parseSemicolonSeparated(%q)[%d] = %q, want %q", tt.input, i, result[i], tt.expected[i])
			}
		}
	}
}
