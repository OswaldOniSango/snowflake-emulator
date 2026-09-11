package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nnnkkk7/snowflake-emulator/pkg/connection"
)

// Store provides persistent storage for sessions using DuckDB.
type Store struct {
	mgr *connection.Manager
}

// NewStore creates a new session store with DuckDB backend.
func NewStore(mgr *connection.Manager) (*Store, error) {
	store := &Store{
		mgr: mgr,
	}

	// Initialize sessions table
	if err := store.initTable(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to initialize sessions table: %w", err)
	}

	return store, nil
}

// initTable creates the sessions table if it doesn't exist.
func (s *Store) initTable(ctx context.Context) error {
	createTableSQL := `
		CREATE TABLE IF NOT EXISTS _sessions (
			token VARCHAR PRIMARY KEY,
			id VARCHAR NOT NULL,
			username VARCHAR NOT NULL,
			database_name VARCHAR NOT NULL,
			current_schema VARCHAR NOT NULL,
			created_at TIMESTAMP NOT NULL,
			last_accessed_at TIMESTAMP NOT NULL,
			expires_at TIMESTAMP NOT NULL,
			parameters VARCHAR
		)
	`
	if _, err := s.mgr.Exec(ctx, createTableSQL); err != nil {
		return err
	}
	for _, migration := range []string{
		`ALTER TABLE _sessions ADD COLUMN IF NOT EXISTS master_token VARCHAR`,
		`ALTER TABLE _sessions ADD COLUMN IF NOT EXISTS user_id VARCHAR`,
		`ALTER TABLE _sessions ADD COLUMN IF NOT EXISTS active_role_id VARCHAR`,
		`ALTER TABLE _sessions ADD COLUMN IF NOT EXISTS active_role VARCHAR`,
		`ALTER TABLE _sessions ADD COLUMN IF NOT EXISTS warehouse VARCHAR`,
		`ALTER TABLE _sessions ADD COLUMN IF NOT EXISTS validity_seconds BIGINT`,
		`ALTER TABLE _sessions ADD COLUMN IF NOT EXISTS master_validity_seconds BIGINT`,
	} {
		if _, err := s.mgr.Exec(ctx, migration); err != nil {
			return fmt.Errorf("migrate sessions: %w", err)
		}
	}
	return nil
}

// Save saves a session to persistent storage.
func (s *Store) Save(ctx context.Context, session *Session) error {
	// Serialize parameters to JSON
	paramsJSON, err := json.Marshal(session.Parameters)
	if err != nil {
		return fmt.Errorf("failed to marshal parameters: %w", err)
	}

	// Use INSERT OR REPLACE to handle both insert and update
	insertSQL := `
		INSERT OR REPLACE INTO _sessions (
			token, id, username, database_name, current_schema,
			created_at, last_accessed_at, expires_at, parameters,
			master_token, user_id, active_role_id, active_role, warehouse,
			validity_seconds, master_validity_seconds
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err = s.mgr.Exec(ctx, insertSQL,
		session.Token,
		session.ID,
		session.Username,
		session.Database,
		session.CurrentSchema,
		session.CreatedAt,
		session.LastAccessedAt,
		session.ExpiresAt,
		string(paramsJSON),
		session.MasterToken,
		session.UserID,
		session.ActiveRoleID,
		session.ActiveRole,
		session.Warehouse,
		session.ValidityInSeconds,
		session.MasterValidityInSeconds,
	)
	if err != nil {
		return fmt.Errorf("failed to save session: %w", err)
	}

	return nil
}

// Load loads a session from persistent storage.
func (s *Store) Load(ctx context.Context, token string) (*Session, error) {
	selectSQL := `
		SELECT id, username, database_name, current_schema,
			   created_at, last_accessed_at, expires_at, parameters,
			   COALESCE(master_token, ''), COALESCE(user_id, ''),
			   COALESCE(active_role_id, ''), COALESCE(active_role, ''),
			   COALESCE(warehouse, ''), COALESCE(validity_seconds, 0),
			   COALESCE(master_validity_seconds, 0)
		FROM _sessions
		WHERE token = ?
	`

	var session Session
	var paramsJSON string

	err := s.mgr.QueryRow(ctx, selectSQL, token).Scan(
		&session.ID,
		&session.Username,
		&session.Database,
		&session.CurrentSchema,
		&session.CreatedAt,
		&session.LastAccessedAt,
		&session.ExpiresAt,
		&paramsJSON,
		&session.MasterToken,
		&session.UserID,
		&session.ActiveRoleID,
		&session.ActiveRole,
		&session.Warehouse,
		&session.ValidityInSeconds,
		&session.MasterValidityInSeconds,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("session not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to load session: %w", err)
	}

	session.Token = token

	// Deserialize parameters
	if err := json.Unmarshal([]byte(paramsJSON), &session.Parameters); err != nil {
		return nil, fmt.Errorf("failed to unmarshal parameters: %w", err)
	}

	return &session, nil
}

// Delete deletes a session from persistent storage.
func (s *Store) Delete(ctx context.Context, token string) error {
	deleteSQL := `DELETE FROM _sessions WHERE token = ?`
	_, err := s.mgr.Exec(ctx, deleteSQL, token)
	return err
}

// Touch durably records session activity without rewriting the full row.
func (s *Store) Touch(ctx context.Context, token string, accessedAt time.Time) error {
	_, err := s.mgr.Exec(ctx, `UPDATE _sessions SET last_accessed_at = ? WHERE token = ?`, accessedAt, token)
	return err
}

// ReplaceToken atomically persists a renewed session and removes its old token.
func (s *Store) ReplaceToken(ctx context.Context, oldToken string, replacement *Session) error {
	paramsJSON, err := json.Marshal(replacement.Parameters)
	if err != nil {
		return fmt.Errorf("failed to marshal parameters: %w", err)
	}
	return s.mgr.ExecTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO _sessions (
			token, id, username, database_name, current_schema, created_at,
			last_accessed_at, expires_at, parameters, master_token, user_id,
			active_role_id, active_role, warehouse, validity_seconds,
			master_validity_seconds
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			replacement.Token, replacement.ID, replacement.Username, replacement.Database,
			replacement.CurrentSchema, replacement.CreatedAt, replacement.LastAccessedAt,
			replacement.ExpiresAt, string(paramsJSON), replacement.MasterToken,
			replacement.UserID, replacement.ActiveRoleID, replacement.ActiveRole,
			replacement.Warehouse, replacement.ValidityInSeconds,
			replacement.MasterValidityInSeconds)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM _sessions WHERE token = ?`, oldToken)
		return err
	})
}

