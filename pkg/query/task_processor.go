package query

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/nnnkkk7/snowflake-emulator/pkg/metadata"
)

var (
	createTaskPattern  = regexp.MustCompile(`(?is)^CREATE\s+(OR\s+REPLACE\s+)?TASK\s+([^\s]+)\s+WAREHOUSE\s*=\s*([^\s]+)\s+SCHEDULE\s*=\s*'([^']+)'\s+AS\s+(.+?)\s*;?\s*$`)
	alterTaskPattern   = regexp.MustCompile(`(?is)^ALTER\s+TASK\s+([^\s;]+)\s+(RESUME|SUSPEND)\s*;?\s*$`)
	dropTaskPattern    = regexp.MustCompile(`(?is)^DROP\s+TASK\s+(IF\s+EXISTS\s+)?([^\s;]+)\s*;?\s*$`)
	executeTaskPattern = regexp.MustCompile(`(?is)^EXECUTE\s+TASK\s+([^\s;]+)\s*;?\s*$`)
)

type TaskProcessor struct {
	repo     *metadata.Repository
	executor *Executor
}

func NewTaskProcessor(repo *metadata.Repository, executor *Executor) *TaskProcessor {
	return &TaskProcessor{repo: repo, executor: executor}
}

func (p *TaskProcessor) Create(ctx context.Context, executionContext ExecutionContext, sql string) (*ExecResult, error) {
	match := createTaskPattern.FindStringSubmatch(strings.TrimSpace(sql))
	if match == nil {
		return nil, fmt.Errorf("unsupported CREATE TASK syntax")
	}
	databaseName, schemaName, taskName, err := resolveQualifiedObjectName(match[2], "task", executionContext)
	if err != nil {
		return nil, err
	}
	schema, err := p.resolveSchema(ctx, databaseName, schemaName)
	if err != nil {
		return nil, err
	}
	warehouseName := strings.ToUpper(strings.Trim(match[3], `"`))
	if err := p.executor.validateExecutionContext(ctx, ExecutionContext{Warehouse: warehouseName}); err != nil {
		return nil, err
	}
	if _, err := parseTaskSchedule(match[4]); err != nil {
		return nil, err
	}
	if _, err := p.repo.CreateTask(ctx, schema.ID, taskName, warehouseName, match[4], match[5], strings.TrimSpace(match[1]) != ""); err != nil {
		return nil, err
	}
	return &ExecResult{}, nil
}

func (p *TaskProcessor) Alter(ctx context.Context, executionContext ExecutionContext, sql string) (*ExecResult, error) {
	match := alterTaskPattern.FindStringSubmatch(strings.TrimSpace(sql))
	if match == nil {
		return nil, fmt.Errorf("unsupported ALTER TASK syntax")
	}
	task, err := p.getByName(ctx, match[1], executionContext)
	if err != nil {
		return nil, err
	}
	state := "SUSPENDED"
	if strings.EqualFold(match[2], "RESUME") {
		state = "STARTED"
	}
	if err := p.repo.SetTaskState(ctx, task.ID, state); err != nil {
		return nil, err
	}
	return &ExecResult{}, nil
}

func (p *TaskProcessor) Drop(ctx context.Context, executionContext ExecutionContext, sql string) (*ExecResult, error) {
	match := dropTaskPattern.FindStringSubmatch(strings.TrimSpace(sql))
	if match == nil {
		return nil, fmt.Errorf("unsupported DROP TASK syntax")
	}
	databaseName, schemaName, taskName, err := resolveQualifiedObjectName(match[2], "task", executionContext)
	if err != nil {
		return nil, err
	}
	schema, err := p.resolveSchema(ctx, databaseName, schemaName)
	if err != nil {
		return nil, err
	}
	if err := p.repo.DropTask(ctx, schema.ID, taskName, strings.TrimSpace(match[1]) != ""); err != nil {
		return nil, err
	}
	return &ExecResult{}, nil
}

