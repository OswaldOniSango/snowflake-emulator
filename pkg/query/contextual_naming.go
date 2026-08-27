package query

import (
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
			return parts[1] + BuildTableName(executionContext.Database, executionContext.Schema, name)
		})
	}
	return result
}
