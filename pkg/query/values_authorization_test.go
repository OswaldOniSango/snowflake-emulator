package query

import (
	"context"
	"errors"
	"testing"

	"github.com/nnnkkk7/snowflake-emulator/pkg/identity"
)

func TestAuthenticatedFromValues(t *testing.T) {
	e, service, wh := setupWarehouseAuthorization(t)
	ctx := context.Background()
	if _, err := wh.CreateWarehouse(ctx, "values_wh", defaultWarehouseSize, ""); err != nil {
		t.Fatal(err)
	}
	ec := authenticatedWarehouseContext(t, service, identity.DemoAdminUser, identity.DemoAdminPassword, "values_wh")
	for _, tc := range []struct {
		name, sql string
		rows      int
	}{
		{"issue77", "SELECT column1 AS id, column2 AS name, column3 AS region FROM VALUES (1, 'Alice', 'NORTH'), (2, 'Bob', 'SOUTH'), (3, 'Carla', 'NORTH') ORDER BY column1", 3},
		{"single", "SELECT column2 FROM VALUES (1, 'a,b')", 1},
		{"escaped", "SELECT column2 FROM VALUES (1, 'O''Brien'), (2, 'FROM secret JOIN hidden')", 2},
		{"subquery", "SELECT * FROM (SELECT column1, column2 FROM VALUES (1, 'Alice'), (2, 'Bob')) v ORDER BY column1", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := e.QueryWithContext(ctx, ec, tc.sql)
			if err != nil {
				t.Fatal(err)
			}
			if len(r.Rows) != tc.rows {
				t.Fatalf("rows: %v", r.Rows)
			}
			if tc.name == "issue77" && (r.Rows[0][0] != int32(1) || r.Rows[0][1] != "Alice" || r.Rows[1][1] != "Bob" || r.Rows[2][1] != "Carla") {
				t.Fatalf("wrong rows or order: %v", r.Rows)
			}
		})
	}
}

func TestValuesCommaJoinStillAuthorizesRealSources(t *testing.T) {
	e, service, wh := setupWarehouseAuthorization(t)
	ctx := context.Background()
	if _, err := wh.CreateWarehouse(ctx, "values_guard", defaultWarehouseSize, ""); err != nil {
		t.Fatal(err)
	}
	db, err := e.repo.CreateDatabase(ctx, "VALUES_GUARD_DB", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Execute(ctx, "CREATE TABLE VALUES_GUARD_DB.PUBLIC.secret (id INTEGER)"); err != nil {
		t.Fatal(err)
	}
	role, err := service.CreateRole(ctx, "values_reader", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateUser(ctx, "values_user", "secret", role.Name, ""); err != nil {
		t.Fatal(err)
	}
	if err := service.GrantWarehousePrivilege(ctx, identity.PrivilegeUsage, "values_guard", role.Name); err != nil {
		t.Fatal(err)
	}
	for _, g := range []struct{ kind, name string }{{"DATABASE", db.Name}, {"SCHEMA", db.Name + ".PUBLIC"}} {
		if err := service.GrantObjectPrivilege(ctx, identity.PrivilegeUsage, g.kind, g.name, role.Name); err != nil {
			t.Fatal(err)
		}
	}
	ec := authenticatedWarehouseContext(t, service, "values_user", "secret", "values_guard")
	ec.Database = db.Name
	ec.Schema = "PUBLIC"
	for _, sql := range []string{"SELECT * FROM VALUES (1, 'a'), (2, 'b') AS v, secret", "SELECT * FROM VALUES (1) AS v JOIN secret ON TRUE", "SELECT * FROM (SELECT * FROM secret) s, VALUES (1) AS v"} {
		if _, err := e.QueryWithContext(ctx, ec, sql); !errors.Is(err, identity.ErrPrivilegeDenied) {
			t.Fatalf("source not denied: %s: %v", sql, err)
		}
	}
}
