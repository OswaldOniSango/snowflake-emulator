package query

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// The name-capture group excludes ")" as well as "(" and whitespace. Every
// existing caller had a table reference followed by whitespace or the end of
// the statement, so this never mattered until a CTE's closing paren sat
// directly against the last table it named — "(SELECT * FROM orders)" — and
// the capture swallowed it as part of the name, which then failed the
// identifier check below and left the whole reference unqualified.
var contextualTablePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(CREATE\s+(?:OR\s+REPLACE\s+)?(?:(?:TEMP|TEMPORARY|TRANSIENT)\s+)?TABLE(?:\s+IF\s+NOT\s+EXISTS)?\s+)([^\s(;)]+)(\s*\()?`),
	regexp.MustCompile(`(?i)\b(CREATE\s+(?:OR\s+REPLACE\s+)?VIEW\s+)([^\s(;)]+)(\s*\()?`),
	regexp.MustCompile(`(?i)\b(INSERT\s+INTO\s+)([^\s(;)]+)(\s*\()?`),
	regexp.MustCompile(`(?i)\b(UPDATE\s+)([^\s(;)]+)(\s*\()?`),
	regexp.MustCompile(`(?i)\b(DELETE\s+FROM\s+)([^\s(;)]+)(\s*\()?`),
	regexp.MustCompile(`(?i)\b(FROM\s+)([^\s(;)]+)(\s*\()?`),
	regexp.MustCompile(`(?i)\b(JOIN\s+)([^\s(;)]+)(\s*\()?`),
	regexp.MustCompile(`(?i)\b(ALTER\s+TABLE\s+)([^\s(;)]+)(\s*\()?`),
	regexp.MustCompile(`(?i)\b(DROP\s+TABLE(?:\s+IF\s+EXISTS)?\s+)([^\s(;)]+)(\s*\()?`),
	regexp.MustCompile(`(?i)\b(DROP\s+VIEW(?:\s+IF\s+EXISTS)?\s+)([^\s(;)]+)(\s*\()?`),
	// MERGE resolves two tables. The USING pattern cannot match a JOIN's
	// USING (col) list, because the capture group rejects a leading "(".
	regexp.MustCompile(`(?i)\b(DESC(?:RIBE)?\s+TABLE\s+)([^\s(;)]+)(\s*\()?`),
	regexp.MustCompile(`(?i)\b(TRUNCATE\s+TABLE\s+)([^\s(;)]+)(\s*\()?`),
	regexp.MustCompile(`(?i)\b(MERGE\s+INTO\s+)([^\s(;)]+)(\s*\()?`),
	regexp.MustCompile(`(?i)\b(USING\s+)([^\s(;)]+)(\s*\()?`),
}

// sqlKeywords are words a table-reference pattern can capture by accident. The
// clearest case is a MERGE's "WHEN MATCHED THEN UPDATE SET", where the UPDATE
// pattern would otherwise rewrite SET as though it named a table.
// tableFunctionPrefixes are the keywords after which an identifier followed by
// "(" is a table function rather than a table: FROM range(5), JOIN read_csv(…).
//
// INSERT INTO and CREATE TABLE are deliberately absent. A parenthesis there
// introduces a column list, and the name before it is a real table that still
// has to be resolved: INSERT INTO users (id, email) must keep working.
var tableFunctionPrefixes = []string{"FROM", "JOIN", "USING"}

// isTableFunctionCall reports whether a match names a function being called
// rather than a table. prefix is the keyword the pattern matched; call is the
// parenthesis that followed the name, empty when none did.
func isTableFunctionCall(prefix, call string) bool {
	if call == "" {
		return false
	}
	upper := strings.ToUpper(strings.TrimSpace(prefix))
	for _, keyword := range tableFunctionPrefixes {
		if upper == keyword {
			return true
		}
	}
	return false
}

// isCreatingTemporaryTable reports whether prefix — the text a
// contextualTablePatterns match captured before the table name — is the
// preamble of a CREATE TEMP/TEMPORARY TABLE. DuckDB refuses to place a TEMP
// table under an explicit schema at all: it always lives in DuckDB's own
// built-in temp catalog, so the name Snowflake gave it must stay bare rather
// than being qualified like a persistent table's. Only the CREATE TABLE
// pattern's captured prefix can ever contain this keyword, so the check is
// safe to apply uniformly across every pattern's replacement.
func isCreatingTemporaryTable(prefix string) bool {
	return strings.Contains(strings.ToUpper(prefix), "TEMP")
}

// cteAliases returns the names bound by a leading WITH clause, upper-cased to
// match how every other identifier in this file is compared, along with the
// index of the statement that follows the CTE definitions — SELECT, INSERT,
// UPDATE, DELETE or MERGE — or -1 when there is no such clause, or its shape
// was not recognized.
//
// A CTE alias shadows any physical table of the same name for the rest of the
// statement it is defined in — later CTEs and the final query can reference
// it by that bare name — so contextualTablePatterns must never qualify it.
// The trailing index is what tells Classify whether that final statement is
// a query, since Classify has no other way to see past the CTE definitions.
// The scan is defensive rather than a full parser: any shape it does not
// recognize (no WITH clause, a WITH that turns out not to be followed by
// "<ident> AS (") simply stops and returns whatever aliases were already
// found, with -1 for the trailing index, rather than guessing.
func cteAliases(sql string) (aliases map[string]bool, queryStart int) {
	aliases = make(map[string]bool)

	i := findKeyword(sql, "WITH", 0)
	if i < 0 {
		return aliases, -1
	}
	i += len("WITH")

	// WITH RECURSIVE names the whole clause, not a CTE alias.
	if name, end := readIdentifierAt(sql, skipSpaceAndComments(sql, i)); strings.EqualFold(name, "RECURSIVE") {
		i = end
	}

	for {
		name, end := readIdentifierAt(sql, skipSpaceAndComments(sql, i))
		if name == "" {
			return aliases, -1
		}
		aliases[strings.ToUpper(name)] = true

		asName, asEnd := readIdentifierAt(sql, skipSpaceAndComments(sql, end))
		if !strings.EqualFold(asName, "AS") {
			return aliases, -1
		}

		j := skipSpaceAndComments(sql, asEnd)
		if j >= len(sql) || sql[j] != '(' {
			return aliases, -1
		}
		closeAt := matchingParen(sql, j)
		if closeAt < 0 {
			return aliases, -1
		}

		// Whatever comes after this CTE's body is either a comma introducing
		// another one, or the statement the whole clause was building up to.
		next := skipSpaceAndComments(sql, closeAt+1)
		if next >= len(sql) || sql[next] != ',' {
			return aliases, next
		}
		i = next + 1
	}
}

// findKeyword returns the index of the first standalone occurrence of keyword
// at or after from, skipping string literals and comments, or -1 if there is
// none.
func findKeyword(sql, keyword string, from int) int {
	for i := from; i < len(sql); {
		if end, skipped := skipNonCode(sql, i); skipped {
			i = end
			continue
		}
		if isIdentifierStart(sql[i]) {
			name, end := readIdentifierAt(sql, i)
			if strings.EqualFold(name, keyword) {
				return i
			}
			i = end
			continue
		}
		i++
	}
	return -1
}

// readIdentifierAt reads the identifier starting at i, or ("", i) when there
// is none there.
func readIdentifierAt(sql string, i int) (name string, end int) {
	if i >= len(sql) || !isIdentifierStart(sql[i]) {
		return "", i
	}
	end = i + 1
	for end < len(sql) && isIdentifierPart(sql[end]) {
		end++
	}
	return sql[i:end], end
}

// skipSpaceAndComments returns the index of the next code character at or
// after i, skipping whitespace and comments — but never a string literal,
// which is a real token here, not filler between two of them.
//
// This is a forward-only scan, deliberately not built on trimLeadingComments:
// that helper trims with strings.TrimSpace, which strips whitespace off BOTH
// ends of whatever it is given. Handed sql[i:] — everything from i to the end
// of the whole statement — it silently trimmed the statement's own trailing
// newline too, and a length-difference computed against that undercounted
// return shifted every position after it by however much was trimmed off the
// tail. A statement ending in a newline, which is the ordinary case for one
// typed into an editor or sent over HTTP, was enough to misread a CTE alias:
// "test" losing its own first letter to a skip that landed one character too
// far in.
func skipSpaceAndComments(sql string, i int) int {
	for i < len(sql) {
		switch {
		case sql[i] == ' ' || sql[i] == '\t' || sql[i] == '\n' || sql[i] == '\r':
			i++
		case strings.HasPrefix(sql[i:], "--"):
			if newline := strings.IndexByte(sql[i:], '\n'); newline >= 0 {
				i += newline + 1
			} else {
				return len(sql)
			}
		case strings.HasPrefix(sql[i:], "/*"):
			end, skipped := skipNonCode(sql, i)
			if !skipped {
				return len(sql)
			}
			i = end
		default:
			return i
		}
	}
	return i
}

// matchingParen returns the index of the ")" that closes the "(" at open,
// treating string literals and comments as opaque so a ")" inside a CTE
// body's own string values does not end the scan early.
func matchingParen(sql string, open int) int {
	depth := 0
	for i := open; i < len(sql); {
		if end, skipped := skipNonCode(sql, i); skipped {
			i = end
			continue
		}
		switch sql[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
		i++
	}
	return -1
}

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

	aliases, _ := cteAliases(sql)
	result := sql
	for _, pattern := range contextualTablePatterns {
		result = pattern.ReplaceAllStringFunc(result, func(match string) string {
			parts := pattern.FindStringSubmatch(match)
			if len(parts) != 4 {
				return match
			}
			name, call := strings.TrimSpace(parts[2]), parts[3]
			if isTableFunctionCall(parts[1], call) {
				return match
			}
			if strings.Contains(name, ".") || strings.HasPrefix(name, "_") || !identifierPattern.MatchString(name) {
				return match
			}
			if sqlKeywords[strings.ToUpper(name)] || aliases[strings.ToUpper(name)] {
				return match
			}
			if isCreatingTemporaryTable(parts[1]) {
				return match
			}
			return parts[1] + BuildTableName(executionContext.Database, executionContext.Schema, name) + call
		})
	}
	return result
}

// rewriteTablesWithContext validates the logical Snowflake namespace before
// mapping short table names to physical DuckDB names.
func (e *Executor) rewriteTablesWithContext(ctx context.Context, executionContext ExecutionContext, sql string) (string, error) {
	aliases, _ := cteAliases(sql)
	result := sql
	var rewriteErr error
	for _, pattern := range contextualTablePatterns {
		result = pattern.ReplaceAllStringFunc(result, func(match string) string {
			if rewriteErr != nil {
				return match
			}
			parts := pattern.FindStringSubmatch(match)
			if len(parts) != 4 {
				return match
			}
			name, call := strings.TrimSpace(parts[2]), parts[3]
			if isTableFunctionCall(parts[1], call) {
				return match
			}
			if strings.HasPrefix(name, "_") || sqlKeywords[strings.ToUpper(name)] || aliases[strings.ToUpper(name)] {
				return match
			}
			if isCreatingTemporaryTable(parts[1]) {
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
			return parts[1] + physicalName + call
		})
	}
	if rewriteErr != nil {
		return "", rewriteErr
	}
	return e.rewriteFunctionCallsWithContext(ctx, executionContext, result)
}

// rewriteFunctionCallsWithContext qualifies calls to user-defined functions
// the same way table references are qualified, so an unqualified call like
// AREA(3, 4) resolves to the physical MACRO CREATE FUNCTION backs it with.
// A builtin like COUNT or UPPER is never touched: the check is membership in
// the small set of functions actually defined for this database.schema,
// fetched once so a statement with several ordinary function calls costs one
// round trip rather than one per call.
func (e *Executor) rewriteFunctionCallsWithContext(ctx context.Context, executionContext ExecutionContext, sql string) (string, error) {
	if executionContext.Database == "" || executionContext.Schema == "" {
		return sql, nil
	}
	names, err := e.repo.ListFunctionNames(ctx, executionContext.Database, executionContext.Schema)
	if err != nil {
		return "", err
	}
	if len(names) == 0 {
		return sql, nil
	}
	known := make(map[string]bool, len(names))
	for _, name := range names {
		known[strings.ToUpper(name)] = true
	}

	var out strings.Builder
	out.Grow(len(sql))
	for i := 0; i < len(sql); {
		if end, skipped := skipNonCode(sql, i); skipped {
			out.WriteString(sql[i:end])
			i = end
			continue
		}
		if !isIdentifierStart(sql[i]) {
			out.WriteByte(sql[i])
			i++
			continue
		}
		end := i + 1
		for end < len(sql) && isIdentifierPart(sql[end]) {
			end++
		}
		name := sql[i:end]
		if known[strings.ToUpper(name)] && callFollows(sql, end) && !isQualified(sql, i) {
			out.WriteString(BuildTableName(executionContext.Database, executionContext.Schema, name))
		} else {
			out.WriteString(name)
		}
		i = end
	}
	return out.String(), nil
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
		// A temporary table was created bare — DuckDB refuses to place a TEMP
		// table under an explicit schema — and DuckDB's own default search
		// path already finds a bare TEMP table regardless of which database
		// or schema is active, which is what makes leaving this reference
		// unqualified correct rather than merely convenient.
		if temp, err := e.repo.TableIsTemporary(ctx, parts[0]); err != nil {
			return "", false, err
		} else if temp {
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
