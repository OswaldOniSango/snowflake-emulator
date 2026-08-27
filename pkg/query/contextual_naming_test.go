package query

import "testing"

func TestRewriteContextualTableReferences(t *testing.T) {
	executionContext := ExecutionContext{Database: "LEARNING_DB", Schema: "PUBLIC"}
	tests := []struct {
		input string
		want  string
	}{
		{"CREATE TABLE users (id INTEGER)", "CREATE TABLE LEARNING_DB.PUBLIC_USERS (id INTEGER)"},
		{"INSERT INTO users VALUES (1)", "INSERT INTO LEARNING_DB.PUBLIC_USERS VALUES (1)"},
		{"SELECT * FROM users", "SELECT * FROM LEARNING_DB.PUBLIC_USERS"},
		{"SELECT * FROM users JOIN roles ON users.id = roles.id", "SELECT * FROM LEARNING_DB.PUBLIC_USERS JOIN LEARNING_DB.PUBLIC_ROLES ON users.id = roles.id"},
		{"SELECT * FROM OTHER_DB.PUBLIC_USERS", "SELECT * FROM OTHER_DB.PUBLIC_USERS"},
	}
	for _, tt := range tests {
		if got := rewriteContextualTableReferences(tt.input, executionContext); got != tt.want {
			t.Errorf("rewriteContextualTableReferences(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
