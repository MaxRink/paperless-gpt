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
		{"preserve_two_decimal_cents", "888.88", "888.88"},
		{"preserve_eu_two_decimal_cents", "888,88", "888.88"},
		{"preserve_us_grouped_cents", "1,234.56", "1234.56"},
		{"preserve_eu_grouped_cents", "1.234,56", "1234.56"},
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
		{"negative_currency_prefix", "-EUR 12,34", "EUR-12.34"},
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
		{"date_dots_normalized", "date", "2023.01.01", "2023-01-01"},
		{"date_day_first_unchanged", "date", "01.02.2023", "01.02.2023"},
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

func TestMonetaryValueGrounded(t *testing.T) {
	content := "Invoice total: EUR 888.88; Subtotal: EUR 800.00; VAT: EUR 168.89; invoice number 888.00\nCredit total: -EUR 12.34\nUnit price: EUR 44.44 x 2; Grand total: EUR 888.88"
	for _, tc := range []struct {
		name    string
		value   interface{}
		content string
		field   string
		want    bool
	}{
		{name: "exact_source_amount", value: "EUR888.88", want: true},
		{name: "wrong_cents_rejected", value: "EUR888.00", want: false},
		{name: "wrong_amount_rejected", value: "EUR999.99", want: false},
		{name: "eu_separator_equivalent", value: "EUR888,88", want: true},
		{name: "subtotal_not_invoice_total", value: "EUR800.00", want: false},
		{name: "tax_uses_tax_label", value: "EUR168.89", want: true},
		{name: "currency_mismatch_rejected", value: "USD888.88", want: false},
		{name: "numeric_value_fail_closed", value: 888.88, want: false},
		{name: "missing_source_amount_fail_closed", value: "EUR123.45", want: false},
		{name: "qualified_net_total_not_invoice_total", value: "EUR800.00", want: false},
		{name: "negative_credit_total", value: "EUR-12.34", want: true},
		{name: "unit_price_not_invoice_total", value: "EUR44.44", want: false},
		{"german_rechnungsbetrag", "EUR888.88", "Rechnungsbetrag: EUR888.88", "Invoice amount", true},
		{"german_gesamtbetrag", "EUR888.88", "Gesamtbetrag: EUR888.88", "Invoice amount", true},
		{"qualified_net_total", "EUR800.00", "Gesamtbetrag netto: EUR800.00; Rechnungsbetrag brutto: EUR888.88", "Invoice amount", false},
		{"currency_without_source_evidence", "USD888.88", "Invoice total: 888.88", "Invoice amount", false},
		{"zwischensumme_not_invoice_amount", "EUR888.88", "Zwischensumme: EUR888.88\nGesamtbetrag: EUR1024.00", "Invoice amount", false},
		{"nettobetrag_not_invoice_amount", "EUR888.88", "Nettobetrag: EUR888.88", "Invoice amount", false},
		{"nettobetrag_is_net_amount", "EUR888.88", "Nettobetrag: EUR888.88", "Net amount", true},
		{"camel_invoice_amount", "EUR888.88", "Invoice total: EUR888.88", "InvoiceAmount", true},
		{"camel_invoice_tax", "EUR168.89", "VAT: EUR168.89", "InvoiceTax", true},
		{"camel_contract_cost", "EUR99.99", "Monthly cost: EUR99.99", "ContractMonthlyCost", true},
		{"camel_net_amount_rejects_gross", "EUR888.88", "Nettobetrag: EUR888.88", "InvoiceAmount", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			field := tc.field
			if field == "" {
				field = "Invoice amount"
			}
			if tc.name == "tax_uses_tax_label" {
				field = "Tax amount"
			}
			candidateContent := tc.content
			if candidateContent == "" {
				candidateContent = content
			}
			if got := monetaryValueGroundedForField(tc.value, field, candidateContent); got != tc.want {
				t.Fatalf("monetaryValueGrounded(%#v) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}
