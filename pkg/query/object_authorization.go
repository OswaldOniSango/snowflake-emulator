package query

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/nnnkkk7/snowflake-emulator/pkg/identity"
)

var (
	readTablePattern   = regexp.MustCompile(`(?i)\b(?:FROM|JOIN)\s+([^\s,;()]+)`)
	insertTablePattern = regexp.MustCompile(`(?i)^\s*(?:INSERT|COPY)\s+INTO\s+([^\s(;]+)`)
	updateTablePattern = regexp.MustCompile(`(?i)^\s*UPDATE\s+([^\s;]+)`)
	deleteTablePattern = regexp.MustCompile(`(?i)^\s*DELETE\s+FROM\s+([^\s;]+)`)
	mergeTablePattern  = regexp.MustCompile(`(?i)^\s*MERGE\s+INTO\s+([^\s;]+)`)
	createTablePattern = regexp.MustCompile(`(?i)^\s*CREATE\s+(?:OR\s+REPLACE\s+)?(?:(?:TEMP|TEMPORARY|TRANSIENT)\s+)?TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?([^\s(;]+)`)
	mergeUsingPattern  = regexp.MustCompile(`(?is)\bUSING\s+([^\s,;()]+)`)
	cteNamePattern     = regexp.MustCompile(`(?is)(?:\bWITH\b|,)\s*([A-Za-z_][A-Za-z0-9_$]*)\s+AS\s*\(`)
)

const (
	objectTypeDatabase = "DATABASE"
	objectTypeSchema   = "SCHEMA"
	objectTypeTable    = "TABLE"
	sqlKeywordSelect   = "SELECT"
	sqlKeywordInsert   = "INSERT"
)

func (e *Executor) authorizeObjectStatement(ctx context.Context, executionContext ExecutionContext, sql string) error {
	if executionContext.Principal == nil || e.identityService == nil {
		return nil
	}
	value := maskSQLLiterals(trimLeadingComments(sql))
	mainStatement := topLevelStatementAfterWith(value)
	upper := strings.ToUpper(value)
	if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(mainStatement)), "MERGE ") && (strings.Contains(upper, "WHEN NOT MATCHED") || strings.Contains(upper, "THEN DELETE")) {
		return fmt.Errorf("authorization for MERGE INSERT/DELETE branches is not supported")
	}
	needsObjectResolution := statementNeedsObjectResolution(value)
	if needsObjectResolution && regexp.MustCompile(`"[^"]*\s+[^"]*"`).MatchString(value) {
		return fmt.Errorf("authorization for quoted identifiers containing whitespace is not supported")
	}
	if err := e.authorizeCreateTable(ctx, executionContext, mainStatement); err != nil {
		return err
	}
	if err := e.authorizeMutationTarget(ctx, executionContext, mainStatement); err != nil {
		return err
	}
	return e.authorizeStatementSources(ctx, executionContext, value, mainStatement)
}

func statementNeedsObjectResolution(value string) bool {
	return readTablePattern.MatchString(value) || insertTablePattern.MatchString(value) ||
		updateTablePattern.MatchString(value) || deleteTablePattern.MatchString(value) ||
		mergeTablePattern.MatchString(value) || createTablePattern.MatchString(value)
}

func (e *Executor) authorizeTable(ctx context.Context, executionContext ExecutionContext, raw, privilege string) error {
	database, schema, table, err := resolveQualifiedObjectName(raw, "table", executionContext)
	if err != nil {
		return err
	}
	roleID := executionContext.Principal.RoleID
	if err := e.identityService.AuthorizeObject(ctx, roleID, objectTypeDatabase, database, identity.PrivilegeUsage); err != nil {
		return err
	}
	if err := e.identityService.AuthorizeObject(ctx, roleID, objectTypeSchema, database+"."+schema, identity.PrivilegeUsage); err != nil {
		return err
	}
	return e.identityService.AuthorizeObject(ctx, roleID, objectTypeTable, database+"."+schema+"."+table, privilege)
}

func (e *Executor) authorizeCreateTable(ctx context.Context, executionContext ExecutionContext, statement string) error {
	match := createTablePattern.FindStringSubmatch(statement)
	if len(match) < 2 {
		return nil
	}
	database, schema, _, err := resolveQualifiedObjectName(match[1], "table", executionContext)
	if err != nil {
		return err
	}
	roleID := executionContext.Principal.RoleID
	if err := e.identityService.AuthorizeObject(ctx, roleID, objectTypeDatabase, database, identity.PrivilegeUsage); err != nil {
		return err
	}
	if err := e.identityService.AuthorizeObject(ctx, roleID, objectTypeSchema, database+"."+schema, identity.PrivilegeUsage); err != nil {
		return err
	}
	return e.identityService.AuthorizeObject(ctx, roleID, objectTypeSchema, database+"."+schema, identity.PrivilegeCreateTable)
}

