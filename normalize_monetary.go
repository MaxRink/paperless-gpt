package main

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
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
func normalizeMonetary(value string) string {
	raw := strings.TrimSpace(value)
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
	numericCharsRe       = regexp.MustCompile(`^[+\-]?[0-9.,\s]+$`)
	monetaryCandidateRe  = regexp.MustCompile(`(?i)[+\-]?\s*(?:[A-Z]{3}\s*)?[$€£¥₹₩₽]?\s*[0-9][0-9.,\s]*[0-9]\s*(?:[A-Z]{3}|[$€£¥₹₩₽])?`)
	monetaryLabelRe      = regexp.MustCompile(`(?i)(amount|total|subtotal|gross|net|price|cost|betrag|gesamt|summe|preis|kosten|brutto|netto|tax|vat|mwst)`)
	camelBoundaryRe      = regexp.MustCompile(`([a-z0-9])([A-Z])`)
)

var monetaryLabelAliases = map[string]map[string]bool{
	"betrag": {"rechnungsbetrag": true, "gesamtbetrag": true},
	"gesamt": {"gesamtbetrag": true, "gesamtsumme": true},
	"summe":  {"rechnungssumme": true, "gesamtsumme": true},
	"net":    {"nettobetrag": true, "nettosumme": true, "nettopreis": true},
	"netto":  {"nettobetrag": true, "nettosumme": true, "nettopreis": true},
	"gross":  {"bruttobetrag": true, "bruttosumme": true, "bruttopreis": true},
	"brutto": {"bruttobetrag": true, "bruttosumme": true, "bruttopreis": true},
}

// monetaryValueGrounded reports whether a normalized monetary value occurs in
// the document text. It prevents a model value such as 888.00 from replacing
// a source amount of 888.88. Matching is numeric and separator-aware so
// common US/EU thousands and decimal formats compare equal. This is a guard,
// not a field selector: the prompt still decides which labelled amount is
// relevant, and ambiguous values are rejected rather than guessed.
func monetaryValueGrounded(value interface{}, content string) bool {
	return monetaryValueGroundedForField(value, "", content)
}

func monetaryValueGroundedForField(value interface{}, fieldName, content string) bool {
	s, ok := value.(string)
	if !ok {
		return false
	}
	wantCurrency, numeric := splitCurrencyAndNumber(s)
	want, ok := parseAmount(numeric)
	if !ok {
		return false
	}
	for _, line := range strings.Split(content, "\n") {
		for _, match := range monetaryCandidateRe.FindAllStringIndex(line, -1) {
			candidate := line[match[0]:match[1]]
			candidateCurrency, candidateNumeric := splitCurrencyAndNumber(candidate)
			got, valid := parseAmount(candidateNumeric)
			context := line[:match[0]]
			if cut := strings.LastIndexAny(context, ";|"); cut >= 0 {
				context = context[cut+1:]
			}
			// A model-supplied currency must be supported by the source and
			// match it exactly. Accepting an unlabelled number here would let
			// an invented USD/EUR code pass when the document has no currency
			// evidence at all.
			if valid && got == want && (wantCurrency == "" || candidateCurrency == wantCurrency) && monetaryLabelMatchesField(fieldName, context) {
				return true
			}
		}
	}
	return false
}

func monetaryLabelMatchesField(fieldName, context string) bool {
	field := strings.ToLower(camelBoundaryRe.ReplaceAllString(fieldName, "$1 $2"))
	ctx := strings.ToLower(context)
	word := hasMonetaryWord
	if (word(ctx, "subtotal") || hasNetQualifier(ctx)) && !word(field, "subtotal") && !word(field, "net") && !word(field, "netto") {
		return false
	}
	labels := []string{"total", "betrag", "summe", "gesamt"}
	switch {
	case word(field, "tax") || word(field, "vat") || word(field, "steuer"):
		labels = []string{"tax", "vat", "mwst", "steuer"}
	case word(field, "subtotal") || word(field, "zwischen"):
		labels = []string{"subtotal", "zwischen", "zwischensumme"}
	case word(field, "gross") || word(field, "brutto"):
		labels = []string{"gross", "brutto"}
	case word(field, "net") || word(field, "netto"):
		labels = []string{"net", "netto"}
	case word(field, "price") || word(field, "preis") || word(field, "cost") || word(field, "kosten"):
		labels = []string{"price", "preis", "cost", "kosten"}
	default:
		// An explicitly named amount field must still be tied to a total-like
		// label; do not accept arbitrary numbers such as invoice IDs.
		if word(field, "amount") || word(field, "betrag") {
			labels = []string{"total", "betrag", "summe", "gesamt"}
		} else if !monetaryLabelRe.MatchString(ctx) {
			return false
		}
	}
	for _, label := range labels {
		if hasMonetaryLabel(ctx, label) {
			return true
		}
	}
	return false
}

// hasMonetaryLabel recognizes only the German compound labels that are known
// to preserve the requested semantic. Broad substring matching would make
// Zwischensumme look like an invoice total, or Nettobetrag look like a gross
// amount, so compounds are explicit and field qualification stays separate.
func hasMonetaryLabel(s, wanted string) bool {
	if hasMonetaryWord(s, wanted) {
		return true
	}
	for _, token := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if monetaryLabelAliases[wanted][token] {
			return true
		}
	}
	return false
}

func hasNetQualifier(s string) bool {
	return hasMonetaryWord(s, "net") || hasMonetaryWord(s, "netto") ||
		hasMonetaryLabel(s, "net") || hasMonetaryLabel(s, "netto")
}

// hasMonetaryWord performs a case-insensitive whole-word match without
// compiling a regular expression for every candidate amount. Keeping this
// parser allocation-light matters because it runs once per model suggestion.
func hasMonetaryWord(s, wanted string) bool {
	wanted = strings.ToLower(wanted)
	for _, token := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if token == wanted {
			return true
		}
	}
	return false
}

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
	// Keep a leading sign attached to the numeric body so values such as
	// "-EUR 12.34" remain parseable while the currency prefix is removed.
	sign := ""
	if s[0] == '-' || s[0] == '+' {
		sign, s = s[:1], strings.TrimSpace(s[1:])
		if s == "" {
			return "", ""
		}
	}

	runes := []rune(s)

	// Symbol prefix, e.g. "$1,053.52".
	if code, ok := currencySymbolToCode[runes[0]]; ok {
		return code, sign + strings.TrimSpace(string(runes[1:]))
	}
	// Symbol suffix, e.g. "1.053,52€".
	if code, ok := currencySymbolToCode[runes[len(runes)-1]]; ok {
		return code, sign + strings.TrimSpace(string(runes[:len(runes)-1]))
	}

	// 3-letter code prefix, e.g. "USD1,053.52" or "USD 1,053.52".
	if m := currencyCodePrefixRe.FindString(s); m != "" {
		rest := strings.TrimSpace(s[len(m):])
		if rest != "" && looksNumeric(rest) {
			return strings.ToUpper(m), sign + rest
		}
	}
	// 3-letter code suffix, e.g. "1053.52 USD".
	if m := currencyCodeSuffixRe.FindString(s); m != "" {
		rest := strings.TrimSpace(s[:len(s)-len(m)])
		if rest != "" && looksNumeric(rest) {
			return strings.ToUpper(m), sign + rest
		}
	}

	if looksNumeric(s) {
		return "", sign + s
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

	hasComma := strings.Contains(s, ",")
	hasDot := strings.Contains(s, ".")

	var intPart, fracPart string
	var ok bool

	switch {
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
