package identity

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/nnnkkk7/snowflake-emulator/pkg/connection"
	"github.com/nnnkkk7/snowflake-emulator/pkg/metadata"
	"golang.org/x/crypto/bcrypt"
)

func testService(t *testing.T) (*Service, *metadata.Repository, *sql.DB) {
	t.Helper()
	db, err := sql.Open("duckdb", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	repo, err := metadata.NewRepository(connection.NewManager(db))
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	return service, repo, db
}

func TestBootstrapCreatesSystemHierarchyAndHashedAdmin(t *testing.T) {
	service, repo, db := testService(t)
	ctx := context.Background()

	roles, err := service.ListRoles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(roles) != 5 {
		t.Fatalf("roles = %d, want 5", len(roles))
	}
	for _, role := range roles {
		if !role.SystemRole {
			t.Errorf("role %s is not marked system", role.Name)
		}
	}

	users, err := service.ListUsers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || users[0].Name != DemoAdminUser {
		t.Fatalf("users = %+v", users)
	}
	adminRecord, err := repo.GetUserRecordByName(ctx, DemoAdminUser)
	if err != nil {
		t.Fatal(err)
	}
	if adminRecord.PasswordHash == DemoAdminPassword {
		t.Fatal("demo password stored as plaintext")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(adminRecord.PasswordHash), []byte(DemoAdminPassword)); err != nil {
		t.Fatalf("stored password is not a valid bcrypt hash: %v", err)
	}
	var plaintextCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM _metadata_users WHERE password_hash = 'admin'`).Scan(&plaintextCount); err != nil {
		t.Fatal(err)
	}
	if plaintextCount != 0 {
		t.Fatal("plaintext demo password found in catalog")
	}

	effective, err := service.EffectiveRoles(ctx, DemoAdminUser)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, role := range effective {
		names = append(names, role.Name)
	}
	for _, expected := range []string{RoleAccountAdmin, RoleSecurityAdmin, RoleUserAdmin, RoleSysAdmin, RolePublic} {
		if !slices.Contains(names, expected) {
			t.Errorf("effective roles %v missing %s", names, expected)
		}
	}

	beforeID := users[0].ID
	if err := service.SetPassword(ctx, DemoAdminUser, "changed"); err != nil {
		t.Fatal(err)
	}
	if _, err := NewService(ctx, repo); err != nil {
		t.Fatal(err)
	}
	after, err := repo.GetUserRecordByName(ctx, DemoAdminUser)
	if err != nil {
		t.Fatal(err)
	}
	if after.ID != beforeID {
		t.Errorf("admin ID changed from %s to %s", beforeID, after.ID)
	}
	if bcrypt.CompareHashAndPassword([]byte(after.PasswordHash), []byte("changed")) != nil {
		t.Fatal("restart reset changed password")
	}
}

func TestBootstrapRepairsPartialSystemCatalog(t *testing.T) {
	db, err := sql.Open("duckdb", "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	repo, err := metadata.NewRepository(connection.NewManager(db))
	if err != nil {
		t.Fatal(err)
	}
	partial, err := repo.CreateRoleRecord(context.Background(), RoleAccountAdmin, "wrong", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateUserRecord(context.Background(), metadata.UserRecord{Name: "EXISTING", PasswordHash: "already-hashed"}); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	repaired, err := repo.GetRoleRecordByName(context.Background(), RoleAccountAdmin)
	if err != nil {
		t.Fatal(err)
	}
	if repaired.ID != partial.ID {
		t.Fatalf("bootstrap changed role ID from %s to %s", partial.ID, repaired.ID)
	}
	if !repaired.SystemRole || repaired.Comment != "System account administrator" {
		t.Fatalf("role not repaired: %+v", repaired)
	}
	users, err := service.ListUsers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || users[0].Name != "EXISTING" {
		t.Fatalf("bootstrap unexpectedly created ADMIN: %+v", users)
	}
	grants, err := repo.ListRoleGrantRecords(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(grants) != 4 {
		t.Fatalf("system hierarchy has %d edges, want 4", len(grants))
	}
}

func TestBootstrapDoesNotCreateAdminWhenAnyUserExists(t *testing.T) {
	db, err := sql.Open("duckdb", "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	repo, err := metadata.NewRepository(connection.NewManager(db))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateUserRecord(context.Background(), metadata.UserRecord{Name: "EXISTING", PasswordHash: "already-hashed"}); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	users, err := service.ListUsers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || users[0].Name != "EXISTING" {
		t.Fatalf("users = %+v", users)
	}
}

func TestAuthenticateDoesNotExposeCredentialFailure(t *testing.T) {
	service, _, _ := testService(t)
	ctx := context.Background()
	principal, err := service.Authenticate(ctx, "admin", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if principal.Username != DemoAdminUser || principal.DefaultRole != RoleAccountAdmin {
		t.Fatalf("principal = %+v", principal)
	}

	for _, test := range []struct{ username, password string }{{"missing", "admin"}, {"admin", "wrong"}} {
		_, err := service.Authenticate(ctx, test.username, test.password)
		if !errors.Is(err, ErrInvalidCredentials) {
			t.Errorf("Authenticate(%q) error = %v", test.username, err)
		}
	}
	if err := service.SetUserDisabled(ctx, DemoAdminUser, true); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Authenticate(ctx, DemoAdminUser, DemoAdminPassword); !errors.Is(err, ErrUserDisabled) {
		t.Fatalf("disabled error = %v", err)
	}
}

func TestRoleGrantsAreIdempotentTransitiveAndRejectCycles(t *testing.T) {
	service, repo, _ := testService(t)
	ctx := context.Background()
	for _, name := range []string{"READER", "DEVELOPER", "LEAD"} {
		if _, err := service.CreateRole(ctx, name, ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := service.GrantRoleToRole(ctx, "READER", "DEVELOPER"); err != nil {
		t.Fatal(err)
	}
	if err := service.GrantRoleToRole(ctx, "READER", "DEVELOPER"); err != nil {
		t.Fatalf("duplicate grant: %v", err)
	}
	if err := service.GrantRoleToRole(ctx, "DEVELOPER", "LEAD"); err != nil {
		t.Fatal(err)
	}
	if err := service.GrantRoleToRole(ctx, "LEAD", "READER"); !errors.Is(err, ErrRoleCycle) {
		t.Fatalf("cycle error = %v", err)
	}
	if err := service.GrantRoleToRole(ctx, "READER", "READER"); !errors.Is(err, ErrRoleCycle) {
		t.Fatalf("self grant error = %v", err)
	}

	lead, err := repo.GetRoleRecordByName(ctx, "LEAD")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateUser(ctx, "alice", "secret", lead.Name, ""); err != nil {
		t.Fatal(err)
	}
	effective, err := service.EffectiveRoles(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, role := range effective {
		names = append(names, role.Name)
	}
	for _, expected := range []string{"LEAD", "DEVELOPER", "READER", RolePublic} {
		if !slices.Contains(names, expected) {
			t.Errorf("roles %v missing %s", names, expected)
		}
	}
}

func TestRoleDeletionRules(t *testing.T) {
	service, _, _ := testService(t)
	ctx := context.Background()
	if err := service.DeleteRole(ctx, RolePublic); !errors.Is(err, ErrSystemRole) {
		t.Fatalf("delete PUBLIC error = %v", err)
	}
	if _, err := service.CreateRole(ctx, "ASSIGNED", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateUser(ctx, "BOB", "secret", "ASSIGNED", ""); err != nil {
		t.Fatal(err)
	}
	if err := service.DeleteRole(ctx, "ASSIGNED"); !errors.Is(err, ErrRoleInUse) {
		t.Fatalf("delete assigned error = %v", err)
	}
	if _, err := service.CreateRole(ctx, "CHILD", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateRole(ctx, "PARENT", ""); err != nil {
		t.Fatal(err)
	}
	if err := service.GrantRoleToRole(ctx, "CHILD", "PARENT"); err != nil {
		t.Fatal(err)
	}
	if err := service.DeleteRole(ctx, "CHILD"); !errors.Is(err, ErrRoleInUse) {
		t.Fatalf("delete inherited error = %v", err)
	}
	if _, err := service.CreateRole(ctx, "UNUSED", ""); err != nil {
		t.Fatal(err)
	}
	if err := service.DeleteRole(ctx, "UNUSED"); err != nil {
		t.Fatal(err)
	}
	if err := service.DeleteRole(ctx, "MISSING"); !errors.Is(err, metadata.ErrIdentityNotFound) {
		t.Fatalf("missing role error = %v", err)
	}
	if err := service.DeleteUser(ctx, "MISSING"); !errors.Is(err, metadata.ErrIdentityNotFound) {
		t.Fatalf("missing user error = %v", err)
	}
}

func TestConcurrentDeleteAndGrantNeverLeavesOrphans(t *testing.T) {
	service, _, db := testService(t)
	ctx := context.Background()
	if _, err := service.CreateUser(ctx, "RACER", "secret", RoleAccountAdmin, ""); err != nil {
		t.Fatal(err)
	}

	for i := range 20 {
		roleName := fmt.Sprintf("RACE_%d", i)
		if _, err := service.CreateRole(ctx, roleName, ""); err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_ = service.GrantRoleToUser(ctx, roleName, "RACER")
		}()
		go func() {
			defer wg.Done()
			<-start
			_ = service.DeleteRole(ctx, roleName)
		}()
		close(start)
		wg.Wait()
	}

	assertNoIdentityOrphans(t, db)
}

func assertNoIdentityOrphans(t *testing.T, db *sql.DB) {
	t.Helper()
	queries := []string{
		`SELECT COUNT(*) FROM _metadata_users users LEFT JOIN _metadata_roles roles
			ON users.default_role_id = roles.id WHERE users.default_role_id IS NOT NULL AND roles.id IS NULL`,
		`SELECT COUNT(*) FROM _metadata_user_role_grants grants
			LEFT JOIN _metadata_users users ON grants.user_id = users.id
			LEFT JOIN _metadata_roles roles ON grants.role_id = roles.id
			WHERE users.id IS NULL OR roles.id IS NULL`,
		`SELECT COUNT(*) FROM _metadata_role_role_grants grants
			LEFT JOIN _metadata_roles children ON grants.child_role_id = children.id
			LEFT JOIN _metadata_roles parents ON grants.parent_role_id = parents.id
			WHERE children.id IS NULL OR parents.id IS NULL`,
	}
	for _, query := range queries {
		var count int
		if err := db.QueryRow(query).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Errorf("identity invariant failed with %d orphan rows for %s", count, query)
		}
	}
}

func TestIdentityPersistsAcrossRepositoryRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.db")
	ctx := context.Background()
	open := func() (*sql.DB, *metadata.Repository, *Service) {
		db, err := sql.Open("duckdb", path)
		if err != nil {
			t.Fatal(err)
		}
		db.SetMaxOpenConns(1)
		repo, err := metadata.NewRepository(connection.NewManager(db))
		if err != nil {
			t.Fatal(err)
		}
		service, err := NewService(ctx, repo)
		if err != nil {
			t.Fatal(err)
		}
		return db, repo, service
	}
	db, _, service := open()
	role, err := service.CreateRole(ctx, "STUDENT", "")
	if err != nil {
		t.Fatal(err)
	}
	user, err := service.CreateUser(ctx, "STUDENT_USER", "secret", role.Name, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, repo, service := open()
	defer func() { _ = db.Close() }()
	got, err := repo.GetUserRecordByName(ctx, user.Name)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != user.ID {
		t.Errorf("user ID changed from %s to %s", user.ID, got.ID)
	}
	if _, err := service.Authenticate(ctx, user.Name, "secret"); err != nil {
		t.Fatal(err)
	}
}
