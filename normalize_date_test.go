package main

import "testing"

func TestNormalizeCreatedDate(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"iso_unchanged", "2023-01-01", "2023-01-01"},
		{"dots", "2023.01.01", "2023-01-01"},
		{"slashes", "2023/01/01", "2023-01-01"},
		{"unpadded", "2023.1.1", "2023-01-01"},
		{"quoted", `"2023.01.01"`, "2023-01-01"},
		{"iso_with_time_t", "2023-01-01T15:04:05Z", "2023-01-01"},
		{"dots_with_time_space", "2023.01.01 15:04:05", "2023-01-01"},
		{"offset", "2023-01-01T15:04:05+01:00", "2023-01-01"},

		// Not a confident year-first date, or not a real day. Leave it.
		{"impossible_day", "2023-01-79", "2023-01-79"},
		{"impossible_month", "2023.13.01", "2023.13.01"},
		{"non_leap", "2026-02-29", "2026-02-29"},
		{"day_first", "01.02.2023", "01.02.2023"},
		{"prose", "the date is 2023.01.01", "the date is 2023.01.01"},
		{"empty", "", ""},
		{"whitespace_garbage", "  not a date  ", "  not a date  "},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeCreatedDate(tc.in)
			if got != tc.want {
				t.Errorf("normalizeCreatedDate(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestValidateCustomFieldValue(t *testing.T) {
	cases := []struct {
		name     string
		dataType string
		value    interface{}
		wantErr  bool
	}{
		{"date_valid", "date", "2026-09-30", false},
		{"date_leap_day", "date", "2024-02-29", false},
		{"date_impossible_day", "date", "2026-09-31", true},
		{"date_not_leap_year", "date", "2026-02-29", true},
		{"date_day_first", "date", "01.02.2023", true},
		{"date_empty", "date", "", true},
		{"date_not_a_string", "date", 20260930, true},
		{"date_nil_clears_field", "date", nil, false},
		{"string_not_checked", "string", "2026-09-31", false},
		{"monetary_not_checked", "monetary", "EUR786.50", false},
		{"unknown_type_not_checked", "", "2026-09-31", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateCustomFieldValue(tc.dataType, tc.value)
			if (err != nil) != tc.wantErr {
				t.Errorf("validateCustomFieldValue(%q, %#v) error = %v, wantErr %v", tc.dataType, tc.value, err, tc.wantErr)
			}
		})
	}
}
