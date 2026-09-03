package query

import "testing"

// TestTranslate_WithoutParser covers the statements the MySQL-based parser
// cannot read. Before the lexical fallback these came back untouched, and then
// failed in DuckDB with "Scalar Function with name IFF does not exist".
func TestTranslate_WithoutParser(t *testing.T) {
	translator := NewTranslator()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "IFF beside a cast",
			input: "SELECT IFF(1 > 0, 'a', 'b'), '2024-01-01'::DATE",
			want:  "SELECT IF(1 > 0, 'a', 'b'), '2024-01-01'::DATE",
		},
		{
			name:  "NVL beside a cast",
			input: "SELECT NVL(x, 'y'), z::VARCHAR FROM t",
			want:  "SELECT COALESCE(x, 'y'), z::VARCHAR FROM t",
		},
		{
			name:  "NVL2 beside a cast",
			input: "SELECT NVL2(a, b, c), d::INT FROM t",
			want:  "SELECT IF(a IS NOT NULL, b, c), d::INT FROM t",
		},
		{
			name:  "DATEADD beside a cast",
			input: "SELECT DATEADD('day', 7, d), e::DATE FROM t",
			want:  "SELECT (CAST(d AS DATE) + interval (7) day), e::DATE FROM t",
		},
		{
			name:  "PARSE_JSON beside a cast",
			input: "SELECT PARSE_JSON(s), n::INT FROM t",
			want:  "SELECT CAST(s AS JSON), n::INT FROM t",
		},
		{
			// The parser reads || as boolean OR, so the AST cannot be used.
			name:  "concatenation with a function",
			input: "SELECT a || b, IFF(x, 'y', 'z') FROM t",
			want:  "SELECT a || b, IF(x, 'y', 'z') FROM t",
		},
		{
			name:  "several functions in one statement",
			input: "SELECT IFF(a, b, c), NVL(d, e), f::INT FROM t",
			want:  "SELECT IF(a, b, c), COALESCE(d, e), f::INT FROM t",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := translator.Translate(tt.input)
			if err != nil {
				t.Fatalf("Translate() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}
		})
	}
}

// TestTranslateFunctionsLexically_LeavesNonCodeAlone pins what the scanner must
// never rewrite: a function's name appearing somewhere it is not a call.
func TestTranslateFunctionsLexically_LeavesNonCodeAlone(t *testing.T) {
	translator := NewTranslator()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "inside a string literal",
			input: "SELECT 'IFF(1,2,3)' FROM t",
			want:  "SELECT 'IFF(1,2,3)' FROM t",
		},
		{
			name:  "inside a doubled-quote escape",
			input: "SELECT 'it''s IFF(x)' FROM t",
			want:  "SELECT 'it''s IFF(x)' FROM t",
		},
		{
			name:  "inside a quoted identifier",
			input: `SELECT "IFF(x)" FROM t`,
			want:  `SELECT "IFF(x)" FROM t`,
		},
		{
			name:  "inside a line comment",
			input: "SELECT 1 -- IFF(a, b, c)\n",
			want:  "SELECT 1 -- IFF(a, b, c)\n",
		},
		{
			name:  "inside a block comment",
			input: "SELECT /* IFF(a, b, c) */ 1",
			want:  "SELECT /* IFF(a, b, c) */ 1",
		},
		{
			name:  "inside a dollar-quoted body",
			input: "SELECT $$ IFF(a, b, c) $$",
			want:  "SELECT $$ IFF(a, b, c) $$",
		},
		{
			name:  "qualified by a schema",
			input: "SELECT my_schema.iff(x)",
			want:  "SELECT my_schema.iff(x)",
		},
		{
			name:  "a column that shares the name",
			input: "SELECT iff FROM t",
			want:  "SELECT iff FROM t",
		},
		{
			name:  "a name that merely starts the same",
			input: "SELECT iffy(x) FROM t",
			want:  "SELECT iffy(x) FROM t",
		},
		{
			name:  "whitespace before the parenthesis is still a call",
			input: "SELECT IFF (a, b, c)",
			want:  "SELECT IF (a, b, c)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := translator.translateFunctionsLexically(tt.input); got != tt.want {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}
		})
	}
}

func TestSkipNonCodeHandlesUnterminatedRegions(t *testing.T) {
	translator := NewTranslator()

	// An unterminated region must consume the rest rather than loop or panic.
	for _, input := range []string{
		"SELECT 'unterminated",
		"SELECT $$ unterminated",
		"SELECT /* unterminated",
		`SELECT "unterminated`,
	} {
		t.Run(input, func(t *testing.T) {
			if got := translator.translateFunctionsLexically(input); got != input {
				t.Errorf("got %q, want it unchanged", got)
			}
		})
	}
}
