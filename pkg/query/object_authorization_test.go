package query

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/nnnkkk7/snowflake-emulator/pkg/identity"
	"github.com/nnnkkk7/snowflake-emulator/pkg/metadata"
	"github.com/nnnkkk7/snowflake-emulator/pkg/warehouse"
)

func TestObjectPrivilegesInheritedRevokedAndPreAdmission(t *testing.T) {
	executor, service, manager := setupWarehouseAuthorization(t)
	ctx := context.Background()
	if _, err := manager.CreateWarehouse(ctx, "object_wh", defaultWarehouseSize, ""); err != nil {
		t.Fatal(err)
	}
	database, err := executor.repo.CreateDatabase(ctx, "OBJECT_DB", "")
	if err != nil {
		t.Fatal(err)
	}
	admin := authenticatedWarehouseContext(t, service, identity.DemoAdminUser, identity.DemoAdminPassword, "object_wh")
	admin.Database, admin.Schema = database.Name, "PUBLIC"
	if _, err := executor.ExecuteWithContext(ctx, admin, "CREATE TABLE records (id INTEGER)"); err != nil {
		t.Fatal(err)
	}
	reader, err := service.CreateRole(ctx, "object_reader", "")
	if err != nil {
		t.Fatal(err)
	}
	parent, err := service.CreateRole(ctx, "object_parent", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.GrantRoleToRole(ctx, reader.Name, parent.Name); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateUser(ctx, "object_user", "secret", parent.Name, ""); err != nil {
		t.Fatal(err)
	}
	user := authenticatedWarehouseContext(t, service, "object_user", "secret", "object_wh")
	user.Database, user.Schema = database.Name, "PUBLIC"
	if err := service.GrantWarehousePrivilege(ctx, identity.PrivilegeUsage, "object_wh", reader.Name); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		"GRANT USAGE ON DATABASE OBJECT_DB TO ROLE object_reader",
		"GRANT USAGE ON SCHEMA OBJECT_DB.PUBLIC TO ROLE object_reader",
	} {
		if _, err := executor.Execute(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := executor.QueryWithContext(ctx, user, "SELECT * FROM records"); !errors.Is(err, identity.ErrPrivilegeDenied) {
		t.Fatalf("SELECT without table grant: %v", err)
	}
	state, _ := manager.GetWarehouse(ctx, "object_wh")
	if state.State != warehouse.StateSuspended || state.Running != 0 || state.Queued != 0 {
		t.Fatalf("denial admitted warehouse: %+v", state)
	}
	if _, err := executor.Execute(ctx, "GRANT SELECT ON TABLE OBJECT_DB.PUBLIC.RECORDS TO ROLE object_reader"); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.QueryWithContext(ctx, user, "SELECT * FROM OBJECT_DB.PUBLIC.RECORDS"); err != nil {
		t.Fatalf("inherited SELECT failed: %v", err)
	}
	if _, err := executor.Execute(ctx, "REVOKE SELECT ON TABLE OBJECT_DB.PUBLIC.RECORDS FROM ROLE object_reader"); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.QueryWithContext(ctx, user, "SELECT * FROM records"); !errors.Is(err, identity.ErrPrivilegeDenied) {
		t.Fatalf("revoked SELECT worked: %v", err)
	}
}

func TestObjectPrivilegeMatrix(t *testing.T) {
	_, service, manager := setupWarehouseAuthorization(t)
	ctx := context.Background()
	if _, err := manager.CreateWarehouse(ctx, "matrix_wh", defaultWarehouseSize, ""); err != nil {
		t.Fatal(err)
	}
	role, err := service.CreateRole(ctx, "matrix_role", "")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct{ privilege, objectType, objectName string }{
		{identity.PrivilegeUsage, "DATABASE", "MATRIX_DB"},
		{identity.PrivilegeUsage, "SCHEMA", "MATRIX_DB.PUBLIC"},
		{identity.PrivilegeCreateTable, "SCHEMA", "MATRIX_DB.PUBLIC"},
		{identity.PrivilegeSelect, "TABLE", "MATRIX_DB.PUBLIC.T"},
		{identity.PrivilegeInsert, "TABLE", "MATRIX_DB.PUBLIC.T"},
		{identity.PrivilegeUpdate, "TABLE", "MATRIX_DB.PUBLIC.T"},
		{identity.PrivilegeDelete, "TABLE", "MATRIX_DB.PUBLIC.T"},
	}
	for _, test := range tests {
		if err := service.GrantObjectPrivilege(ctx, test.privilege, test.objectType, test.objectName, role.Name); err != nil {
			t.Fatal(err)
		}
		if err := service.AuthorizeObject(ctx, role.ID, test.objectType, test.objectName, test.privilege); err != nil {
			t.Errorf("%s: %v", test.privilege, err)
		}
		if err := service.RevokeObjectPrivilege(ctx, test.privilege, test.objectType, test.objectName, role.Name); err != nil {
			t.Fatal(err)
		}
		if err := service.AuthorizeObject(ctx, role.ID, test.objectType, test.objectName, test.privilege); !errors.Is(err, identity.ErrPrivilegeDenied) {
			t.Errorf("revoked %s: %v", test.privilege, err)
		}
	}
}

func TestObjectGrantValidationAndCleanup(t *testing.T) {
	executor, service, _ := setupWarehouseAuthorization(t)
	ctx := context.Background()
	role, err := service.CreateRole(ctx, "cleanup_role", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := executor.Execute(ctx, "GRANT USAGE ON DATABASE MISSING_DB TO ROLE cleanup_role"); err == nil {
		t.Fatal("grant to missing database succeeded")
	}
	database, err := executor.repo.CreateDatabase(ctx, "CLEANUP_DB", "")
	if err != nil {
		t.Fatal(err)
	}
	schema, err := executor.repo.GetSchemaByName(ctx, database.ID, "PUBLIC")
	if err != nil {
		t.Fatal(err)
	}
	table, err := executor.repo.CreateTable(ctx, schema.ID, "TO_DROP", []metadata.ColumnDef{{Name: "ID", Type: "INTEGER"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := executor.Execute(ctx, "GRANT SELECT ON TABLE CLEANUP_DB.PUBLIC.TO_DROP TO ROLE cleanup_role"); err != nil {
		t.Fatal(err)
	}
	if err := executor.repo.DeleteTableMetadata(ctx, schema.ID, table.Name); err != nil {
		t.Fatal(err)
	}
	grants, err := executor.repo.ListObjectPrivilegeRecords(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(grants) != 0 {
		t.Fatalf("table grants survived drop: %+v", grants)
	}
	if err := service.GrantObjectPrivilege(ctx, identity.PrivilegeUsage, "DATABASE", database.Name, role.Name); err != nil {
		t.Fatal(err)
	}
	if err := service.DeleteRole(ctx, role.Name); err != nil {
		t.Fatal(err)
	}
	grants, err = executor.repo.ListObjectPrivilegeRecords(ctx)
	if err != nil || len(grants) != 0 {
		t.Fatalf("role grants survived role drop: %+v, %v", grants, err)
	}
}

func TestComplexMergeAuthorizationFailsClosed(t *testing.T) {
	executor, service, manager := setupWarehouseAuthorization(t)
	ctx := context.Background()
	if _, err := manager.CreateWarehouse(ctx, "merge_auth_wh", defaultWarehouseSize, ""); err != nil {
		t.Fatal(err)
	}
	role, err := service.CreateRole(ctx, "merge_auth_role", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateUser(ctx, "merge_auth_user", "secret", role.Name, ""); err != nil {
		t.Fatal(err)
	}
	if err := service.GrantWarehousePrivilege(ctx, identity.PrivilegeUsage, "merge_auth_wh", role.Name); err != nil {
		t.Fatal(err)
	}
	database, err := executor.repo.CreateDatabase(ctx, "MERGE_AUTH_DB", "")
	if err != nil {
		t.Fatal(err)
	}
	executionContext := authenticatedWarehouseContext(t, service, "merge_auth_user", "secret", "merge_auth_wh")
	executionContext.Database, executionContext.Schema = database.Name, "PUBLIC"
	statement := "MERGE INTO target USING source ON target.id=source.id WHEN NOT MATCHED THEN INSERT VALUES(source.id)"
	if _, err := executor.ExecuteWithContext(ctx, executionContext, statement); err == nil {
		t.Fatal("complex MERGE did not fail closed")
	}
	state, _ := manager.GetWarehouse(ctx, "merge_auth_wh")
	if state.State != warehouse.StateSuspended || state.Running != 0 || state.Queued != 0 {
		t.Fatalf("failed MERGE admitted work: %+v", state)
	}
}

func TestCTASRequiresCreateAndSourceSelect(t *testing.T) {
	executor, service, manager := setupWarehouseAuthorization(t)
	ctx := context.Background()
	if _, err := manager.CreateWarehouse(ctx, "ctas_wh", defaultWarehouseSize, ""); err != nil {
		t.Fatal(err)
	}
	database, err := executor.repo.CreateDatabase(ctx, "CTAS_DB", "")
	if err != nil {
		t.Fatal(err)
	}
	admin := authenticatedWarehouseContext(t, service, identity.DemoAdminUser, identity.DemoAdminPassword, "ctas_wh")
	admin.Database, admin.Schema = database.Name, "PUBLIC"
	if _, err := executor.ExecuteWithContext(ctx, admin, "CREATE TABLE source_data (id INTEGER)"); err != nil {
		t.Fatal(err)
	}
	role, err := service.CreateRole(ctx, "ctas_role", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateUser(ctx, "ctas_user", "secret", role.Name, ""); err != nil {
		t.Fatal(err)
	}
	for _, grant := range []struct{ privilege, objectType, objectName string }{
		{identity.PrivilegeUsage, "DATABASE", database.Name}, {identity.PrivilegeUsage, "SCHEMA", database.Name + ".PUBLIC"},
		{identity.PrivilegeCreateTable, "SCHEMA", database.Name + ".PUBLIC"},
	} {
		if err := service.GrantObjectPrivilege(ctx, grant.privilege, grant.objectType, grant.objectName, role.Name); err != nil {
			t.Fatal(err)
		}
	}
	if err := service.GrantWarehousePrivilege(ctx, identity.PrivilegeUsage, "ctas_wh", role.Name); err != nil {
		t.Fatal(err)
	}
	user := authenticatedWarehouseContext(t, service, "ctas_user", "secret", "ctas_wh")
	user.Database, user.Schema = database.Name, "PUBLIC"
	statement := "CREATE TABLE copied AS SELECT * FROM source_data"
	if _, err := executor.ExecuteWithContext(ctx, user, statement); !errors.Is(err, identity.ErrPrivilegeDenied) {
		t.Fatalf("CTAS without SELECT: %v", err)
	}
	state, _ := manager.GetWarehouse(ctx, "ctas_wh")
	if state.State != warehouse.StateSuspended {
		t.Fatalf("denied CTAS resumed warehouse: %+v", state)
	}
	if err := service.GrantObjectPrivilege(ctx, identity.PrivilegeSelect, "TABLE", database.Name+".PUBLIC.SOURCE_DATA", role.Name); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.ExecuteWithContext(ctx, user, statement); err != nil {
		t.Fatalf("authorized CTAS failed: %v", err)
	}
}

func TestObjectPrivilegeSQLMatrixShowPersistenceAndQualifiedIsolation(t *testing.T) {
	executor, service, manager := setupWarehouseAuthorization(t)
	ctx := context.Background()
	if _, err := manager.CreateWarehouse(ctx, "dml_wh", defaultWarehouseSize, ""); err != nil {
		t.Fatal(err)
	}
	database, err := executor.repo.CreateDatabase(ctx, "DML_DB", "")
	if err != nil {
		t.Fatal(err)
	}
	other, err := executor.repo.CreateDatabase(ctx, "OTHER_DB", "")
	if err != nil {
		t.Fatal(err)
	}
	admin := authenticatedWarehouseContext(t, service, identity.DemoAdminUser, identity.DemoAdminPassword, "dml_wh")
	admin.Database, admin.Schema = database.Name, "PUBLIC"
	if _, err := executor.ExecuteWithContext(ctx, admin, "CREATE TABLE items (id INTEGER, value VARCHAR)"); err != nil {
		t.Fatal(err)
	}
	otherAdmin := admin
	otherAdmin.Database = other.Name
	if _, err := executor.ExecuteWithContext(ctx, otherAdmin, "CREATE TABLE items (id INTEGER)"); err != nil {
		t.Fatal(err)
	}
	role, err := service.CreateRole(ctx, "dml_role", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateUser(ctx, "dml_user", "secret", role.Name, ""); err != nil {
		t.Fatal(err)
	}
	if err := service.GrantWarehousePrivilege(ctx, identity.PrivilegeUsage, "dml_wh", role.Name); err != nil {
		t.Fatal(err)
	}
	for _, grant := range []struct{ p, typ, name string }{
		{identity.PrivilegeUsage, "DATABASE", database.Name},
		{identity.PrivilegeUsage, "SCHEMA", database.Name + ".PUBLIC"},
	} {
		if err := service.GrantObjectPrivilege(ctx, grant.p, grant.typ, grant.name, role.Name); err != nil {
			t.Fatal(err)
		}
	}
	user := authenticatedWarehouseContext(t, service, "dml_user", "secret", "dml_wh")
	user.Database, user.Schema = database.Name, "PUBLIC"

	steps := []struct{ sql, privilege, objectType, objectName string }{
		{"CREATE TABLE own_table (id INTEGER)", identity.PrivilegeCreateTable, "SCHEMA", database.Name + ".PUBLIC"},
		{"INSERT INTO items VALUES (1, 'one')", identity.PrivilegeInsert, "TABLE", database.Name + ".PUBLIC.ITEMS"},
		{"UPDATE items SET value='two' WHERE id=1", identity.PrivilegeUpdate, "TABLE", database.Name + ".PUBLIC.ITEMS"},
		{"DELETE FROM items WHERE id=1", identity.PrivilegeDelete, "TABLE", database.Name + ".PUBLIC.ITEMS"},
	}
	for _, step := range steps {
		if _, err := executor.ExecuteWithContext(ctx, user, step.sql); !errors.Is(err, identity.ErrPrivilegeDenied) {
			t.Fatalf("%s without grant: %v", step.privilege, err)
		}
		if err := service.GrantObjectPrivilege(ctx, step.privilege, step.objectType, step.objectName, role.Name); err != nil {
			t.Fatal(err)
		}
		if _, err := executor.ExecuteWithContext(ctx, user, step.sql); err != nil {
			t.Fatalf("%s with grant: %v", step.privilege, err)
		}
	}

	show, err := executor.Query(ctx, "SHOW GRANTS TO ROLE dml_role")
	if err != nil {
		t.Fatal(err)
	}
	if row := findIdentityRow(show, identity.PrivilegeInsert); row == nil || row[1] != "TABLE" || row[2] != "DML_DB.PUBLIC.ITEMS" {
		t.Fatalf("object grant absent from SHOW: %#v", show.Rows)
	}
	restored, err := identity.NewService(ctx, executor.repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := restored.AuthorizeObject(ctx, role.ID, "TABLE", "DML_DB.PUBLIC.ITEMS", identity.PrivilegeUpdate); err != nil {
		t.Fatalf("grant lost after service reconstruction: %v", err)
	}

	if err := service.RevokeObjectPrivilege(ctx, identity.PrivilegeInsert, "TABLE", "DML_DB.PUBLIC.ITEMS", role.Name); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.ExecuteWithContext(ctx, user, "COPY INTO items FROM @missing_stage"); !errors.Is(err, identity.ErrPrivilegeDenied) {
		t.Fatalf("COPY without INSERT: %v", err)
	}
	if err := service.GrantObjectPrivilege(ctx, identity.PrivilegeInsert, "TABLE", "DML_DB.PUBLIC.ITEMS", role.Name); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.ExecuteWithContext(ctx, user, "COPY INTO items FROM @missing_stage"); errors.Is(err, identity.ErrPrivilegeDenied) {
		t.Fatalf("COPY retained INSERT denial: %v", err)
	}

	if err := service.GrantObjectPrivilege(ctx, identity.PrivilegeSelect, "TABLE", "DML_DB.PUBLIC.ITEMS", role.Name); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.QueryWithContext(ctx, user, "SELECT * FROM OTHER_DB.PUBLIC.ITEMS"); !errors.Is(err, identity.ErrPrivilegeDenied) {
		t.Fatalf("qualified cross-database read authorized wrong object: %v", err)
	}
}

func TestPrivilegeCompatibilityAndNamespaceCleanup(t *testing.T) {
	executor, service, _ := setupWarehouseAuthorization(t)
	ctx := context.Background()
	role, err := service.CreateRole(ctx, "compat_role", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []struct{ p, typ string }{
		{identity.PrivilegeUsage, "TABLE"}, {identity.PrivilegeCreateTable, "DATABASE"},
		{identity.PrivilegeSelect, "SCHEMA"}, {identity.PrivilegeInsert, "DATABASE"},
	} {
		if err := service.GrantObjectPrivilege(ctx, invalid.p, invalid.typ, "ANY", role.Name); err == nil {
			t.Errorf("accepted %s ON %s", invalid.p, invalid.typ)
		}
	}
	database, err := executor.repo.CreateDatabase(ctx, "CLEAN_NAMESPACE_DB", "")
	if err != nil {
		t.Fatal(err)
	}
	schema, err := executor.repo.GetSchemaByName(ctx, database.ID, "PUBLIC")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.GrantObjectPrivilege(ctx, identity.PrivilegeUsage, "DATABASE", database.Name, role.Name); err != nil {
		t.Fatal(err)
	}
	if err := service.GrantObjectPrivilege(ctx, identity.PrivilegeUsage, "SCHEMA", database.Name+".PUBLIC", role.Name); err != nil {
		t.Fatal(err)
	}
	if err := executor.repo.DropSchema(ctx, schema.ID); err != nil {
		t.Fatal(err)
	}
	grants, err := executor.repo.ListObjectPrivilegeRecords(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, grant := range grants {
		if strings.HasPrefix(grant.ObjectName, database.Name+".PUBLIC") {
			t.Fatalf("schema grant survived drop: %+v", grant)
		}
	}
	if err := executor.repo.DropDatabase(ctx, database.ID); err != nil {
		t.Fatal(err)
	}
	grants, err = executor.repo.ListObjectPrivilegeRecords(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, grant := range grants {
		if strings.HasPrefix(grant.ObjectName, database.Name) {
			t.Fatalf("database grant survived drop: %+v", grant)
		}
	}
}
