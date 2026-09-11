package query

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nnnkkk7/snowflake-emulator/pkg/identity"
	"github.com/nnnkkk7/snowflake-emulator/pkg/metadata"
)

func setupIdentityExecutor(t *testing.T) (*Executor, *identity.Service) {
	t.Helper()
	executor, repo := setupTestExecutor(t)
	service, err := identity.NewService(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	executor.Configure(WithIdentityService(service))
	return executor, service
}

func TestParseIdentityOptions(t *testing.T) {
	for _, test := range []struct {
		name, input     string
		wantRole        string
		wantPassword    string
		wantComment     string
		wantDisabled    bool
		wantErrContains string
	}{
		{name: "all options", input: `PASSWORD='s''ecret' DEFAULT_ROLE=developer DISABLED=TRUE COMMENT='student'`, wantPassword: "s'ecret", wantRole: "DEVELOPER", wantDisabled: true, wantComment: "student"},
		{name: "comma separated", input: `PASSWORD = 'secret', DEFAULT_ROLE = "Study Role", DISABLED = FALSE`, wantPassword: "secret", wantRole: "Study Role"},
		{name: "invalid boolean", input: `DISABLED=MAYBE`, wantErrContains: "TRUE or FALSE"},
		{name: "unknown option", input: `EMAIL='x'`, wantErrContains: "unsupported"},
		{name: "password must be quoted", input: `PASSWORD=secret`, wantErrContains: "string literal"},
	} {
		t.Run(test.name, func(t *testing.T) {
			options, err := parseIdentityOptions(test.input)
			if test.wantErrContains != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErrContains) {
					t.Fatalf("got %v, want error containing %q", err, test.wantErrContains)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if options.password != test.wantPassword || options.defaultRole != test.wantRole || options.comment != test.wantComment || options.disabled != test.wantDisabled {
				t.Fatalf("unexpected options: %+v", options)
			}
		})
	}
}

func TestRedactSensitiveSQL(t *testing.T) {
	redacted := RedactSensitiveSQL("CREATE USER alice PASSWORD = 's''ecret' COMMENT='safe'")
	if strings.Contains(redacted, "ecret") || !strings.Contains(redacted, "PASSWORD = '********'") {
		t.Fatalf("password was not redacted: %s", redacted)
	}
}

func TestIdentitySQLLifecycleAndShows(t *testing.T) {
	executor, service := setupIdentityExecutor(t)
	ctx := context.Background()
	execute := func(sql string) {
		t.Helper()
		if _, err := executor.ExecuteWithContext(ctx, ExecutionContext{}, sql); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	query := func(sql string) *Result {
		t.Helper()
		result, err := executor.QueryWithContext(ctx, ExecutionContext{}, sql)
		if err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		return result
	}

	execute("CREATE ROLE IF NOT EXISTS developer COMMENT='Build role'")
	execute("CREATE ROLE IF NOT EXISTS developer")
	execute("CREATE ROLE reader")
	execute("CREATE USER alice PASSWORD='first' DEFAULT_ROLE=developer DISABLED=FALSE COMMENT='Learner'")
	execute("GRANT ROLE reader TO ROLE developer")
	execute("GRANT ROLE reader TO ROLE developer")
	execute("GRANT ROLE reader TO USER alice")

	users := query("SHOW USERS")
	if row := findIdentityRow(users, "ALICE"); row == nil || row[1] != false || row[2] != "DEVELOPER" || row[3] != "Learner" {
		t.Fatalf("unexpected SHOW USERS row: %#v", row)
	}
	if len(query("SHOW ROLES").Rows) < 7 {
		t.Fatal("created roles are absent from SHOW ROLES")
	}
	toUser := query("SHOW GRANTS TO USER alice")
	if findIdentityRow(toUser, "READER") == nil || findIdentityRow(toUser, identity.RolePublic) == nil {
		t.Fatalf("direct and implicit grants missing: %#v", toUser.Rows)
	}
	toRole := query("SHOW GRANTS TO ROLE developer")
	if row := findIdentityRow(toRole, "READER"); row == nil || row[1] != "ROLE" || row[2] != "DEVELOPER" {
		t.Fatalf("unexpected grants to role: %#v", toRole.Rows)
	}
	ofRole := query("SHOW GRANTS OF ROLE reader")
	if findGrantRecipient(ofRole, "ROLE", "DEVELOPER") == nil || findGrantRecipient(ofRole, "USER", "ALICE") == nil {
		t.Fatalf("unexpected grants of role: %#v", ofRole.Rows)
	}

	execute("ALTER USER alice SET PASSWORD='second', DISABLED=TRUE, COMMENT='Paused'")
	if _, err := service.Authenticate(ctx, "alice", "second"); !errors.Is(err, identity.ErrUserDisabled) {
		t.Fatalf("disabled user authenticated: %v", err)
	}
	execute("ALTER USER alice SET DISABLED=FALSE")
	if _, err := service.Authenticate(ctx, "alice", "first"); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Fatalf("old password still works: %v", err)
	}
	if _, err := service.Authenticate(ctx, "alice", "second"); err != nil {
		t.Fatalf("new password does not work: %v", err)
	}

	execute("REVOKE ROLE reader FROM USER alice")
	execute("REVOKE ROLE reader FROM USER alice")
	execute("REVOKE ROLE reader FROM ROLE developer")
	execute("REVOKE ROLE reader FROM ROLE developer")
	execute("DROP USER alice")
	execute("DROP USER IF EXISTS alice")
	execute("DROP ROLE reader")
	execute("DROP ROLE developer")
	execute("DROP ROLE IF EXISTS missing_role")
}

func TestCreateUserIfNotExists(t *testing.T) {
	executor, _ := setupIdentityExecutor(t)
	ctx := context.Background()
	if _, err := executor.Execute(ctx, "CREATE USER IF NOT EXISTS alice PASSWORD='first'"); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.Execute(ctx, "CREATE USER IF NOT EXISTS alice PASSWORD='ignored'"); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.Execute(ctx, "CREATE USER alice PASSWORD='duplicate'"); err == nil {
		t.Fatal("duplicate CREATE USER without IF NOT EXISTS should fail")
	}
}

func TestShowGrantsFlexibleWhitespaceAndDirection(t *testing.T) {
	executor, _ := setupIdentityExecutor(t)
	ctx := context.Background()
	for _, sql := range []string{
		"CREATE ROLE developer", "CREATE ROLE reader",
		"CREATE USER alice PASSWORD='secret' DEFAULT_ROLE=developer",
		"GRANT ROLE reader TO ROLE developer", "GRANT ROLE reader TO USER alice",
	} {
		if _, err := executor.Execute(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	for _, test := range []struct {
		name, sql, targetType, grantee string
	}{
		{name: "to user", sql: " show\tgrants\n to\tuser alice ; ", targetType: "USER", grantee: "ALICE"},
		{name: "to role", sql: "SHOW  GRANTS\tTO\nROLE developer;", targetType: "ROLE", grantee: "DEVELOPER"},
		{name: "of role", sql: "ShOw\nGrAnTs\tOf  RoLe reader ;", targetType: "ROLE", grantee: "DEVELOPER"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := executor.Query(ctx, test.sql)
			if err != nil {
				t.Fatal(err)
			}
			if findGrantRecipient(result, test.targetType, test.grantee) == nil {
				t.Fatalf("wrong directionality: %#v", result.Rows)
			}
		})
	}
}

func TestIdentityPasswordsAreRedactedFromHistories(t *testing.T) {
	executor, repo := setupTestExecutor(t)
	service, err := identity.NewService(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	executor.Configure(WithIdentityService(service))
	ctx := context.Background()
	const secret = "literal-secret-value"
	if _, err := executor.ExecuteWithHistory(ctx, "session", "create-user", "CREATE USER alice PASSWORD='"+secret+"'"); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.ExecuteWithHistory(ctx, "session", "alter-user", "ALTER USER alice SET PASSWORD='"+secret+"-two'"); err != nil {
		t.Fatal(err)
	}
	history, err := repo.GetQueryHistory(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range history {
		if strings.Contains(entry.SQLText, secret) {
			t.Fatalf("secret leaked into query history: %s", entry.SQLText)
		}
	}
	manager := NewStatementManager(time.Hour)
	statement := manager.CreateStatement("ALTER USER alice SET PASSWORD='"+secret+"'", "", "", "")
	if strings.Contains(statement.SQLText, secret) || !strings.Contains(statement.SQLText, "********") {
		t.Fatalf("secret leaked into statement manager: %s", statement.SQLText)
	}
}

func TestIdentitySQLRejectsInvalidHierarchyAndProtectedRoles(t *testing.T) {
	executor, service := setupIdentityExecutor(t)
	ctx := context.Background()
	for _, sql := range []string{"CREATE ROLE first", "CREATE ROLE second"} {
		if _, err := executor.Execute(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := executor.Execute(ctx, "GRANT ROLE first TO ROLE first"); !errors.Is(err, identity.ErrRoleCycle) {
		t.Fatalf("self grant: %v", err)
	}
	if _, err := executor.Execute(ctx, "GRANT ROLE first TO ROLE second"); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.Execute(ctx, "GRANT ROLE second TO ROLE first"); !errors.Is(err, identity.ErrRoleCycle) {
		t.Fatalf("cycle: %v", err)
	}
	for _, sql := range []string{
		"DROP ROLE PUBLIC",
		"GRANT ROLE PUBLIC TO USER ADMIN",
		"REVOKE ROLE PUBLIC FROM USER ADMIN",
		"ALTER USER ADMIN SET DEFAULT_ROLE=missing_role",
	} {
		if _, err := executor.Execute(ctx, sql); err == nil {
			t.Fatalf("expected %s to fail", sql)
		}
	}
	before, err := service.EffectiveRoles(ctx, identity.DemoAdminUser)
	if err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{"GRANT ROLE SYSADMIN TO ROLE first", "GRANT ROLE first TO ROLE SYSADMIN"} {
		if _, err := executor.Execute(ctx, sql); !errors.Is(err, identity.ErrSystemRole) {
			t.Fatalf("expected protected hierarchy error for %s, got %v", sql, err)
		}
	}
	after, err := service.EffectiveRoles(ctx, identity.DemoAdminUser)
	if err != nil {
		t.Fatal(err)
	}
	if roleNames(before) != roleNames(after) {
		t.Fatalf("rejected grants changed bootstrap hierarchy: before=%v after=%v", before, after)
	}
}

func TestUserMultiOptionStatementsAreAtomic(t *testing.T) {
	executor, service := setupIdentityExecutor(t)
	ctx := context.Background()
	if _, err := executor.Execute(ctx, "CREATE ROLE developer"); err != nil {
		t.Fatal(err)
	}
	reader, err := service.CreateRole(ctx, "reader", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := executor.Execute(ctx, "CREATE USER alice PASSWORD='original' DEFAULT_ROLE=developer COMMENT='before'"); err != nil {
		t.Fatal(err)
	}
	_, err = executor.Execute(ctx, "ALTER USER alice SET PASSWORD='changed', DISABLED=TRUE, COMMENT='after', DEFAULT_ROLE=missing_role")
	if err == nil {
		t.Fatal("ALTER with missing default role should fail")
	}
	if _, err := service.Authenticate(ctx, "alice", "original"); err != nil {
		t.Fatalf("original password changed after failed ALTER: %v", err)
	}
	if _, err := service.Authenticate(ctx, "alice", "changed"); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Fatalf("new password survived failed ALTER: %v", err)
	}
	users, err := service.ListUsers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var alice *identity.User
	for i := range users {
		if users[i].Name == "ALICE" {
			alice = &users[i]
		}
	}
	if alice == nil || alice.Disabled || alice.Comment != "before" {
		t.Fatalf("failed ALTER partially changed user: %+v", alice)
	}

	_, err = executor.Execute(ctx, "CREATE USER broken PASSWORD='secret' DISABLED=TRUE COMMENT='partial' DEFAULT_ROLE=missing_role")
	if err == nil {
		t.Fatal("CREATE with missing default role should fail")
	}
	users, err = service.ListUsers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, user := range users {
		if user.Name == "BROKEN" {
			t.Fatalf("failed CREATE exposed partial user: %+v", user)
		}
	}

	if _, err := executor.Execute(ctx, "ALTER USER alice SET PASSWORD='successful', DEFAULT_ROLE=reader, DISABLED=TRUE, COMMENT='together'"); err != nil {
		t.Fatal(err)
	}
	users, err = service.ListUsers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	alice = nil
	for i := range users {
		if users[i].Name == "ALICE" {
			alice = &users[i]
		}
	}
	if alice == nil || !alice.Disabled || alice.Comment != "together" || alice.DefaultRoleID != reader.ID {
		t.Fatalf("successful ALTER did not update every field together: %+v", alice)
	}
	if _, err := service.Authenticate(ctx, "alice", "successful"); !errors.Is(err, identity.ErrUserDisabled) {
		t.Fatalf("updated password/disabled state mismatch: %v", err)
	}
	if _, err := executor.Execute(ctx, "ALTER USER alice SET DISABLED=FALSE"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Authenticate(ctx, "alice", "successful"); err != nil {
		t.Fatalf("updated password failed after re-enable: %v", err)
	}
}

func roleNames(roles []metadata.RoleRecord) string {
	names := make([]string, len(roles))
	for i := range roles {
		names[i] = roles[i].Name
	}
	return strings.Join(names, ",")
}

func findIdentityRow(result *Result, name string) []interface{} {
	for _, row := range result.Rows {
		if row[0] == name {
			return row
		}
	}
	return nil
}

func findGrantRecipient(result *Result, targetType, name string) []interface{} {
	for _, row := range result.Rows {
		if row[1] == targetType && row[2] == name {
			return row
		}
	}
	return nil
}
