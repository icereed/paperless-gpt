package main

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// yearFirstDatePattern matches a year-first date, optionally followed by a
// time. Separators may be '-', '.' or '/'. Day-first dates are left alone:
// 01.02.2023 is 1 February in one locale and 2 January in another, and a
// wrong rewrite is worse than leaving the value for the existing check.
var yearFirstDatePattern = regexp.MustCompile(`^(\d{4})[./-](\d{1,2})[./-](\d{1,2})(?:[T ]\d{2}:\d{2}(?::\d{2})?(?:\.\d+)?(?:Z|[+-]\d{2}:?\d{2})?)?$`)

// normalizeCreatedDate converts a year-first date into the YYYY-MM-DD that
// paperless-ngx accepts. A value that is not a real calendar date is
// returned unchanged, so the caller still sees what the model wrote.
func normalizeCreatedDate(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.Trim(s, `"'`)
	s = strings.TrimSpace(s)

	match := yearFirstDatePattern.FindStringSubmatch(s)
	if match == nil {
		return raw
	}

	month, day := match[2], match[3]
	if len(month) == 1 {
		month = "0" + month
	}
	if len(day) == 1 {
		day = "0" + day
	}
	if len(month) != 2 || len(day) != 2 {
		return raw
	}

	candidate := match[1] + "-" + month + "-" + day
	if _, err := time.Parse("2006-01-02", candidate); err != nil {
		return raw
	}
	return candidate
}

// validateCustomFieldValue reports whether paperless-ngx would reject an
// already-normalized custom field value for the field's data type. Only date
// fields are checked: the value must be a real calendar date in YYYY-MM-DD,
// the same rule the created date follows. nil clears a field and is always
// accepted; every other data type passes through to paperless-ngx.
func validateCustomFieldValue(dataType string, value interface{}) error {
	if value == nil || dataType != "date" {
		return nil
	}
	s, ok := value.(string)
	if !ok {
		return fmt.Errorf("expected a YYYY-MM-DD string, got %T", value)
	}
	_, err := time.Parse("2006-01-02", s)
	return err
}
