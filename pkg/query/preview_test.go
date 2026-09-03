package query

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

// TestResolveHandler pins the routing the preview reports. It has to mirror the
// executor's dispatch, or the console would attribute a statement to the wrong
// component and show a translation that never runs.
func TestResolveHandler(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want Handler
	}{
		{name: "select", sql: "SELECT 1", want: HandlerTranslator},
		{name: "insert", sql: "INSERT INTO t VALUES (1)", want: HandlerTranslator},
		{name: "create table", sql: "CREATE TABLE t (id INTEGER)", want: HandlerTranslator},
		{name: "merge", sql: "MERGE INTO t USING s ON t.id = s.id", want: HandlerMerge},
		{name: "copy", sql: "COPY INTO t FROM @stage", want: HandlerCopy},
		{name: "begin", sql: "BEGIN", want: HandlerTransaction},
		{name: "commit", sql: "COMMIT", want: HandlerTransaction},
		{name: "call", sql: "CALL p()", want: HandlerProcedure},
		{name: "create procedure", sql: "CREATE PROCEDURE p() RETURNS VARCHAR LANGUAGE SQL AS $$ BEGIN END $$", want: HandlerProcedure},
		{name: "drop procedure", sql: "DROP PROCEDURE p()", want: HandlerProcedure},
		{name: "show procedures", sql: "SHOW PROCEDURES", want: HandlerProcedure},
		{name: "create stream", sql: "CREATE STREAM s ON TABLE t", want: HandlerStream},
		{name: "drop stream", sql: "DROP STREAM s", want: HandlerStream},
		{name: "show streams", sql: "SHOW STREAMS", want: HandlerStream},
		{name: "create task", sql: "CREATE TASK j WAREHOUSE = w AS SELECT 1", want: HandlerTask},
		{name: "alter task", sql: "ALTER TASK j RESUME", want: HandlerTask},
		{name: "execute task", sql: "EXECUTE TASK j", want: HandlerTask},
		{name: "show tasks", sql: "SHOW TASKS", want: HandlerTask},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResolveHandler(tt.sql); got != tt.want {
				t.Errorf("ResolveHandler(%q) = %q, want %q", tt.sql, got, tt.want)
			}
		})
	}
}

func TestPreviewTranslation(t *testing.T) {
	withContext := ExecutionContext{Database: "TEST_DB", Schema: "PUBLIC"}

	tests := []struct {
		name             string
		sql              string
		executionContext ExecutionContext
		wantTranslated   string
		wantHandler      Handler
		wantComplete     bool
	}{
		{
			name:             "a function is translated",
			sql:              "SELECT IFF(a, 'y', 'n') FROM t",
			executionContext: ExecutionContext{},
			wantTranslated:   "select IF(a, 'y', 'n') from t",
			wantHandler:      HandlerTranslator,
			wantComplete:     true,
		},
		{
			// The rewriting of short names is the transformation people do not
			// expect, so the preview has to show it.
			name:             "short names resolve against the context",
			sql:              "SELECT * FROM users",
			executionContext: withContext,
			wantTranslated:   "select * from TEST_DB.PUBLIC_USERS",
			wantHandler:      HandlerTranslator,
			wantComplete:     true,
		},
		{
			name:             "without a context names are left alone",
			sql:              "SELECT * FROM users",
			executionContext: ExecutionContext{},
			wantTranslated:   "select * from users",
			wantHandler:      HandlerTranslator,
			wantComplete:     true,
		},
		{
			name:             "a processor-handled statement is flagged incomplete",
			sql:              "COPY INTO t FROM @stage",
			executionContext: withContext,
			wantHandler:      HandlerCopy,
			wantComplete:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := PreviewTranslation(tt.sql, tt.executionContext)
			if err != nil {
				t.Fatalf("PreviewTranslation() error = %v", err)
			}

			if got.Statement != tt.sql {
				t.Errorf("Statement = %q, want %q", got.Statement, tt.sql)
			}
			if tt.wantTranslated != "" && got.Translated != tt.wantTranslated {
				t.Errorf("Translated = %q, want %q", got.Translated, tt.wantTranslated)
			}
			if got.HandledBy != tt.wantHandler {
				t.Errorf("HandledBy = %q, want %q", got.HandledBy, tt.wantHandler)
			}
			if got.Complete != tt.wantComplete {
				t.Errorf("Complete = %v, want %v", got.Complete, tt.wantComplete)
			}
			if !got.Complete && got.Note == "" {
				t.Error("an incomplete preview must explain why")
			}
			if got.Complete && got.Note != "" {
				t.Errorf("a complete preview should carry no note, got %q", got.Note)
			}
		})
	}
}

func TestPreviewTranslationRejectsAnEmptyStatement(t *testing.T) {
	if _, err := PreviewTranslation("", ExecutionContext{}); err == nil {
		t.Error("expected an error for an empty statement")
	}
}

