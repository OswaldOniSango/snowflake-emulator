package query

import (
	"context"
	"testing"
)

func TestParseUseRole(t *testing.T) {
	role, handled, err := ParseUseRole("-- session\nUSE ROLE developer;")
	if err != nil || !handled || role != "DEVELOPER" {
		t.Fatalf("got role=%q handled=%v err=%v", role, handled, err)
	}
	if _, handled, err := ParseUseRole("USE WAREHOUSE compute_wh"); err != nil || handled {
		t.Fatalf("non-role USE was handled: %v", err)
	}
	if _, handled, err := ParseUseRole("USE ROLE"); err == nil || !handled {
		t.Fatal("malformed USE ROLE should be handled with an error")
	}
	role, handled, err = ParseUseRole(`USE ROLE "Case Sensitive Role"`)
	if err != nil || !handled || role != "Case Sensitive Role" {
		t.Fatalf("quoted role was not parsed: role=%q handled=%v err=%v", role, handled, err)
	}
}

func TestCurrentIdentityFunctions(t *testing.T) {
	executor, _ := setupTestExecutor(t)
	result, err := executor.QueryWithContext(context.Background(), ExecutionContext{
		Role: "DEVELOPER", Principal: &PrincipalContext{UserID: "user-id", Username: "O'MALLEY", RoleID: "role-id"},
	}, "SELECT CURRENT_USER() AS current_user, CURRENT_ROLE() AS current_role, 'CURRENT_USER()' AS literal")
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Rows[0]; got[0] != "O'MALLEY" || got[1] != "DEVELOPER" || got[2] != "CURRENT_USER()" {
		t.Fatalf("unexpected row: %#v", got)
	}
}
