package query

import "strings"

// RequiresWarehouse reports whether a statement performs data compute rather
// than metadata-only work. Assigned-object commands acquire their own warehouse.
func RequiresWarehouse(sql string) bool {
	leading := strings.ToUpper(strings.TrimSpace(leadingSQL(sql)))
	for _, prefix := range []string{"SHOW ", "DESCRIBE ", "DESC ", "EXPLAIN ", "EXECUTE TASK"} {
		if strings.HasPrefix(leading, prefix) {
			return false
		}
	}
	if strings.HasPrefix(leading, "SELECT ") || strings.HasPrefix(leading, "WITH ") ||
		strings.HasPrefix(leading, "CALL ") || strings.HasPrefix(leading, "INSERT ") ||
		strings.HasPrefix(leading, "UPDATE ") || strings.HasPrefix(leading, "DELETE ") ||
		strings.HasPrefix(leading, "MERGE ") || strings.HasPrefix(leading, "COPY ") {
		return true
	}
	if strings.HasPrefix(leading, "CREATE ") && strings.Contains(leading, " TABLE ") && strings.Contains(leading, " AS ") {
		// Dynamic tables acquire the warehouse stored on the object definition,
		// including the CREATE OR REPLACE form.
		return !strings.Contains(leading, " DYNAMIC TABLE ")
	}
	if strings.HasPrefix(leading, "ALTER DYNAMIC TABLE") && strings.HasSuffix(strings.TrimSuffix(leading, ";"), " REFRESH") {
		return false
	}
	return false
}
