package metadata

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// CreateProcedure stores a SQL procedure in the catalog.
// When replace is true, an existing procedure with the same schema and name is replaced.
func (r *Repository) CreateProcedure(ctx context.Context, schemaID, name, arguments, returnType, language, body, comment string, replace bool) (*Procedure, error) {
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("procedure name cannot be empty")
	}
	if strings.TrimSpace(body) == "" {
		return nil, fmt.Errorf("procedure body cannot be empty")
	}

	normalizedName := strings.ToUpper(name)
	normalizedLanguage := strings.ToUpper(language)
	if normalizedLanguage != "SQL" {
		return nil, fmt.Errorf("unsupported procedure language %s: only SQL is supported", language)
	}

	id := uuid.New().String()
	err := r.mgr.ExecTx(ctx, func(tx *sql.Tx) error {
		if replace {
			if _, err := tx.ExecContext(ctx, `DELETE FROM _metadata_procedures WHERE schema_id = ? AND name = ?`, schemaID, normalizedName); err != nil {
				return fmt.Errorf("failed to replace procedure: %w", err)
			}
		}

		_, err := tx.ExecContext(ctx, `INSERT INTO _metadata_procedures
			(id, schema_id, name, arguments, return_type, language, body, comment, created_at, owner)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, ?)`,
			id, schemaID, normalizedName, arguments, strings.ToUpper(returnType), normalizedLanguage, body, comment, "")
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE") || strings.Contains(err.Error(), "Constraint Error") {
				return fmt.Errorf("procedure %s already exists in schema", normalizedName)
			}
			return fmt.Errorf("failed to create procedure: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return r.GetProcedure(ctx, id)
}

// GetProcedure retrieves a procedure by ID.
func (r *Repository) GetProcedure(ctx context.Context, id string) (*Procedure, error) {
	return scanProcedure(r.mgr.QueryRow(ctx, `SELECT id, schema_id, name, arguments, return_type, language, body, comment, created_at, owner
		FROM _metadata_procedures WHERE id = ?`, id), "procedure with ID %s not found", id)
}

// GetProcedureByName retrieves a procedure by schema and name.
func (r *Repository) GetProcedureByName(ctx context.Context, schemaID, name string) (*Procedure, error) {
	return scanProcedure(r.mgr.QueryRow(ctx, `SELECT id, schema_id, name, arguments, return_type, language, body, comment, created_at, owner
		FROM _metadata_procedures WHERE schema_id = ? AND name = ?`, schemaID, strings.ToUpper(name)), "procedure %s not found", name)
}

func scanProcedure(row *sql.Row, notFoundFormat, value string) (*Procedure, error) {
	var procedure Procedure
	var comment, owner sql.NullString
	var createdAt sql.NullTime
	if err := row.Scan(&procedure.ID, &procedure.SchemaID, &procedure.Name, &procedure.Arguments, &procedure.ReturnType, &procedure.Language, &procedure.Body, &comment, &createdAt, &owner); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf(notFoundFormat, value)
		}
		return nil, fmt.Errorf("failed to get procedure: %w", err)
	}
	if comment.Valid {
		procedure.Comment = comment.String
	}
	if createdAt.Valid {
		procedure.CreatedAt = createdAt.Time
	}
	if owner.Valid {
		procedure.Owner = owner.String
	}
	return &procedure, nil
}

// ListProcedures retrieves procedures, optionally limited to one schema.
func (r *Repository) ListProcedures(ctx context.Context, schemaID string) ([]*Procedure, error) {
	query := `SELECT id, schema_id, name, arguments, return_type, language, body, comment, created_at, owner FROM _metadata_procedures`
	var args []any
	if schemaID != "" {
		query += sqlWhereSchemaID
		args = append(args, schemaID)
	}
	query += sqlOrderByName

	rows, err := r.mgr.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list procedures: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var procedures []*Procedure
	for rows.Next() {
		var procedure Procedure
		var comment, owner sql.NullString
		var createdAt sql.NullTime
		if err := rows.Scan(&procedure.ID, &procedure.SchemaID, &procedure.Name, &procedure.Arguments, &procedure.ReturnType, &procedure.Language, &procedure.Body, &comment, &createdAt, &owner); err != nil {
			return nil, fmt.Errorf("failed to scan procedure: %w", err)
		}
		if comment.Valid {
			procedure.Comment = comment.String
		}
		if createdAt.Valid {
			procedure.CreatedAt = createdAt.Time
		}
		if owner.Valid {
			procedure.Owner = owner.String
		}
		procedures = append(procedures, &procedure)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating procedures: %w", err)
	}
	return procedures, nil
}

// DropProcedure removes a procedure from the catalog.
func (r *Repository) DropProcedure(ctx context.Context, schemaID, name string, ifExists bool) error {
	result, err := r.mgr.Exec(ctx, `DELETE FROM _metadata_procedures WHERE schema_id = ? AND name = ?`, schemaID, strings.ToUpper(name))
	if err != nil {
		return fmt.Errorf("failed to drop procedure: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 && !ifExists {
		return fmt.Errorf("procedure %s not found", name)
	}
	return nil
}
