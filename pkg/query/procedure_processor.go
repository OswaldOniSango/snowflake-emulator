package query

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/nnnkkk7/snowflake-emulator/pkg/metadata"
	servertypes "github.com/nnnkkk7/snowflake-emulator/server/types"
)

var (
	createProcedurePattern = regexp.MustCompile(`(?is)^CREATE\s+(OR\s+REPLACE\s+)?PROCEDURE\s+([^\s(]+)\s*\((.*?)\)\s+RETURNS\s+([^\s]+(?:\s*\([^)]*\))?)\s+LANGUAGE\s+(\w+)\s+AS\s+\$\$(.*)\$\$\s*;?\s*$`)
	dropProcedurePattern   = regexp.MustCompile(`(?is)^DROP\s+PROCEDURE\s+(IF\s+EXISTS\s+)?([^\s(;]+)(?:\s*\([^)]*\))?\s*;?\s*$`)
	callProcedurePattern   = regexp.MustCompile(`(?is)^CALL\s+([^\s(]+)\s*\((.*)\)\s*;?\s*$`)
)

// ProcedureArgument describes one SQL procedure parameter.
type ProcedureArgument struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// ProcedureProcessor parses and executes the supported SQL procedure subset.
type ProcedureProcessor struct {
	repo     *metadata.Repository
	executor *Executor
}

// NewProcedureProcessor creates a procedure processor.
func NewProcedureProcessor(repo *metadata.Repository, executor *Executor) *ProcedureProcessor {
	return &ProcedureProcessor{repo: repo, executor: executor}
}

// Create parses CREATE PROCEDURE and stores it in the catalog.
func (p *ProcedureProcessor) Create(ctx context.Context, executionContext ExecutionContext, sql string) (*ExecResult, error) {
	match := createProcedurePattern.FindStringSubmatch(strings.TrimSpace(sql))
	if match == nil {
		return nil, fmt.Errorf("unsupported CREATE PROCEDURE syntax")
	}

	databaseName, schemaName, procedureName, err := resolveQualifiedObjectName(match[2], "procedure", executionContext)
	if err != nil {
		return nil, err
	}
	arguments, err := parseProcedureArguments(match[3])
	if err != nil {
		return nil, err
	}
	encodedArguments, err := json.Marshal(arguments)
	if err != nil {
		return nil, fmt.Errorf("failed to encode procedure arguments: %w", err)
	}
	schema, err := p.resolveSchema(ctx, databaseName, schemaName)
	if err != nil {
		return nil, err
	}

	_, err = p.repo.CreateProcedure(ctx, schema.ID, procedureName, string(encodedArguments), match[4], match[5], strings.TrimSpace(match[6]), "", strings.TrimSpace(match[1]) != "")
	if err != nil {
		return nil, err
	}
	return &ExecResult{}, nil
}

// Drop parses DROP PROCEDURE and removes it from the catalog.
func (p *ProcedureProcessor) Drop(ctx context.Context, executionContext ExecutionContext, sql string) (*ExecResult, error) {
	match := dropProcedurePattern.FindStringSubmatch(strings.TrimSpace(sql))
	if match == nil {
		return nil, fmt.Errorf("unsupported DROP PROCEDURE syntax")
	}
	databaseName, schemaName, procedureName, err := resolveQualifiedObjectName(match[2], "procedure", executionContext)
	if err != nil {
		return nil, err
	}
	schema, err := p.resolveSchema(ctx, databaseName, schemaName)
	if err != nil {
		return nil, err
	}
	if err := p.repo.DropProcedure(ctx, schema.ID, procedureName, strings.TrimSpace(match[1]) != ""); err != nil {
		return nil, err
	}
	return &ExecResult{}, nil
}

// Call executes a stored SQL procedure and returns its RETURN or final SELECT result.
func (p *ProcedureProcessor) Call(ctx context.Context, executionContext ExecutionContext, sql string) (*Result, error) {
	match := callProcedurePattern.FindStringSubmatch(strings.TrimSpace(sql))
	if match == nil {
		return nil, fmt.Errorf("unsupported CALL syntax")
	}
	databaseName, schemaName, procedureName, err := resolveQualifiedObjectName(match[1], "procedure", executionContext)
	if err != nil {
		return nil, err
	}
	schema, err := p.resolveSchema(ctx, databaseName, schemaName)
	if err != nil {
		return nil, err
	}
	procedure, err := p.repo.GetProcedureByName(ctx, schema.ID, procedureName)
	if err != nil {
		return nil, err
	}

	var parameters []ProcedureArgument
	if err := json.Unmarshal([]byte(procedure.Arguments), &parameters); err != nil {
		return nil, fmt.Errorf("invalid stored procedure arguments: %w", err)
	}
	values := splitSQLList(match[2])
	if strings.TrimSpace(match[2]) == "" {
		values = nil
	}
	if len(values) != len(parameters) {
		return nil, fmt.Errorf("procedure %s expects %d arguments, got %d", procedure.Name, len(parameters), len(values))
	}

	body := procedure.Body
	for i, parameter := range parameters {
		body = replaceNamedBinding(body, parameter.Name, strings.TrimSpace(values[i]))
	}
	return p.executeBody(ctx, executionContext, procedure.Name, body)
}

