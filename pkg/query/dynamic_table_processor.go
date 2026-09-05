package query

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/nnnkkk7/snowflake-emulator/pkg/metadata"
)

const dynamicTableType = "DYNAMIC TABLE"

var (
	createDynamicTablePattern   = regexp.MustCompile(`(?is)^\s*CREATE\s+(OR\s+REPLACE\s+)?DYNAMIC\s+TABLE\s+([^\s;]+)\s+TARGET_LAG\s*=\s*'([^']+)'\s+WAREHOUSE\s*=\s*([^\s;]+)\s+AS\s+(.+?)\s*;?\s*$`)
	alterDynamicTablePattern    = regexp.MustCompile(`(?is)^\s*ALTER\s+DYNAMIC\s+TABLE\s+([^\s;]+)\s+REFRESH\s*;?\s*$`)
	dropDynamicTablePattern     = regexp.MustCompile(`(?is)^\s*DROP\s+DYNAMIC\s+TABLE\s+(IF\s+EXISTS\s+)?([^\s;]+)\s*;?\s*$`)
	targetLagPattern            = regexp.MustCompile(`(?i)^(DOWNSTREAM|[1-9]\d*\s+(SECOND|MINUTE|HOUR|DAY)S?)$`)
	dynamicTableMutationPattern = regexp.MustCompile(`(?is)^\s*(?:INSERT\s+INTO|UPDATE|DELETE\s+FROM|TRUNCATE\s+TABLE|ALTER\s+TABLE|DROP\s+TABLE(?:\s+IF\s+EXISTS)?|MERGE\s+INTO|COPY\s+INTO|CREATE\s+OR\s+REPLACE\s+TABLE)\s+([^\s(;]+)`)
)

type DynamicTableProcessor struct {
	repo     *metadata.Repository
	executor *Executor
	mu       *sync.Mutex
}

func (p *DynamicTableProcessor) RejectOrdinaryMutation(ctx context.Context, executionContext ExecutionContext, sql string) error {
	statement := trimLeadingComments(sql)
	if strings.HasPrefix(strings.ToUpper(statement), "WITH") {
		_, statementStart := cteAliases(statement)
		if statementStart >= 0 {
			statement = statement[statementStart:]
		}
	}
	match := dynamicTableMutationPattern.FindStringSubmatch(statement)
	if match == nil {
		return nil
	}
	databaseName, schemaName, name, err := resolveQualifiedObjectName(match[1], "table", executionContext)
	if err != nil {
		return nil
	}
	schema, err := p.resolveSchema(ctx, databaseName, schemaName)
	if err != nil {
		return nil
	}
	object, err := p.repo.GetTableByName(ctx, schema.ID, name)
	if err == nil && object.TableType == dynamicTableType {
		return fmt.Errorf("dynamic table %s cannot be modified with ordinary table statements; use ALTER DYNAMIC TABLE ... REFRESH or DROP DYNAMIC TABLE", name)
	}
	return nil
}

func NewDynamicTableProcessor(repo *metadata.Repository, executor *Executor) *DynamicTableProcessor {
	return &DynamicTableProcessor{repo: repo, executor: executor, mu: &sync.Mutex{}}
}

func (p *DynamicTableProcessor) Create(ctx context.Context, executionContext ExecutionContext, sql string) (*ExecResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	match := createDynamicTablePattern.FindStringSubmatch(trimLeadingComments(sql))
	if match == nil {
		return nil, fmt.Errorf("unsupported CREATE DYNAMIC TABLE syntax")
	}
	databaseName, schemaName, name, err := resolveQualifiedObjectName(match[2], "dynamic table", executionContext)
	if err != nil {
		return nil, err
	}
	schema, err := p.resolveSchema(ctx, databaseName, schemaName)
	if err != nil {
		return nil, err
	}
	replace := strings.TrimSpace(match[1]) != ""
	if existing, lookupErr := p.repo.GetTableByName(ctx, schema.ID, name); lookupErr == nil {
		if !replace || existing.TableType != dynamicTableType {
			return nil, fmt.Errorf("object %s already exists", name)
		}
	}
	if !targetLagPattern.MatchString(strings.TrimSpace(match[3])) {
		return nil, fmt.Errorf("unsupported TARGET_LAG %q", match[3])
	}
	warehouse := strings.ToUpper(strings.Trim(match[4], `"`))
	if err := p.executor.validateExecutionContext(ctx, ExecutionContext{Warehouse: warehouse}); err != nil {
		return nil, err
	}
	definition := strings.TrimSpace(match[5])
	definitionContext := executionContext
	definitionContext.Warehouse = warehouse
	translated, err := p.translatedDefinition(ctx, definitionContext, definition)
	if err != nil {
		return nil, err
	}
	physical := BuildTableName(databaseName, schemaName, name)
	materializeSQL := "CREATE OR REPLACE TABLE " + physical + " AS " + translated
	if _, err := p.repo.MaterializeDynamicTable(ctx, schema.ID, name, match[3], warehouse, definition,
		executionContext.Database, executionContext.Schema, databaseName, strings.ToUpper(schemaName)+"_"+strings.ToUpper(name), materializeSQL); err != nil {
		return nil, err
	}
	return &ExecResult{}, nil
}

