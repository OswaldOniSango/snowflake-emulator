package warehouse

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/nnnkkk7/snowflake-emulator/pkg/connection"
	"github.com/nnnkkk7/snowflake-emulator/pkg/metadata"
)

func TestAcquireQueuesFIFOAtWarehouseCapacity(t *testing.T) {
	m := NewManager()
	ctx := context.Background()
	if _, err := m.CreateWarehouse(ctx, "learning_wh", "X-SMALL", ""); err != nil {
		t.Fatal(err)
	}
	first, err := m.Acquire(ctx, "learning_wh", nil)
	if err != nil {
		t.Fatal(err)
	}
	admitted := make(chan int, 2)
	queued := make(chan int, 2)
	for id := 1; id <= 2; id++ {
		go func() {
			lease, acquireErr := m.Acquire(ctx, "learning_wh", func() { queued <- id })
			if acquireErr != nil {
				admitted <- -id
				return
			}
			admitted <- id
			lease.Release()
		}()
		select {
		case got := <-queued:
			if got != id {
				t.Fatalf("queued statement = %d, want %d", got, id)
			}
		case <-time.After(time.Second):
			t.Fatalf("statement %d was not queued", id)
		}
	}
	state, _ := m.GetWarehouse(ctx, "learning_wh")
	if state.Running != 1 || state.Queued != 2 {
		t.Fatalf("load = running %d queued %d", state.Running, state.Queued)
	}
	first.Release()
	for want := 1; want <= 2; want++ {
		select {
		case got := <-admitted:
			if got != want {
				t.Fatalf("admission order = %d, want %d", got, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("queued statement %d was not admitted", want)
		}
	}
}

func TestWarehouseSizeDoublesLogicalCapacity(t *testing.T) {
	wants := map[string]int{
		"X-SMALL":  1,
		"SMALL":    2,
		"MEDIUM":   4,
		"LARGE":    8,
		"X-LARGE":  16,
		"2X-LARGE": 32,
		"6X-LARGE": 512,
	}
	for size, want := range wants {
		if got := capacity(size); got != want {
			t.Errorf("capacity(%s) = %d, want %d", size, got, want)
		}
	}
}

func TestCancelRemovesQueuedStatement(t *testing.T) {
	m := NewManager()
	ctx := context.Background()
	_, _ = m.CreateWarehouse(ctx, "cancel_wh", "X-SMALL", "")
	first, _ := m.Acquire(ctx, "cancel_wh", nil)
	queuedCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	waiting := make(chan struct{}, 1)
	go func() {
		_, err := m.Acquire(queuedCtx, "cancel_wh", func() { waiting <- struct{}{} })
		done <- err
	}()
	<-waiting
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled acquisition succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("canceled waiter did not return")
	}
	state, _ := m.GetWarehouse(ctx, "cancel_wh")
	if state.Running != 1 || state.Queued != 0 {
		t.Fatalf("load after cancellation = %#v", state)
	}
	first.Release()
}

func TestWarehousesAdmitIndependently(t *testing.T) {
	m := NewManager()
	ctx := context.Background()
	_, _ = m.CreateWarehouse(ctx, "one", "X-SMALL", "")
	_, _ = m.CreateWarehouse(ctx, "two", "X-SMALL", "")
	one, _ := m.Acquire(ctx, "one", nil)
	two, err := m.Acquire(ctx, "two", nil)
	if err != nil {
		t.Fatal(err)
	}
	one.Release()
	two.Release()
}

func TestPersistentManagerRestoresConfigurationSuspended(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "warehouse.db")
	open := func() (*sql.DB, *metadata.Repository) {
		db, err := sql.Open("duckdb", path)
		if err != nil {
			t.Fatal(err)
		}
		db.SetMaxOpenConns(1)
		repo, err := metadata.NewRepository(connection.NewManager(db))
		if err != nil {
			_ = db.Close()
			t.Fatal(err)
		}
		return db, repo
	}
	db, repo := open()
	first, err := NewPersistentManager(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.CreateWarehouseWithSettings(ctx, "durable_wh", "learning", Settings{Size: "MEDIUM", AutoResume: false, AutoSuspend: 45}); err != nil {
		t.Fatal(err)
	}
	if err := first.ResumeWarehouse(ctx, "durable_wh"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, repo = open()
	defer func() { _ = db.Close() }()
	restored, err := NewPersistentManager(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	value, err := restored.GetWarehouse(ctx, "durable_wh")
	if err != nil {
		t.Fatal(err)
	}
	if value.State != StateSuspended || value.Size != "MEDIUM" || value.AutoResume || value.AutoSuspend != 45 || value.Comment != "learning" {
		t.Fatalf("restored warehouse = %#v", value)
	}
}

func TestAcquireHonorsAutoResumeAndSuspendQuiesces(t *testing.T) {
	m := NewManager()
	ctx := context.Background()
	if _, err := m.CreateWarehouseWithSettings(ctx, "manual_wh", "", Settings{Size: "X-SMALL", AutoResume: false, AutoSuspend: 60}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Acquire(ctx, "manual_wh", nil); err == nil {
		t.Fatal("suspended warehouse with auto resume disabled accepted work")
	}
	if err := m.ResumeWarehouse(ctx, "manual_wh"); err != nil {
		t.Fatal(err)
	}
	lease, err := m.Acquire(ctx, "manual_wh", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SuspendWarehouse(ctx, "manual_wh"); err != nil {
		t.Fatal(err)
	}
	state, _ := m.GetWarehouse(ctx, "manual_wh")
	if state.State != StateSuspending {
		t.Fatalf("state = %s", state.State)
	}
	if err := m.DropWarehouse(ctx, "manual_wh"); err == nil {
		t.Fatal("busy warehouse was dropped")
	}
	lease.Release()
	state, _ = m.GetWarehouse(ctx, "manual_wh")
	if state.State != StateSuspended {
		t.Fatalf("state after release = %s", state.State)
	}
}

func TestAutoSuspendCountsFromLastCompletion(t *testing.T) {
	m := NewManager()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	ctx := context.Background()
	if _, err := m.CreateWarehouseWithSettings(ctx, "auto_wh", "", Settings{Size: "X-SMALL", AutoResume: true, AutoSuspend: 10}); err != nil {
		t.Fatal(err)
	}
	lease, err := m.Acquire(ctx, "auto_wh", nil)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Hour)
	if err := m.CheckAutoSuspend(ctx); err != nil {
		t.Fatal(err)
	}
	state, _ := m.GetWarehouse(ctx, "auto_wh")
	if state.State != StateActive {
		t.Fatal("running warehouse auto-suspended")
	}
	lease.Release()
	now = now.Add(9 * time.Second)
	_ = m.CheckAutoSuspend(ctx)
	state, _ = m.GetWarehouse(ctx, "auto_wh")
	if state.State != StateActive {
		t.Fatal("warehouse suspended too early")
	}
	now = now.Add(time.Second)
	_ = m.CheckAutoSuspend(ctx)
	state, _ = m.GetWarehouse(ctx, "auto_wh")
	if state.State != StateSuspended {
		t.Fatalf("state = %s", state.State)
	}
}
