package metadata

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// CreateFunction stores a SQL scalar function in the catalog.
// When replace is true, an existing function with the same schema and name is replaced.
func (r *Repository) CreateFunction(ctx context.Context, schemaID, name, arguments, returnType, body, comment string, replace bool) (*Function, error) {
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("function name cannot be empty")
	}
	if strings.TrimSpace(body) == "" {
		return nil, fmt.Errorf("function body cannot be empty")
	}

	normalizedName := strings.ToUpper(name)
	id := uuid.New().String()
	err := r.mgr.ExecTx(ctx, func(tx *sql.Tx) error {
		if replace {
			if _, err := tx.ExecContext(ctx, `DELETE FROM _metadata_functions WHERE schema_id = ? AND name = ?`, schemaID, normalizedName); err != nil {
				return fmt.Errorf("failed to replace function: %w", err)
			}
		}

		_, err := tx.ExecContext(ctx, `INSERT INTO _metadata_functions
			(id, schema_id, name, arguments, return_type, body, comment, created_at, owner)
			VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, ?)`,
			id, schemaID, normalizedName, arguments, strings.ToUpper(returnType), body, comment, "")
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE") || strings.Contains(err.Error(), "Constraint Error") {
				return fmt.Errorf("function %s already exists in schema", normalizedName)
			}
			return fmt.Errorf("failed to create function: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return r.GetFunction(ctx, id)
}

// GetFunction retrieves a function by ID.
func (r *Repository) GetFunction(ctx context.Context, id string) (*Function, error) {
	return scanFunction(r.mgr.QueryRow(ctx, `SELECT id, schema_id, name, arguments, return_type, body, comment, created_at, owner
		FROM _metadata_functions WHERE id = ?`, id), "function with ID %s not found", id)
}

// GetFunctionByName retrieves a function by schema and name.
func (r *Repository) GetFunctionByName(ctx context.Context, schemaID, name string) (*Function, error) {
	return scanFunction(r.mgr.QueryRow(ctx, `SELECT id, schema_id, name, arguments, return_type, body, comment, created_at, owner
		FROM _metadata_functions WHERE schema_id = ? AND name = ?`, schemaID, strings.ToUpper(name)), "function %s not found", name)
}

// FunctionExistsByName reports whether a function named name exists in the
// database.schema namespace — a name-based lookup for the SQL rewriter, which
// only has the Snowflake namespace text to go on, not a resolved schema ID.
func (r *Repository) FunctionExistsByName(ctx context.Context, database, schema, name string) (bool, error) {
	const query = `
		SELECT 1 FROM _metadata_functions f
		JOIN _metadata_schemas s ON f.schema_id = s.id
		JOIN _metadata_databases d ON s.database_id = d.id
		WHERE d.name = ? AND s.name = ? AND f.name = ?
		LIMIT 1`

	var found int
	err := r.mgr.QueryRow(ctx, query, strings.ToUpper(database), strings.ToUpper(schema), strings.ToUpper(name)).Scan(&found)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to check whether function %s exists: %w", name, err)
	}
	return true, nil
}

func scanFunction(row *sql.Row, notFoundFormat, value string) (*Function, error) {
	var function Function
	var comment, owner sql.NullString
	var createdAt sql.NullTime
	if err := row.Scan(&function.ID, &function.SchemaID, &function.Name, &function.Arguments, &function.ReturnType, &function.Body, &comment, &createdAt, &owner); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf(notFoundFormat, value)
		}
		return nil, fmt.Errorf("failed to get function: %w", err)
	}
	if comment.Valid {
		function.Comment = comment.String
	}
	if createdAt.Valid {
		function.CreatedAt = createdAt.Time
	}
	if owner.Valid {
		function.Owner = owner.String
	}
	return &function, nil
}

// ListFunctions retrieves functions, optionally limited to one schema.
func (r *Repository) ListFunctions(ctx context.Context, schemaID string) ([]*Function, error) {
	query := `SELECT id, schema_id, name, arguments, return_type, body, comment, created_at, owner FROM _metadata_functions`
	var args []any
	if schemaID != "" {
		query += sqlWhereSchemaID
		args = append(args, schemaID)
	}
	query += sqlOrderByName

	rows, err := r.mgr.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list functions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var functions []*Function
	for rows.Next() {
		var function Function
		var comment, owner sql.NullString
		var createdAt sql.NullTime
		if err := rows.Scan(&function.ID, &function.SchemaID, &function.Name, &function.Arguments, &function.ReturnType, &function.Body, &comment, &createdAt, &owner); err != nil {
			return nil, fmt.Errorf("failed to scan function: %w", err)
		}
		if comment.Valid {
			function.Comment = comment.String
		}
		if createdAt.Valid {
			function.CreatedAt = createdAt.Time
		}
		if owner.Valid {
			function.Owner = owner.String
		}
		functions = append(functions, &function)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating functions: %w", err)
	}
	return functions, nil
}

// ListFunctionNames returns the upper-cased names of every function defined
// in the database.schema namespace — a single round trip the SQL rewriter
// uses to build a local lookup set instead of a query per candidate call.
func (r *Repository) ListFunctionNames(ctx context.Context, database, schema string) ([]string, error) {
	const query = `
		SELECT f.name FROM _metadata_functions f
		JOIN _metadata_schemas s ON f.schema_id = s.id
		JOIN _metadata_databases d ON s.database_id = d.id
		WHERE d.name = ? AND s.name = ?`

	rows, err := r.mgr.Query(ctx, query, strings.ToUpper(database), strings.ToUpper(schema))
	if err != nil {
		return nil, fmt.Errorf("failed to list function names: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("failed to scan function name: %w", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating function names: %w", err)
	}
	return names, nil
}

// DropFunction removes a function from the catalog.
func (r *Repository) DropFunction(ctx context.Context, schemaID, name string, ifExists bool) error {
	result, err := r.mgr.Exec(ctx, `DELETE FROM _metadata_functions WHERE schema_id = ? AND name = ?`, schemaID, strings.ToUpper(name))
	if err != nil {
		return fmt.Errorf("failed to drop function: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 && !ifExists {
		return fmt.Errorf("function %s not found", name)
	}
	return nil
}
