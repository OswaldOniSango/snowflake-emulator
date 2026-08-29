package query

import "testing"

func TestTranslator_PreservesSnowflakeConcatenation(t *testing.T) {
	translator := NewTranslator()
	input := "SELECT 'Hello, ' || 'Snowflake'"

	result, err := translator.Translate(input)
	if err != nil {
		t.Fatalf("Translate() error = %v", err)
	}
	if result != input {
		t.Fatalf("Translate() = %q, want %q", result, input)
	}
}
