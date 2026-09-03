package query

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/nnnkkk7/snowflake-emulator/pkg/metadata"
	"github.com/nnnkkk7/snowflake-emulator/pkg/stage"
)

var (
	createStagePattern = regexp.MustCompile(`(?is)^CREATE\s+(OR\s+REPLACE\s+)?STAGE\s+(IF\s+NOT\s+EXISTS\s+)?([^\s;]+)(?:\s+COMMENT\s*=\s*'((?:''|[^'])*)')?\s*;?\s*$`)
	dropStagePattern   = regexp.MustCompile(`(?is)^DROP\s+STAGE\s+(IF\s+EXISTS\s+)?([^\s;]+)\s*;?\s*$`)
	listStagePattern   = regexp.MustCompile(`(?is)^LIST\s+@([^\s/;]+)(?:/([^\s;]+))?(?:\s+PATTERN\s*=\s*'([^']+)')?\s*;?\s*$`)
)

// StageProcessor manages the supported named internal stage subset.
type StageProcessor struct {
	manager *stage.Manager
	repo    *metadata.Repository
}

// NewStageProcessor creates a processor backed by the internal stage manager.
func NewStageProcessor(manager *stage.Manager, repo *metadata.Repository) *StageProcessor {
	return &StageProcessor{manager: manager, repo: repo}
}

// Create persists a named internal stage and creates its storage directory.
func (p *StageProcessor) Create(ctx context.Context, executionContext ExecutionContext, sql string) (*ExecResult, error) {
	match := createStagePattern.FindStringSubmatch(strings.TrimSpace(sql))
	if match == nil {
		return nil, fmt.Errorf("unsupported CREATE STAGE syntax; only named internal stages are supported")
	}
	if match[1] != "" && match[2] != "" {
		return nil, fmt.Errorf("CREATE OR REPLACE STAGE cannot use IF NOT EXISTS")
	}

	databaseName, schemaName, stageName, err := resolveQualifiedObjectName(match[3], "stage", executionContext)
	if err != nil {
		return nil, err
	}
	schemaMetadata, err := p.resolveSchema(ctx, databaseName, schemaName)
	if err != nil {
		return nil, err
	}

	if _, err := p.manager.GetStage(ctx, schemaMetadata.ID, stageName); err == nil {
		switch {
		case match[2] != "":
			return &ExecResult{}, nil
		case match[1] != "":
			if err := p.manager.DropStage(ctx, schemaMetadata.ID, stageName); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("stage %s already exists", stageName)
		}
	}

	comment := strings.ReplaceAll(match[4], "''", "'")
	if _, err := p.manager.CreateStage(ctx, schemaMetadata.ID, stageName, "INTERNAL", "", comment); err != nil {
		return nil, err
	}
	return &ExecResult{}, nil
}

// Drop removes a named internal stage and its files.
func (p *StageProcessor) Drop(ctx context.Context, executionContext ExecutionContext, sql string) (*ExecResult, error) {
	match := dropStagePattern.FindStringSubmatch(strings.TrimSpace(sql))
	if match == nil {
		return nil, fmt.Errorf("unsupported DROP STAGE syntax")
	}
	databaseName, schemaName, stageName, err := resolveQualifiedObjectName(match[2], "stage", executionContext)
	if err != nil {
		return nil, err
	}
	schemaMetadata, err := p.resolveSchema(ctx, databaseName, schemaName)
	if err != nil {
		return nil, err
	}
	if _, err := p.manager.GetStage(ctx, schemaMetadata.ID, stageName); err != nil {
		if match[1] != "" {
			return &ExecResult{}, nil
		}
		return nil, err
	}
	if err := p.manager.DropStage(ctx, schemaMetadata.ID, stageName); err != nil {
		return nil, err
	}
	return &ExecResult{}, nil
}

// List returns the files stored under a named internal stage.
func (p *StageProcessor) List(ctx context.Context, executionContext ExecutionContext, sql string) (*Result, error) {
	match := listStagePattern.FindStringSubmatch(strings.TrimSpace(sql))
	if match == nil {
		return nil, fmt.Errorf("unsupported LIST syntax")
	}
	databaseName, schemaName, stageName, err := resolveQualifiedObjectName(match[1], "stage", executionContext)
	if err != nil {
		return nil, err
	}
	schemaMetadata, err := p.resolveSchema(ctx, databaseName, schemaName)
	if err != nil {
		return nil, err
	}
	files, err := p.manager.ListFiles(ctx, schemaMetadata.ID, stageName, match[3])
	if err != nil {
		return nil, err
	}

	rows := make([][]interface{}, 0, len(files))
	prefix := strings.TrimSuffix(match[2], "/")
	for _, file := range files {
		if prefix != "" && file.Name != prefix && !strings.HasPrefix(file.Name, prefix+"/") {
			continue
		}
		rows = append(rows, []interface{}{file.Name, file.Size, file.ModifiedTime})
	}
	columns := []string{columnName, "size", "last_modified"}
	return &Result{Columns: columns, ColumnTypes: textColumnMetadata(columns), Rows: rows, TotalRows: len(rows)}, nil
}

// Show returns stages stored in the emulator catalog.
func (p *StageProcessor) Show(ctx context.Context) (*Result, error) {
	stages, err := p.repo.ListStages(ctx, "")
	if err != nil {
		return nil, err
	}
	rows := make([][]interface{}, 0, len(stages))
	for _, item := range stages {
		schemaMetadata, err := p.repo.GetSchema(ctx, item.SchemaID)
		if err != nil {
			return nil, err
		}
		database, err := p.repo.GetDatabase(ctx, schemaMetadata.DatabaseID)
		if err != nil {
			return nil, err
		}
		rows = append(rows, []interface{}{item.CreatedAt, item.Name, database.Name, schemaMetadata.Name, item.StageType, item.Comment})
	}
	columns := []string{columnCreatedOn, columnName, "database_name", "schema_name", "type", "comment"}
	return &Result{Columns: columns, ColumnTypes: textColumnMetadata(columns), Rows: rows, TotalRows: len(rows)}, nil
}

func (p *StageProcessor) resolveSchema(ctx context.Context, databaseName, schemaName string) (*metadata.Schema, error) {
	database, err := p.repo.GetDatabaseByName(ctx, databaseName)
	if err != nil {
		return nil, err
	}
	return p.repo.GetSchemaByName(ctx, database.ID, schemaName)
}
