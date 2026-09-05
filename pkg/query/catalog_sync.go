package query

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"

	"github.com/nnnkkk7/snowflake-emulator/pkg/metadata"
)

const (
	baseTableType      = "BASE TABLE"
	transientTableType = "TRANSIENT"
)

var (
	createSchemaSQLPattern = regexp.MustCompile(`(?is)^\s*CREATE\s+(OR\s+REPLACE\s+)?SCHEMA\s+(IF\s+NOT\s+EXISTS\s+)?([^\s;]+)(?:\s+COMMENT\s*=\s*'((?:''|[^'])*)')?\s*;?\s*$`)
	dropSchemaSQLPattern   = regexp.MustCompile(`(?is)^\s*DROP\s+SCHEMA\s+(IF\s+EXISTS\s+)?([^\s;]+)(?:\s+CASCADE|\s+RESTRICT)?\s*;?\s*$`)
	createTableSQLPattern  = regexp.MustCompile(`(?is)^\s*CREATE\s+(?:OR\s+REPLACE\s+)?(?:(TEMP|TEMPORARY|TRANSIENT)\s+)?TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?([^\s(;]+)`)
	dropTableSQLPattern    = regexp.MustCompile(`(?is)^\s*DROP\s+TABLE\s+(?:IF\s+EXISTS\s+)?([^\s(;]+)`)
)

func (e *Executor) executeCreateSchema(ctx context.Context, executionContext ExecutionContext, statement string) (*ExecResult, error) {
	match := createSchemaSQLPattern.FindStringSubmatch(trimLeadingComments(statement))
	if match == nil {
		return nil, fmt.Errorf("unsupported CREATE SCHEMA syntax")
	}
	if strings.TrimSpace(match[1]) != "" {
		return nil, fmt.Errorf("CREATE OR REPLACE SCHEMA is not supported yet")
	}

	databaseName, schemaName, err := resolveSchemaName(match[3], executionContext)
	if err != nil {
		return nil, err
	}
	database, err := e.repo.GetDatabaseByName(ctx, databaseName)
	if err != nil {
		return nil, err
	}
	if _, err := e.repo.GetSchemaByName(ctx, database.ID, schemaName); err == nil {
		if strings.TrimSpace(match[2]) != "" {
			return &ExecResult{}, nil
		}
		return nil, fmt.Errorf("schema %s already exists in database %s", schemaName, databaseName)
	}

	comment := strings.ReplaceAll(match[4], "''", "'")
	if _, err := e.repo.CreateSchema(ctx, database.ID, schemaName, comment); err != nil {
		return nil, err
	}
	return &ExecResult{}, nil
}

func (e *Executor) executeDropSchema(ctx context.Context, executionContext ExecutionContext, statement string) (*ExecResult, error) {
	match := dropSchemaSQLPattern.FindStringSubmatch(trimLeadingComments(statement))
	if match == nil {
		return nil, fmt.Errorf("unsupported DROP SCHEMA syntax")
	}
	databaseName, schemaName, err := resolveSchemaName(match[2], executionContext)
	if err != nil {
		return nil, err
	}
	database, err := e.repo.GetDatabaseByName(ctx, databaseName)
	if err != nil {
		return nil, err
	}
	schema, err := e.repo.GetSchemaByName(ctx, database.ID, schemaName)
	if err != nil {
		if strings.TrimSpace(match[1]) != "" {
			return &ExecResult{}, nil
		}
		return nil, err
	}
	if err := e.repo.DropSchema(ctx, schema.ID); err != nil {
		return nil, err
	}
	return &ExecResult{}, nil
}

func resolveSchemaName(name string, executionContext ExecutionContext) (string, string, error) {
	parts := strings.Split(strings.TrimSpace(name), ".")
	switch len(parts) {
	case 1:
		if executionContext.Database == "" {
			return "", "", fmt.Errorf("CREATE SCHEMA %s requires a database context", name)
		}
		return strings.ToUpper(executionContext.Database), strings.ToUpper(parts[0]), nil
	case 2:
		return strings.ToUpper(parts[0]), strings.ToUpper(parts[1]), nil
	default:
		return "", "", fmt.Errorf("invalid schema name %s", name)
	}
}

func (e *Executor) registerSQLTable(ctx context.Context, executionContext ExecutionContext, statement string) error {
	match := createTableSQLPattern.FindStringSubmatch(trimLeadingComments(statement))
	if match == nil || executionContext.Database == "" || executionContext.Schema == "" || strings.Contains(match[2], ".") {
		return nil
	}
	tableName := strings.ToUpper(match[2])
	tableType := baseTableType
	switch strings.ToUpper(match[1]) {
	case "TEMP", "TEMPORARY":
		return nil
	case transientTableType:
		tableType = transientTableType
	}

	database, err := e.repo.GetDatabaseByName(ctx, executionContext.Database)
	if err != nil {
		return err
	}
	schema, err := e.repo.GetSchemaByName(ctx, database.ID, executionContext.Schema)
	if err != nil {
		return err
	}
	columns, err := e.describePhysicalTable(ctx, executionContext, tableName)
	if err != nil {
		return err
	}
	_, err = e.repo.RegisterTable(ctx, schema.ID, tableName, tableType, columns)
	return err
}

func (e *Executor) unregisterSQLTable(ctx context.Context, executionContext ExecutionContext, statement string) error {
	match := dropTableSQLPattern.FindStringSubmatch(trimLeadingComments(statement))
	if match == nil || executionContext.Database == "" || executionContext.Schema == "" || strings.Contains(match[1], ".") {
		return nil
	}
	database, err := e.repo.GetDatabaseByName(ctx, executionContext.Database)
	if err != nil {
		return err
	}
	schema, err := e.repo.GetSchemaByName(ctx, database.ID, executionContext.Schema)
	if err != nil {
		return err
	}
	return e.repo.DeleteTableMetadata(ctx, schema.ID, match[1])
}

func (e *Executor) describePhysicalTable(ctx context.Context, executionContext ExecutionContext, tableName string) ([]metadata.ColumnDef, error) {
	rows, err := e.mgr.Query(ctx, `SELECT column_name, data_type, is_nullable, column_default
		FROM duckdb_columns()
		WHERE schema_name = ? AND table_name = ?
		ORDER BY column_index`, strings.ToUpper(executionContext.Database), strings.ToUpper(executionContext.Schema)+"_"+tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect table %s: %w", tableName, err)
	}
	defer func() { _ = rows.Close() }()

	var columns []metadata.ColumnDef
	for rows.Next() {
		var column metadata.ColumnDef
		var defaultValue sql.NullString
		if err := rows.Scan(&column.Name, &column.Type, &column.Nullable, &defaultValue); err != nil {
			return nil, fmt.Errorf("failed to inspect table %s column: %w", tableName, err)
		}
		column.Name = strings.ToUpper(column.Name)
		column.Type = strings.ToUpper(column.Type)
		if defaultValue.Valid {
			column.Default = &defaultValue.String
		}
		columns = append(columns, column)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to inspect table %s columns: %w", tableName, err)
	}
	if len(columns) == 0 {
		return nil, fmt.Errorf("table %s was created but its columns could not be inspected", tableName)
	}
	return columns, nil
}
