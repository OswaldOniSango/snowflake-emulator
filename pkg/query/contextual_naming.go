package query

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

var contextualTablePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(CREATE\s+(?:OR\s+REPLACE\s+)?(?:(?:TEMP|TEMPORARY|TRANSIENT)\s+)?TABLE(?:\s+IF\s+NOT\s+EXISTS)?\s+)([^\s(;]+)`),
	regexp.MustCompile(`(?i)\b(INSERT\s+INTO\s+)([^\s(;]+)`),
	regexp.MustCompile(`(?i)\b(UPDATE\s+)([^\s(;]+)`),
	regexp.MustCompile(`(?i)\b(DELETE\s+FROM\s+)([^\s(;]+)`),
	regexp.MustCompile(`(?i)\b(FROM\s+)([^\s(;]+)`),
	regexp.MustCompile(`(?i)\b(JOIN\s+)([^\s(;]+)`),
	regexp.MustCompile(`(?i)\b(ALTER\s+TABLE\s+)([^\s(;]+)`),
	regexp.MustCompile(`(?i)\b(DROP\s+TABLE(?:\s+IF\s+EXISTS)?\s+)([^\s(;]+)`),
	// MERGE resolves two tables. The USING pattern cannot match a JOIN's
	// USING (col) list, because the capture group rejects a leading "(".
	regexp.MustCompile(`(?i)\b(DESC(?:RIBE)?\s+TABLE\s+)([^\s(;]+)`),
	regexp.MustCompile(`(?i)\b(TRUNCATE\s+TABLE\s+)([^\s(;]+)`),
	regexp.MustCompile(`(?i)\b(MERGE\s+INTO\s+)([^\s(;]+)`),
	regexp.MustCompile(`(?i)\b(USING\s+)([^\s(;]+)`),
}

// sqlKeywords are words a table-reference pattern can capture by accident. The
// clearest case is a MERGE's "WHEN MATCHED THEN UPDATE SET", where the UPDATE
// pattern would otherwise rewrite SET as though it named a table.
var sqlKeywords = map[string]bool{
	"SET":    true,
	"VALUES": true,
	"SELECT": true,
	"WHERE":  true,
	"FROM":   true,
	"USING":  true,
	"ON":     true,
	"AS":     true,
}

// rewriteContextualTableReferences maps unqualified Snowflake table names to
// the emulator's physical DuckDB naming convention DATABASE.SCHEMA_TABLE.
func rewriteContextualTableReferences(sql string, executionContext ExecutionContext) string {
	if executionContext.Database == "" || executionContext.Schema == "" {
		return sql
	}

	result := sql
	for _, pattern := range contextualTablePatterns {
		result = pattern.ReplaceAllStringFunc(result, func(match string) string {
			parts := pattern.FindStringSubmatch(match)
			if len(parts) != 3 {
				return match
			}
			name := strings.TrimSpace(parts[2])
			if strings.Contains(name, ".") || strings.HasPrefix(name, "_") || !identifierPattern.MatchString(name) {
				return match
			}
			if sqlKeywords[strings.ToUpper(name)] {
				return match
			}
			return parts[1] + BuildTableName(executionContext.Database, executionContext.Schema, name)
		})
	}
	return result
}

// rewriteTablesWithContext validates the logical Snowflake namespace before
// mapping short table names to physical DuckDB names.
func (e *Executor) rewriteTablesWithContext(ctx context.Context, executionContext ExecutionContext, sql string) (string, error) {
	result := sql
	var rewriteErr error
	for _, pattern := range contextualTablePatterns {
		result = pattern.ReplaceAllStringFunc(result, func(match string) string {
			if rewriteErr != nil {
				return match
			}
			parts := pattern.FindStringSubmatch(match)
			if len(parts) != 3 {
				return match
			}
			name := strings.TrimSpace(parts[2])
			if strings.HasPrefix(name, "_") || sqlKeywords[strings.ToUpper(name)] {
				return match
			}
			physicalName, changed, err := e.resolveTableReference(ctx, executionContext, name)
			if err != nil {
				rewriteErr = err
				return match
			}
			if !changed {
				return match
			}
			return parts[1] + physicalName
		})
	}
	if rewriteErr != nil {
		return "", rewriteErr
	}
	return result, nil
}

func (e *Executor) resolveTableReference(ctx context.Context, executionContext ExecutionContext, name string) (string, bool, error) {
	parts := strings.Split(name, ".")
	for _, part := range parts {
		if !identifierPattern.MatchString(part) {
			return "", false, nil
		}
	}

	switch len(parts) {
	case 1:
		if executionContext.Database == "" || executionContext.Schema == "" {
			return "", false, nil
		}
		return BuildTableName(executionContext.Database, executionContext.Schema, parts[0]), true, nil
	case 2:
		if physical, ok, err := e.existingPhysicalReference(ctx, parts[0], parts[1]); err != nil {
			return "", false, err
		} else if ok {
			return physical, false, nil
		}
		if executionContext.Database == "" {
			return "", false, fmt.Errorf("table reference %s requires a database context", name)
		}
		if err := e.validateObjectNamespace(ctx, executionContext.Database, parts[0]); err != nil {
			return "", false, err
		}
		return BuildTableName(executionContext.Database, parts[0], parts[1]), true, nil
	case 3:
		if err := e.validateObjectNamespace(ctx, parts[0], parts[1]); err != nil {
			return "", false, err
		}
		return BuildTableName(parts[0], parts[1], parts[2]), true, nil
	default:
		return "", false, fmt.Errorf("invalid table reference %s", name)
	}
}

func (e *Executor) existingPhysicalReference(ctx context.Context, databaseName, physicalTableName string) (string, bool, error) {
	database, err := e.repo.GetDatabaseByName(ctx, databaseName)
	if err != nil {
		return "", false, nil
	}
	schemas, err := e.repo.ListSchemas(ctx, database.ID)
	if err != nil {
		return "", false, err
	}
	normalizedTableName := strings.ToUpper(physicalTableName)
	for _, schema := range schemas {
		if strings.HasPrefix(normalizedTableName, schema.Name+"_") {
			return strings.ToUpper(databaseName) + "." + normalizedTableName, true, nil
		}
	}
	return "", false, nil
}

func (e *Executor) validateObjectNamespace(ctx context.Context, databaseName, schemaName string) error {
	database, err := e.repo.GetDatabaseByName(ctx, databaseName)
	if err != nil {
		return fmt.Errorf("database %s not found: %w", strings.ToUpper(databaseName), err)
	}
	if _, err := e.repo.GetSchemaByName(ctx, database.ID, schemaName); err != nil {
		return fmt.Errorf("schema %s not found in database %s: %w", strings.ToUpper(schemaName), strings.ToUpper(databaseName), err)
	}
	return nil
}

// validateExecutionContext checks every explicitly supplied namespace before a
// statement is classified or executed.
func (e *Executor) validateExecutionContext(ctx context.Context, executionContext ExecutionContext) error {
	var databaseID string
	if executionContext.Database != "" {
		database, err := e.repo.GetDatabaseByName(ctx, executionContext.Database)
		if err != nil {
			return fmt.Errorf("database %s not found: %w", executionContext.Database, err)
		}
		databaseID = database.ID
	}

	if executionContext.Schema != "" {
		if databaseID == "" {
			return fmt.Errorf("schema %s requires a database context", executionContext.Schema)
		}
		if _, err := e.repo.GetSchemaByName(ctx, databaseID, executionContext.Schema); err != nil {
			return fmt.Errorf("schema %s not found in database %s: %w", executionContext.Schema, executionContext.Database, err)
		}
	}

	if executionContext.Warehouse != "" {
		if e.warehouseValidator == nil {
			return fmt.Errorf("warehouse %s cannot be validated: warehouse manager is not configured", executionContext.Warehouse)
		}
		if err := e.warehouseValidator(ctx, executionContext.Warehouse); err != nil {
			return fmt.Errorf("warehouse %s not found: %w", executionContext.Warehouse, err)
		}
	}

	if executionContext.Role != "" {
		return fmt.Errorf("role %s cannot be validated: role management is not implemented", executionContext.Role)
	}

	return nil
}

// physicalNameError rewrites the emulator's physical table names back to the
// names the caller wrote.
//
// Statements are rewritten to DATABASE.SCHEMA_TABLE before they reach DuckDB,
// so an engine error names objects the user never mentioned — and quotes the
// translated SQL back at them. Reporting "PUBLIC_ORDERS does not exist" for
// a query that said "orders" is worse than unhelpful; it describes an
// emulator internal as though it were the user's schema.
func physicalNameError(err error, executionContext ExecutionContext) error {
	if err == nil || executionContext.Database == "" || executionContext.Schema == "" {
		return err
	}

	message := err.Error()
	qualified := executionContext.Database + "." + executionContext.Schema + "_"
	bare := executionContext.Schema + "_"

	// The qualified form is replaced first: it contains the bare one.
	cleaned := strings.ReplaceAll(message, qualified, "")
	cleaned = strings.ReplaceAll(cleaned, bare, "")
	if cleaned == message {
		return err
	}

	return fmt.Errorf("%s", cleaned)
}
