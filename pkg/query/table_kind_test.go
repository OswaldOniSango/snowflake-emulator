package query

import (
	"context"
	"testing"
)

func TestClassifier_IsCreateTableKinds(t *testing.T) {
	t.Parallel()

	tests := []string{
		"CREATE TABLE permanent_table (id INTEGER)",
		"CREATE TEMP TABLE temp_table (id INTEGER)",
		"CREATE TEMPORARY TABLE temporary_table (id INTEGER)",
		"CREATE TRANSIENT TABLE transient_table (id INTEGER)",
		"CREATE OR REPLACE TRANSIENT TABLE transient_table (id INTEGER)",
	}

	classifier := NewClassifier()
	for _, sql := range tests {
		if !classifier.IsCreateTable(sql) {
			t.Errorf("IsCreateTable(%q) = false, want true", sql)
		}
	}
}

func TestTranslator_CreateTableKinds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		sql  string
		want string
	}{
		{
			name: "temporary passes through",
			sql:  "CREATE TEMPORARY TABLE temporary_table (id INTEGER)",
			want: "CREATE TEMPORARY TABLE temporary_table (id INTEGER)",
		},
		{
			name: "transient becomes persistent DuckDB table",
			sql:  "CREATE TRANSIENT TABLE transient_table (id INTEGER)",
			want: "CREATE TABLE transient_table (id INTEGER)",
		},
		{
			name: "or replace transient is preserved",
			sql:  "CREATE OR REPLACE TRANSIENT TABLE transient_table (id INTEGER)",
			want: "CREATE OR REPLACE TABLE transient_table (id INTEGER)",
		},
	}

	translator := NewTranslator()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := translator.Translate(tt.sql)
			if err != nil {
				t.Fatalf("Translate() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("Translate() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExecutor_CreateTemporaryAndTransientTables(t *testing.T) {
	executor, _ := setupTestExecutor(t)
	ctx := context.Background()

	statements := []struct {
		create    string
		insert    string
		selectSQL string
	}{
		{
			create:    "CREATE TEMPORARY TABLE lesson_temp (id INTEGER, name VARCHAR)",
			insert:    "INSERT INTO lesson_temp VALUES (1, 'temporary')",
			selectSQL: "SELECT name FROM lesson_temp WHERE id = 1",
		},
		{
			create:    "CREATE TRANSIENT TABLE lesson_transient (id INTEGER, name VARCHAR)",
			insert:    "INSERT INTO lesson_transient VALUES (1, 'transient')",
			selectSQL: "SELECT name FROM lesson_transient WHERE id = 1",
		},
	}

	for _, statement := range statements {
		if _, err := executor.Execute(ctx, statement.create); err != nil {
			t.Fatalf("Execute(%q) error = %v", statement.create, err)
		}
		if _, err := executor.Execute(ctx, statement.insert); err != nil {
			t.Fatalf("Execute(%q) error = %v", statement.insert, err)
		}
		result, err := executor.Query(ctx, statement.selectSQL)
		if err != nil {
			t.Fatalf("Query(%q) error = %v", statement.selectSQL, err)
		}
		if len(result.Rows) != 1 {
			t.Fatalf("Query(%q) returned %d rows, want 1", statement.selectSQL, len(result.Rows))
		}
	}
}


func TestTranslator_CreateTransientTableWithLeadingComments(t *testing.T) {
	translator := NewTranslator()
	tests := []struct {
		name string
		sql  string
		want string
	}{
		{
			name: "line comments before transient table",
			sql: "-- ============================================================\n" +
				"-- TRANSIENT TABLE\n" +
				"-- ============================================================\n" +
				"\n" +
				"CREATE TRANSIENT TABLE release_audit_log (\n" +
				"    message VARCHAR\n" +
				");",
			want: "-- ============================================================\n" +
				"-- TRANSIENT TABLE\n" +
				"-- ============================================================\n" +
				"\n" +
				"CREATE TABLE release_audit_log (\n" +
				"    message VARCHAR\n" +
				");",
		},
		{
			name: "block comment before transient table",
			sql:  "/* setup */ CREATE TRANSIENT TABLE t (id INTEGER)",
			want: "/* setup */ CREATE TABLE t (id INTEGER)",
		},
		{
			name: "line comments before or-replace transient",
			sql:  "-- recreate\nCREATE OR REPLACE TRANSIENT TABLE t (id INTEGER)",
			want: "-- recreate\nCREATE OR REPLACE TABLE t (id INTEGER)",
		},
		{
			name: "ordinary create table with comments unchanged",
			sql:  "-- note\nCREATE TABLE t (id INTEGER)",
			want: "-- note\nCREATE TABLE t (id INTEGER)",
		},
		{
			name: "temp table with comments unchanged",
			sql:  "-- note\nCREATE TEMP TABLE t (id INTEGER)",
			want: "-- note\nCREATE TEMP TABLE t (id INTEGER)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := translator.Translate(tt.sql)
			if err != nil {
				t.Fatalf("Translate: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

func TestClassifier_IsCreateTransientTableWithLeadingComments(t *testing.T) {
	classifier := NewClassifier()
	cases := []string{
		"-- setup\nCREATE TRANSIENT TABLE t (id INTEGER)",
		"/* setup */ CREATE TRANSIENT TABLE t (id INTEGER)",
		"-- setup\nCREATE OR REPLACE TRANSIENT TABLE t (id INTEGER)",
		"-- setup\nCREATE TABLE t (id INTEGER)",
		"-- setup\nCREATE TEMP TABLE t (id INTEGER)",
		"-- setup\nCREATE TEMPORARY TABLE t (id INTEGER)",
	}
	for _, sql := range cases {
		if !classifier.IsCreateTable(sql) {
			t.Errorf("IsCreateTable did not recognise %q", sql)
		}
	}
}
