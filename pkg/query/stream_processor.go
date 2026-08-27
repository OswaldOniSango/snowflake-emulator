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

// NewStreamProcessor creates a stream processor.
func NewStreamProcessor(repo *metadata.Repository, executor *Executor) *StreamProcessor {
	return &StreamProcessor{repo: repo, executor: executor}
}

// Create parses CREATE STREAM and stores the source-table offset.
func (p *StreamProcessor) Create(ctx context.Context, sql string) (*ExecResult, error) {
	match := createStreamPattern.FindStringSubmatch(strings.TrimSpace(sql))
	if match == nil {
		return nil, fmt.Errorf("unsupported CREATE STREAM syntax")
	}

	streamDatabase, streamSchema, streamName, err := parseQualifiedObjectName(match[2], "stream")
	if err != nil {
		return nil, err
	}
	sourceDatabase, sourceSchema, sourceTable, err := parseQualifiedObjectName(match[3], "source table")
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
func (p *StreamProcessor) Drop(ctx context.Context, sql string) (*ExecResult, error) {
	match := dropStreamPattern.FindStringSubmatch(strings.TrimSpace(sql))
	if match == nil {
		return nil, fmt.Errorf("unsupported DROP STREAM syntax")
	}
	databaseName, schemaName, streamName, err := parseQualifiedObjectName(match[2], "stream")
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
	columns := []string{"created_on", "name", "table_name", "type", "offset"}
	return &Result{Columns: columns, ColumnTypes: textColumnMetadata(columns), Rows: rows}, nil
}

// RewriteReferences replaces logical stream names with append-only DuckDB subqueries.
func (p *StreamProcessor) RewriteReferences(ctx context.Context, sql string) (string, error) {
	streams, err := p.repo.ListStreams(ctx, "")
	if err != nil {
		return "", err
	}
	result := sql
	for _, stream := range streams {
		schema, err := p.repo.GetSchema(ctx, stream.SchemaID)
		if err != nil {
			return "", err
		}
		database, err := p.repo.GetDatabase(ctx, schema.DatabaseID)
		if err != nil {
			return "", err
		}
		logicalName := database.Name + "." + schema.Name + "." + stream.Name
		physicalSource := BuildTableName(stream.SourceDatabase, stream.SourceSchema, stream.SourceTable)
		replacement := fmt.Sprintf(`(SELECT *, 'INSERT' AS "METADATA$ACTION", FALSE AS "METADATA$ISUPDATE", CAST(rowid AS VARCHAR) AS "METADATA$ROW_ID" FROM %s WHERE rowid > %d)`, physicalSource, stream.Offset)
		pattern := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(logicalName) + `\b`)
		result = pattern.ReplaceAllStringFunc(result, func(string) string { return replacement })
	}
	return result, nil
}

func (p *StreamProcessor) resolveSchema(ctx context.Context, databaseName, schemaName string) (*metadata.Schema, error) {
	database, err := p.repo.GetDatabaseByName(ctx, databaseName)
	if err != nil {
		return nil, err
	}
	return p.repo.GetSchemaByName(ctx, database.ID, schemaName)
}

func parseQualifiedObjectName(name, objectType string) (string, string, string, error) {
	parts := strings.Split(name, ".")
	if len(parts) != 3 {
		return "", "", "", fmt.Errorf("%s name %s must be fully qualified as DATABASE.SCHEMA.NAME", objectType, name)
	}
	for _, part := range parts {
		if !identifierPattern.MatchString(part) {
			return "", "", "", fmt.Errorf("invalid %s identifier %s", objectType, part)
		}
	}
	return strings.ToUpper(parts[0]), strings.ToUpper(parts[1]), strings.ToUpper(parts[2]), nil
}
