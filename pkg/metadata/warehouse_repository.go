package metadata

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// WarehouseRecord is the durable portion of a virtual warehouse. Runtime
// counters and queues deliberately remain in the warehouse coordinator.
type WarehouseRecord struct {
	ID              string
	Name            string
	State           string
	Size            string
	Comment         string
	CreatedAt       time.Time
	Owner           string
	AutoResume      bool
	AutoSuspend     int
	LastResumedAt   *time.Time
	LastSuspendedAt *time.Time
	LastActivityAt  *time.Time
}

// UpsertWarehouse persists a warehouse's stable configuration and lifecycle.
func (r *Repository) UpsertWarehouse(ctx context.Context, value *WarehouseRecord) error {
	_, err := r.mgr.Exec(ctx, `INSERT INTO _metadata_warehouses
		(id, name, state, size, comment, created_at, owner, auto_resume, auto_suspend,
		 last_resumed_at, last_suspended_at, last_activity_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (name) DO UPDATE SET
		 state = EXCLUDED.state, size = EXCLUDED.size, comment = EXCLUDED.comment,
		 owner = EXCLUDED.owner, auto_resume = EXCLUDED.auto_resume,
		 auto_suspend = EXCLUDED.auto_suspend, last_resumed_at = EXCLUDED.last_resumed_at,
		 last_suspended_at = EXCLUDED.last_suspended_at, last_activity_at = EXCLUDED.last_activity_at`,
		value.ID, value.Name, value.State, value.Size, value.Comment, value.CreatedAt,
		value.Owner, value.AutoResume, value.AutoSuspend, value.LastResumedAt,
		value.LastSuspendedAt, value.LastActivityAt)
	if err != nil {
		return fmt.Errorf("persist warehouse %s: %w", value.Name, err)
	}
	return nil
}

// ListWarehouseRecords loads every persisted warehouse.
func (r *Repository) ListWarehouseRecords(ctx context.Context) ([]WarehouseRecord, error) {
	rows, err := r.mgr.Query(ctx, `SELECT id, name, state, size, COALESCE(comment, ''),
		created_at, COALESCE(owner, ''), auto_resume, auto_suspend,
		last_resumed_at, last_suspended_at, last_activity_at
		FROM _metadata_warehouses ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list warehouses: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var values []WarehouseRecord
	for rows.Next() {
		var value WarehouseRecord
		var resumed, suspended, activity sql.NullTime
		if err := rows.Scan(&value.ID, &value.Name, &value.State, &value.Size, &value.Comment,
			&value.CreatedAt, &value.Owner, &value.AutoResume, &value.AutoSuspend,
			&resumed, &suspended, &activity); err != nil {
			return nil, fmt.Errorf("scan warehouse: %w", err)
		}
		if resumed.Valid {
			value.LastResumedAt = &resumed.Time
		}
		if suspended.Valid {
			value.LastSuspendedAt = &suspended.Time
		}
		if activity.Valid {
			value.LastActivityAt = &activity.Time
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

// DeleteWarehouseRecord removes durable warehouse metadata.
func (r *Repository) DeleteWarehouseRecord(ctx context.Context, name string) error {
	result, err := r.mgr.Exec(ctx, `DELETE FROM _metadata_warehouses WHERE name = ?`, name)
	if err != nil {
		return fmt.Errorf("delete warehouse %s: %w", name, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("warehouse %s not found", name)
	}
	return nil
}
