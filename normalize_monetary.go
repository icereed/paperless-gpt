package main

import (
	"fmt"
	"regexp"
	"strings"
)

// normalizeMonetary turns an LLM-emitted monetary value into the
// Paperless-canonical form: optional 3-letter currency code immediately
// followed by a plain number with exactly two decimals and no thousands
// separators (e.g. "USD1053.52", "1053.52").
//
// It disambiguates US thousands ("1,053.52") from EU decimal ("1.053,52")
// instead of doing a naive comma->dot swap, which would corrupt US values:
//
//   - If both '.' and ',' are present, whichever appears LAST is the decimal
//     separator; the other is the thousands separator.
//   - If only one is present, it's the decimal when 1-2 digits follow it,
//     or a thousands separator when 3 digits follow it (and the integer part
//     has 1-3 digits).
//
// If the input cannot be confidently parsed, it is returned unchanged so
// paperless-ngx's validation surfaces the bad value rather than silently
// corrupting it.
var easternArabicDigitsReplacer = strings.NewReplacer(
	"٠", "0", "١", "1", "٢", "2", "٣", "3", "٤", "4",
	"٥", "5", "٦", "6", "٧", "7", "٨", "8", "٩", "9",
	"۰", "0", "۱", "1", "۲", "2", "۳", "3", "۴", "4",
	"۵", "5", "۶", "6", "۷", "7", "۸", "8", "۹", "9",
)

// arabicCurrencyReplacer maps unambiguous Arabic currency abbreviations and
// country-qualified names to ISO-4217 codes. Bare names shared across
// multiple currencies ("ريال", "درهم", "دينار", "جنيه", "ليرة") are left
// untouched so ambiguous inputs pass through unchanged instead of recording
// the wrong currency.
var arabicCurrencyReplacer = strings.NewReplacer(
	"ر.س.", "SAR", "ر.س", "SAR", "ريال سعودي", "SAR",
	"د.إ.", "AED", "د.إ", "AED", "درهم إماراتي", "AED", "درهم اماراتي", "AED",
	"ر.ق.", "QAR", "ر.ق", "QAR", "ريال قطري", "QAR",
	"ر.ع.", "OMR", "ر.ع", "OMR", "ريال عماني", "OMR", "ريال عُماني", "OMR",
	"د.ك.", "KWD", "د.ك", "KWD", "دينار كويتي", "KWD",
	"د.ب.", "BHD", "د.ب", "BHD", "دينار بحريني", "BHD",
	"د.أ.", "JOD", "د.أ", "JOD", "دينار أردني", "JOD", "دينار اردني", "JOD",
	"ج.م.", "EGP", "ج.م", "EGP", "جنيه مصري", "EGP",
	"د.م.", "MAD", "د.م", "MAD", "درهم مغربي", "MAD",
)

func normalizeMonetary(value string) string {
	raw := strings.TrimSpace(arabicCurrencyReplacer.Replace(easternArabicDigitsReplacer.Replace(value)))
	if raw == "" {
		return value
	}

	currency, numeric := splitCurrencyAndNumber(raw)
	if numeric == "" {
		return value
	}

	parsed, ok := parseAmount(numeric)
	if !ok {
		return value
	}

	if currency != "" {
		return fmt.Sprintf("%s%s", currency, parsed)
	}
	return parsed
}

// normalizeCustomFieldValue applies type-aware normalization to a single
// custom-field value. Monetary strings become the paperless-ngx amount
// form, and date strings that are a year-first calendar date become
// YYYY-MM-DD. Everything else (numbers, bools, other types, nil, and a
// date that cannot be read confidently) passes through unchanged.
func normalizeCustomFieldValue(dataType string, value interface{}) interface{} {
	s, ok := value.(string)
	if !ok {
		return value
	}
	switch dataType {
	case "monetary":
		return normalizeMonetary(s)
	case "date":
		return normalizeCreatedDate(s)
	default:
		return value
	}
}

