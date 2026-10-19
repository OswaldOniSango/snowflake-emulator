package query

import (
	"context"
	"strings"
	"testing"
)

func TestNumberDeclarations(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"CREATE TABLE t (amount NUMBER(10,2))", "CREATE TABLE t (amount DECIMAL(10,2))"},
		{"create transient table t (amount number (10))", "create table t (amount DECIMAL(10,0))"},
		{"CREATE TABLE t (NUMBER NUMBER, note VARCHAR DEFAULT 'NUMBER(10,2)')", "CREATE TABLE t (NUMBER DECIMAL(38,0), note VARCHAR DEFAULT 'NUMBER(10,2)')"},
		{"-- NUMBER\nCREATE TABLE t (\"NUMBER\" /* NUMBER */ NUMBER(38,37))", "-- NUMBER\nCREATE TABLE t (\"NUMBER\" /* NUMBER */ DECIMAL(38,37))"},
		{"SELECT 'NUMBER', NUMBER FROM t", "SELECT 'NUMBER', NUMBER FROM t"},
		{"CREATE TABLE t (x NUMBER /* note */ (10, /* scale */ 2))", "CREATE TABLE t (x DECIMAL(10,2) /* note */\n /* scale */\n)"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			got, err := NewTranslator().Translate(tc.input)
			if err != nil || !strings.EqualFold(got, tc.want) {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
	for _, decl := range []string{"NUMBER(0)", "NUMBER(39)", "NUMBER(10,11)", "NUMBER(38,38)", "NUMBER(10,-1)", "NUMBER(x)", "NUMBER(10,2,1)"} {
		if _, err := NewTranslator().Translate("CREATE TABLE t (v " + decl + ")"); err == nil {
			t.Errorf("accepted %s", decl)
		}
	}
}

func TestNumberExecution(t *testing.T) {
	e, _ := setupTestExecutor(t)
	ctx := context.Background()
	for i, decl := range []string{"NUMBER(10,2)", "NUMBER(10)", "NUMBER", "nUmBeR ( 10 , 2 )"} {
		table := []string{"n_decimal", "n_precision", "n_default", "n_mixed"}[i]
		if _, err := e.Execute(ctx, "CREATE TABLE "+table+" (amount "+decl+")"); err != nil {
			t.Fatal(err)
		}
		value := "123"
		if i == 0 || i == 3 {
			value = "123.45"
		}
		if _, err := e.Execute(ctx, "INSERT INTO "+table+" VALUES ("+value+")"); err != nil {
			t.Fatal(err)
		}
		r, err := e.Query(ctx, "SELECT CAST(amount AS VARCHAR) FROM "+table)
		if err != nil || len(r.Rows) != 1 || r.Rows[0][0] != value {
			t.Fatalf("%s: %+v %v", decl, r, err)
		}
	}
}