func (p *TaskProcessor) Show(ctx context.Context) (*Result, error) {
	tasks, err := p.repo.ListTasks(ctx, "")
	if err != nil {
		return nil, err
	}
	rows := make([][]interface{}, 0, len(tasks))
	for _, task := range tasks {
		schema, err := p.repo.GetSchema(ctx, task.SchemaID)
		if err != nil {
			return nil, err
		}
		database, err := p.repo.GetDatabase(ctx, schema.DatabaseID)
		if err != nil {
			return nil, err
		}
		rows = append(rows, []interface{}{task.CreatedAt, task.Name, database.Name, schema.Name, task.Warehouse, task.Schedule, task.State, task.Definition, task.LastExecutedAt, task.LastCompletedAt, task.LastError})
	}
	columns := []string{columnCreatedOn, columnName, "database_name", "schema_name", "warehouse", "schedule", "state", "definition", "last_executed_on", "last_completed_on", "last_error"}
	return &Result{Columns: columns, ColumnTypes: textColumnMetadata(columns), Rows: rows}, nil
}

func (p *TaskProcessor) Execute(ctx context.Context, executionContext ExecutionContext, sql string) (*ExecResult, error) {
	match := executeTaskPattern.FindStringSubmatch(strings.TrimSpace(sql))
	if match == nil {
		return nil, fmt.Errorf("unsupported EXECUTE TASK syntax")
	}
	task, err := p.getByName(ctx, match[1], executionContext)
	if err != nil {
		return nil, err
	}
	return p.executeStoredTask(ctx, task, executionContext)
}

func (p *TaskProcessor) executeStoredTask(ctx context.Context, task *metadata.Task, executionContext ExecutionContext) (*ExecResult, error) {
	schema, err := p.repo.GetSchema(ctx, task.SchemaID)
	if err != nil {
		return nil, err
	}
	database, err := p.repo.GetDatabase(ctx, schema.DatabaseID)
	if err != nil {
		return nil, err
	}
	taskContext := ExecutionContext{Database: database.Name, Schema: schema.Name, Warehouse: task.Warehouse, Role: executionContext.Role, SessionID: executionContext.SessionID}

	classifier := NewClassifier()
	var result *ExecResult
	switch {
	case classifier.IsCall(task.Definition):
		_, err = p.executor.QueryWithContext(ctx, taskContext, task.Definition)
		result = &ExecResult{}
	case classifier.Classify(task.Definition).IsQuery:
		err = fmt.Errorf("task definition must be DML or CALL")
	default:
		result, err = p.executor.ExecuteWithContext(ctx, taskContext, task.Definition)
	}
	errorMessage := ""
	if err != nil {
		errorMessage = err.Error()
	}
	if recordErr := p.repo.RecordTaskExecution(ctx, task.ID, errorMessage); recordErr != nil {
		if err == nil {
			return nil, recordErr
		}
		return nil, fmt.Errorf("%w; additionally failed to record task execution: %w", err, recordErr)
	}
	if err != nil {
		return nil, fmt.Errorf("task %s execution failed: %w", task.Name, err)
	}
	return result, nil
}

func (p *TaskProcessor) getByName(ctx context.Context, name string, executionContext ExecutionContext) (*metadata.Task, error) {
	databaseName, schemaName, taskName, err := resolveQualifiedObjectName(name, "task", executionContext)
	if err != nil {
		return nil, err
	}
	schema, err := p.resolveSchema(ctx, databaseName, schemaName)
	if err != nil {
		return nil, err
	}
	return p.repo.GetTaskByName(ctx, schema.ID, taskName)
}

func (p *TaskProcessor) resolveSchema(ctx context.Context, databaseName, schemaName string) (*metadata.Schema, error) {
	database, err := p.repo.GetDatabaseByName(ctx, databaseName)
	if err != nil {
		return nil, err
	}
	return p.repo.GetSchemaByName(ctx, database.ID, schemaName)
}