// TestPreviewTranslationDoesNotExecute guards the endpoint's core promise: the
// preview must be derivable from the statement alone, with no database.
func TestPreviewTranslationIsPure(t *testing.T) {
	sql := "INSERT INTO users VALUES (1)"

	first, err := PreviewTranslation(sql, ExecutionContext{Database: "TEST_DB", Schema: "PUBLIC"})
	if err != nil {
		t.Fatalf("PreviewTranslation() error = %v", err)
	}
	second, err := PreviewTranslation(sql, ExecutionContext{Database: "TEST_DB", Schema: "PUBLIC"})
	if err != nil {
		t.Fatalf("PreviewTranslation() error = %v", err)
	}

	if diff := cmp.Diff(first, second); diff != "" {
		t.Errorf("preview is not repeatable (-first +second):\n%s", diff)
	}
}

// TestFunctionRewrites pins what the console lists as substitutions. It scans
// with the same rules the lexical translation uses, so a name that is not a
// call — inside a literal, a comment, or qualified by a schema — is not a
// rewrite and must not be reported as one.
func TestFunctionRewrites(t *testing.T) {
	translator := NewTranslator()

	tests := []struct {
		name string
		sql  string
		want []Rewrite
	}{
		{
			name: "simple renames",
			sql:  "SELECT IFF(a, 'y', 'n'), NVL(b, 'x') FROM t",
			want: []Rewrite{{From: "IFF", To: "IF"}, {From: "NVL", To: "COALESCE"}},
		},
		{
			// A marker is an internal placeholder; a reader needs the shape it
			// becomes, not the placeholder.
			name: "a marked function reports its DuckDB form",
			sql:  "SELECT DATEADD(day, 1, d)",
			want: []Rewrite{{From: "DATEADD", To: "date + INTERVAL n part"}},
		},
		{
			name: "each function is reported once",
			sql:  "SELECT IFF(a, 1, 0), IFF(b, 1, 0)",
			want: []Rewrite{{From: "IFF", To: "IF"}},
		},
		{
			name: "reported in upper case regardless of how it was written",
			sql:  "SELECT iff(a, 1, 0)",
			want: []Rewrite{{From: "IFF", To: "IF"}},
		},
		{
			name: "a name inside a literal is not a rewrite",
			sql:  "SELECT 'IFF(a, 1, 0)' FROM t",
			want: []Rewrite{},
		},
		{
			name: "a name inside a comment is not a rewrite",
			sql:  "SELECT 1 -- IFF(a, 1, 0)\n",
			want: []Rewrite{},
		},
		{
			name: "a qualified call is somebody else's function",
			sql:  "SELECT my_schema.iff(a)",
			want: []Rewrite{},
		},
		{
			name: "a column that shares the name is not a call",
			sql:  "SELECT iff FROM t",
			want: []Rewrite{},
		},
		{
			name: "a statement with nothing to translate",
			sql:  "SELECT 1",
			want: []Rewrite{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if diff := cmp.Diff(tt.want, translator.FunctionRewrites(tt.sql)); diff != "" {
				t.Errorf("rewrites mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestObjectRewrites(t *testing.T) {
	executionContext := ExecutionContext{Database: "TEST_DB", Schema: "PUBLIC"}

	tests := []struct {
		name      string
		original  string
		rewritten string
		want      []Rewrite
	}{
		{
			name:      "one table",
			original:  "SELECT * FROM users",
			rewritten: "SELECT * FROM TEST_DB.PUBLIC_USERS",
			want:      []Rewrite{{From: "USERS", To: "TEST_DB.PUBLIC_USERS"}},
		},
		{
			name:      "two tables",
			original:  "SELECT * FROM users JOIN orders ON true",
			rewritten: "SELECT * FROM TEST_DB.PUBLIC_USERS JOIN TEST_DB.PUBLIC_ORDERS ON true",
			want: []Rewrite{
				{From: "USERS", To: "TEST_DB.PUBLIC_USERS"},
				{From: "ORDERS", To: "TEST_DB.PUBLIC_ORDERS"},
			},
		},
		{
			name:      "the same table twice is reported once",
			original:  "SELECT * FROM users u JOIN users v ON true",
			rewritten: "SELECT * FROM TEST_DB.PUBLIC_USERS u JOIN TEST_DB.PUBLIC_USERS v ON true",
			want:      []Rewrite{{From: "USERS", To: "TEST_DB.PUBLIC_USERS"}},
		},
		{
			name:      "nothing was rewritten",
			original:  "SELECT 1",
			rewritten: "SELECT 1",
			want:      []Rewrite{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := objectRewrites(tt.original, tt.rewritten, executionContext)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("rewrites mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
