package query

import (
	"fmt"
	"strings"
)

// ParseUseRole recognizes the session command and returns its normalized role.
func ParseUseRole(sql string) (string, bool, error) {
	statement := strings.TrimSpace(trimLeadingComments(sql))
	statement = strings.TrimSuffix(statement, ";")
	parts := strings.Fields(statement)
	if len(parts) < 2 || !strings.EqualFold(parts[0], "USE") || !strings.EqualFold(parts[1], "ROLE") {
		return "", false, nil
	}
	rest := strings.TrimSpace(statement[len(parts[0]):])
	rest = strings.TrimSpace(rest[len(parts[1]):])
	if rest == "" {
		return "", true, fmt.Errorf("USE ROLE requires exactly one role name")
	}
	name := rest
	if strings.HasPrefix(rest, `"`) {
		end := skipQuoted(rest, 0, '"')
		if end != len(rest) || end < 2 || rest[end-1] != '"' {
			return "", true, fmt.Errorf("USE ROLE requires exactly one role name")
		}
		name = strings.ReplaceAll(rest[1:end-1], `""`, `"`)
	} else {
		if len(strings.Fields(rest)) != 1 {
			return "", true, fmt.Errorf("USE ROLE requires exactly one role name")
		}
		name = strings.ToUpper(name)
	}
	if name == "" {
		return "", true, fmt.Errorf("role name cannot be empty")
	}
	return name, true, nil
}

func rewriteSessionFunctions(sql string, executionContext ExecutionContext) string {
	currentUser := ""
	if executionContext.Principal != nil {
		currentUser = executionContext.Principal.Username
	}
	values := map[string]string{
		"CURRENT_USER": sqlStringLiteral(currentUser),
		"CURRENT_ROLE": sqlStringLiteral(executionContext.Role),
	}
	var result strings.Builder
	for i := 0; i < len(sql); {
		if end, skipped := skipNonCode(sql, i); skipped {
			result.WriteString(sql[i:end])
			i = end
			continue
		}
		if !isIdentifierStart(sql[i]) {
			result.WriteByte(sql[i])
			i++
			continue
		}
		end := i + 1
		for end < len(sql) && isIdentifierPart(sql[end]) {
			end++
		}
		name := strings.ToUpper(sql[i:end])
		replacement, known := values[name]
		if !known || isQualified(sql, i) {
			result.WriteString(sql[i:end])
			i = end
			continue
		}
		callEnd, ok := emptyCallEnd(sql, end)
		if !ok {
			result.WriteString(sql[i:end])
			i = end
			continue
		}
		result.WriteString(replacement)
		i = callEnd
	}
	return result.String()
}

func emptyCallEnd(sql string, from int) (int, bool) {
	i := from
	for i < len(sql) && strings.ContainsRune(" \t\r\n", rune(sql[i])) {
		i++
	}
	if i >= len(sql) || sql[i] != '(' {
		return from, false
	}
	i++
	for i < len(sql) && strings.ContainsRune(" \t\r\n", rune(sql[i])) {
		i++
	}
	if i >= len(sql) || sql[i] != ')' {
		return from, false
	}
	return i + 1, true
}

func sqlStringLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
