package query

import (
	"context"
	"strings"
	"sync"
	"testing"
)

func TestSessionVariables(t *testing.T) {
	e, _ := setupTestExecutor(t)
	ctx := context.Background()
	first := ExecutionContext{SessionID: "one", Variables: &sync.Map{}}
	second := ExecutionContext{SessionID: "two", Variables: &sync.Map{}}
	for _, sql := range []string{"SET siteid = 35;", "SET label = 'O''Brien';", "SET enabled = TRUE", "SET missing_value = NULL", "SET amount = 123.45"} {
		if _, err := e.ExecuteWithContext(ctx, first, sql); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	r, err := e.QueryWithContext(ctx, first, "SELECT $SITEID + 1, $label, $enabled, $missing_value, CAST($amount AS VARCHAR) WHERE $siteid = 35")
	if err != nil || len(r.Rows) != 1 || r.Rows[0][1] != "O'Brien" || r.Rows[0][2] != true || r.Rows[0][3] != nil || r.Rows[0][4] != "123.45" {
		t.Fatalf("result %+v, %v", r, err)
	}
	if _, err := e.QueryWithContext(ctx, second, "SELECT $siteid"); err == nil || !strings.Contains(err.Error(), "not defined") {
		t.Fatalf("cross-session value leaked: %v", err)
	}
	if _, err := e.ExecuteWithContext(ctx, first, "SET siteid = $siteid + 7"); err != nil {
		t.Fatal(err)
	}
	r, err = e.QueryWithContext(ctx, first, "SELECT CAST($siteid AS VARCHAR)")
	if err != nil || r.Rows[0][0] != "42" {
		t.Fatalf("reassignment: %+v %v", r, err)
	}
	for _, sql := range []string{"SET broken =", "SET (a,b) = (1,2)", "SET value = no_such_function()", "SET value = repeat('x',17000)"} {
		if _, err := e.ExecuteWithContext(ctx, first, sql); err == nil {
			t.Errorf("accepted %s", sql)
		}
	}
	if _, err := e.ExecuteWithContext(ctx, first, "UNSET siteid"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.QueryWithContext(ctx, first, "SELECT $siteid"); err == nil {
		t.Fatal("unset value survived")
	}
}

func TestVariableLexicalSafety(t *testing.T) {
	ec := ExecutionContext{SessionID: "lexical", Variables: &sync.Map{}}
	ec.Variables.Store("X", "35")
	sql := "SELECT '$x', \"$x\", $$ $x $$, $1, col$x, $x -- $undefined\n/* $undefined */"
	got, err := expandSessionVariables(sql, ec)
	want := "SELECT '$x', \"$x\", $$ $x $$, $1, col$x, (35) -- $undefined\n/* $undefined */"
	if err != nil || got != want {
		t.Fatalf("%s %v", got, err)
	}
}

func TestSessionVariableBindingsAndLiteralIntegrity(t *testing.T) {
	e, repo := setupTestExecutor(t)
	ctx := context.Background()
	db, err := repo.CreateDatabase(ctx, "VARIABLE_DB", "")
	if err != nil {
		t.Fatal(err)
	}
	ec := ExecutionContext{Database: db.Name, Schema: "PUBLIC", SessionID: "binding", Variables: &sync.Map{}}
	text := "FROM imaginary JOIN another, O'Brien"
	if _, err := e.ExecuteWithContext(ctx, ec, "SET label = "+sqlStringLiteral(text)); err != nil {
		t.Fatal(err)
	}
	r, err := e.QueryWithContext(ctx, ec, "SELECT $label")
	if err != nil || r.Rows[0][0] != text {
		t.Fatalf("literal changed: %+v %v", r, err)
	}
	if _, err := e.ExecuteWithContext(ctx, ec, "SET x = 35"); err != nil {
		t.Fatal(err)
	}
	r, err = e.QueryWithBindingsAndContext(ctx, ec, "SELECT CAST($x + :1 AS VARCHAR)", map[string]*QueryBindingValue{"1": {Type: "FIXED", Value: "7"}})
	if err != nil || r.Rows[0][0] != "42" {
		t.Fatalf("binding changed: %+v %v", r, err)
	}
}
