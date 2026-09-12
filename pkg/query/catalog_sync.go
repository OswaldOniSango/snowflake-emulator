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
	createDatabaseSQLPattern = regexp.MustCompile(`(?is)^\s*CREATE\s+(OR\s+REPLACE\s+)?DATABASE\s+(IF\s+NOT\s+EXISTS\s+)?([^\s;]+)(?:\s+COMMENT\s*=\s*'((?:''|[^'])*)')?\s*;?\s*$`)
	dropDatabaseSQLPattern   = regexp.MustCompile(`(?is)^\s*DROP\s+DATABASE\s+(IF\s+EXISTS\s+)?([^\s;]+)(?:\s+CASCADE|\s+RESTRICT)?\s*;?\s*$`)
	createSchemaSQLPattern   = regexp.MustCompile(`(?is)^\s*CREATE\s+(OR\s+REPLACE\s+)?SCHEMA\s+(IF\s+NOT\s+EXISTS\s+)?([^\s;]+)(?:\s+COMMENT\s*=\s*'((?:''|[^'])*)')?\s*;?\s*$`)
	dropSchemaSQLPattern     = regexp.MustCompile(`(?is)^\s*DROP\s+SCHEMA\s+(IF\s+EXISTS\s+)?([^\s;]+)(?:\s+CASCADE|\s+RESTRICT)?\s*;?\s*$`)
	createTableSQLPattern    = regexp.MustCompile(`(?is)^\s*CREATE\s+(?:OR\s+REPLACE\s+)?(?:(TEMP|TEMPORARY|TRANSIENT)\s+)?TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?([^\s(;]+)`)
	dropTableSQLPattern      = regexp.MustCompile(`(?is)^\s*DROP\s+TABLE\s+(?:IF\s+EXISTS\s+)?([^\s(;]+)`)
)

func (e *Executor) executeCreateDatabase(ctx context.Context, statement string) (*ExecResult, error) {
	match := createDatabaseSQLPattern.FindStringSubmatch(trimLeadingComments(statement))
	if match == nil {
		return nil, fmt.Errorf("unsupported CREATE DATABASE syntax")
	}
	if strings.TrimSpace(match[1]) != "" {
		return nil, fmt.Errorf("CREATE OR REPLACE DATABASE is not supported yet")
	}
	name := strings.ToUpper(strings.TrimSpace(match[3]))
	if strings.Contains(name, ".") {
		return nil, fmt.Errorf("invalid database name %s", match[3])
	}
	if _, err := e.repo.GetDatabaseByName(ctx, name); err == nil {
		if strings.TrimSpace(match[2]) != "" {
			return &ExecResult{}, nil
		}
		return nil, fmt.Errorf("database %s already exists", name)
	}
	comment := strings.ReplaceAll(match[4], "''", "'")
	if _, err := e.repo.CreateDatabase(ctx, name, comment); err != nil {
		return nil, err
	}
	return &ExecResult{}, nil
}

func (e *Executor) executeDropDatabase(ctx context.Context, statement string) (*ExecResult, error) {
	match := dropDatabaseSQLPattern.FindStringSubmatch(trimLeadingComments(statement))
	if match == nil {
		return nil, fmt.Errorf("unsupported DROP DATABASE syntax")
	}
	name := strings.ToUpper(strings.TrimSpace(match[2]))
	database, err := e.repo.GetDatabaseByName(ctx, name)
	if err != nil {
		if strings.TrimSpace(match[1]) != "" {
			return &ExecResult{}, nil
		}
		return nil, err
	}
	if err := e.repo.DropDatabase(ctx, database.ID); err != nil {
		return nil, err
	}
	return &ExecResult{}, nil
}

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
	if match == nil {
		return nil
	}
	tableType := baseTableType
	switch strings.ToUpper(match[1]) {
	case "TEMP", "TEMPORARY":
		return nil
	case transientTableType:
		tableType = transientTableType
	}
	nameParts := strings.Split(match[2], ".")
	if (len(nameParts) == 1 && (executionContext.Database == "" || executionContext.Schema == "")) ||
		(len(nameParts) == 2 && executionContext.Database == "") ||
		isPhysicalCatalogTableName(nameParts, executionContext) {
		// Context-free Execute calls may intentionally use DuckDB's already
		// physical DB.TABLE form. Preserve that low-level compatibility.
		return nil
	}
	databaseName, schemaName, tableName, err := resolveQualifiedObjectName(match[2], "table", executionContext)
	if err != nil {
		return err
	}

	database, err := e.repo.GetDatabaseByName(ctx, databaseName)
	if err != nil {
		return err
	}
	schema, err := e.repo.GetSchemaByName(ctx, database.ID, schemaName)
	if err != nil {
		return err
	}
	tableContext := executionContext
	tableContext.Database, tableContext.Schema = databaseName, schemaName
	columns, err := e.describePhysicalTable(ctx, tableContext, tableName)
	if err != nil {
		return err
	}
	_, err = e.repo.RegisterTable(ctx, schema.ID, tableName, tableType, columns)
	return err
}

func (e *Executor) unregisterSQLTable(ctx context.Context, executionContext ExecutionContext, statement string) error {
	match := dropTableSQLPattern.FindStringSubmatch(trimLeadingComments(statement))
	if match == nil {
		return nil
	}
	nameParts := strings.Split(match[1], ".")
	if (len(nameParts) == 1 && (executionContext.Database == "" || executionContext.Schema == "")) ||
		(len(nameParts) == 2 && executionContext.Database == "") ||
		isPhysicalCatalogTableName(nameParts, executionContext) {
		return nil
	}
	databaseName, schemaName, tableName, err := resolveQualifiedObjectName(match[1], "table", executionContext)
	if err != nil {
		return err
	}
	database, err := e.repo.GetDatabaseByName(ctx, databaseName)
	if err != nil {
		return err
	}
	schema, err := e.repo.GetSchemaByName(ctx, database.ID, schemaName)
	if err != nil {
		return err
	}
	return e.repo.DeleteTableMetadata(ctx, schema.ID, tableName)
}

func isPhysicalCatalogTableName(parts []string, executionContext ExecutionContext) bool {
	return len(parts) == 2 &&
		strings.EqualFold(parts[0], executionContext.Database) &&
		strings.HasPrefix(strings.ToUpper(parts[1]), strings.ToUpper(executionContext.Schema)+"_")
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
