package metadata

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrIdentityNotFound = errors.New("identity object not found")
	ErrIdentityInUse    = errors.New("identity object is in use")
	ErrIdentityCycle    = errors.New("role grant would create a cycle")
)

const (
	identityRolesTable = "_metadata_roles"
	identityUsersTable = "_metadata_users"
)

type UserRecord struct {
	ID                 string
	Name               string
	PasswordHash       string
	DefaultRoleID      string
	Disabled           bool
	MustChangePassword bool
	Comment            string
	CreatedAt          time.Time
}

type RoleRecord struct {
	ID         string
	Name       string
	Comment    string
	SystemRole bool
	CreatedAt  time.Time
}

type RoleGrantRecord struct {
	ChildRoleID  string
	ParentRoleID string
	CreatedAt    time.Time
}

func normalizeIdentityName(name string) string { return strings.ToUpper(strings.TrimSpace(name)) }

func (r *Repository) CreateRoleRecord(ctx context.Context, name, comment string, system bool) (*RoleRecord, error) {
	name = normalizeIdentityName(name)
	if name == "" {
		return nil, fmt.Errorf("role name cannot be empty")
	}
	id := uuid.NewString()
	_, err := r.mgr.Exec(ctx, `INSERT INTO _metadata_roles (id, name, comment, system_role)
		VALUES (?, ?, ?, ?)`, id, name, comment, system)
	if err != nil {
		return nil, fmt.Errorf("create role %s: %w", name, err)
	}
	return r.GetRoleRecordByName(ctx, name)
}

func (r *Repository) GetRoleRecordByName(ctx context.Context, name string) (*RoleRecord, error) {
	var role RoleRecord
	err := r.mgr.QueryRow(ctx, `SELECT id, name, COALESCE(comment, ''), system_role, created_at
		FROM _metadata_roles WHERE name = ?`, normalizeIdentityName(name)).Scan(
		&role.ID, &role.Name, &role.Comment, &role.SystemRole, &role.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: role %s", ErrIdentityNotFound, normalizeIdentityName(name))
	}
	if err != nil {
		return nil, fmt.Errorf("get role: %w", err)
	}
	return &role, nil
}

func (r *Repository) ListRoleRecords(ctx context.Context) ([]RoleRecord, error) {
	rows, err := r.mgr.Query(ctx, `SELECT id, name, COALESCE(comment, ''), system_role, created_at
		FROM _metadata_roles ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var roles []RoleRecord
	for rows.Next() {
		var role RoleRecord
		if err := rows.Scan(&role.ID, &role.Name, &role.Comment, &role.SystemRole, &role.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan role: %w", err)
		}
		roles = append(roles, role)
	}
	return roles, rows.Err()
}

func (r *Repository) CreateUserRecord(ctx context.Context, user UserRecord) (*UserRecord, error) {
	user.Name = normalizeIdentityName(user.Name)
	if user.Name == "" || user.PasswordHash == "" {
		return nil, fmt.Errorf("user name and password hash are required")
	}
	if user.ID == "" {
		user.ID = uuid.NewString()
	}
	err := r.mgr.ExecTx(ctx, func(tx *sql.Tx) error {
		if user.DefaultRoleID != "" {
			exists, err := identityExists(ctx, tx, identityRolesTable, user.DefaultRoleID)
			if err != nil {
				return err
			}
			if !exists {
				return fmt.Errorf("%w: default role", ErrIdentityNotFound)
			}
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO _metadata_users
			(id, name, password_hash, default_role_id, disabled, must_change_password, comment)
			VALUES (?, ?, ?, NULLIF(?, ''), ?, ?, ?)`, user.ID, user.Name, user.PasswordHash,
			user.DefaultRoleID, user.Disabled, user.MustChangePassword, user.Comment)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("create user %s: %w", user.Name, err)
	}
	return r.GetUserRecordByName(ctx, user.Name)
}

