package metadata

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// CreateStream stores a stream definition and its initial source-table offset.
func (r *Repository) CreateStream(ctx context.Context, schemaID, name, sourceDatabase, sourceSchema, sourceTable, streamType string, offset int64, replace bool) (*Stream, error) {
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("stream name cannot be empty")
	}
	if strings.TrimSpace(sourceTable) == "" {
		return nil, fmt.Errorf("stream source table cannot be empty")
	}
	if streamType == "" {
		streamType = "APPEND_ONLY"
	}

	normalizedName := strings.ToUpper(name)
	id := uuid.New().String()
	err := r.mgr.ExecTx(ctx, func(tx *sql.Tx) error {
		if replace {
			if _, err := tx.ExecContext(ctx, `DELETE FROM _metadata_streams WHERE schema_id = ? AND name = ?`, schemaID, normalizedName); err != nil {
				return fmt.Errorf("failed to replace stream: %w", err)
			}
		}

		_, err := tx.ExecContext(ctx, `INSERT INTO _metadata_streams
			(id, schema_id, name, source_database, source_schema, source_table, stream_type, stream_offset, created_at, owner)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, ?)`,
			id, schemaID, normalizedName, strings.ToUpper(sourceDatabase), strings.ToUpper(sourceSchema), strings.ToUpper(sourceTable), strings.ToUpper(streamType), offset, "")
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE") || strings.Contains(err.Error(), "Constraint Error") {
				return fmt.Errorf("stream %s already exists in schema", normalizedName)
			}
			return fmt.Errorf("failed to create stream: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return r.GetStream(ctx, id)
}

// GetStream retrieves a stream by ID.
func (r *Repository) GetStream(ctx context.Context, id string) (*Stream, error) {
	return scanStream(r.mgr.QueryRow(ctx, `SELECT id, schema_id, name, source_database, source_schema, source_table, stream_type, stream_offset, created_at, owner
		FROM _metadata_streams WHERE id = ?`, id), "stream with ID %s not found", id)
}

// GetStreamByName retrieves a stream by schema and name.
func (r *Repository) GetStreamByName(ctx context.Context, schemaID, name string) (*Stream, error) {
	return scanStream(r.mgr.QueryRow(ctx, `SELECT id, schema_id, name, source_database, source_schema, source_table, stream_type, stream_offset, created_at, owner
		FROM _metadata_streams WHERE schema_id = ? AND name = ?`, schemaID, strings.ToUpper(name)), "stream %s not found", name)
}

func scanStream(row *sql.Row, notFoundFormat, value string) (*Stream, error) {
	var stream Stream
	var createdAt sql.NullTime
	var owner sql.NullString
	if err := row.Scan(&stream.ID, &stream.SchemaID, &stream.Name, &stream.SourceDatabase, &stream.SourceSchema, &stream.SourceTable, &stream.StreamType, &stream.Offset, &createdAt, &owner); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf(notFoundFormat, value)
		}
		return nil, fmt.Errorf("failed to get stream: %w", err)
	}
	if createdAt.Valid {
		stream.CreatedAt = createdAt.Time
	}
	if owner.Valid {
		stream.Owner = owner.String
	}
	return &stream, nil
}

// ListStreams retrieves streams, optionally limited to one schema.
func (r *Repository) ListStreams(ctx context.Context, schemaID string) ([]*Stream, error) {
	query := `SELECT id, schema_id, name, source_database, source_schema, source_table, stream_type, stream_offset, created_at, owner FROM _metadata_streams`
	var args []any
	if schemaID != "" {
		query += ` WHERE schema_id = ?`
		args = append(args, schemaID)
	}
	query += ` ORDER BY name`

	rows, err := r.mgr.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list streams: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var streams []*Stream
	for rows.Next() {
		var stream Stream
		var createdAt sql.NullTime
		var owner sql.NullString
		if err := rows.Scan(&stream.ID, &stream.SchemaID, &stream.Name, &stream.SourceDatabase, &stream.SourceSchema, &stream.SourceTable, &stream.StreamType, &stream.Offset, &createdAt, &owner); err != nil {
			return nil, fmt.Errorf("failed to scan stream: %w", err)
		}
		if createdAt.Valid {
			stream.CreatedAt = createdAt.Time
		}
		if owner.Valid {
			stream.Owner = owner.String
		}
		streams = append(streams, &stream)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating streams: %w", err)
	}
	return streams, nil
}

// DropStream removes a stream definition from the catalog.
func (r *Repository) DropStream(ctx context.Context, schemaID, name string, ifExists bool) error {
	result, err := r.mgr.Exec(ctx, `DELETE FROM _metadata_streams WHERE schema_id = ? AND name = ?`, schemaID, strings.ToUpper(name))
	if err != nil {
		return fmt.Errorf("failed to drop stream: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 && !ifExists {
		return fmt.Errorf("stream %s not found", name)
	}
	return nil
}