// Show returns all procedures currently stored in the emulator catalog.
func (p *ProcedureProcessor) Show(ctx context.Context, _ string) (*Result, error) {
	procedures, err := p.repo.ListProcedures(ctx, "")
	if err != nil {
		return nil, err
	}
	rows := make([][]interface{}, 0, len(procedures))
	for _, procedure := range procedures {
		rows = append(rows, []interface{}{procedure.CreatedAt, procedure.Name, procedure.Arguments, procedure.ReturnType, procedure.Language})
	}
	columns := []string{"created_on", "name", "arguments", "return_type", "language"}
	return &Result{Columns: columns, ColumnTypes: textColumnMetadata(columns), Rows: rows}, nil
}

func (p *ProcedureProcessor) executeBody(ctx context.Context, executionContext ExecutionContext, procedureName, body string) (*Result, error) {
	body = strings.TrimSpace(body)
	upperBody := strings.ToUpper(body)
	if strings.HasPrefix(upperBody, "BEGIN") && strings.HasSuffix(upperBody, "END") {
		body = strings.TrimSpace(body[len("BEGIN") : len(body)-len("END")])
	}

	var finalResult *Result
	for _, statement := range splitSQLStatements(body) {
		trimmed := strings.TrimSpace(statement)
		if trimmed == "" {
			continue
		}
		upperStatement := strings.ToUpper(trimmed)
		switch {
		case strings.HasPrefix(upperStatement, "RETURN "):
			return p.executor.QueryWithContext(ctx, executionContext, "SELECT "+strings.TrimSpace(trimmed[len("RETURN "):])+" AS "+procedureName)
		case IsQuery(trimmed):
			result, err := p.executor.QueryWithContext(ctx, executionContext, trimmed)
			if err != nil {
				return nil, err
			}
			finalResult = result
		default:
			if _, err := p.executor.ExecuteWithContext(ctx, executionContext, trimmed); err != nil {
				return nil, err
			}
		}
	}
	if finalResult != nil {
		return finalResult, nil
	}
	columns := []string{procedureName}
	return &Result{Columns: columns, ColumnTypes: textColumnMetadata(columns), Rows: [][]interface{}{{nil}}}, nil
}

func (p *ProcedureProcessor) resolveSchema(ctx context.Context, databaseName, schemaName string) (*metadata.Schema, error) {
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

func parseQualifiedProcedureName(name string) (string, string, string, error) {
	parts := strings.Split(name, ".")
	if len(parts) != 3 {
		return "", "", "", fmt.Errorf("procedure name %s must be fully qualified as DATABASE.SCHEMA.NAME", name)
	}
	return strings.ToUpper(parts[0]), strings.ToUpper(parts[1]), strings.ToUpper(parts[2]), nil
}

func parseProcedureArguments(value string) ([]ProcedureArgument, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	parts := splitSQLList(value)
	arguments := make([]ProcedureArgument, 0, len(parts))
	for _, part := range parts {
		fields := strings.Fields(part)
		if len(fields) < 2 {
			return nil, fmt.Errorf("invalid procedure argument %q: expected NAME TYPE", part)
		}
		arguments = append(arguments, ProcedureArgument{Name: strings.ToUpper(fields[0]), Type: strings.ToUpper(strings.Join(fields[1:], " "))})
	}
	return arguments, nil
}

func splitSQLList(value string) []string {
	return splitSQL(value, ',')
}

func splitSQLStatements(value string) []string {
	return splitSQL(value, ';')
}

func splitSQL(value string, separator rune) []string {
	var result []string
	start, depth := 0, 0
	inQuote := false
	runes := []rune(value)
	for i, current := range runes {
		if current == '\'' {
			if inQuote && i+1 < len(runes) && runes[i+1] == '\'' {
				continue
			}
			inQuote = !inQuote
			continue
		}
		if inQuote {
			continue
		}
		switch current {
		case '(':
			depth++
		case ')':
			depth--
		default:
			if current == separator && depth == 0 {
				result = append(result, string(runes[start:i]))
				start = i + 1
			}
		}
	}
	result = append(result, string(runes[start:]))
	return result
}

func replaceNamedBinding(sql, name, value string) string {
	for i := 0; i < len(sql); {
		if sql[i] == ':' && strings.EqualFold(sql[i+1:min(i+1+len(name), len(sql))], name) {
			end := i + 1 + len(name)
			if end == len(sql) || (!unicode.IsLetter(rune(sql[end])) && !unicode.IsDigit(rune(sql[end])) && sql[end] != '_') {
				sql = sql[:i] + value + sql[end:]
				i += len(value)
				continue
			}
		}
		i++
	}
	return sql
}

func textColumnMetadata(columns []string) []servertypes.ColumnMetadata {
	metadata := make([]servertypes.ColumnMetadata, len(columns))
	for i, column := range columns {
		metadata[i] = servertypes.ColumnMetadata{Name: column, Type: TypeText, Nullable: true}
	}
	return metadata
}
