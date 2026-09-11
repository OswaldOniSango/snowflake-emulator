package metadata

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// WarehousePrivilegeRecord is one direct privilege granted to a role.
type WarehousePrivilegeRecord struct {
	RoleID        string
	WarehouseName string
	Privilege     string
	CreatedAt     time.Time
}

func (r *Repository) GrantWarehousePrivilegeRecord(ctx context.Context, roleID, warehouseName, privilege string) error {
	return r.mgr.ExecTx(ctx, func(tx *sql.Tx) error {
		var roleExists, warehouseExists bool
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) > 0 FROM _metadata_roles WHERE id = ?`, roleID).Scan(&roleExists); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) > 0 FROM _metadata_warehouses WHERE name = ?`, strings.ToUpper(warehouseName)).Scan(&warehouseExists); err != nil {
			return err
		}
		if !roleExists || !warehouseExists {
			return fmt.Errorf("%w: privilege target", ErrIdentityNotFound)
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO _metadata_warehouse_privilege_grants
			(role_id, warehouse_name, privilege) VALUES (?, ?, ?)
			ON CONFLICT (role_id, warehouse_name, privilege) DO NOTHING`, roleID,
			strings.ToUpper(warehouseName), strings.ToUpper(privilege))
		return err
	})
}

func (r *Repository) RevokeWarehousePrivilegeRecord(ctx context.Context, roleID, warehouseName, privilege string) error {
	result, err := r.mgr.Exec(ctx, `DELETE FROM _metadata_warehouse_privilege_grants
		WHERE role_id = ? AND warehouse_name = ? AND privilege = ?`, roleID,
		strings.ToUpper(warehouseName), strings.ToUpper(privilege))
	if err != nil {
		return err
	}
	_, err = result.RowsAffected()
	return err
}

func (r *Repository) ListWarehousePrivilegeRecords(ctx context.Context) ([]WarehousePrivilegeRecord, error) {
	rows, err := r.mgr.Query(ctx, `SELECT role_id, warehouse_name, privilege, created_at
		FROM _metadata_warehouse_privilege_grants ORDER BY warehouse_name, privilege, role_id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var records []WarehousePrivilegeRecord
	for rows.Next() {
		var record WarehousePrivilegeRecord
		if err := rows.Scan(&record.RoleID, &record.WarehouseName, &record.Privilege, &record.CreatedAt); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return records, nil
}