func (p *DynamicTableProcessor) Refresh(ctx context.Context, executionContext ExecutionContext, sql string) (*ExecResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	match := alterDynamicTablePattern.FindStringSubmatch(trimLeadingComments(sql))
	if match == nil {
		return nil, fmt.Errorf("unsupported ALTER DYNAMIC TABLE syntax")
	}
	dynamicTable, objectContext, err := p.getByName(ctx, match[1], executionContext)
	if err != nil {
		return nil, err
	}
	if err := p.executor.validateExecutionContext(ctx, ExecutionContext{Warehouse: dynamicTable.Warehouse}); err != nil {
		return nil, err
	}
	definitionContext := ExecutionContext{Database: dynamicTable.DefinitionDatabase, Schema: dynamicTable.DefinitionSchema, Warehouse: dynamicTable.Warehouse, Role: executionContext.Role, SessionID: executionContext.SessionID}
	translated, err := p.translatedDefinition(ctx, definitionContext, dynamicTable.Definition)
	if err != nil {
		return nil, err
	}
	physical := BuildTableName(objectContext.Database, objectContext.Schema, dynamicTable.Name)
	materializeSQL := "CREATE OR REPLACE TABLE " + physical + " AS " + translated
	if err := p.repo.RefreshDynamicTable(ctx, dynamicTable, objectContext.Database, strings.ToUpper(objectContext.Schema)+"_"+dynamicTable.Name, materializeSQL); err != nil {
		return nil, err
	}
	return &ExecResult{}, nil
}

func (p *DynamicTableProcessor) Drop(ctx context.Context, executionContext ExecutionContext, sql string) (*ExecResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	match := dropDynamicTablePattern.FindStringSubmatch(trimLeadingComments(sql))
	if match == nil {
		return nil, fmt.Errorf("unsupported DROP DYNAMIC TABLE syntax")
	}
	databaseName, schemaName, name, err := resolveQualifiedObjectName(match[2], "dynamic table", executionContext)
	if err != nil {
		return nil, err
	}
	schema, err := p.resolveSchema(ctx, databaseName, schemaName)
	if err != nil {
		return nil, err
	}
	if _, err := p.repo.GetDynamicTableByName(ctx, schema.ID, name); err != nil {
		if strings.TrimSpace(match[1]) != "" {
			return &ExecResult{}, nil
		}
		return nil, err
	}
	physical := BuildTableName(databaseName, schemaName, name)
	if err := p.repo.DropDynamicTableAtomic(ctx, schema.ID, name, "DROP TABLE "+physical); err != nil {
		return nil, err
	}
	return &ExecResult{}, nil
}

func (p *DynamicTableProcessor) Show(ctx context.Context, executionContext ExecutionContext, sql string) (*Result, error) {
	if !strings.EqualFold(strings.TrimSpace(strings.TrimSuffix(trimLeadingComments(sql), ";")), "SHOW DYNAMIC TABLES") {
		return nil, fmt.Errorf("unsupported SHOW DYNAMIC TABLES syntax")
	}
	if executionContext.Database == "" || executionContext.Schema == "" {
		return nil, fmt.Errorf("SHOW DYNAMIC TABLES requires database and schema context")
	}
	database, err := p.repo.GetDatabaseByName(ctx, executionContext.Database)
	if err != nil {
		return nil, err
	}
	schema, err := p.repo.GetSchemaByName(ctx, database.ID, executionContext.Schema)
	if err != nil {
		return nil, err
	}
	values, err := p.repo.ListDynamicTables(ctx, schema.ID)
	if err != nil {
		return nil, err
	}
	columns := []string{columnCreatedOn, columnName, columnDatabase, columnSchema, "target_lag", "warehouse", "definition", "last_refreshed_on"}
	result := &Result{Columns: columns, ColumnTypes: textColumnMetadata(columns)}
	for _, value := range values {
		result.Rows = append(result.Rows, []interface{}{value.CreatedAt, value.Name, database.Name, schema.Name, value.TargetLag, value.Warehouse, value.Definition, value.LastRefreshedAt})
	}
	result.TotalRows = len(result.Rows)
	return result, nil
}

func (p *DynamicTableProcessor) translatedDefinition(ctx context.Context, executionContext ExecutionContext, definition string) (string, error) {
	rewritten, err := p.executor.rewriteTablesWithContext(ctx, executionContext, definition)
	if err != nil {
		return "", err
	}
	translated, err := p.executor.translator.Translate(rewritten)
	if err != nil {
		return "", fmt.Errorf("translation error: %w", err)
	}
	return translated, nil
}

func (p *DynamicTableProcessor) getByName(ctx context.Context, name string, executionContext ExecutionContext) (*metadata.DynamicTable, ExecutionContext, error) {
	databaseName, schemaName, objectName, err := resolveQualifiedObjectName(name, "dynamic table", executionContext)
	if err != nil {
		return nil, ExecutionContext{}, err
	}
	schema, err := p.resolveSchema(ctx, databaseName, schemaName)
	if err != nil {
		return nil, ExecutionContext{}, err
	}
	value, err := p.repo.GetDynamicTableByName(ctx, schema.ID, objectName)
	return value, ExecutionContext{Database: databaseName, Schema: schemaName, Warehouse: valueWarehouse(value), Role: executionContext.Role, SessionID: executionContext.SessionID}, err
}

func valueWarehouse(value *metadata.DynamicTable) string {
	if value == nil {
		return ""
	}
	return value.Warehouse
}

func (p *DynamicTableProcessor) resolveSchema(ctx context.Context, databaseName, schemaName string) (*metadata.Schema, error) {
	database, err := p.repo.GetDatabaseByName(ctx, databaseName)
	if err != nil {
		return nil, err
	}
	return p.repo.GetSchemaByName(ctx, database.ID, schemaName)
}