// CreateUserWithRoleRecord atomically creates a user and assigns its default role.
func (r *Repository) CreateUserWithRoleRecord(ctx context.Context, user UserRecord) error {
	user.Name = normalizeIdentityName(user.Name)
	if user.Name == "" || user.PasswordHash == "" || user.DefaultRoleID == "" {
		return fmt.Errorf("user name, password hash, and default role are required")
	}
	if user.ID == "" {
		user.ID = uuid.NewString()
	}
	return r.mgr.ExecTx(ctx, func(tx *sql.Tx) error {
		if exists, err := identityExists(ctx, tx, identityRolesTable, user.DefaultRoleID); err != nil {
			return err
		} else if !exists {
			return fmt.Errorf("%w: default role", ErrIdentityNotFound)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO _metadata_users
			(id, name, password_hash, default_role_id, disabled, must_change_password, comment)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, user.ID, user.Name, user.PasswordHash, user.DefaultRoleID,
			user.Disabled, user.MustChangePassword, user.Comment); err != nil {
			return fmt.Errorf("create user %s: %w", user.Name, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO _metadata_user_role_grants (user_id, role_id)
			VALUES (?, ?)`, user.ID, user.DefaultRoleID); err != nil {
			return fmt.Errorf("grant default role: %w", err)
		}
		return nil
	})
}

func (r *Repository) GetUserRecordByName(ctx context.Context, name string) (*UserRecord, error) {
	var user UserRecord
	err := r.mgr.QueryRow(ctx, `SELECT id, name, password_hash, COALESCE(default_role_id, ''), disabled,
		must_change_password, COALESCE(comment, ''), created_at FROM _metadata_users WHERE name = ?`,
		normalizeIdentityName(name)).Scan(&user.ID, &user.Name, &user.PasswordHash, &user.DefaultRoleID,
		&user.Disabled, &user.MustChangePassword, &user.Comment, &user.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: user %s", ErrIdentityNotFound, normalizeIdentityName(name))
	}
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	return &user, nil
}

func (r *Repository) ListUserRecords(ctx context.Context) ([]UserRecord, error) {
	rows, err := r.mgr.Query(ctx, `SELECT id, name, password_hash, COALESCE(default_role_id, ''), disabled,
		must_change_password, COALESCE(comment, ''), created_at FROM _metadata_users ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var users []UserRecord
	for rows.Next() {
		var user UserRecord
		if err := rows.Scan(&user.ID, &user.Name, &user.PasswordHash, &user.DefaultRoleID, &user.Disabled,
			&user.MustChangePassword, &user.Comment, &user.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

func (r *Repository) UpdateUserRecord(ctx context.Context, user UserRecord) error {
	return r.mgr.ExecTx(ctx, func(tx *sql.Tx) error {
		if user.DefaultRoleID != "" {
			exists, err := identityExists(ctx, tx, identityRolesTable, user.DefaultRoleID)
			if err != nil {
				return err
			}
			if !exists {
				return fmt.Errorf("%w: default role", ErrIdentityNotFound)
			}
		}
		result, err := tx.ExecContext(ctx, `UPDATE _metadata_users SET password_hash = ?, default_role_id = NULLIF(?, ''),
			disabled = ?, must_change_password = ?, comment = ? WHERE id = ?`, user.PasswordHash,
			user.DefaultRoleID, user.Disabled, user.MustChangePassword, user.Comment, user.ID)
		if err != nil {
			return fmt.Errorf("update user %s: %w", user.Name, err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return fmt.Errorf("%w: user %s", ErrIdentityNotFound, user.Name)
		}
		return nil
	})
}

// UpdateUserConfigurationRecord atomically replaces every mutable user field
// and ensures the selected default role is granted in the same transaction.
func (r *Repository) UpdateUserConfigurationRecord(ctx context.Context, user UserRecord) error {
	return r.mgr.ExecTx(ctx, func(tx *sql.Tx) error {
		if exists, err := identityExists(ctx, tx, identityRolesTable, user.DefaultRoleID); err != nil {
			return err
		} else if !exists {
			return fmt.Errorf("%w: default role", ErrIdentityNotFound)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO _metadata_user_role_grants (user_id, role_id)
			VALUES (?, ?) ON CONFLICT (user_id, role_id) DO NOTHING`, user.ID, user.DefaultRoleID); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `UPDATE _metadata_users SET password_hash = ?, default_role_id = ?,
			disabled = ?, must_change_password = ?, comment = ? WHERE id = ?`, user.PasswordHash,
			user.DefaultRoleID, user.Disabled, user.MustChangePassword, user.Comment, user.ID)
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return fmt.Errorf("%w: user %s", ErrIdentityNotFound, user.Name)
		}
		return nil
	})
}

// SetDefaultUserRoleRecord atomically grants and selects a user's default role.
func (r *Repository) SetDefaultUserRoleRecord(ctx context.Context, userID, roleID string) error {
	return r.mgr.ExecTx(ctx, func(tx *sql.Tx) error {
		if exists, err := identityExists(ctx, tx, identityRolesTable, roleID); err != nil {
			return err
		} else if !exists {
			return fmt.Errorf("%w: role", ErrIdentityNotFound)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO _metadata_user_role_grants (user_id, role_id)
			VALUES (?, ?) ON CONFLICT (user_id, role_id) DO NOTHING`, userID, roleID); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `UPDATE _metadata_users SET default_role_id = ? WHERE id = ?`, roleID, userID)
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return fmt.Errorf("%w: user", ErrIdentityNotFound)
		}
		return nil
	})
}

func (r *Repository) DeleteUserRecord(ctx context.Context, id string) error {
	return r.mgr.ExecTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM _metadata_user_role_grants WHERE user_id = ?`, id); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `DELETE FROM _metadata_users WHERE id = ?`, id)
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return fmt.Errorf("%w: user", ErrIdentityNotFound)
		}
		return nil
	})
}

func (r *Repository) GrantRoleToUserRecord(ctx context.Context, userID, roleID string) error {
	return r.mgr.ExecTx(ctx, func(tx *sql.Tx) error {
		for table, id := range map[string]string{identityUsersTable: userID, identityRolesTable: roleID} {
			exists, err := identityExists(ctx, tx, table, id)
			if err != nil {
				return err
			}
			if !exists {
				return fmt.Errorf("%w: grant target", ErrIdentityNotFound)
			}
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO _metadata_user_role_grants (user_id, role_id) VALUES (?, ?)
			ON CONFLICT (user_id, role_id) DO NOTHING`, userID, roleID)
		return err
	})
}

func (r *Repository) RevokeRoleFromUserRecord(ctx context.Context, userID, roleID string) error {
	return r.mgr.ExecTx(ctx, func(tx *sql.Tx) error {
		var defaultRoleID string
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(default_role_id, '') FROM _metadata_users WHERE id = ?`, userID).Scan(&defaultRoleID); errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: user", ErrIdentityNotFound)
		} else if err != nil {
			return err
		}
		if defaultRoleID == roleID {
			return fmt.Errorf("%w: default role", ErrIdentityInUse)
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM _metadata_user_role_grants WHERE user_id = ? AND role_id = ?`, userID, roleID)
		return err
	})
}

func (r *Repository) ListDirectUserRoleIDs(ctx context.Context, userID string) ([]string, error) {
	rows, err := r.mgr.Query(ctx, `SELECT role_id FROM _metadata_user_role_grants WHERE user_id = ? ORDER BY role_id`, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r *Repository) GrantRoleToRoleRecord(ctx context.Context, childID, parentID string) error {
	return r.mgr.ExecTx(ctx, func(tx *sql.Tx) error {
		if childID == parentID {
			return ErrIdentityCycle
		}
		for _, id := range []string{childID, parentID} {
			exists, err := identityExists(ctx, tx, identityRolesTable, id)
			if err != nil {
				return err
			}
			if !exists {
				return fmt.Errorf("%w: role", ErrIdentityNotFound)
			}
		}
		var createsCycle bool
		err := tx.QueryRowContext(ctx, `WITH RECURSIVE ancestors(id) AS (
			SELECT parent_role_id FROM _metadata_role_role_grants WHERE child_role_id = ?
			UNION
			SELECT grant_row.parent_role_id FROM _metadata_role_role_grants grant_row
			JOIN ancestors ON grant_row.child_role_id = ancestors.id
		) SELECT COUNT(*) > 0 FROM ancestors WHERE id = ?`, parentID, childID).Scan(&createsCycle)
		if err != nil {
			return err
		}
		if createsCycle {
			return ErrIdentityCycle
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO _metadata_role_role_grants (child_role_id, parent_role_id)
			VALUES (?, ?) ON CONFLICT (child_role_id, parent_role_id) DO NOTHING`, childID, parentID)
		return err
	})
}

func (r *Repository) RevokeRoleFromRoleRecord(ctx context.Context, childID, parentID string) error {
	_, err := r.mgr.Exec(ctx, `DELETE FROM _metadata_role_role_grants WHERE child_role_id = ? AND parent_role_id = ?`, childID, parentID)
	return err
}

func (r *Repository) ListRoleGrantRecords(ctx context.Context) ([]RoleGrantRecord, error) {
	rows, err := r.mgr.Query(ctx, `SELECT child_role_id, parent_role_id, created_at FROM _metadata_role_role_grants`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var grants []RoleGrantRecord
	for rows.Next() {
		var g RoleGrantRecord
		if err := rows.Scan(&g.ChildRoleID, &g.ParentRoleID, &g.CreatedAt); err != nil {
			return nil, err
		}
		grants = append(grants, g)
	}
	return grants, rows.Err()
}

func (r *Repository) DeleteRoleRecord(ctx context.Context, id string) error {
	return r.mgr.ExecTx(ctx, func(tx *sql.Tx) error {
		var references int
		if err := tx.QueryRowContext(ctx, `SELECT
			(SELECT COUNT(*) FROM _metadata_users WHERE default_role_id = ?) +
			(SELECT COUNT(*) FROM _metadata_user_role_grants WHERE role_id = ?) +
			(SELECT COUNT(*) FROM _metadata_role_role_grants WHERE child_role_id = ? OR parent_role_id = ?) +
			(SELECT COUNT(*) FROM _metadata_warehouse_privilege_grants WHERE role_id = ?)`,
			id, id, id, id, id).Scan(&references); err != nil {
			return err
		}
		if references != 0 {
			return ErrIdentityInUse
		}
		result, err := tx.ExecContext(ctx, `DELETE FROM _metadata_roles WHERE id = ?`, id)
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return fmt.Errorf("%w: role", ErrIdentityNotFound)
		}
		return nil
	})
}

// BootstrapIdentity atomically creates missing system roles, their hierarchy,
// and the demo administrator only when no user exists.
func (r *Repository) BootstrapIdentity(ctx context.Context, roles []RoleRecord, hierarchy [][2]string, admin UserRecord) error {
	return r.mgr.ExecTx(ctx, func(tx *sql.Tx) error {
		for _, role := range roles {
			if _, err := tx.ExecContext(ctx, `INSERT INTO _metadata_roles (id, name, comment, system_role)
				VALUES (?, ?, ?, TRUE) ON CONFLICT (name) DO UPDATE SET
				comment = EXCLUDED.comment, system_role = TRUE`, role.ID, role.Name, role.Comment); err != nil {
				return err
			}
		}
		for _, edge := range hierarchy {
			if _, err := tx.ExecContext(ctx, `INSERT INTO _metadata_role_role_grants (child_role_id, parent_role_id)
				SELECT child.id, parent.id FROM _metadata_roles child, _metadata_roles parent
				WHERE child.name = ? AND parent.name = ? ON CONFLICT (child_role_id, parent_role_id) DO NOTHING`, edge[0], edge[1]); err != nil {
				return err
			}
		}
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM _metadata_users`).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			return nil
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO _metadata_users
			(id, name, password_hash, default_role_id, disabled, must_change_password, comment)
			SELECT ?, ?, ?, id, FALSE, FALSE, ? FROM _metadata_roles WHERE name = 'ACCOUNTADMIN'`,
			admin.ID, admin.Name, admin.PasswordHash, admin.Comment); err != nil {
			return err
		}
		var accountAdminID string
		if err := tx.QueryRowContext(ctx, `SELECT id FROM _metadata_roles WHERE name = 'ACCOUNTADMIN'`).Scan(&accountAdminID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO _metadata_user_role_grants (user_id, role_id) VALUES (?, ?)`, admin.ID, accountAdminID)
		return err
	})
}

func identityExists(ctx context.Context, tx *sql.Tx, table, id string) (bool, error) {
	query := fmt.Sprintf(`SELECT COUNT(*) > 0 FROM %s WHERE id = ?`, table) //nolint:gosec // table is an internal constant
	var exists bool
	if err := tx.QueryRowContext(ctx, query, id).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}
