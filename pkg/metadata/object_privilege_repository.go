package metadata

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

type ObjectPrivilegeRecord struct {
	RoleID, ObjectType, ObjectName, Privilege string
	CreatedAt                                 time.Time
}

func (r *Repository) GrantObjectPrivilegeRecord(ctx context.Context, roleID, objectType, objectName, privilege string) error {
	_, err := r.mgr.Exec(ctx, `INSERT INTO _metadata_object_privilege_grants
		(role_id, object_type, object_name, privilege) VALUES (?, ?, ?, ?)
		ON CONFLICT (role_id, object_type, object_name, privilege) DO NOTHING`, roleID,
		strings.ToUpper(objectType), strings.ToUpper(objectName), strings.ToUpper(privilege))
	return err
}

func (r *Repository) RevokeObjectPrivilegeRecord(ctx context.Context, roleID, objectType, objectName, privilege string) error {
	_, err := r.mgr.Exec(ctx, `DELETE FROM _metadata_object_privilege_grants
		WHERE role_id=? AND object_type=? AND object_name=? AND privilege=?`, roleID,
		strings.ToUpper(objectType), strings.ToUpper(objectName), strings.ToUpper(privilege))
	return err
}

func (r *Repository) ListObjectPrivilegeRecords(ctx context.Context) ([]ObjectPrivilegeRecord, error) {
	rows, err := r.mgr.Query(ctx, `SELECT role_id, object_type, object_name, privilege, created_at
		FROM _metadata_object_privilege_grants ORDER BY object_type, object_name, privilege, role_id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var result []ObjectPrivilegeRecord
	for rows.Next() {
		var value ObjectPrivilegeRecord
		if err := rows.Scan(&value.RoleID, &value.ObjectType, &value.ObjectName, &value.Privilege, &value.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

func (r *Repository) deleteObjectPrivileges(ctx context.Context, objectType, objectName string) error {
	_, err := r.mgr.Exec(ctx, `DELETE FROM _metadata_object_privilege_grants
		WHERE (object_type = ? AND object_name = ?)
		   OR object_name LIKE ?`, strings.ToUpper(objectType), strings.ToUpper(objectName), strings.ToUpper(objectName)+".%")
	return err
}

func deleteObjectPrivilegesTx(ctx context.Context, tx *sql.Tx, objectType, objectName string) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM _metadata_object_privilege_grants
		WHERE (object_type = ? AND object_name = ?) OR object_name LIKE ?`, strings.ToUpper(objectType), strings.ToUpper(objectName), strings.ToUpper(objectName)+".%")
	return err
}
