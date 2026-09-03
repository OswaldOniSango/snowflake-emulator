package query

import "testing"

func TestTrimLeadingComments(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want string
	}{
		{name: "plain statement", sql: "SELECT 1", want: "SELECT 1"},
		{name: "leading whitespace", sql: "  \n\t SELECT 1", want: "SELECT 1"},
		{name: "line comment", sql: "-- note\nSELECT 1", want: "SELECT 1"},
		{name: "block comment", sql: "/* note */ SELECT 1", want: "SELECT 1"},
		{name: "nested block comment", sql: "/* a /* b */ c */ SELECT 1", want: "SELECT 1"},
		{name: "several comments", sql: "-- one\n /* two */\n-- three\nSELECT 1", want: "SELECT 1"},
		{name: "comment inside the statement is kept", sql: "SELECT 1 -- trailing", want: "SELECT 1 -- trailing"},
		{name: "line comment with no newline leaves nothing", sql: "-- only a comment", want: ""},
		{name: "unterminated block comment leaves nothing", sql: "/* never closed", want: ""},
		{name: "empty input", sql: "", want: ""},
		{name: "a division is not a comment", sql: "SELECT 4/2", want: "SELECT 4/2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := trimLeadingComments(tt.sql); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestClassifyIgnoresLeadingComments pins the bug this normalisation fixes: a
// commented SELECT was classified as DML, ran down the exec path, and had its
// result set silently discarded.
func TestClassifyIgnoresLeadingComments(t *testing.T) {
	classifier := NewClassifier()

	tests := []struct {
		name        string
		sql         string
		wantQuery   bool
		wantDDL     bool
		wantTypeSet StatementType
	}{
		{name: "select", sql: "SELECT 1", wantQuery: true, wantTypeSet: StatementTypeQuery},
		{
			name:        "select behind a line comment",
			sql:         "-- what this does\nSELECT 1",
			wantQuery:   true,
			wantTypeSet: StatementTypeQuery,
		},
		{
			name:        "select behind a block comment",
			sql:         "/* what this does */ SELECT 1",
			wantQuery:   true,
			wantTypeSet: StatementTypeQuery,
		},
		{
			name:        "create behind a comment is still DDL",
			sql:         "-- setup\nCREATE TABLE t (id INTEGER)",
			wantDDL:     true,
			wantTypeSet: StatementTypeDDLCreate,
		},
		{
			name:        "merge behind a comment is still a merge",
			sql:         "/* upsert */ MERGE INTO t USING s ON t.id = s.id",
			wantTypeSet: StatementTypeMerge,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifier.Classify(tt.sql)

			if got.IsQuery != tt.wantQuery {
				t.Errorf("IsQuery = %v, want %v", got.IsQuery, tt.wantQuery)
			}
			if got.IsDDL != tt.wantDDL {
				t.Errorf("IsDDL = %v, want %v", got.IsDDL, tt.wantDDL)
			}
			if got.Type != tt.wantTypeSet {
				t.Errorf("Type = %v, want %v", got.Type, tt.wantTypeSet)
			}
		})
	}
}

// TestObjectHelpersIgnoreLeadingComments covers the narrower helpers, which
// share the same prefix matching.
func TestObjectHelpersIgnoreLeadingComments(t *testing.T) {
	classifier := NewClassifier()

	tests := []struct {
		name  string
		sql   string
		check func(string) bool
	}{
		{name: "IsCall", sql: "-- run it\nCALL p()", check: classifier.IsCall},
		{name: "IsShowProcedures", sql: "/* list */ SHOW PROCEDURES", check: classifier.IsShowProcedures},
		{name: "IsShowStreams", sql: "-- list\nSHOW STREAMS", check: classifier.IsShowStreams},
		{name: "IsCreateStream", sql: "-- new\nCREATE STREAM s ON TABLE t", check: classifier.IsCreateStream},
		{name: "IsCreateTask", sql: "/* new */ CREATE TASK job", check: classifier.IsCreateTask},
		{name: "IsDropStream", sql: "-- bye\nDROP STREAM s", check: classifier.IsDropStream},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.check(tt.sql) {
				t.Errorf("%s did not recognise %q", tt.name, tt.sql)
			}
		})
	}
}