func (e *Executor) authorizeMutationTarget(ctx context.Context, executionContext ExecutionContext, statement string) error {
	for _, target := range []struct {
		pattern   *regexp.Regexp
		privilege string
	}{
		{insertTablePattern, identity.PrivilegeInsert},
		{updateTablePattern, identity.PrivilegeUpdate},
		{deleteTablePattern, identity.PrivilegeDelete},
		{mergeTablePattern, identity.PrivilegeUpdate},
	} {
		if match := target.pattern.FindStringSubmatch(statement); len(match) >= 2 {
			if err := e.authorizeTable(ctx, executionContext, match[1], target.privilege); err != nil {
				return err
			}
		}
	}
	return nil
}

func (e *Executor) authorizeStatementSources(ctx context.Context, executionContext ExecutionContext, value, mainStatement string) error {
	cteNames := map[string]bool{}
	for _, match := range cteNamePattern.FindAllStringSubmatch(value, -1) {
		cteNames[strings.ToUpper(match[1])] = true
	}
	sources := statementReadSources(value)
	if match := mergeUsingPattern.FindStringSubmatch(mainStatement); len(match) >= 2 {
		sources = append(sources, match[1])
	}
	seen := map[string]bool{}
	for _, source := range sources {
		name := strings.Trim(source, `"`)
		key := strings.ToUpper(name)
		if seen[key] || cteNames[key] || sqlKeywords[key] || strings.HasPrefix(name, "(") {
			continue
		}
		seen[key] = true
		if strings.HasPrefix(name, "@") {
			continue
		}
		if target := deleteTablePattern.FindStringSubmatch(mainStatement); len(target) >= 2 && strings.EqualFold(name, target[1]) {
			continue
		}
		if err := e.authorizeTable(ctx, executionContext, name, identity.PrivilegeSelect); err != nil {
			return err
		}
	}
	return nil
}

func topLevelStatementAfterWith(sql string) string {
	trimmed := strings.TrimSpace(sql)
	if !strings.HasPrefix(strings.ToUpper(trimmed), "WITH ") {
		return trimmed
	}
	depth, quote := 0, byte(0)
	for i := 0; i < len(trimmed); i++ {
		ch := trimmed[i]
		if quote != 0 {
			if ch == quote {
				if i+1 < len(trimmed) && trimmed[i+1] == quote {
					i++
					continue
				}
				quote = 0
			}
			continue
		}
		if ch == '\'' || ch == '"' {
			quote = ch
			continue
		}
		if ch == '(' {
			depth++
			continue
		}
		if ch == ')' {
			depth--
			continue
		}
		if depth != 0 || (i > 0 && (isIdentifierChar(trimmed[i-1]))) {
			continue
		}
		for _, keyword := range []string{sqlKeywordInsert, "UPDATE", "DELETE", "MERGE", sqlKeywordSelect, "CREATE"} {
			if len(trimmed)-i >= len(keyword) && strings.EqualFold(trimmed[i:i+len(keyword)], keyword) && (i+len(keyword) == len(trimmed) || !isIdentifierChar(trimmed[i+len(keyword)])) {
				return trimmed[i:]
			}
		}
	}
	return trimmed
}

func isIdentifierChar(ch byte) bool {
	return ch == '_' || ch == '$' || ch >= '0' && ch <= '9' || ch >= 'A' && ch <= 'Z' || ch >= 'a' && ch <= 'z'
}

// statementReadSources scans FROM lists by parenthesis depth, so commas in
// VALUES rows and function arguments cannot become table names. It still visits
// nested queries and tables following a VALUES literal in a comma join.
func statementReadSources(sql string) []string {
	sql = maskSQLLiterals(sql)
	var sources []string
	depth := 0
	inFrom := map[int]bool{}
	expect := map[int]bool{}
	for i := 0; i < len(sql); {
		switch sql[i] {
		case '(':
			expect[depth] = false
			depth++
			i++
			continue
		case ')':
			delete(inFrom, depth)
			delete(expect, depth)
			if depth > 0 {
				depth--
			}
			i++
			continue
		case ',':
			if inFrom[depth] {
				expect[depth] = true
			}
			i++
			continue
		}
		if expect[depth] && (isIdentifierStart(sql[i]) || sql[i] == '"' || sql[i] == '@') {
			start := i
			for i < len(sql) && !strings.ContainsRune(" \t\r\n(),;", rune(sql[i])) {
				if sql[i] == '"' {
					i = skipQuoted(sql, i, '"')
				} else {
					i++
				}
			}
			name := sql[start:i]
			if isOrdinarySource(sql, name, i) {
				sources = append(sources, name)
			}
			expect[depth] = false
			continue
		}
		name, end := readIdentifierAt(sql, i)
		if name == "" {
			i++
			continue
		}
		switch strings.ToUpper(name) {
		case sqlKeywordFrom:
			inFrom[depth] = true
			expect[depth] = true
		case "JOIN":
			expect[depth] = true
		case "WHERE", "GROUP", "ORDER", "QUALIFY", "LIMIT", "HAVING", "UNION", "EXCEPT", "INTERSECT", "WINDOW":
			inFrom[depth] = false
			expect[depth] = false
		}
		i = end
	}
	return sources
}

func isOrdinarySource(sql, name string, end int) bool {
	next := skipSpaceAndComments(sql, end)
	return !sqlKeywords[strings.ToUpper(name)] && (next >= len(sql) || sql[next] != '(')
}