// DeleteExpired deletes all expired sessions and returns the count.
func (s *Store) DeleteExpired(ctx context.Context) (int, error) {
	// First count expired sessions
	countSQL := `SELECT COUNT(*) FROM _sessions WHERE expires_at < ?`
	var count int64
	err := s.mgr.QueryRow(ctx, countSQL, time.Now()).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count expired sessions: %w", err)
	}

	// Delete expired sessions
	deleteSQL := `DELETE FROM _sessions WHERE expires_at < ?`
	_, err = s.mgr.Exec(ctx, deleteSQL, time.Now())
	if err != nil {
		return 0, fmt.Errorf("failed to delete expired sessions: %w", err)
	}

	return int(count), nil
}

// ListAll returns all sessions from storage.
func (s *Store) ListAll(ctx context.Context) ([]*Session, error) {
	selectSQL := `
		SELECT token, id, username, database_name, current_schema,
			   created_at, last_accessed_at, expires_at, parameters,
			   COALESCE(master_token, ''), COALESCE(user_id, ''),
			   COALESCE(active_role_id, ''), COALESCE(active_role, ''),
			   COALESCE(warehouse, ''), COALESCE(validity_seconds, 0),
			   COALESCE(master_validity_seconds, 0)
		FROM _sessions
		ORDER BY created_at DESC
	`

	rows, err := s.mgr.Query(ctx, selectSQL)
	if err != nil {
		return nil, fmt.Errorf("failed to query sessions: %w", err)
	}
	defer rows.Close()

	var sessions []*Session
	for rows.Next() {
		var session Session
		var paramsJSON string

		err := rows.Scan(
			&session.Token,
			&session.ID,
			&session.Username,
			&session.Database,
			&session.CurrentSchema,
			&session.CreatedAt,
			&session.LastAccessedAt,
			&session.ExpiresAt,
			&paramsJSON,
			&session.MasterToken,
			&session.UserID,
			&session.ActiveRoleID,
			&session.ActiveRole,
			&session.Warehouse,
			&session.ValidityInSeconds,
			&session.MasterValidityInSeconds,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan session: %w", err)
		}

		// Deserialize parameters
		if err := json.Unmarshal([]byte(paramsJSON), &session.Parameters); err != nil {
			return nil, fmt.Errorf("failed to unmarshal parameters: %w", err)
		}

		sessions = append(sessions, &session)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return sessions, nil
}

// NewManagerWithStore creates a session manager that uses persistent storage.
func NewManagerWithStore(sessionTimeout time.Duration, store *Store) *Manager {
	mgr := NewManager(sessionTimeout)
	mgr.store = store
	return mgr
}

// NewPersistentManager restores unexpired sessions and their master tokens.
func NewPersistentManager(ctx context.Context, sessionTimeout time.Duration, store *Store) (*Manager, error) {
	mgr := NewManagerWithStore(sessionTimeout, store)
	sessions, err := store.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	for _, value := range sessions {
		if now.After(value.ExpiresAt) {
			continue
		}
		if value.ValidityInSeconds == 0 {
			value.ValidityInSeconds = int64(sessionTimeout.Seconds())
		}
		if value.MasterValidityInSeconds == 0 {
			value.MasterValidityInSeconds = int64(sessionTimeout.Seconds()) * 4
		}
		mgr.sessions[value.Token] = value
		if value.MasterToken != "" {
			mgr.masterTokens[value.MasterToken] = value
		}
	}
	return mgr, nil
}
