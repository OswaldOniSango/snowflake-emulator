package query

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

func setupDynamicSchedule(t *testing.T) (*Executor, ExecutionContext, func(string)) {
	t.Helper()
	e, repo := setupTestExecutor(t)
	e.Configure(WithWarehouseValidator(func(context.Context, string) error { return nil }))
	db, err := repo.CreateDatabase(context.Background(), "SCHEDULE_DB", "")
	if err != nil {
		t.Fatal(err)
	}
	ec := ExecutionContext{Database: db.Name, Schema: "PUBLIC", Warehouse: "WH"}
	exec := func(sql string) {
		t.Helper()
		if _, err := e.ExecuteWithContext(context.Background(), ec, sql); err != nil {
			t.Fatal(err)
		}
	}
	exec("CREATE TABLE source_data (id INTEGER)")
	exec("INSERT INTO source_data VALUES (1)")
	exec("CREATE DYNAMIC TABLE snapshot TARGET_LAG = '1 MINUTE' WAREHOUSE = WH AS SELECT * FROM source_data")
	return e, ec, exec
}

func TestDynamicSchedulerLagFailureRetryAndConcurrency(t *testing.T) {
	e, ec, exec := setupDynamicSchedule(t)
	ctx := context.Background()
	values, err := e.repo.ListDynamicTables(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	initial := *values[0].LastRefreshedAt
	scheduler := NewDynamicTableScheduler(e, time.Second)
	exec("INSERT INTO source_data VALUES (2)")
	if err := scheduler.RunDueTables(ctx, initial.Add(59*time.Second)); err != nil {
		t.Fatal(err)
	}
	r, _ := e.QueryWithContext(ctx, ec, "SELECT * FROM snapshot")
	if len(r.Rows) != 1 {
		t.Fatal("refreshed before lag")
	}
	now := initial.Add(time.Minute)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- scheduler.RunDueTables(ctx, now) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	r, _ = e.QueryWithContext(ctx, ec, "SELECT * FROM snapshot")
	if len(r.Rows) != 2 {
		t.Fatalf("stale rows: %v", r.Rows)
	}
	exec("DROP TABLE source_data")
	later := now.Add(time.Minute)
	if err := scheduler.RunDueTables(ctx, later); err == nil {
		t.Fatal("missing source did not fail")
	}
	values, _ = e.repo.ListDynamicTables(ctx, "")
	v := values[0]
	if v.LastRefreshError == "" || !v.LastRefreshedAt.Equal(now) || v.LastAttemptedAt == nil {
		t.Fatalf("failure state: %+v", v)
	}
	r, _ = e.QueryWithContext(ctx, ec, "SELECT * FROM snapshot")
	if len(r.Rows) != 2 {
		t.Fatal("failed refresh lost last successful rows")
	}
	exec("CREATE TABLE source_data (id INTEGER)")
	exec("INSERT INTO source_data VALUES (3)")
	restarted := NewDynamicTableScheduler(e, time.Second)
	if err := restarted.RunDueTables(ctx, later.Add(29*time.Second)); err != nil {
		t.Fatal(err)
	}
	r, _ = e.QueryWithContext(ctx, ec, "SELECT * FROM snapshot")
	if len(r.Rows) != 2 {
		t.Fatal("ignored retry backoff")
	}
	if err := restarted.RunDueTables(ctx, later.Add(30*time.Second)); err != nil {
		t.Fatal(err)
	}
	values, _ = e.repo.ListDynamicTables(ctx, "")
	if values[0].LastRefreshError != "" {
		t.Fatal("successful retry retained error")
	}
	r, _ = e.QueryWithContext(ctx, ec, "SELECT * FROM snapshot")
	if len(r.Rows) != 1 || r.Rows[0][0] != int32(3) {
		t.Fatalf("retry rows: %v", r.Rows)
	}
	exec("CREATE OR REPLACE DYNAMIC TABLE snapshot TARGET_LAG = 'DOWNSTREAM' WAREHOUSE = WH AS SELECT * FROM source_data")
	exec("INSERT INTO source_data VALUES (4)")
	if err := restarted.RunDueTables(ctx, later.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	r, _ = e.QueryWithContext(ctx, ec, "SELECT * FROM snapshot")
	if len(r.Rows) != 1 {
		t.Fatal("DOWNSTREAM auto-refreshed without dependency scheduler")
	}
	exec("DROP DYNAMIC TABLE snapshot")
	if err := restarted.RunDueTables(ctx, later.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
}

func TestTargetLagValidation(t *testing.T) {
	for _, value := range []string{"60 seconds", "1 MINUTE", "24 HOURS", "1 DAY", "DOWNSTREAM"} {
		if _, err := parseTargetLag(value); err != nil {
			t.Error(err)
		}
	}
	for _, value := range []string{"0 MINUTES", "59 SECONDS", "1 WEEK", "999999999999999999999 DAYS", "106752 DAYS", "-1 HOUR"} {
		if _, err := parseTargetLag(value); err == nil {
			t.Errorf("accepted %s", value)
		}
	}
	e, _, exec := setupDynamicSchedule(t)
	exec("CREATE OR REPLACE DYNAMIC TABLE snapshot TARGET_LAG = '60 SECONDS' WAREHOUSE = WH AS SELECT * FROM source_data")
	e.Configure(WithWarehouseValidator(func(context.Context, string) error { return context.DeadlineExceeded }))
	values, _ := e.repo.ListDynamicTables(context.Background(), "")
	err := NewDynamicTableScheduler(e, time.Second).RunDueTables(context.Background(), values[0].CreatedAt.Add(time.Hour))
	if err == nil || !strings.Contains(err.Error(), "deadline") {
		t.Fatalf("warehouse unavailable: %v", err)
	}
}

func TestPinnedDynamicRefreshDoesNotWaitForSchedulerMutex(t *testing.T) {
	e, ec, _ := setupDynamicSchedule(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	e.dynamicTableProcessor.mu.Lock()
	defer e.dynamicTableProcessor.mu.Unlock()
	err := e.withPinnedConnection(ctx, func(pinned *Executor) error {
		_, err := pinned.ExecuteWithContext(ctx, ec, "ALTER DYNAMIC TABLE snapshot REFRESH")
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "busy") {
		t.Fatalf("pinned refresh did not fail fast: %v", err)
	}
	if ctx.Err() != nil {
		t.Fatal("pinned refresh blocked until timeout")
	}
}
