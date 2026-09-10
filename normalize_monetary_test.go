package main

import "testing"

func TestNormalizeMonetary(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		// Real failing values from the issue — these are the values
		// paperless-ngx's monetary validator rejected with HTTP 400.
		{"failing_usd_us_thousands_1", "USD1,053.52", "USD1053.52"},
		{"failing_usd_us_thousands_2", "USD1,013.54", "USD1013.54"},

		// Already-canonical values from working documents must pass through.
		{"canonical_usd_large", "USD440000.00", "USD440000.00"},
		{"canonical_no_currency", "1053.52", "1053.52"},
		{"canonical_zero", "0.00", "0.00"},

		// US: comma-thousands, dot-decimal.
		{"us_thousands_no_currency", "1,053.52", "1053.52"},
		{"us_multiple_thousands", "1,053,000.50", "1053000.50"},
		{"us_dollar_symbol", "$1,053.52", "USD1053.52"},

		// EU: dot-thousands, comma-decimal.
		{"eu_with_currency_code", "EUR1.053,52", "EUR1053.52"},
		{"eu_no_currency", "1.053,52", "1053.52"},
		{"eu_euro_symbol", "€1.053,52", "EUR1053.52"},
		{"eu_multiple_thousands", "1.053.000,50", "1053000.50"},

		// Single separator, no thousands grouping.
		{"eu_comma_decimal_only", "1053,52", "1053.52"},
		{"us_thousands_no_decimal", "1,053", "1053.00"},
		{"eu_thousands_no_decimal", "1.053", "1053.00"},

		// Integer / partial-decimal handling — pad to exactly two decimals,
		// truncate excess precision deterministically.
		{"integer_pads", "1053", "1053.00"},
		{"one_decimal_pads", "1053.5", "1053.50"},
		{"three_decimals_truncate", "1053.525", "1053.52"},
		{"four_decimals_truncate", "1053.5299", "1053.52"},

		// Currency-code variants.
		{"code_prefix_lowercase", "usd1053.52", "USD1053.52"},
		{"code_prefix_with_space", "USD 1,053.52", "USD1053.52"},
		{"code_suffix_with_space", "1053.52 USD", "USD1053.52"},

		// Signs.
		{"negative_us", "-1,053.52", "-1053.52"},
		{"explicit_positive", "+1053.52", "1053.52"},

		// Leading-zero cleanup.
		{"leading_zeros", "0001053.52", "1053.52"},

		// Whitespace-only handling.
		{"whitespace_padding", "  USD1,053.52  ", "USD1053.52"},

		// Sub-unit values (the integer side is zero).
		{"sub_unit", "0.05", "0.05"},
		{"sub_unit_eu", "0,05", "0.05"},

		// Passthrough on inputs we cannot confidently parse — better to
		// surface the bad value via paperless-ngx's validator than to
		// guess and silently corrupt it.
		{"empty", "", ""},
		{"garbage_passthrough", "abc", "abc"},

		// High-precision decimals: paperless monetary is two-decimal, so
		// excess precision truncates rather than passes through (a stuck
		// document is worse than a small precision loss).
		{"four_digits_after_separator", "1.5299", "1.52"},
		{"non_thousands_group_truncates", "12,3456", "12.34"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeMonetary(tc.in)
			if got != tc.want {
				t.Errorf("normalizeMonetary(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNormalizeCustomFieldValue(t *testing.T) {
	cases := []struct {
		name     string
		dataType string
		in       interface{}
		want     interface{}
	}{
		{"monetary_string_normalized", "monetary", "USD1,053.52", "USD1053.52"},
		{"monetary_already_canonical", "monetary", "USD440000.00", "USD440000.00"},

		// Non-monetary data types must pass through byte-for-byte even if
		// the value happens to look like a monetary string.
		{"string_passes_through", "string", "USD1,053.52", "USD1,053.52"},
		{"date_passes_through", "date", "2026-02-29", "2026-02-29"},
		{"integer_passes_through", "integer", "1,053", "1,053"},

		// Non-string monetary values (numbers, nil) must pass through —
		// only string normalization is in scope.
		{"monetary_number_passes_through", "monetary", 1053.52, 1053.52},
		{"monetary_int_passes_through", "monetary", 1053, 1053},
		{"monetary_nil_passes_through", "monetary", nil, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeCustomFieldValue(tc.dataType, tc.in)
			if got != tc.want {
				t.Errorf("normalizeCustomFieldValue(%q, %#v) = %#v, want %#v",
					tc.dataType, tc.in, got, tc.want)
			}
		})
	}
}