var (
	currencyCodePrefixRe = regexp.MustCompile(`^([A-Za-z]{3})`)
	currencyCodeSuffixRe = regexp.MustCompile(`([A-Za-z]{3})$`)
	numericCharsRe       = regexp.MustCompile(`^[+\-]?[0-9.,٫٬\s]+$`)
)

var currencySymbolToCode = map[rune]string{
	'$': "USD",
	'€': "EUR",
	'£': "GBP",
	'¥': "JPY",
	'₹': "INR",
	'₩': "KRW",
	'₽': "RUB",
}

// splitCurrencyAndNumber pulls a 3-letter ISO-style code or recognized
// symbol off the start or end of s and returns (uppercase code, numeric
// body). Returns ("", "") if s contains nothing that looks numeric.
func splitCurrencyAndNumber(s string) (string, string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", ""
	}

	runes := []rune(s)

	// Symbol prefix, e.g. "$1,053.52".
	if code, ok := currencySymbolToCode[runes[0]]; ok {
		return code, strings.TrimSpace(string(runes[1:]))
	}
	// Symbol suffix, e.g. "1.053,52€".
	if code, ok := currencySymbolToCode[runes[len(runes)-1]]; ok {
		return code, strings.TrimSpace(string(runes[:len(runes)-1]))
	}

	// 3-letter code prefix, e.g. "USD1,053.52" or "USD 1,053.52".
	if m := currencyCodePrefixRe.FindString(s); m != "" {
		rest := strings.TrimSpace(s[len(m):])
		if rest != "" && looksNumeric(rest) {
			return strings.ToUpper(m), rest
		}
	}
	// 3-letter code suffix, e.g. "1053.52 USD".
	if m := currencyCodeSuffixRe.FindString(s); m != "" {
		rest := strings.TrimSpace(s[:len(s)-len(m)])
		if rest != "" && looksNumeric(rest) {
			return strings.ToUpper(m), rest
		}
	}

	if looksNumeric(s) {
		return "", s
	}
	return "", ""
}

func looksNumeric(s string) bool {
	return numericCharsRe.MatchString(strings.TrimSpace(s))
}

// parseAmount turns a numeric string (no currency code) into "N.NN".
// Returns ("", false) if the input cannot be confidently parsed.
func parseAmount(s string) (string, bool) {
	s = strings.ReplaceAll(strings.TrimSpace(s), " ", "")
	if s == "" {
		return "", false
	}

	sign := ""
	switch s[0] {
	case '-':
		sign = "-"
		s = s[1:]
	case '+':
		s = s[1:]
	}
	if s == "" {
		return "", false
	}

	hasArabicDecimal := strings.ContainsRune(s, '٫')
	hasArabicThousands := strings.ContainsRune(s, '٬')
	hasComma := strings.Contains(s, ",")
	hasDot := strings.Contains(s, ".")

	var intPart, fracPart string
	var ok bool

	switch {
	case hasArabicDecimal:
		intPart, fracPart, ok = parseArabicDecimal(s)
	case hasArabicThousands:
		intPart, fracPart, ok = parseArabicThousands(s, hasDot, hasComma)
	case hasComma && hasDot:
		intPart, fracPart, ok = parseBothSeparators(s)
	case hasComma:
		intPart, fracPart, ok = parseSingleSeparator(s, ',')
	case hasDot:
		intPart, fracPart, ok = parseSingleSeparator(s, '.')
	default:
		intPart, fracPart, ok = s, "", isDigits(s)
	}
	if !ok {
		return "", false
	}

	// Pad/truncate fractional part to exactly two digits. Truncation rather
	// than rounding keeps behavior deterministic; LLMs rarely emit extra
	// precision intentionally.
	switch {
	case len(fracPart) == 0:
		fracPart = "00"
	case len(fracPart) == 1:
		fracPart = fracPart + "0"
	case len(fracPart) > 2:
		fracPart = fracPart[:2]
	}

	intPart = strings.TrimLeft(intPart, "0")
	if intPart == "" {
		intPart = "0"
	}

	return sign + intPart + "." + fracPart, true
}

