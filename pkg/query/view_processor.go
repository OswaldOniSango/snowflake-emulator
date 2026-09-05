package query

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

const viewTableType = "VIEW"

var (
	createViewSQLPattern = regexp.MustCompile(`(?is)^\s*CREATE\s+(OR\s+REPLACE\s+)?VIEW\s+([^\s(;]+)(?:\s*\([^)]*\))?\s+AS\s+.+;?\s*$`)
	dropViewSQLPattern   = regexp.MustCompile(`(?is)^\s*DROP\s+VIEW\s+(IF\s+EXISTS\s+)?([^\s;]+)\s*;?\s*$`)
)

func (e *Executor) executeCreateView(ctx context.Context, executionContext ExecutionContext, statement string) (*ExecResult, error) {
	match := createViewSQLPattern.FindStringSubmatch(trimLeadingComments(statement))
	if match == nil {
		return nil, fmt.Errorf("unsupported CREATE VIEW syntax")
	}
	databaseName, schemaName, viewName, err := resolveQualifiedObjectName(match[2], "view", executionContext)
	if err != nil {
		return nil, err
	}
	if err := e.validateObjectNamespace(ctx, databaseName, schemaName); err != nil {
		return nil, err
	}

	rewritten, err := e.rewriteTablesWithContext(ctx, executionContext, statement)
	if err != nil {
		return nil, err
	}
	translated, err := e.translator.Translate(rewritten)
	if err != nil {
		return nil, fmt.Errorf("translation error: %w", err)
	}
	if _, err := e.mgr.Exec(ctx, translated); err != nil {
		return nil, fmt.Errorf("create view execution error: %w", physicalNameError(err, executionContext))
	}

	database, err := e.repo.GetDatabaseByName(ctx, databaseName)
	if err != nil {
		return nil, err
	}
	schema, err := e.repo.GetSchemaByName(ctx, database.ID, schemaName)
	if err != nil {
		return nil, err
	}
	columns, err := e.describePhysicalTable(ctx, ExecutionContext{Database: databaseName, Schema: schemaName}, viewName)
	if err != nil {
		return nil, err
	}
	if _, err := e.repo.RegisterTable(ctx, schema.ID, viewName, viewTableType, columns); err != nil {
		return nil, err
	}
	return &ExecResult{}, nil
}

func (e *Executor) executeDropView(ctx context.Context, executionContext ExecutionContext, statement string) (*ExecResult, error) {
	match := dropViewSQLPattern.FindStringSubmatch(trimLeadingComments(statement))
	if match == nil {
		return nil, fmt.Errorf("unsupported DROP VIEW syntax")
	}
	databaseName, schemaName, viewName, err := resolveQualifiedObjectName(match[2], "view", executionContext)
	if err != nil {
		return nil, err
	}
	if err := e.validateObjectNamespace(ctx, databaseName, schemaName); err != nil {
		return nil, err
	}
	rewritten, err := e.rewriteTablesWithContext(ctx, executionContext, statement)
	if err != nil {
		return nil, err
	}
	if _, err := e.mgr.Exec(ctx, rewritten); err != nil {
		return nil, fmt.Errorf("drop view execution error: %w", physicalNameError(err, executionContext))
	}
	database, err := e.repo.GetDatabaseByName(ctx, databaseName)
	if err != nil {
		return nil, err
	}
	schema, err := e.repo.GetSchemaByName(ctx, database.ID, schemaName)
	if err != nil {
		return nil, err
	}
	if err := e.repo.DeleteTableMetadata(ctx, schema.ID, viewName); err != nil {
		return nil, err
	}
	return &ExecResult{}, nil
}

func (e *Executor) showViews(ctx context.Context, executionContext ExecutionContext, statement string) (*Result, error) {
	if !strings.EqualFold(strings.TrimSpace(strings.TrimSuffix(trimLeadingComments(statement), ";")), "SHOW VIEWS") {
		return nil, fmt.Errorf("unsupported SHOW VIEWS syntax")
	}
	if executionContext.Database == "" || executionContext.Schema == "" {
		return nil, fmt.Errorf("SHOW VIEWS requires database and schema context")
	}
	database, err := e.repo.GetDatabaseByName(ctx, executionContext.Database)
	if err != nil {
		return nil, err
	}
	schema, err := e.repo.GetSchemaByName(ctx, database.ID, executionContext.Schema)
	if err != nil {
		return nil, err
	}
	tables, err := e.repo.ListViews(ctx, schema.ID)
	if err != nil {
		return nil, err
	}
	columns := []string{columnName, columnDatabase, columnSchema, "kind"}
	result := &Result{Columns: columns, ColumnTypes: textColumnMetadata(columns)}
	for _, table := range tables {
		result.Rows = append(result.Rows, []interface{}{table.Name, database.Name, schema.Name, viewTableType})
	}
	result.TotalRows = len(result.Rows)
	return result, nil
}
