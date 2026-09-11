package query

import (
	"context"
	"strings"
	"testing"

	"github.com/nnnkkk7/snowflake-emulator/pkg/warehouse"
)

func TestRequiresWarehouse(t *testing.T) {
	tests := []struct {
		sql  string
		want bool
	}{
		{"SELECT 1", true}, {"WITH x AS (SELECT 1) SELECT * FROM x", true}, {"INSERT INTO t VALUES (1)", true},
		{"UPDATE t SET id = 2", true}, {"DELETE FROM t", true}, {"MERGE INTO t USING s ON t.id = s.id WHEN MATCHED THEN DELETE", true},
		{"COPY INTO t FROM @s", true}, {"CALL p()", true}, {"CREATE TABLE t AS SELECT 1", true},
		{"CREATE TEMPORARY TABLE t AS SELECT 1", true}, {"CREATE TRANSIENT TABLE t AS SELECT 1", true},
		{"CREATE TABLE t (id INT)", false}, {"CREATE VIEW v AS SELECT 1", false}, {"CREATE DYNAMIC TABLE d TARGET_LAG = '1 MINUTE' WAREHOUSE = wh AS SELECT 1", false},
		{"CREATE OR REPLACE DYNAMIC TABLE d TARGET_LAG = '1 MINUTE' WAREHOUSE = wh AS SELECT 1", false},
		{"SHOW TABLES", false}, {"EXPLAIN SELECT 1", false}, {"EXECUTE TASK t", false},
	}
	for _, tt := range tests {
		t.Run(tt.sql, func(t *testing.T) {
			if got := RequiresWarehouse(tt.sql); got != tt.want {
				t.Fatalf("RequiresWarehouse = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWarehouseSQLAndComputeRequirement(t *testing.T) {
	executor, _ := setupTestExecutor(t)
	manager := warehouse.NewManager()
	executor.Configure(WithWarehouseManager(manager))
	ctx := context.Background()
	if _, err := executor.QueryWithContext(ctx, ExecutionContext{}, "SELECT 1"); err == nil || !strings.Contains(err.Error(), "warehouse is required") {
		t.Fatalf("missing warehouse error = %v", err)
	}
	if _, err := executor.ExecuteWithContext(ctx, ExecutionContext{}, "CREATE WAREHOUSE learning_wh WAREHOUSE_SIZE=SMALL AUTO_RESUME=TRUE AUTO_SUSPEND=30"); err != nil {
		t.Fatal(err)
	}
	result, err := executor.QueryWithContext(ctx, ExecutionContext{}, "SHOW WAREHOUSES")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 1 || result.Rows[0][0] != "LEARNING_WH" {
		t.Fatalf("rows = %#v", result.Rows)
	}
	if _, err := executor.QueryWithContext(ctx, ExecutionContext{Warehouse: "LEARNING_WH"}, "SELECT 1"); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.ExecuteWithContext(ctx, ExecutionContext{}, "ALTER WAREHOUSE learning_wh SUSPEND"); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.ExecuteWithContext(ctx, ExecutionContext{}, "ALTER WAREHOUSE learning_wh SET WAREHOUSE_SIZE=MEDIUM AUTO_RESUME=FALSE AUTO_SUSPEND=0"); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.QueryWithContext(ctx, ExecutionContext{Warehouse: "LEARNING_WH"}, "SELECT 1"); err == nil || !strings.Contains(err.Error(), "AUTO_RESUME is disabled") {
		t.Fatalf("suspended error = %v", err)
	}
	if _, err := executor.ExecuteWithContext(ctx, ExecutionContext{}, "DROP WAREHOUSE learning_wh"); err != nil {
		t.Fatal(err)
	}
}
