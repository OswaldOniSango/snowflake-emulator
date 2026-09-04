package query

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/nnnkkk7/snowflake-emulator/pkg/metadata"
)

var (
	createStreamPattern = regexp.MustCompile(`(?is)^CREATE\s+(OR\s+REPLACE\s+)?STREAM\s+([^\s]+)\s+ON\s+TABLE\s+([^\s;]+)(?:\s+APPEND_ONLY\s*=\s*(TRUE|FALSE))?\s*;?\s*$`)
	dropStreamPattern   = regexp.MustCompile(`(?is)^DROP\s+STREAM\s+(IF\s+EXISTS\s+)?([^\s;]+)\s*;?\s*$`)
	identifierPattern   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_$]*$`)
)

// StreamProcessor manages the supported append-only stream subset.
type StreamProcessor struct {
	repo     *metadata.Repository
	executor *Executor
}

type streamConsumption struct {
	stream *metadata.Stream
	offset int64
}

// NewStreamProcessor creates a stream processor.
func NewStreamProcessor(repo *metadata.Repository, executor *Executor) *StreamProcessor {
	return &StreamProcessor{repo: repo, executor: executor}
}

// Create parses CREATE STREAM and stores the source-table offset.
func (p *StreamProcessor) Create(ctx context.Context, executionContext ExecutionContext, sql string) (*ExecResult, error) {
	match := createStreamPattern.FindStringSubmatch(trimLeadingComments(sql))
	if match == nil {
		return nil, fmt.Errorf("unsupported CREATE STREAM syntax")
	}

	streamDatabase, streamSchema, streamName, err := resolveQualifiedObjectName(match[2], "stream", executionContext)
	if err != nil {
		return nil, err
	}
	sourceDatabase, sourceSchema, sourceTable, err := resolveQualifiedObjectName(match[3], "source table", executionContext)
	if err != nil {
		return nil, err
	}
	if match[4] != "" && !strings.EqualFold(match[4], "TRUE") {
		return nil, fmt.Errorf("only APPEND_ONLY = TRUE streams are supported")
	}

	streamSchemaMetadata, err := p.resolveSchema(ctx, streamDatabase, streamSchema)
	if err != nil {
		return nil, err
	}
	if _, err := p.resolveSchema(ctx, sourceDatabase, sourceSchema); err != nil {
		return nil, err
	}

	physicalSource := BuildTableName(sourceDatabase, sourceSchema, sourceTable)
	var offset int64
	query := fmt.Sprintf("SELECT COALESCE(MAX(rowid), -1) FROM %s", physicalSource)
	if err := p.executor.mgr.QueryRow(ctx, query).Scan(&offset); err != nil {
		return nil, fmt.Errorf("source table %s not found or unreadable: %w", match[3], err)
	}

	_, err = p.repo.CreateStream(ctx, streamSchemaMetadata.ID, streamName, sourceDatabase, sourceSchema, sourceTable, "APPEND_ONLY", offset, strings.TrimSpace(match[1]) != "")
	if err != nil {
		return nil, err
	}
	return &ExecResult{}, nil
}

// Drop parses DROP STREAM and removes its catalog definition.
func (p *StreamProcessor) Drop(ctx context.Context, executionContext ExecutionContext, sql string) (*ExecResult, error) {
	match := dropStreamPattern.FindStringSubmatch(trimLeadingComments(sql))
	if match == nil {
		return nil, fmt.Errorf("unsupported DROP STREAM syntax")
	}
	databaseName, schemaName, streamName, err := resolveQualifiedObjectName(match[2], "stream", executionContext)
	if err != nil {
		return nil, err
	}
	schema, err := p.resolveSchema(ctx, databaseName, schemaName)
	if err != nil {
		return nil, err
	}
	if err := p.repo.DropStream(ctx, schema.ID, streamName, strings.TrimSpace(match[1]) != ""); err != nil {
		return nil, err
	}
	return &ExecResult{}, nil
}

// Show returns all streams stored in the emulator catalog.
func (p *StreamProcessor) Show(ctx context.Context, _ string) (*Result, error) {
	streams, err := p.repo.ListStreams(ctx, "")
	if err != nil {
		return nil, err
	}
	rows := make([][]interface{}, 0, len(streams))
	for _, stream := range streams {
		source := stream.SourceDatabase + "." + stream.SourceSchema + "." + stream.SourceTable
		rows = append(rows, []interface{}{stream.CreatedAt, stream.Name, source, stream.StreamType, stream.Offset})
	}
	columns := []string{columnCreatedOn, columnName, "table_name", "type", "offset"}
	return &Result{Columns: columns, ColumnTypes: textColumnMetadata(columns), Rows: rows}, nil
}

// RewriteReferences replaces logical stream names with append-only DuckDB subqueries.
func (p *StreamProcessor) RewriteReferences(ctx context.Context, executionContext ExecutionContext, sql string) (string, error) {
	rewritten, _, err := p.rewriteReferences(ctx, executionContext, sql, false)
	return rewritten, err
}

// rewriteReferencesForConsumption freezes each referenced stream at its
// current high-water mark so a successful DML statement can advance to the
// exact set of rows it consumed.
func (p *StreamProcessor) rewriteReferencesForConsumption(ctx context.Context, executionContext ExecutionContext, sql string) (string, []streamConsumption, error) {
	return p.rewriteReferences(ctx, executionContext, sql, true)
}

func (p *StreamProcessor) rewriteReferences(ctx context.Context, executionContext ExecutionContext, sql string, consuming bool) (string, []streamConsumption, error) {
	streams, err := p.repo.ListStreams(ctx, "")
	if err != nil {
		return "", nil, err
	}
	result := sql
	consumptions := make([]streamConsumption, 0)
	for _, stream := range streams {
		schema, err := p.repo.GetSchema(ctx, stream.SchemaID)
		if err != nil {
			return "", nil, err
		}
		database, err := p.repo.GetDatabase(ctx, schema.DatabaseID)
		if err != nil {
			return "", nil, err
		}
		physicalSource := BuildTableName(stream.SourceDatabase, stream.SourceSchema, stream.SourceTable)
		names := []string{database.Name + "." + schema.Name + "." + stream.Name}
		if strings.EqualFold(executionContext.Database, database.Name) {
			names = append(names, schema.Name+"."+stream.Name)
			if strings.EqualFold(executionContext.Schema, schema.Name) {
				names = append(names, stream.Name)
			}
		}
		matched := false
		patterns := make([]*regexp.Regexp, 0, len(names))
		for _, logicalName := range names {
			pattern := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(logicalName) + `\b`)
			patterns = append(patterns, pattern)
			matched = matched || pattern.MatchString(result)
		}
		if !matched {
			continue
		}

		highWater := int64(-1)
		where := fmt.Sprintf("rowid > %d", stream.Offset)
		if consuming {
			if err := p.executor.mgr.QueryRow(ctx, fmt.Sprintf("SELECT COALESCE(MAX(rowid), -1) FROM %s", physicalSource)).Scan(&highWater); err != nil {
				return "", nil, fmt.Errorf("failed to read stream %s offset: %w", stream.Name, err)
			}
			where += fmt.Sprintf(" AND rowid <= %d", highWater)
			consumptions = append(consumptions, streamConsumption{stream: stream, offset: highWater})
		}
		replacement := fmt.Sprintf(`(SELECT *, 'INSERT' AS "METADATA$ACTION", FALSE AS "METADATA$ISUPDATE", CAST(rowid AS VARCHAR) AS "METADATA$ROW_ID" FROM %s WHERE %s)`, physicalSource, where)
		for _, pattern := range patterns {
			result = pattern.ReplaceAllStringFunc(result, func(string) string { return replacement })
		}
	}
	return result, consumptions, nil
}

func (p *StreamProcessor) advanceOffsets(ctx context.Context, consumptions []streamConsumption) error {
	for _, consumption := range consumptions {
		if consumption.offset <= consumption.stream.Offset {
			continue
		}
		if err := p.repo.UpdateStreamOffset(ctx, consumption.stream.ID, consumption.offset); err != nil {
			return fmt.Errorf("failed to consume stream %s: %w", consumption.stream.Name, err)
		}
	}
	return nil
}

func (p *StreamProcessor) resolveSchema(ctx context.Context, databaseName, schemaName string) (*metadata.Schema, error) {
	database, err := p.repo.GetDatabaseByName(ctx, databaseName)
	if err != nil {
		return nil, err
	}
	return p.repo.GetSchemaByName(ctx, database.ID, schemaName)
}

func resolveQualifiedObjectName(name, objectType string, executionContext ExecutionContext) (string, string, string, error) {
	parts := strings.Split(name, ".")
	var databaseName, schemaName, objectName string
	switch len(parts) {
	case 1:
		databaseName, schemaName, objectName = executionContext.Database, executionContext.Schema, parts[0]
		if databaseName == "" || schemaName == "" {
			return "", "", "", fmt.Errorf("%s name %s requires database and schema context", objectType, name)
		}
	case 2:
		databaseName, schemaName, objectName = executionContext.Database, parts[0], parts[1]
		if databaseName == "" {
			return "", "", "", fmt.Errorf("%s name %s requires database context", objectType, name)
		}
	case 3:
		databaseName, schemaName, objectName = parts[0], parts[1], parts[2]
	default:
		return "", "", "", fmt.Errorf("invalid %s name %s", objectType, name)
	}
	for _, part := range []string{databaseName, schemaName, objectName} {
		if !identifierPattern.MatchString(part) {
			return "", "", "", fmt.Errorf("invalid %s identifier %s", objectType, part)
		}
	}
	return strings.ToUpper(databaseName), strings.ToUpper(schemaName), strings.ToUpper(objectName), nil
}
