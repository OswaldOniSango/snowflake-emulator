package query

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/nnnkkk7/snowflake-emulator/pkg/metadata"
)

var (
	createFunctionPattern = regexp.MustCompile(`(?is)^CREATE\s+(OR\s+REPLACE\s+)?FUNCTION\s+([^\s(]+)\s*\((.*?)\)\s+RETURNS\s+([^\s]+(?:\s*\([^)]*\))?)\s*(?:LANGUAGE\s+(\w+)\s*)?AS\s+\$\$(.*)\$\$\s*;?\s*$`)
	dropFunctionPattern   = regexp.MustCompile(`(?is)^DROP\s+FUNCTION\s+(IF\s+EXISTS\s+)?([^\s(;]+)(?:\s*\([^)]*\))?\s*;?\s*$`)
)

// FunctionProcessor parses and executes the supported SQL scalar
// user-defined function subset.
//
// Unlike a procedure — which the emulator's own interpreter runs statement by
// statement, because DuckDB has no equivalent of Snowflake Scripting — a SQL
// UDF is a single expression, and DuckDB already has exactly that: a MACRO.
// CREATE FUNCTION becomes CREATE MACRO under the emulator's usual physical
// name, so calling the function is just calling it — no interpreter, no
// per-call overhead beyond what DuckDB already does for its own functions.
type FunctionProcessor struct {
	repo     *metadata.Repository
	executor *Executor
}

// NewFunctionProcessor creates a function processor.
func NewFunctionProcessor(repo *metadata.Repository, executor *Executor) *FunctionProcessor {
	return &FunctionProcessor{repo: repo, executor: executor}
}

// Create parses CREATE FUNCTION, stores it in the catalog, and creates the
// backing DuckDB MACRO that makes it callable.
func (p *FunctionProcessor) Create(ctx context.Context, executionContext ExecutionContext, sql string) (*ExecResult, error) {
	match := createFunctionPattern.FindStringSubmatch(trimLeadingComments(sql))
	if match == nil {
		return nil, fmt.Errorf("unsupported CREATE FUNCTION syntax")
	}

	replace := strings.TrimSpace(match[1]) != ""
	databaseName, schemaName, functionName, err := resolveQualifiedObjectName(match[2], "function", executionContext)
	if err != nil {
		return nil, err
	}
	arguments, err := parseProcedureArguments(match[3])
	if err != nil {
		return nil, err
	}
	returnType := match[4]
	if language := strings.TrimSpace(match[5]); language != "" && !strings.EqualFold(language, "SQL") {
		return nil, fmt.Errorf("only LANGUAGE SQL functions are supported")
	}
	body := strings.TrimSpace(match[6])
	if body == "" {
		return nil, fmt.Errorf("function %s body cannot be empty", functionName)
	}

	schema, err := p.resolveSchema(ctx, databaseName, schemaName)
	if err != nil {
		return nil, err
	}
	encodedArguments, err := json.Marshal(arguments)
	if err != nil {
		return nil, fmt.Errorf("failed to encode function arguments: %w", err)
	}
	if _, err := p.repo.CreateFunction(ctx, schema.ID, functionName, string(encodedArguments), returnType, body, "", replace); err != nil {
		return nil, err
	}

	physicalName := BuildTableName(databaseName, schemaName, functionName)
	parameterNames := make([]string, len(arguments))
	for i, argument := range arguments {
		parameterNames[i] = argument.Name
	}
	macroKeyword := "CREATE MACRO"
	if replace {
		macroKeyword = "CREATE OR REPLACE MACRO"
	}
	macroSQL := fmt.Sprintf("%s %s(%s) AS %s", macroKeyword, physicalName, strings.Join(parameterNames, ", "), body)
	if _, err := p.executor.executeRawWithContext(ctx, executionContext, macroSQL); err != nil {
		return nil, fmt.Errorf("failed to create backing function: %w", err)
	}

	return &ExecResult{}, nil
}

// Drop parses DROP FUNCTION, removes it from the catalog, and drops the
// backing DuckDB MACRO.
func (p *FunctionProcessor) Drop(ctx context.Context, executionContext ExecutionContext, sql string) (*ExecResult, error) {
	match := dropFunctionPattern.FindStringSubmatch(trimLeadingComments(sql))
	if match == nil {
		return nil, fmt.Errorf("unsupported DROP FUNCTION syntax")
	}
	ifExists := strings.TrimSpace(match[1]) != ""
	databaseName, schemaName, functionName, err := resolveQualifiedObjectName(match[2], "function", executionContext)
	if err != nil {
		return nil, err
	}
	schema, err := p.resolveSchema(ctx, databaseName, schemaName)
	if err != nil {
		return nil, err
	}
	if err := p.repo.DropFunction(ctx, schema.ID, functionName, ifExists); err != nil {
		return nil, err
	}

	physicalName := BuildTableName(databaseName, schemaName, functionName)
	dropSQL := "DROP MACRO IF EXISTS " + physicalName
	if _, err := p.executor.executeRawWithContext(ctx, executionContext, dropSQL); err != nil {
		return nil, fmt.Errorf("failed to drop backing function: %w", err)
	}
	return &ExecResult{}, nil
}

// Show returns all functions currently stored in the emulator catalog.
func (p *FunctionProcessor) Show(ctx context.Context) (*Result, error) {
	functions, err := p.repo.ListFunctions(ctx, "")
	if err != nil {
		return nil, err
	}
	rows := make([][]interface{}, 0, len(functions))
	for _, function := range functions {
		rows = append(rows, []interface{}{function.CreatedAt, function.Name, function.Arguments, function.ReturnType})
	}
	columns := []string{columnCreatedOn, columnName, "arguments", "return_type"}
	return &Result{Columns: columns, ColumnTypes: textColumnMetadata(columns), Rows: rows}, nil
}

func (p *FunctionProcessor) resolveSchema(ctx context.Context, databaseName, schemaName string) (*metadata.Schema, error) {
	database, err := p.repo.GetDatabaseByName(ctx, databaseName)
	if err != nil {
		return nil, err
	}
	schema, err := p.repo.GetSchemaByName(ctx, database.ID, schemaName)
	if err != nil {
		return nil, err
	}
	return schema, nil
}
