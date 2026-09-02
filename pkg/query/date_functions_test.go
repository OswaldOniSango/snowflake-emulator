package query

import "testing"

func TestUnquoteDatePart(t *testing.T) {
	tests := []struct {
		name string
		part string
		want string
	}{
		{name: "bare keyword", part: "day", want: "day"},
		{name: "single quoted", part: "'day'", want: "day"},
		{name: "double quoted", part: `"month"`, want: "month"},
		{name: "surrounding whitespace", part: "  'year' ", want: "year"},
		{name: "mismatched quotes are left alone", part: "'day\"", want: `'day"`},
		{name: "a quoted expression is not a date part", part: "'a b'", want: "'a b'"},
		{name: "an unquoted expression is left alone", part: "col", want: "col"},
		{name: "empty", part: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := unquoteDatePart(tt.part); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestTranslator_DateFunctionQuoting pins both spellings Snowflake accepts.
// The two DuckDB targets need opposite things — interval takes a bare keyword,
// DATE_DIFF takes a string — so each spelling used to work with exactly one of
// the two functions and fail with the other.
func TestTranslator_DateFunctionQuoting(t *testing.T) {
	translator := NewTranslator()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "DATEADD with a bare part",
			input: "SELECT DATEADD(day, 7, d)",
			want:  "select (CAST(d AS DATE) + interval (7) day)",
		},
		{
			name:  "DATEADD with a quoted part",
			input: "SELECT DATEADD('day', 7, d)",
			want:  "select (CAST(d AS DATE) + interval (7) day)",
		},
		{
			// "interval -1 year" is a DuckDB syntax error, so the count is
			// parenthesised. Subtracting an interval is the common case.
			name:  "DATEADD with a negative offset",
			input: "SELECT DATEADD(year, -1, d)",
			want:  "select (CAST(d AS DATE) + interval (-1) year)",
		},
		{
			name:  "DATEDIFF with a bare part",
			input: "SELECT DATEDIFF(day, a, b)",
			want:  "select DATE_DIFF('day', CAST(a AS DATE), CAST(b AS DATE))",
		},
		{
			name:  "DATEDIFF with a quoted part",
			input: "SELECT DATEDIFF('day', a, b)",
			want:  "select DATE_DIFF('day', CAST(a AS DATE), CAST(b AS DATE))",
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