func parseArabicDecimal(s string) (intPart, fracPart string, ok bool) {
	parts := strings.Split(s, "٫")
	if len(parts) != 2 {
		return "", "", false
	}
	left, right := parts[0], parts[1]
	if !isDigits(right) {
		return "", "", false
	}
	switch {
	case strings.ContainsRune(left, '٬') && !strings.ContainsAny(left, ".,"):
		intPart, ok = parseThousandsGroups(left, '٬')
	case strings.Contains(left, ",") && !strings.ContainsAny(left, ".٬"):
		intPart, ok = parseThousandsGroups(left, ',')
	case strings.Contains(left, ".") && !strings.ContainsAny(left, ",٬"):
		intPart, ok = parseThousandsGroups(left, '.')
	default:
		intPart, ok = left, isDigits(left)
	}
	if !ok {
		return "", "", false
	}
	return intPart, right, true
}

func parseArabicThousands(s string, hasDot, hasComma bool) (intPart, fracPart string, ok bool) {
	if hasComma {
		return "", "", false
	}
	if hasDot {
		parts := strings.Split(s, ".")
		if len(parts) != 2 || !isDigits(parts[1]) {
			return "", "", false
		}
		intPart, ok = parseThousandsGroups(parts[0], '٬')
		if !ok {
			return "", "", false
		}
		return intPart, parts[1], true
	}
	intPart, ok = parseThousandsGroups(s, '٬')
	if !ok {
		return "", "", false
	}
	return intPart, "", true
}

func parseThousandsGroups(s string, sep rune) (string, bool) {
	parts := strings.Split(s, string(sep))
	if len(parts) == 1 {
		if !isDigits(parts[0]) {
			return "", false
		}
		return parts[0], true
	}
	if len(parts[0]) < 1 || len(parts[0]) > 3 || !isDigits(parts[0]) {
		return "", false
	}
	for _, p := range parts[1:] {
		if len(p) != 3 || !isDigits(p) {
			return "", false
		}
	}
	return strings.Join(parts, ""), true
}

func parseBothSeparators(s string) (intPart, fracPart string, ok bool) {
	lastComma := strings.LastIndex(s, ",")
	lastDot := strings.LastIndex(s, ".")
	var decimalSep, thousandsSep string
	if lastComma > lastDot {
		decimalSep, thousandsSep = ",", "."
	} else {
		decimalSep, thousandsSep = ".", ","
	}
	stripped := strings.ReplaceAll(s, thousandsSep, "")
	parts := strings.Split(stripped, decimalSep)
	if len(parts) != 2 {
		return "", "", false
	}
	if !isDigits(parts[0]) || !isDigits(parts[1]) {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func parseSingleSeparator(s string, sep rune) (intPart, fracPart string, ok bool) {
	parts := strings.Split(s, string(sep))

	// Multiple occurrences => all thousands separators.
	if len(parts) > 2 {
		if len(parts[0]) < 1 || len(parts[0]) > 3 || !isDigits(parts[0]) {
			return "", "", false
		}
		for _, p := range parts[1:] {
			if len(p) != 3 || !isDigits(p) {
				return "", "", false
			}
		}
		return strings.Join(parts, ""), "", true
	}

	// Exactly one occurrence. The classic thousands-grouping pattern is
	// exactly 3 digits on the right AND 1-3 digits on the left (e.g. "1,053"
	// or "1.053"). Anything else is a decimal — including 3-digit-right with
	// a 4+ digit left (e.g. "1053.525" can only be a decimal, since
	// thousands groups beyond the leftmost are exactly 3 digits). Excess
	// decimal precision is truncated by the caller to two digits.
	left, right := parts[0], parts[1]
	if !isDigits(left) || !isDigits(right) {
		return "", "", false
	}

	if len(right) == 3 && len(left) >= 1 && len(left) <= 3 {
		return left + right, "", true
	}
	return left, right, true
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
