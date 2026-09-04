package query

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// TestTranslator_IFF tests translation of Snowflake IFF function.
// IFF(condition, true_value, false_value) → IF(condition, true_value, false_value)
func TestTranslator_IFF(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  bool
	}{
		{
			name:     "SimpleIFF",
			input:    "SELECT IFF(age > 18, 'adult', 'minor') FROM users",
			expected: "SELECT IF(age > 18, 'adult', 'minor') FROM users",
			wantErr:  false,
		},
		{
			name:     "IFFWithNull",
			input:    "SELECT IFF(value IS NULL, 0, value) FROM data",
			expected: "SELECT IF(value IS NULL, 0, value) FROM data",
			wantErr:  false,
		},
		{
			name:     "NestedIFF",
			input:    "SELECT IFF(x > 10, IFF(y > 5, 'A', 'B'), 'C') FROM test",
			expected: "SELECT IF(x > 10, IF(y > 5, 'A', 'B'), 'C') FROM test",
			wantErr:  false,
		},
		{
			name:     "IFFWithComplexCondition",
			input:    "SELECT IFF(score >= 90 AND attendance > 80, 'Pass', 'Fail') FROM students",
			expected: "SELECT IF(score >= 90 AND attendance > 80, 'Pass', 'Fail') FROM students",
			wantErr:  false,
		},
		{
			name:     "IFFInWHERE",
			input:    "SELECT * FROM users WHERE IFF(active, 1, 0) = 1",
			expected: "SELECT * FROM users WHERE IF(active, 1, 0) = 1",
			wantErr:  false,
		},
		{
			name:     "MultipleIFF",
			input:    "SELECT IFF(a, 1, 0), IFF(b, 2, 0), IFF(c, 3, 0) FROM test",
			expected: "SELECT IF(a, 1, 0), IF(b, 2, 0), IF(c, 3, 0) FROM test",
			wantErr:  false,
		},
		{
			name:     "IFFWithNullComparison",
			input:    "SELECT IFF(col IS NOT NULL, col, 'N/A') FROM data",
			expected: "SELECT IF(col IS NOT NULL, col, 'N/A') FROM data",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			translator := NewTranslator()
			result, err := translator.Translate(tt.input)

			if (err != nil) != tt.wantErr {
				t.Errorf("Translate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if diff := cmp.Diff(tt.expected, result); diff != "" {
					t.Errorf("Translate() mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

// TestTranslator_NVL tests translation of Snowflake NVL and IFNULL functions.
// NVL(expr1, expr2) → COALESCE(expr1, expr2)
// IFNULL(expr1, expr2) → COALESCE(expr1, expr2)
func TestTranslator_NVL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  bool
	}{
		{
			name:     "SimpleNVL",
			input:    "SELECT NVL(name, 'Unknown') FROM users",
			expected: "SELECT COALESCE(name, 'Unknown') FROM users",
			wantErr:  false,
		},
		{
			name:     "NVLWithNumbers",
			input:    "SELECT NVL(score, 0) FROM results",
			expected: "SELECT COALESCE(score, 0) FROM results",
			wantErr:  false,
		},
		{
			name:     "MultipleNVL",
			input:    "SELECT NVL(first_name, 'N/A'), NVL(last_name, 'N/A') FROM people",
			expected: "SELECT COALESCE(first_name, 'N/A'), COALESCE(last_name, 'N/A') FROM people",
			wantErr:  false,
		},
		{
			name:     "IFNULL_Translation",
			input:    "SELECT IFNULL(email, 'noreply@example.com') FROM users",
			expected: "SELECT COALESCE(email, 'noreply@example.com') FROM users",
			wantErr:  false,
		},
		{
			name:     "NestedNVL",
			input:    "SELECT NVL(NVL(col1, col2), 'default') FROM test",
			expected: "SELECT COALESCE(COALESCE(col1, col2), 'default') FROM test",
			wantErr:  false,
		},
		{
			name:     "NVLInWHERE",
			input:    "SELECT * FROM users WHERE NVL(status, 'active') = 'active'",
			expected: "SELECT * FROM users WHERE COALESCE(status, 'active') = 'active'",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			translator := NewTranslator()
			result, err := translator.Translate(tt.input)

			if (err != nil) != tt.wantErr {
				t.Errorf("Translate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if diff := cmp.Diff(tt.expected, result); diff != "" {
					t.Errorf("Translate() mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

// TestTranslator_CONCAT tests CONCAT function handling.
// CONCAT is passed through without translation.
func TestTranslator_CONCAT(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  bool
	}{
		{
			name:     "SimpleCONCAT_PassThrough",
			input:    "SELECT CONCAT(first_name, ' ', last_name) FROM users",
			expected: "SELECT CONCAT(first_name, ' ', last_name) FROM users",
			wantErr:  false,
		},
		{
			name:     "CONCATThreeStrings_PassThrough",
			input:    "SELECT CONCAT(city, ', ', state) FROM addresses",
			expected: "SELECT CONCAT(city, ', ', state) FROM addresses",
			wantErr:  false,
		},
		{
			name:     "CONCATWithColumns_PassThrough",
			input:    "SELECT CONCAT('Name: ', name) FROM people",
			expected: "SELECT CONCAT('Name: ', name) FROM people",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			translator := NewTranslator()
			result, err := translator.Translate(tt.input)

			if (err != nil) != tt.wantErr {
				t.Errorf("Translate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if diff := cmp.Diff(tt.expected, result); diff != "" {
					t.Errorf("Translate() mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

// TestTranslator_DateTimeFunctions tests translation of date/time functions.
func TestTranslator_DateTimeFunctions(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  bool
	}{
		{
			name:     "CURRENT_TIMESTAMP_RemoveParens",
			input:    "SELECT CURRENT_TIMESTAMP() FROM dual",
			expected: "SELECT CURRENT_TIMESTAMP",
			wantErr:  false,
		},
		{
			name:     "CURRENT_DATE_RemoveParens",
			input:    "SELECT CURRENT_DATE() FROM dual",
			expected: "SELECT CURRENT_DATE",
			wantErr:  false,
		},
		{
			name:     "CURRENT_TIMESTAMP_NoParens_PassThrough",
			input:    "SELECT CURRENT_TIMESTAMP FROM dual",
			expected: "SELECT CURRENT_TIMESTAMP",
			wantErr:  false,
		},
		{
			name:     "CURRENT_DATE_NoParens_PassThrough",
			input:    "SELECT CURRENT_DATE FROM dual",
			expected: "SELECT CURRENT_DATE",
			wantErr:  false,
		},
		{
			name:     "DATEADD_Translation",
			input:    "SELECT DATEADD(day, 7, order_date) FROM orders",
			expected: "SELECT (CAST(order_date AS DATE) + interval (7) day) FROM orders",
			wantErr:  false,
		},
		{
			name:     "DATEDIFF_Translation",
			input:    "SELECT DATEDIFF(day, start_date, end_date) FROM events",
			expected: "SELECT DATE_DIFF('day', CAST(start_date AS DATE), CAST(end_date AS DATE)) FROM events",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			translator := NewTranslator()
			result, err := translator.Translate(tt.input)

			if (err != nil) != tt.wantErr {
				t.Errorf("Translate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if diff := cmp.Diff(tt.expected, result); diff != "" {
					t.Errorf("Translate() mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

// TestTranslator_StringFunctions tests translation of string functions.
func TestTranslator_StringFunctions(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  bool
	}{
		{
			name:     "LENGTH",
			input:    "SELECT LENGTH(name) FROM users",
			expected: "SELECT LENGTH(name) FROM users",
			wantErr:  false,
		},
		{
			name:     "SUBSTR",
			input:    "SELECT SUBSTR(text, 1, 10) FROM documents",
			expected: "SELECT SUBSTR(text, 1, 10) FROM documents",
			wantErr:  false,
		},
		{
			name:     "UPPER",
			input:    "SELECT UPPER(name) FROM users",
			expected: "SELECT UPPER(name) FROM users",
			wantErr:  false,
		},
		{
			name:     "LOWER",
			input:    "SELECT LOWER(email) FROM users",
			expected: "SELECT LOWER(email) FROM users",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			translator := NewTranslator()
			result, err := translator.Translate(tt.input)

			if (err != nil) != tt.wantErr {
				t.Errorf("Translate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if diff := cmp.Diff(tt.expected, result); diff != "" {
					t.Errorf("Translate() mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

// TestTranslator_ComplexQuery tests translation of complex queries with multiple functions.
func TestTranslator_ComplexQuery(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  bool
	}{
		{
			name: "BasicFunctions_IFF_NVL",
			input: `SELECT
				NVL(email, 'no-email@example.com') AS email,
				IFF(age >= 18, 'adult', 'minor') AS age_group
			FROM users`,
			expected: `SELECT
				COALESCE(email, 'no-email@example.com') AS email,
				IF(age >= 18, 'adult', 'minor') AS age_group
			FROM users`,
			wantErr: false,
		},
		{
			name: "WithWHEREClause_IFF",
			input: `SELECT name, score
			FROM results
			WHERE IFF(category = 'A', score > 80, score > 60)`,
			expected: `SELECT name, score
			FROM results
			WHERE IF(category = 'A', score > 80, score > 60)`,
			wantErr: false,
		},
		{
			name:     "NestedIFF_NVL",
			input:    `SELECT IFF(status = 'active', NVL(name, 'Unknown'), 'Inactive') FROM users`,
			expected: `SELECT IF(status = 'active', COALESCE(name, 'Unknown'), 'Inactive') FROM users`,
			wantErr:  false,
		},
		{
			name:     "MultipleNVL_WithIFF",
			input:    `SELECT NVL(first_name, 'N/A'), NVL(last_name, 'N/A'), IFF(active, 1, 0) FROM people`,
			expected: `SELECT COALESCE(first_name, 'N/A'), COALESCE(last_name, 'N/A'), IF(active, 1, 0) FROM people`,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			translator := NewTranslator()
			result, err := translator.Translate(tt.input)

			if (err != nil) != tt.wantErr {
				t.Errorf("Translate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				// Normalize whitespace for comparison
				normalizedExpected := normalizeWhitespace(tt.expected)
				normalizedResult := normalizeWhitespace(result)
				if diff := cmp.Diff(normalizedExpected, normalizedResult); diff != "" {
					t.Errorf("Translate() mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

// TestTranslator_NVL2 tests NVL2 function translation.
// NVL2(expr, not_null_result, null_result) → IF(expr IS NOT NULL, not_null_result, null_result)
func TestTranslator_NVL2(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  bool
	}{
		{
			name:     "SimpleNVL2",
			input:    "SELECT NVL2(col, 'not null', 'null') FROM test",
			expected: "SELECT IF(col IS NOT NULL, 'not null', 'null') FROM test",
			wantErr:  false,
		},
		{
			name:     "NVL2WithNumbers",
			input:    "SELECT NVL2(score, score * 2, 0) FROM results",
			expected: "SELECT IF(score IS NOT NULL, score * 2, 0) FROM results",
			wantErr:  false,
		},
		{
			name:     "NVL2InWHERE",
			input:    "SELECT * FROM users WHERE NVL2(status, 1, 0) = 1",
			expected: "SELECT * FROM users WHERE IF(status IS NOT NULL, 1, 0) = 1",
			wantErr:  false,
		},
		{
			name:     "MultipleNVL2",
			input:    "SELECT NVL2(a, 'A', 'X'), NVL2(b, 'B', 'Y') FROM test",
			expected: "SELECT IF(a IS NOT NULL, 'A', 'X'), IF(b IS NOT NULL, 'B', 'Y') FROM test",
			wantErr:  false,
		},
		{
			name:     "NestedNVL2WithNVL",
			input:    "SELECT NVL2(col, NVL(inner_col, 'default'), 'null') FROM test",
			expected: "SELECT IF(col IS NOT NULL, COALESCE(inner_col, 'default'), 'null') FROM test",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			translator := NewTranslator()
			result, err := translator.Translate(tt.input)

			if (err != nil) != tt.wantErr {
				t.Errorf("Translate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if diff := cmp.Diff(tt.expected, result); diff != "" {
					t.Errorf("Translate() mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

// TestTranslator_TO_VARIANT tests TO_VARIANT function translation.
// TO_VARIANT(x) → CAST(x AS JSON) (DuckDB compatible)
func TestTranslator_TO_VARIANT(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  bool
	}{
		{
			name:     "SimpleTO_VARIANT",
			input:    "SELECT TO_VARIANT(data) FROM test",
			expected: "SELECT CAST(data AS JSON) FROM test",
			wantErr:  false,
		},
		{
			name:     "TO_VARIANTWithString",
			input:    "SELECT TO_VARIANT('hello') FROM dual",
			expected: "SELECT CAST('hello' AS JSON)",
			wantErr:  false,
		},
		{
			name:     "TO_VARIANTWithNumber",
			input:    "SELECT TO_VARIANT(123) FROM dual",
			expected: "SELECT CAST(123 AS JSON)",
			wantErr:  false,
		},
		{
			name:     "MultipleTO_VARIANT",
			input:    "SELECT TO_VARIANT(a), TO_VARIANT(b) FROM test",
			expected: "SELECT CAST(a AS JSON), CAST(b AS JSON) FROM test",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			translator := NewTranslator()
			result, err := translator.Translate(tt.input)

			if (err != nil) != tt.wantErr {
				t.Errorf("Translate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if diff := cmp.Diff(tt.expected, result); diff != "" {
					t.Errorf("Translate() mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

// TestTranslator_PARSE_JSON tests PARSE_JSON function translation.
// PARSE_JSON(str) → CAST(str AS JSON) (DuckDB compatible)
func TestTranslator_PARSE_JSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  bool
	}{
		{
			name:     "SimplePARSE_JSON",
			input:    "SELECT PARSE_JSON(json_str) FROM test",
			expected: "SELECT CAST(json_str AS JSON) FROM test",
			wantErr:  false,
		},
		{
			name:     "PARSE_JSONWithLiteral",
			input:    `SELECT PARSE_JSON('{"key": "value"}') FROM dual`,
			expected: `SELECT CAST('{"key": "value"}' AS JSON)`,
			wantErr:  false,
		},
		{
			name:     "PARSE_JSONInWHERE",
			input:    "SELECT * FROM test WHERE PARSE_JSON(data) IS NOT NULL",
			expected: "SELECT * FROM test WHERE CAST(data AS JSON) IS NOT NULL",
			wantErr:  false,
		},
		{
			name:     "MultiplePARSE_JSON",
			input:    "SELECT PARSE_JSON(a), PARSE_JSON(b) FROM test",
			expected: "SELECT CAST(a AS JSON), CAST(b AS JSON) FROM test",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			translator := NewTranslator()
			result, err := translator.Translate(tt.input)

			if (err != nil) != tt.wantErr {
				t.Errorf("Translate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if diff := cmp.Diff(tt.expected, result); diff != "" {
					t.Errorf("Translate() mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

// TestTranslator_DATEADD tests DATEADD function translation.
// DATEADD(part, n, date) → (CAST(date AS DATE) + INTERVAL n part) for DuckDB
func TestTranslator_DATEADD(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  bool
	}{
		{
			name:     "DATEADDDays",
			input:    "SELECT DATEADD(day, 7, order_date) FROM orders",
			expected: "SELECT (CAST(order_date AS DATE) + interval (7) day) FROM orders",
			wantErr:  false,
		},
		{
			name:     "DATEADDMonths",
			input:    "SELECT DATEADD(month, 1, start_date) FROM subscriptions",
			expected: "SELECT (CAST(start_date AS DATE) + interval (1) month) FROM subscriptions",
			wantErr:  false,
		},
		{
			name:     "DATEADDYears",
			input:    "SELECT DATEADD(year, 5, birth_date) FROM users",
			expected: "SELECT (CAST(birth_date AS DATE) + interval (5) year) FROM users",
			wantErr:  false,
		},
		{
			name:     "DATEADDNegative",
			input:    "SELECT DATEADD(day, -30, CURRENT_DATE()) FROM dual",
			expected: "SELECT (CAST(CURRENT_DATE AS DATE) + interval (-30) day)",
			wantErr:  false,
		},
		{
			name:     "DATEADDHours",
			input:    "SELECT DATEADD(hour, 24, created_at) FROM events",
			expected: "SELECT (CAST(created_at AS DATE) + interval (24) hour) FROM events",
			wantErr:  false,
		},
		{
			name:     "MultipleDATEADD",
			input:    "SELECT DATEADD(day, 1, date1), DATEADD(month, 2, date2) FROM test",
			expected: "SELECT (CAST(date1 AS DATE) + interval (1) day), (CAST(date2 AS DATE) + interval (2) month) FROM test",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			translator := NewTranslator()
			result, err := translator.Translate(tt.input)

			if (err != nil) != tt.wantErr {
				t.Errorf("Translate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if diff := cmp.Diff(tt.expected, result); diff != "" {
					t.Errorf("Translate() mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

// TestTranslator_DATEDIFF tests DATEDIFF function translation.
// DATEDIFF(part, start, end) → DATE_DIFF('part', start, end) for DuckDB
func TestTranslator_DATEDIFF(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  bool
	}{
		{
			name:     "DATEDIFFDays",
			input:    "SELECT DATEDIFF(day, start_date, end_date) FROM events",
			expected: "SELECT DATE_DIFF('day', CAST(start_date AS DATE), CAST(end_date AS DATE)) FROM events",
			wantErr:  false,
		},
		{
			name:     "DATEDIFFMonths",
			input:    "SELECT DATEDIFF(month, hire_date, CURRENT_DATE()) FROM employees",
			expected: "SELECT DATE_DIFF('month', CAST(hire_date AS DATE), CAST(CURRENT_DATE AS DATE)) FROM employees",
			wantErr:  false,
		},
		{
			name:     "DATEDIFFYears",
			input:    "SELECT DATEDIFF(year, birth_date, CURRENT_DATE()) FROM users",
			expected: "SELECT DATE_DIFF('year', CAST(birth_date AS DATE), CAST(CURRENT_DATE AS DATE)) FROM users",
			wantErr:  false,
		},
		{
			name:     "DATEDIFFInWHERE",
			input:    "SELECT * FROM orders WHERE DATEDIFF(day, order_date, ship_date) > 5",
			expected: "SELECT * FROM orders WHERE DATE_DIFF('day', CAST(order_date AS DATE), CAST(ship_date AS DATE)) > 5",
			wantErr:  false,
		},
		{
			name:     "MultipleDATEDIFF",
			input:    "SELECT DATEDIFF(day, a, b), DATEDIFF(month, c, d) FROM test",
			expected: "SELECT DATE_DIFF('day', CAST(a AS DATE), CAST(b AS DATE)), DATE_DIFF('month', CAST(c AS DATE), CAST(d AS DATE)) FROM test",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			translator := NewTranslator()
			result, err := translator.Translate(tt.input)

			if (err != nil) != tt.wantErr {
				t.Errorf("Translate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if diff := cmp.Diff(tt.expected, result); diff != "" {
					t.Errorf("Translate() mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

// TestTranslator_OBJECT_CONSTRUCT tests OBJECT_CONSTRUCT function translation.
// OBJECT_CONSTRUCT('key1', val1, ...) → json_object('key1', val1, ...) for DuckDB
func TestTranslator_OBJECT_CONSTRUCT(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  bool
	}{
		{
			name:     "SimpleOBJECT_CONSTRUCT",
			input:    "SELECT OBJECT_CONSTRUCT('key', value) FROM test",
			expected: "SELECT json_object('key', value) FROM test",
			wantErr:  false,
		},
		{
			name:     "OBJECT_CONSTRUCTMultipleKeys",
			input:    "SELECT OBJECT_CONSTRUCT('name', name, 'age', age) FROM users",
			expected: "SELECT json_object('name', name, 'age', age) FROM users",
			wantErr:  false,
		},
		{
			name:     "OBJECT_CONSTRUCTWithLiterals",
			input:    "SELECT OBJECT_CONSTRUCT('status', 'active', 'count', 42) FROM dual",
			expected: "SELECT json_object('status', 'active', 'count', 42)",
			wantErr:  false,
		},
		{
			name:     "MultipleOBJECT_CONSTRUCT",
			input:    "SELECT OBJECT_CONSTRUCT('a', 1), OBJECT_CONSTRUCT('b', 2) FROM test",
			expected: "SELECT json_object('a', 1), json_object('b', 2) FROM test",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			translator := NewTranslator()
			result, err := translator.Translate(tt.input)

			if (err != nil) != tt.wantErr {
				t.Errorf("Translate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if diff := cmp.Diff(tt.expected, result); diff != "" {
					t.Errorf("Translate() mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

// TestTranslator_FLATTEN tests FLATTEN function translation:
// FLATTEN(array) → UNNEST(array) for DuckDB.
func TestTranslator_FLATTEN(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  bool
	}{
		{
			name:     "SimpleFLATTEN",
			input:    "SELECT FLATTEN(array_col) FROM test",
			expected: "SELECT UNNEST(array_col) FROM test",
			wantErr:  false,
		},
		{
			// The parser rejects "=>", so this goes through the lexical path.
			// It used to come back untranslated, and then failed in DuckDB,
			// which has no FLATTEN.
			name:     "FLATTENWithNamedParam",
			input:    "SELECT * FROM TABLE(FLATTEN(input => array_col))",
			expected: "SELECT * FROM TABLE(UNNEST(input => array_col))",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			translator := NewTranslator()
			result, err := translator.Translate(tt.input)

			if (err != nil) != tt.wantErr {
				t.Errorf("Translate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if diff := cmp.Diff(tt.expected, result); diff != "" {
					t.Errorf("Translate() mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

// TestTranslator_LISTAGG tests LISTAGG function translation.
// LISTAGG(col, sep) → STRING_AGG(col, sep)
func TestTranslator_LISTAGG(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  bool
	}{
		{
			name:     "SimpleLISTAGG",
			input:    "SELECT LISTAGG(name, ',') FROM test GROUP BY category",
			expected: "SELECT STRING_AGG(name, ',') FROM test GROUP BY category",
			wantErr:  false,
		},
		{
			name:     "LISTAGGWithSpace",
			input:    "SELECT LISTAGG(name, ', ') FROM test GROUP BY category",
			expected: "SELECT STRING_AGG(name, ', ') FROM test GROUP BY category",
			wantErr:  false,
		},
		{
			name:     "MultipleLISTAGG",
			input:    "SELECT LISTAGG(a, '-'), LISTAGG(b, '|') FROM test GROUP BY c",
			expected: "SELECT STRING_AGG(a, '-'), STRING_AGG(b, '|') FROM test GROUP BY c",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			translator := NewTranslator()
			result, err := translator.Translate(tt.input)

			if (err != nil) != tt.wantErr {
				t.Errorf("Translate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if diff := cmp.Diff(tt.expected, result); diff != "" {
					t.Errorf("Translate() mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

// TestTranslator_CombinedFunctions tests combinations of multiple translated functions.
func TestTranslator_CombinedFunctions(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  bool
	}{
		{
			name:     "NVL2WithDATEADD",
			input:    "SELECT NVL2(end_date, DATEADD(day, 7, end_date), CURRENT_DATE()) FROM projects",
			expected: "SELECT IF(end_date IS NOT NULL, (CAST(end_date AS DATE) + interval (7) day), CURRENT_DATE) FROM projects",
			wantErr:  false,
		},
		{
			name:     "PARSE_JSONWithNVL",
			input:    "SELECT NVL(PARSE_JSON(json_col), PARSE_JSON('{}')) FROM test",
			expected: "SELECT COALESCE(CAST(json_col AS JSON), CAST('{}' AS JSON)) FROM test",
			wantErr:  false,
		},
		{
			name:     "IFFWithDATEDIFF",
			input:    "SELECT IFF(DATEDIFF(day, start_date, end_date) > 30, 'long', 'short') FROM events",
			expected: "SELECT IF(DATE_DIFF('day', CAST(start_date AS DATE), CAST(end_date AS DATE)) > 30, 'long', 'short') FROM events",
			wantErr:  false,
		},
		{
			name:     "OBJECT_CONSTRUCTWithNVL",
			input:    "SELECT OBJECT_CONSTRUCT('name', NVL(name, 'Unknown')) FROM users",
			expected: "SELECT json_object('name', COALESCE(name, 'Unknown')) FROM users",
			wantErr:  false,
		},
		{
			name:     "ComplexCombined",
			input:    "SELECT NVL2(data, TO_VARIANT(data), PARSE_JSON('null')) FROM test",
			expected: "SELECT IF(data IS NOT NULL, CAST(data AS JSON), CAST('null' AS JSON)) FROM test",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			translator := NewTranslator()
			result, err := translator.Translate(tt.input)

			if (err != nil) != tt.wantErr {
				t.Errorf("Translate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if diff := cmp.Diff(tt.expected, result); diff != "" {
					t.Errorf("Translate() mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

// TestTranslator_PassThrough tests that standard SQL passes through unchanged.
func TestTranslator_PassThrough(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  bool
	}{
		{
			name:     "SimpleSELECT",
			input:    "SELECT * FROM users",
			expected: "SELECT * FROM users",
			wantErr:  false,
		},
		{
			name:     "SELECTWithWHERE",
			input:    "SELECT id, name FROM users WHERE age > 18",
			expected: "SELECT id, name FROM users WHERE age > 18",
			wantErr:  false,
		},
		{
			name:     "JoinQuery",
			input:    "SELECT u.name, o.total FROM users u JOIN orders o ON u.id = o.user_id",
			expected: "SELECT u.name, o.total FROM users u JOIN orders o ON u.id = o.user_id",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			translator := NewTranslator()
			result, err := translator.Translate(tt.input)

			if (err != nil) != tt.wantErr {
				t.Errorf("Translate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if diff := cmp.Diff(tt.expected, result); diff != "" {
					t.Errorf("Translate() mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

// TestTranslator_ErrorCases tests error handling and edge cases.
func TestTranslator_ErrorCases(t *testing.T) {
	tests := []struct {
		name             string
		input            string
		wantErr          bool
		expectedContains string // For graceful degradation, check if result contains this
	}{
		{
			name:    "EmptyString",
			input:   "",
			wantErr: true,
		},
		{
			name:             "InvalidSQL_GracefulDegradation",
			input:            "SELECT FROM",
			wantErr:          false,
			expectedContains: "SELECT FROM", // Returns original
		},
		{
			// Malformed either way: DuckDB rejects it whether or not IFF was
			// translated. What matters is that translation no longer depends
			// on the statement being parseable.
			name:             "UnbalancedParentheses",
			input:            "SELECT IFF(age > 18, 'adult' FROM users",
			wantErr:          false,
			expectedContains: "SELECT IF(age > 18, 'adult' FROM users",
		},
		{
			name:             "CompletelyInvalidSQL",
			input:            "THIS IS NOT SQL AT ALL",
			wantErr:          false,
			expectedContains: "THIS IS NOT SQL AT ALL", // Returns original
		},
		{
			name:             "OnlyWhitespace",
			input:            "   \t\n   ",
			wantErr:          false,
			expectedContains: "", // Should return empty after trim
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			translator := NewTranslator()
			result, err := translator.Translate(tt.input)

			if (err != nil) != tt.wantErr {
				t.Errorf("Translate() error = %v, wantErr %v", err, tt.wantErr)
			}

			// For graceful degradation cases
			if !tt.wantErr && err == nil && tt.expectedContains != "" {
				if !strings.Contains(result, tt.expectedContains) {
					t.Errorf("Expected result to contain %q, got %q", tt.expectedContains, result)
				}
			}
		})
	}
}

// TestTranslator_EdgeCases tests edge cases and boundary conditions.
func TestTranslator_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  bool
	}{
		{
			name:     "CaseSensitivity_IFF",
			input:    "SELECT iff(col, 1, 0), IFF(col2, 2, 0), Iff(col3, 3, 0) FROM test",
			expected: "SELECT IF(col, 1, 0), IF(col2, 2, 0), IF(col3, 3, 0) FROM test",
			wantErr:  false,
		},
		{
			name:     "CaseSensitivity_NVL",
			input:    "SELECT nvl(col, 0), NVL(col2, 0), Nvl(col3, 0) FROM test",
			expected: "SELECT COALESCE(col, 0), COALESCE(col2, 0), COALESCE(col3, 0) FROM test",
			wantErr:  false,
		},
		{
			name:     "MixedFunctions",
			input:    "SELECT IFF(NVL(a, 0) > 5, 'high', 'low') FROM test",
			expected: "SELECT IF(COALESCE(a, 0) > 5, 'high', 'low') FROM test",
			wantErr:  false,
		},
		{
			name:     "FunctionInJOIN",
			input:    "SELECT * FROM t1 JOIN t2 ON IFF(t1.id IS NULL, 0, t1.id) = t2.id",
			expected: "SELECT * FROM t1 JOIN t2 ON IF(t1.id IS NULL, 0, t1.id) = t2.id",
			wantErr:  false,
		},
		{
			name:     "FunctionInGROUPBY",
			input:    "SELECT IFF(status, 'active', 'inactive'), COUNT(*) FROM users GROUP BY IFF(status, 'active', 'inactive')",
			expected: "SELECT IF(status, 'active', 'inactive'), COUNT(*) FROM users GROUP BY IF(status, 'active', 'inactive')",
			wantErr:  false,
		},
		{
			name:     "FunctionInHAVING",
			input:    "SELECT category, COUNT(*) FROM items GROUP BY category HAVING NVL(COUNT(*), 0) > 10",
			expected: "SELECT category, COUNT(*) FROM items GROUP BY category HAVING COALESCE(COUNT(*), 0) > 10",
			wantErr:  false,
		},
		{
			name:     "FunctionInORDERBY",
			input:    "SELECT * FROM users ORDER BY IFF(premium, 1, 2), NVL(name, 'ZZZ')",
			expected: "SELECT * FROM users ORDER BY IF(premium, 1, 2), COALESCE(name, 'ZZZ')", // Parser adds ASC
			wantErr:  false,
		},
		{
			// The parser cannot read a derived table, so IFF inside a subquery
			// used to survive untranslated all the way to DuckDB.
			name:     "SubqueryWithFunctions",
			input:    "SELECT * FROM (SELECT IFF(a, 1, 0) AS flag FROM test) WHERE flag = 1",
			expected: "SELECT * FROM (SELECT IF(a, 1, 0) AS flag FROM test) WHERE flag = 1",
			wantErr:  false,
		},
		{
			name:     "CURRENT_TIMESTAMP_MultipleOccurrences",
			input:    "SELECT CURRENT_TIMESTAMP(), CURRENT_TIMESTAMP(), CURRENT_DATE() FROM dual",
			expected: "SELECT CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_DATE",
			wantErr:  false,
		},
		{
			name:     "StringsWithFunctionNames",
			input:    "SELECT 'IFF', 'NVL', 'CURRENT_TIMESTAMP' FROM test",
			expected: "SELECT 'IFF', 'NVL', 'CURRENT_TIMESTAMP' FROM test",
			wantErr:  false,
		},
		{
			name:     "CommentsWithFunctions",
			input:    "SELECT /* IFF comment */ id, /* NVL comment */ name FROM test",
			expected: "SELECT /* IFF comment */ id, /* NVL comment */ name FROM test", // Parser preserves some comments
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			translator := NewTranslator()
			result, err := translator.Translate(tt.input)

			if (err != nil) != tt.wantErr {
				t.Errorf("Translate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				// Normalize whitespace for comparison
				normalizedExpected := normalizeWhitespace(tt.expected)
				normalizedResult := normalizeWhitespace(result)
				if diff := cmp.Diff(normalizedExpected, normalizedResult); diff != "" {
					t.Errorf("Translate() mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

// normalizeWhitespace removes extra whitespace and newlines for comparison.
func normalizeWhitespace(s string) string {
	// Simple normalization: replace multiple whitespace with single space
	result := ""
	prevSpace := false
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if !prevSpace {
				result += " "
				prevSpace = true
			}
		} else {
			result += string(r)
			prevSpace = false
		}
	}
	return result
}

// The translator used to parse the statement into a MySQL AST and serialize it
// back, which rewrote everything it touched on the way through. Scanning for
// the calls instead leaves the rest of the statement exactly as written, and
// these pin the differences that were visible to a reader.
func TestTranslator_LeavesTheStatementAsWritten(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "keywords keep the case they were written in",
			input:    "SELECT IFF(a, 1, 0) FROM t",
			expected: "SELECT IF(a, 1, 0) FROM t",
		},
		{
			name:     "lower-case SQL is left lower-case",
			input:    "select iff(a, 1, 0) from t",
			expected: "select IF(a, 1, 0) from t",
		},
		{
			name: "line breaks and indentation survive",
			input: `SELECT
	IFF(a, 1, 0)
FROM t`,
			expected: `SELECT
	IF(a, 1, 0)
FROM t`,
		},
		{
			name:     "a comment between the columns is kept",
			input:    "SELECT /* keep me */ id, /* and me */ name FROM test",
			expected: "SELECT /* keep me */ id, /* and me */ name FROM test",
		},
		{
			name:     "a double quote inside a string literal is not escaped",
			input:    `SELECT PARSE_JSON('{"key": "value"}')`,
			expected: `SELECT CAST('{"key": "value"}' AS JSON)`,
		},
		{
			name:     "an alias is not given an AS it was written without",
			input:    "SELECT IFF(a, 1, 0) FROM users u",
			expected: "SELECT IF(a, 1, 0) FROM users u",
		},
		{
			name:     "ORDER BY is not given an explicit ASC",
			input:    "SELECT * FROM users ORDER BY IFF(premium, 1, 2)",
			expected: "SELECT * FROM users ORDER BY IF(premium, 1, 2)",
		},
		{
			name:     "a Snowflake cast the MySQL grammar rejects is still translated",
			input:    "SELECT IFF(a, 1, 0)::TEXT FROM t",
			expected: "SELECT IF(a, 1, 0)::TEXT FROM t",
		},
		{
			name:     "concatenation is not read as boolean OR",
			input:    "SELECT NVL(a, '') || NVL(b, '') FROM t",
			expected: "SELECT COALESCE(a, '') || COALESCE(b, '') FROM t",
		},
		{
			name:     "empty parentheses come off CURRENT_TIMESTAMP",
			input:    "SELECT CURRENT_TIMESTAMP()",
			expected: "SELECT CURRENT_TIMESTAMP",
		},
		{
			name:     "CURRENT_DATE written without parentheses is left alone",
			input:    "SELECT CURRENT_DATE",
			expected: "SELECT CURRENT_DATE",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := NewTranslator().Translate(tt.input)
			if err != nil {
				t.Fatalf("Translate() error = %v", err)
			}
			if diff := cmp.Diff(tt.expected, result); diff != "" {
				t.Errorf("Translate() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestTranslator_CreateTableAsSelectTranslatesItsBody pins that CREATE TABLE
// ... AS <query> is not simply passed through the way a column-definition
// CREATE TABLE is. The AS <query> body is a full statement in its own right —
// exactly the query a plain SELECT would need translated — while the DDL
// preamble, and the table name within it, are left exactly as given here:
// qualification already happened earlier in the pipeline, before Translate
// ever sees the statement.
func TestTranslator_CreateTableAsSelectTranslatesItsBody(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "a simple rename in the body",
			input:    "CREATE TABLE t AS SELECT IFF(1 > 0, 'yes', 'no') AS a",
			expected: "CREATE TABLE t AS SELECT IF(1 > 0, 'yes', 'no') AS a",
		},
		{
			name:     "a marker-based rewrite in the body",
			input:    "CREATE TABLE t AS SELECT DATEADD(day, 30, CURRENT_DATE) AS a",
			expected: "CREATE TABLE t AS SELECT (CAST(CURRENT_DATE AS DATE) + interval (30) day) AS a",
		},
		{
			name: "several functions in one body",
			input: "CREATE TABLE t AS (\nSELECT\n    IFF(1 > 0, 'yes', 'no')          AS iff_translates,\n" +
				"    NVL(NULL, 'fallback')            AS nvl_translates,\n" +
				"    DATEADD(day, 30, CURRENT_DATE)   AS dateadd_translates\n)",
			expected: "CREATE TABLE t AS (\nSELECT\n    IF(1 > 0, 'yes', 'no')          AS iff_translates,\n" +
				"    COALESCE(NULL, 'fallback')            AS nvl_translates,\n" +
				"    (CAST(CURRENT_DATE AS DATE) + interval (30) day)   AS dateadd_translates\n)",
		},
		{
			name:     "CREATE TEMPORARY TABLE AS is recognized too",
			input:    "CREATE TEMPORARY TABLE t AS SELECT IFF(1 > 0, 'yes', 'no') AS a",
			expected: "CREATE TEMPORARY TABLE t AS SELECT IF(1 > 0, 'yes', 'no') AS a",
		},
		{
			name:     "CREATE OR REPLACE TABLE AS is recognized too",
			input:    "CREATE OR REPLACE TABLE t AS SELECT IFF(1 > 0, 'yes', 'no') AS a",
			expected: "CREATE OR REPLACE TABLE t AS SELECT IF(1 > 0, 'yes', 'no') AS a",
		},
		{
			name:     "CREATE TABLE IF NOT EXISTS AS is recognized too",
			input:    "CREATE TABLE IF NOT EXISTS t AS SELECT IFF(1 > 0, 'yes', 'no') AS a",
			expected: "CREATE TABLE IF NOT EXISTS t AS SELECT IF(1 > 0, 'yes', 'no') AS a",
		},
		{
			name:     "the table name is left as already qualified",
			input:    "CREATE TABLE TEST_DB.PUBLIC_T AS SELECT IFF(1 > 0, 'yes', 'no') AS a",
			expected: "CREATE TABLE TEST_DB.PUBLIC_T AS SELECT IF(1 > 0, 'yes', 'no') AS a",
		},
		{
			name:     "a WITH clause in the body translates too",
			input:    "CREATE TABLE t AS WITH counts AS (SELECT IFF(1 > 0, 1, 0) AS a) SELECT * FROM counts",
			expected: "CREATE TABLE t AS WITH counts AS (SELECT IF(1 > 0, 1, 0) AS a) SELECT * FROM counts",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			translator := NewTranslator()
			result, err := translator.Translate(tt.input)
			if err != nil {
				t.Fatalf("Translate() error = %v", err)
			}
			if diff := cmp.Diff(tt.expected, result); diff != "" {
				t.Errorf("Translate() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestTranslator_ColumnDefinitionCreateTableIsStillPassedThrough pins that the
// ordinary column-definition CREATE TABLE — with no AS <query> — keeps being
// passed through untouched, so a type name is never mistaken for a function
// call to translate.
func TestTranslator_ColumnDefinitionCreateTableIsStillPassedThrough(t *testing.T) {
	tests := []string{
		"CREATE TABLE t (id INT, amount DECIMAL(10,2))",
		"CREATE TEMPORARY TABLE t (id INT)",
		"CREATE TABLE IF NOT EXISTS t (id INT)",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			result, err := NewTranslator().Translate(input)
			if err != nil {
				t.Fatalf("Translate() error = %v", err)
			}
			if result != input {
				t.Errorf("Translate(%q) = %q, want it unchanged", input, result)
			}
		})
	}
}
