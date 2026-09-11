package query

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/nnnkkk7/snowflake-emulator/pkg/connection"
	"github.com/nnnkkk7/snowflake-emulator/pkg/identity"
	"github.com/nnnkkk7/snowflake-emulator/pkg/metadata"
	"github.com/nnnkkk7/snowflake-emulator/pkg/warehouse"
)

func setupWarehouseAuthorization(t *testing.T) (*Executor, *identity.Service, *warehouse.Manager) {
	t.Helper()
	db, err := sql.Open("duckdb", "")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	connectionManager := connection.NewManager(db)
	repo, err := metadata.NewRepository(connectionManager)
	if err != nil {
		t.Fatal(err)
	}
	service, err := identity.NewService(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := warehouse.NewPersistentManager(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	executor := NewExecutor(connectionManager, repo)
	executor.Configure(WithWarehouseManager(manager), WithIdentityService(service))
	return executor, service, manager
}

func authenticatedWarehouseContext(t *testing.T, service *identity.Service, user, password, warehouseName string) ExecutionContext {
	t.Helper()
	principal, err := service.Authenticate(context.Background(), user, password)
	if err != nil {
		t.Fatal(err)
	}
	role, err := service.ResolveActiveRole(context.Background(), principal.UserID, "")
	if err != nil {
		t.Fatal(err)
	}
	return ExecutionContext{Warehouse: warehouseName, Role: role.Name, Principal: &PrincipalContext{
		UserID: principal.UserID, Username: principal.Username, RoleID: role.ID,
	}}
}

func TestWarehouseUsageAuthorizationBeforeAdmission(t *testing.T) {
	executor, service, manager := setupWarehouseAuthorization(t)
	ctx := context.Background()
	if _, err := manager.CreateWarehouseWithSettings(ctx, "study_wh", "", warehouse.Settings{Size: defaultWarehouseSize, AutoResume: true, AutoSuspend: 600}); err != nil {
		t.Fatal(err)
	}
	reader, err := service.CreateRole(ctx, "reader", "")
	if err != nil {
		t.Fatal(err)
	}
	developer, err := service.CreateRole(ctx, "developer", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateUser(ctx, "alice", "secret", developer.Name, ""); err != nil {
		t.Fatal(err)
	}
	if err := service.GrantRoleToRole(ctx, reader.Name, developer.Name); err != nil {
		t.Fatal(err)
	}
	executionContext := authenticatedWarehouseContext(t, service, "alice", "secret", "study_wh")

	if _, err := executor.QueryWithContext(ctx, executionContext, "SELECT 1"); !errors.Is(err, identity.ErrPrivilegeDenied) {
		t.Fatalf("query without USAGE: %v", err)
	}
	state, _ := manager.GetWarehouse(ctx, "study_wh")
	if state.State != warehouse.StateSuspended || state.Running != 0 || state.Queued != 0 {
		t.Fatalf("denied query changed admission state: %+v", state)
	}
	if err := service.GrantWarehousePrivilege(ctx, identity.PrivilegeUsage, "study_wh", reader.Name); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.QueryWithContext(ctx, executionContext, "SELECT 1"); err != nil {
		t.Fatalf("inherited USAGE was not honored: %v", err)
	}
	if err := service.RevokeWarehousePrivilege(ctx, identity.PrivilegeUsage, "study_wh", reader.Name); err != nil {
		t.Fatal(err)
	}
	if err := manager.SuspendWarehouse(ctx, "study_wh"); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.QueryWithContext(ctx, executionContext, "SELECT 1"); !errors.Is(err, identity.ErrPrivilegeDenied) {
		t.Fatalf("revoked USAGE still allowed query: %v", err)
	}
	state, _ = manager.GetWarehouse(ctx, "study_wh")
	if state.State != warehouse.StateSuspended || state.Running != 0 || state.Queued != 0 {
		t.Fatalf("revoked query changed admission state: %+v", state)
	}
}

func TestWarehouseOperateAndAccountAdminCompatibility(t *testing.T) {
	executor, service, manager := setupWarehouseAuthorization(t)
	ctx := context.Background()
	if _, err := manager.CreateWarehouse(ctx, "ops_wh", defaultWarehouseSize, ""); err != nil {
		t.Fatal(err)
	}
	operator, err := service.CreateRole(ctx, "operator", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateUser(ctx, "bob", "secret", operator.Name, ""); err != nil {
		t.Fatal(err)
	}
	bob := authenticatedWarehouseContext(t, service, "bob", "secret", "ops_wh")
	if _, err := executor.ExecuteWithContext(ctx, bob, "ALTER WAREHOUSE ops_wh RESUME"); !errors.Is(err, identity.ErrPrivilegeDenied) {
		t.Fatalf("OPERATE was not enforced: %v", err)
	}
	if _, err := executor.Execute(ctx, "GRANT OPERATE ON WAREHOUSE ops_wh TO ROLE operator"); err != nil {
		t.Fatal(err)
	}
	show, err := executor.Query(ctx, "SHOW GRANTS TO ROLE operator")
	if err != nil {
		t.Fatal(err)
	}
	if row := findIdentityRow(show, identity.PrivilegeOperate); row == nil || row[1] != "WAREHOUSE" || row[2] != "OPS_WH" {
		t.Fatalf("warehouse grant missing from SHOW GRANTS: %#v", show.Rows)
	}
	restoredService, err := identity.NewService(ctx, executor.repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := restoredService.AuthorizeWarehouse(ctx, operator.ID, "ops_wh", identity.PrivilegeOperate); err != nil {
		t.Fatalf("warehouse grant did not survive service restart: %v", err)
	}
	if _, err := executor.ExecuteWithContext(ctx, bob, "ALTER WAREHOUSE ops_wh RESUME"); err != nil {
		t.Fatalf("granted OPERATE failed: %v", err)
	}
	if _, err := executor.Execute(ctx, "REVOKE OPERATE ON WAREHOUSE ops_wh FROM ROLE operator"); err != nil {
		t.Fatal(err)
	}
	if err := manager.SuspendWarehouse(ctx, "ops_wh"); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.ExecuteWithContext(ctx, bob, "ALTER WAREHOUSE ops_wh RESUME"); !errors.Is(err, identity.ErrPrivilegeDenied) {
		t.Fatalf("revoked OPERATE still worked: %v", err)
	}
	admin := authenticatedWarehouseContext(t, service, identity.DemoAdminUser, identity.DemoAdminPassword, "ops_wh")
	if _, err := executor.ExecuteWithContext(ctx, admin, "ALTER WAREHOUSE ops_wh RESUME"); err != nil {
		t.Fatalf("ACCOUNTADMIN compatibility failed: %v", err)
	}
	if _, err := manager.CreateWarehouse(ctx, "drop_wh", defaultWarehouseSize, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.ExecuteWithContext(ctx, bob, "DROP WAREHOUSE drop_wh"); !errors.Is(err, identity.ErrPrivilegeDenied) {
		t.Fatalf("DROP did not require OPERATE: %v", err)
	}
	if err := service.GrantWarehousePrivilege(ctx, identity.PrivilegeOperate, "drop_wh", operator.Name); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.ExecuteWithContext(ctx, bob, "DROP WAREHOUSE drop_wh"); err != nil {
		t.Fatalf("DROP with OPERATE failed: %v", err)
	}
}

func TestScheduledTaskUsesPersistedOwnerRoleAuthorization(t *testing.T) {
	executor, service, manager := setupWarehouseAuthorization(t)
	ctx := context.Background()
	if _, err := manager.CreateWarehouse(ctx, "task_auth_wh", defaultWarehouseSize, ""); err != nil {
		t.Fatal(err)
	}
	role, err := service.CreateRole(ctx, "task_owner", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateUser(ctx, "task_user", "secret", role.Name, ""); err != nil {
		t.Fatal(err)
	}
	database, err := executor.repo.CreateDatabase(ctx, "TASK_AUTH_DB", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := executor.repo.GetSchemaByName(ctx, database.ID, "PUBLIC"); err != nil {
		t.Fatal(err)
	}
	admin := authenticatedWarehouseContext(t, service, identity.DemoAdminUser, identity.DemoAdminPassword, "task_auth_wh")
	admin.Database, admin.Schema = database.Name, "PUBLIC"
	if _, err := executor.ExecuteWithContext(ctx, admin, "CREATE TABLE task_log (value INTEGER)"); err != nil {
		t.Fatal(err)
	}
	owner := authenticatedWarehouseContext(t, service, "task_user", "secret", "task_auth_wh")
	owner.Database, owner.Schema = database.Name, "PUBLIC"
	for _, grant := range []struct{ privilege, objectType, objectName string }{
		{identity.PrivilegeUsage, "DATABASE", database.Name},
		{identity.PrivilegeUsage, "SCHEMA", database.Name + ".PUBLIC"},
		{identity.PrivilegeInsert, "TABLE", database.Name + ".PUBLIC.TASK_LOG"},
	} {
		if err := service.GrantObjectPrivilege(ctx, grant.privilege, grant.objectType, grant.objectName, role.Name); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := executor.ExecuteWithContext(ctx, owner, "CREATE TASK guarded_task WAREHOUSE=task_auth_wh SCHEDULE='1 SECOND' AS INSERT INTO task_log VALUES (1)"); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.ExecuteWithContext(ctx, owner, "ALTER TASK guarded_task RESUME"); err != nil {
		t.Fatal(err)
	}
	scheduler := NewTaskScheduler(executor.repo, executor, time.Second)
	if err := scheduler.RunDueTasks(ctx, time.Now().Add(2*time.Second)); !errors.Is(err, identity.ErrPrivilegeDenied) && (err == nil || !strings.Contains(err.Error(), "lacks USAGE")) {
		t.Fatalf("scheduled task bypassed owner authorization: %v", err)
	}
	if err := service.GrantWarehousePrivilege(ctx, identity.PrivilegeUsage, "task_auth_wh", role.Name); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.RunDueTasks(ctx, time.Now().Add(4*time.Second)); err != nil {
		t.Fatalf("scheduled task with owner USAGE failed: %v", err)
	}
	if err := service.RevokeWarehousePrivilege(ctx, identity.PrivilegeUsage, "task_auth_wh", role.Name); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.RunDueTasks(ctx, time.Now().Add(6*time.Second)); err == nil || !strings.Contains(err.Error(), "lacks USAGE") {
		t.Fatalf("scheduled task ignored revoked owner USAGE: %v", err)
	}
}

func TestRequestRoleCannotSpoofWarehouseAccess(t *testing.T) {
	executor, _, manager := setupWarehouseAuthorization(t)
	ctx := context.Background()
	if _, err := manager.CreateWarehouse(ctx, "spoof_wh", defaultWarehouseSize, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.QueryWithContext(ctx, ExecutionContext{Warehouse: "spoof_wh", Role: identity.RoleAccountAdmin}, "SELECT 1"); err == nil {
		t.Fatal("untrusted role was accepted")
	}
	state, _ := manager.GetWarehouse(ctx, "spoof_wh")
	if state.State != warehouse.StateSuspended || state.Running != 0 || state.Queued != 0 {
		t.Fatalf("spoof attempt changed admission state: %+v", state)
	}
}

func TestDynamicTableAuthorizationHappensBeforeAdmission(t *testing.T) {
	executor, service, manager := setupWarehouseAuthorization(t)
	ctx := context.Background()
	if _, err := manager.CreateWarehouse(ctx, "dynamic_auth_wh", defaultWarehouseSize, ""); err != nil {
		t.Fatal(err)
	}
	role, err := service.CreateRole(ctx, "dynamic_owner", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateUser(ctx, "dynamic_user", "secret", role.Name, ""); err != nil {
		t.Fatal(err)
	}
	database, err := executor.repo.CreateDatabase(ctx, "DYNAMIC_AUTH_DB", "")
	if err != nil {
		t.Fatal(err)
	}
	executionContext := authenticatedWarehouseContext(t, service, "dynamic_user", "secret", "dynamic_auth_wh")
	executionContext.Database, executionContext.Schema = database.Name, "PUBLIC"
	for _, grant := range []struct{ privilege, objectType, objectName string }{
		{identity.PrivilegeUsage, "DATABASE", database.Name},
		{identity.PrivilegeUsage, "SCHEMA", database.Name + ".PUBLIC"},
		{identity.PrivilegeCreateTable, "SCHEMA", database.Name + ".PUBLIC"},
	} {
		if err := service.GrantObjectPrivilege(ctx, grant.privilege, grant.objectType, grant.objectName, role.Name); err != nil {
			t.Fatal(err)
		}
	}
	statement := "CREATE DYNAMIC TABLE guarded TARGET_LAG='1 MINUTE' WAREHOUSE=dynamic_auth_wh AS SELECT 1 AS id"
	if _, err := executor.ExecuteWithContext(ctx, executionContext, statement); !errors.Is(err, identity.ErrPrivilegeDenied) {
		t.Fatalf("dynamic table creation bypassed USAGE: %v", err)
	}
	state, err := manager.GetWarehouse(ctx, "dynamic_auth_wh")
	if err != nil {
		t.Fatal(err)
	}
	if state.State != warehouse.StateSuspended || state.Running != 0 || state.Queued != 0 {
		t.Fatalf("denied dynamic table changed admission state: %+v", state)
	}
	if err := service.GrantWarehousePrivilege(ctx, identity.PrivilegeUsage, "dynamic_auth_wh", role.Name); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.ExecuteWithContext(ctx, executionContext, statement); err != nil {
		t.Fatalf("authorized dynamic table creation failed: %v", err)
	}
	schema, err := executor.repo.GetSchemaByName(ctx, database.ID, "PUBLIC")
	if err != nil {
		t.Fatal(err)
	}
	value, err := executor.repo.GetDynamicTableByName(ctx, schema.ID, "guarded")
	if err != nil || value.Owner != role.ID {
		t.Fatalf("persisted dynamic owner = %#v, error = %v", value, err)
	}
	if _, err := identity.NewService(ctx, executor.repo); err != nil {
		t.Fatal(err)
	}
	if _, err := warehouse.NewPersistentManager(ctx, executor.repo); err != nil {
		t.Fatal(err)
	}
	value, err = executor.repo.GetDynamicTableByName(ctx, schema.ID, "guarded")
	if err != nil || value.Owner != role.ID {
		t.Fatalf("dynamic owner after reconstruction = %#v, error = %v", value, err)
	}
	if err := service.RevokeWarehousePrivilege(ctx, identity.PrivilegeUsage, "dynamic_auth_wh", role.Name); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.ExecuteWithContext(ctx, executionContext, "ALTER DYNAMIC TABLE guarded REFRESH"); !errors.Is(err, identity.ErrPrivilegeDenied) {
		t.Fatalf("dynamic refresh ignored revoked USAGE: %v", err)
	}
}

func TestCopyAndStreamConsumptionDenyBeforeWarehouseAdmission(t *testing.T) {
	executor, service, manager := setupWarehouseAuthorization(t)
	ctx := context.Background()
	if _, err := manager.CreateWarehouse(ctx, "data_wh", defaultWarehouseSize, ""); err != nil {
		t.Fatal(err)
	}
	role, err := service.CreateRole(ctx, "loader", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateUser(ctx, "loader_user", "secret", role.Name, ""); err != nil {
		t.Fatal(err)
	}
	executionContext := authenticatedWarehouseContext(t, service, "loader_user", "secret", "data_wh")
	database, err := executor.repo.CreateDatabase(ctx, "DATA_AUTH_DB", "")
	if err != nil {
		t.Fatal(err)
	}
	executionContext.Database, executionContext.Schema = database.Name, "PUBLIC"
	statements := []string{
		"COPY INTO target_table FROM @input_stage",
		"INSERT INTO target_table SELECT * FROM source_stream",
	}
	for _, statement := range statements {
		if _, err := executor.ExecuteWithContext(ctx, executionContext, statement); !errors.Is(err, identity.ErrPrivilegeDenied) {
			t.Fatalf("%q was not denied before object processing: %v", statement, err)
		}
		state, stateErr := manager.GetWarehouse(ctx, "data_wh")
		if stateErr != nil {
			t.Fatal(stateErr)
		}
		if state.State != warehouse.StateSuspended || state.Running != 0 || state.Queued != 0 {
			t.Fatalf("denied %q changed admission state: %+v", statement, state)
		}
	}
}

func TestNestedCallAcquiresWarehouseOnceAndUsesCallerRights(t *testing.T) {
	executor, service, manager := setupWarehouseAuthorization(t)
	ctx := context.Background()
	if _, err := manager.CreateWarehouse(ctx, "call_wh", defaultWarehouseSize, ""); err != nil {
		t.Fatal(err)
	}
	role, err := service.CreateRole(ctx, "caller_role", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateUser(ctx, "caller_user", "secret", role.Name, ""); err != nil {
		t.Fatal(err)
	}
	if err := service.GrantWarehousePrivilege(ctx, identity.PrivilegeUsage, "call_wh", role.Name); err != nil {
		t.Fatal(err)
	}
	database, err := executor.repo.CreateDatabase(ctx, "CALL_AUTH_DB", "")
	if err != nil {
		t.Fatal(err)
	}
	caller := authenticatedWarehouseContext(t, service, "caller_user", "secret", "call_wh")
	caller.Database, caller.Schema = database.Name, "PUBLIC"
	for _, grant := range []struct{ privilege, objectType, objectName string }{
		{identity.PrivilegeUsage, "DATABASE", database.Name},
		{identity.PrivilegeUsage, "SCHEMA", database.Name + ".PUBLIC"},
		{identity.PrivilegeCreateTable, "SCHEMA", database.Name + ".PUBLIC"},
		{identity.PrivilegeInsert, "TABLE", database.Name + ".PUBLIC.CALL_LOG"},
		{identity.PrivilegeSelect, "TABLE", database.Name + ".PUBLIC.CALL_LOG"},
		{identity.PrivilegeSelect, "TABLE", database.Name + ".PUBLIC.OWNED_STREAM"},
	} {
		if err := service.GrantObjectPrivilege(ctx, grant.privilege, grant.objectType, grant.objectName, role.Name); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := executor.ExecuteWithContext(ctx, caller, "CREATE PROCEDURE nested_write() RETURNS VARCHAR LANGUAGE SQL AS $$ BEGIN CREATE TABLE IF NOT EXISTS call_log (username VARCHAR, role_name VARCHAR); INSERT INTO call_log VALUES (CURRENT_USER(), CURRENT_ROLE()); RETURN 'ok'; END $$"); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.ExecuteWithContext(ctx, caller, "CREATE TABLE stream_source (id INTEGER)"); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.ExecuteWithContext(ctx, caller, "CREATE STREAM owned_stream ON TABLE stream_source"); err != nil {
		t.Fatal(err)
	}
	schema, err := executor.repo.GetSchemaByName(ctx, database.ID, "PUBLIC")
	if err != nil {
		t.Fatal(err)
	}
	procedure, err := executor.repo.GetProcedureByName(ctx, schema.ID, "nested_write")
	if err != nil || procedure.Owner != role.ID {
		t.Fatalf("persisted procedure owner = %#v, error = %v", procedure, err)
	}
	stream, err := executor.repo.GetStreamByName(ctx, schema.ID, "owned_stream")
	if err != nil || stream.Owner != role.ID {
		t.Fatalf("persisted stream owner = %#v, error = %v", stream, err)
	}
	var admissions atomic.Int32
	caller.OnWarehouseRunning = func() { admissions.Add(1) }
	if _, err := executor.QueryWithContext(ctx, caller, "CALL nested_write()"); err != nil {
		t.Fatal(err)
	}
	if got := admissions.Load(); got != 1 {
		t.Fatalf("warehouse running callback count = %d, want one outer CALL admission", got)
	}
	result, err := executor.QueryWithContext(ctx, caller, "SELECT username, role_name FROM call_log")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 1 || result.Rows[0][0] != "CALLER_USER" || result.Rows[0][1] != role.Name {
		t.Fatalf("procedure did not use caller role: %#v", result.Rows)
	}
	if err := service.RevokeObjectPrivilege(ctx, identity.PrivilegeInsert, "TABLE", database.Name+".PUBLIC.CALL_LOG", role.Name); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.QueryWithContext(ctx, caller, "CALL nested_write()"); !errors.Is(err, identity.ErrPrivilegeDenied) {
		t.Fatalf("nested procedure DML skipped object authorization: %v", err)
	}
	if err := service.GrantObjectPrivilege(ctx, identity.PrivilegeInsert, "TABLE", database.Name+".PUBLIC.CALL_LOG", role.Name); err != nil {
		t.Fatal(err)
	}

	// Reconstruct the identity service and warehouse manager over the same
	// persistent catalog, then verify creator metadata is unchanged.
	restoredService, err := identity.NewService(ctx, executor.repo)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := warehouse.NewPersistentManager(ctx, executor.repo); err != nil {
		t.Fatal(err)
	}
	procedure, err = executor.repo.GetProcedureByName(ctx, schema.ID, "nested_write")
	if err != nil || procedure.Owner != role.ID {
		t.Fatalf("procedure owner after reconstruction = %#v, error = %v", procedure, err)
	}
	stream, err = executor.repo.GetStreamByName(ctx, schema.ID, "owned_stream")
	if err != nil || stream.Owner != role.ID {
		t.Fatalf("stream owner after reconstruction = %#v, error = %v", stream, err)
	}
	if err := restoredService.RevokeWarehousePrivilege(ctx, identity.PrivilegeUsage, "call_wh", role.Name); err != nil {
		t.Fatal(err)
	}
	if err := manager.SuspendWarehouse(ctx, "call_wh"); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.QueryWithContext(ctx, caller, "CALL nested_write()"); !errors.Is(err, identity.ErrPrivilegeDenied) {
		t.Fatalf("CALL ignored revoked USAGE: %v", err)
	}
	if _, err := executor.ExecuteWithContext(ctx, caller, "INSERT INTO call_log SELECT CURRENT_USER(), CURRENT_ROLE() FROM owned_stream"); !errors.Is(err, identity.ErrPrivilegeDenied) {
		t.Fatalf("stream consumption ignored revoked USAGE: %v", err)
	}
	state, err := manager.GetWarehouse(ctx, "call_wh")
	if err != nil {
		t.Fatal(err)
	}
	if state.State != warehouse.StateSuspended || state.Running != 0 || state.Queued != 0 {
		t.Fatalf("revoked CALL/stream consumption changed admission: %+v", state)
	}
}

func TestAuthorizedWarehouseStillHonorsLifecycleValidation(t *testing.T) {
	executor, service, manager := setupWarehouseAuthorization(t)
	ctx := context.Background()
	admin := authenticatedWarehouseContext(t, service, identity.DemoAdminUser, identity.DemoAdminPassword, "missing_wh")
	if _, err := executor.QueryWithContext(ctx, admin, "SELECT 1"); err == nil {
		t.Fatal("missing warehouse should fail after authorization")
	}
	if _, err := manager.CreateWarehouseWithSettings(ctx, "manual_wh", "", warehouse.Settings{Size: defaultWarehouseSize, AutoResume: false, AutoSuspend: 0}); err != nil {
		t.Fatal(err)
	}
	admin.Warehouse = "manual_wh"
	if _, err := executor.QueryWithContext(ctx, admin, "SELECT 1"); err == nil {
		t.Fatal("authorized session should not bypass disabled AUTO_RESUME")
	}
}
