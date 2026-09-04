package query

import (
	"fmt"
	"regexp"
	"strings"
)

// Translator converts Snowflake SQL to DuckDB-compatible SQL.
//
// Translation is lexical: the statement is scanned for function calls and each
// one is replaced in place, leaving everything else — including the author's
// own formatting — untouched. It used to parse the statement into a MySQL AST
// and serialize it back, which could not survive Snowflake spellings the MySQL
// grammar rejects, and rewrote the rest of the statement on the way through.
type Translator struct {
	functionMap map[string]FunctionTranslator
}

// FunctionTranslator defines how to translate a specific function.
//
// A function is either a straight rename, or too different to express as one
// and replaced by a marker that handleComplexTransformations expands once the
// whole statement has been scanned.
type FunctionTranslator struct {
	Name   string // DuckDB function name, for a simple rename
	Marker string // Placeholder emitted for a function that needs rewriting
}

// NewTranslator creates a new SQL translator with registered function mappings.
func NewTranslator() *Translator {
	t := &Translator{
		functionMap: make(map[string]FunctionTranslator),
	}
	t.registerFunctions()
	return t
}

// duckDBCoalesce is the DuckDB function both NVL and IFNULL become.
const duckDBCoalesce = "COALESCE"

// registerFunctions registers all Snowflake to DuckDB function translations.
func (t *Translator) registerFunctions() {
	// Simple function renames
	t.functionMap["IFF"] = FunctionTranslator{Name: "IF"}
	t.functionMap["NVL"] = FunctionTranslator{Name: duckDBCoalesce}
	t.functionMap["IFNULL"] = FunctionTranslator{Name: duckDBCoalesce}
	t.functionMap["LISTAGG"] = FunctionTranslator{Name: "STRING_AGG"}
	t.functionMap["OBJECT_CONSTRUCT"] = FunctionTranslator{Name: "json_object"}
	t.functionMap["FLATTEN"] = FunctionTranslator{Name: "UNNEST"}

	// Functions with no DuckDB equivalent of the same shape. Each is replaced
	// by its marker while scanning, and expanded once the whole statement has
	// been rewritten — the expansion needs the argument text, which is only
	// unambiguous after the call's parentheses have been matched.
	//
	// NVL2(a, b, c)            → IF(a IS NOT NULL, b, c)
	// TO_VARIANT(x)            → CAST(x AS JSON)
	// PARSE_JSON(x)            → CAST(x AS JSON)
	// DATEADD(part, n, date)   → (date + INTERVAL n part)
	// DATEDIFF(part, a, b)     → DATE_DIFF('part', a, b)
	// CURRENT_TIMESTAMP and CURRENT_DATE are keywords in DuckDB, not functions,
	// so the empty parentheses Snowflake allows have to come off. Written
	// without them they are not calls at all and the scanner leaves them be.
	t.functionMap["CURRENT_TIMESTAMP"] = FunctionTranslator{Marker: "__CURRENT_TIMESTAMP__"}
	t.functionMap["CURRENT_DATE"] = FunctionTranslator{Marker: "__CURRENT_DATE__"}

	t.functionMap["NVL2"] = FunctionTranslator{Marker: "__NVL2__"}
	t.functionMap["TO_VARIANT"] = FunctionTranslator{Marker: "__TO_VARIANT__"}
	t.functionMap["PARSE_JSON"] = FunctionTranslator{Marker: "__PARSE_JSON__"}
	t.functionMap["DATEADD"] = FunctionTranslator{Marker: "__DATEADD__"}
	t.functionMap["DATEDIFF"] = FunctionTranslator{Marker: "__DATEDIFF__"}
}

// Translate converts Snowflake SQL to DuckDB-compatible SQL.
func (t *Translator) Translate(sql string) (string, error) {
	if sql == "" {
		return "", fmt.Errorf("empty SQL statement")
	}

	// Trim whitespace
	sql = strings.TrimSpace(sql)

	// DuckDB supports TEMP/TEMPORARY tables, but it has no TRANSIENT keyword.
	// A Snowflake transient table is therefore stored as a regular persistent
	// DuckDB table. Its Snowflake-specific kind will eventually live in the
	// emulator catalog once SQL DDL and metadata creation are unified.
	sql = translateCreateTableKind(sql)

	// CREATE TABLE ... AS <query> has no column-definition list to protect —
	// its body is a query like any other, and needs the same function
	// translation a plain SELECT would get. It is peeled off and translated
	// on its own; the DDL preamble is left exactly as it arrived here.
	if preamble, body, ok := splitCreateTableAs(sql); ok {
		return preamble + t.handleComplexTransformations(t.translateFunctionsLexically(body)), nil
	}

	// DDL and the SHOW family carry no function calls worth translating, and
	// a CREATE TABLE body in particular holds type names that must not be
	// touched. leadingSQL looks past a leading comment, so a commented DDL
	// takes this branch too.
	upperSQL := leadingSQL(sql)
	if strings.HasPrefix(upperSQL, "CREATE ") ||
		strings.HasPrefix(upperSQL, "DROP ") ||
		strings.HasPrefix(upperSQL, "ALTER ") ||
		strings.HasPrefix(upperSQL, "TRUNCATE ") ||
		strings.HasPrefix(upperSQL, "SHOW ") ||
		strings.HasPrefix(upperSQL, "DESCRIBE ") ||
		strings.HasPrefix(upperSQL, "DESC ") ||
		strings.HasPrefix(upperSQL, "EXPLAIN ") {
		return sql, nil
	}

	return t.handleComplexTransformations(t.translateFunctionsLexically(sql)), nil
}

// createTableAsPattern recognizes the DDL preamble of CREATE TABLE ... AS
// <query> — the table name may already be DATABASE.SCHEMA_TABLE qualified by
// the time Translate sees it, since table-name resolution runs earlier in the
// pipeline. It deliberately does not match a column-definition CREATE TABLE:
// a "(" right after the name, with no AS before it, does not match "AS\s*"
// and falls through to the untouched pass-through instead.
var createTableAsPattern = regexp.MustCompile(
	`(?is)^CREATE\s+(?:OR\s+REPLACE\s+)?(?:(?:TEMP|TEMPORARY|TRANSIENT)\s+)?TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?[^\s(;]+\s+AS\s*`,
)

// splitCreateTableAs separates a CREATE TABLE ... AS <query> statement into
// its DDL preamble and query body, or reports ok=false for anything else —
// including a column-definition CREATE TABLE, which has no AS <query> to
// split off. A leading comment is skipped only to find the split point;
// preamble still carries it, so nothing about the original text is lost.
func splitCreateTableAs(sql string) (preamble, body string, ok bool) {
	trimmed := trimLeadingComments(sql)
	prefixLen := len(sql) - len(trimmed)

	loc := createTableAsPattern.FindStringIndex(trimmed)
	if loc == nil {
		return "", "", false
	}
	splitAt := prefixLen + loc[1]
	return sql[:splitAt], sql[splitAt:], true
}

// translateCreateTableKind converts Snowflake CREATE TABLE modifiers that
// DuckDB does not understand while preserving modifiers it supports.
func translateCreateTableKind(sql string) string {
	upperSQL := strings.ToUpper(sql)
	replacements := []struct {
		from string
		to   string
	}{
		{"CREATE OR REPLACE TRANSIENT TABLE", "CREATE OR REPLACE TABLE"},
		{"CREATE TRANSIENT TABLE", "CREATE TABLE"},
	}

	for _, replacement := range replacements {
		if strings.HasPrefix(upperSQL, replacement.from) {
			return replacement.to + sql[len(replacement.from):]
		}
	}

	return sql
}

// handleComplexTransformations handles transformations that require more than simple renames.
// This handles marked functions and CURRENT_TIMESTAMP/CURRENT_DATE.
func (t *Translator) handleComplexTransformations(sql string) string {
	// DUAL is Oracle's one-row table, which Snowflake accepts and DuckDB has
	// no need of: SELECT without FROM is already one row.
	sql = removeDualSuffix(sql)

	// CURRENT_TIMESTAMP() and CURRENT_DATE() lose their parentheses.
	sql = t.transformMarkedFunction(sql, "__CURRENT_TIMESTAMP__", func(string) string {
		return "CURRENT_TIMESTAMP"
	})
	sql = t.transformMarkedFunction(sql, "__CURRENT_DATE__", func(string) string {
		return "CURRENT_DATE"
	})

	// Handle TO_VARIANT: __TO_VARIANT__(x) → CAST(x AS JSON)
	sql = t.transformMarkedFunction(sql, "__TO_VARIANT__", func(args string) string {
		return fmt.Sprintf("CAST(%s AS JSON)", args)
	})

	// Handle PARSE_JSON: __PARSE_JSON__(x) → CAST(x AS JSON)
	sql = t.transformMarkedFunction(sql, "__PARSE_JSON__", func(args string) string {
		return fmt.Sprintf("CAST(%s AS JSON)", args)
	})

	// Handle NVL2: __NVL2__(a, b, c) → IF(a IS NOT NULL, b, c)
	sql = t.transformMarkedFunction(sql, "__NVL2__", func(args string) string {
		parts := splitFunctionArgs(args, 3)
		if len(parts) != 3 {
			return "__NVL2__(" + args + ")"
		}
		return fmt.Sprintf("IF(%s IS NOT NULL, %s, %s)",
			strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), strings.TrimSpace(parts[2]))
	})

	// Handle DATEADD: __DATEADD__(part, n, date) → (CAST(date AS DATE) + interval n part)
	sql = t.transformDATEADD(sql)

	// Handle DATEDIFF: __DATEDIFF__(part, start, end) → DATE_DIFF('part', start, end)
	sql = t.transformDATEDIFF(sql)

	return sql
}

// transformMarkedFunction transforms a marked function using a custom transformer.
func (t *Translator) transformMarkedFunction(sql, marker string, transformer func(args string) string) string {
	for {
		idx := strings.Index(sql, marker+"(")
		if idx == -1 {
			break
		}

		// Find the matching closing parenthesis
		start := idx + len(marker) + 1
		depth := 1
		end := start
		for end < len(sql) && depth > 0 {
			switch sql[end] {
			case '(':
				depth++
			case ')':
				depth--
			}
			end++
		}

		if depth == 0 {
			args := sql[start : end-1]
			replacement := transformer(args)
			sql = sql[:idx] + replacement + sql[end:]
		} else {
			break
		}
	}
	return sql
}

// transformDATEADD transforms DATEADD: __DATEADD__(part, n, date) → (CAST(date AS DATE) + interval n part)
func (t *Translator) transformDATEADD(sql string) string {
	return t.transformMarkedFunction(sql, "__DATEADD__", func(args string) string {
		parts := splitFunctionArgs(args, 3)
		if len(parts) != 3 {
			return "__DATEADD__(" + args + ")"
		}
		part := unquoteDatePart(parts[0])
		n := strings.TrimSpace(parts[1])
		date := strings.TrimSpace(parts[2])
		// DuckDB's interval syntax takes a bare keyword, so a quoted part has
		// to lose its quotes: "interval 7 'day'" is a syntax error. The count
		// is parenthesised because a negative one is too: DATEADD(year, -1, d)
		// would otherwise produce "interval -1 year".
		return fmt.Sprintf("(CAST(%s AS DATE) + interval (%s) %s)", date, n, part)
	})
}

// transformDATEDIFF transforms DATEDIFF: __DATEDIFF__(part, start, end) → DATE_DIFF('part', CAST(start AS DATE), CAST(end AS DATE))
func (t *Translator) transformDATEDIFF(sql string) string {
	return t.transformMarkedFunction(sql, "__DATEDIFF__", func(args string) string {
		parts := splitFunctionArgs(args, 3)
		if len(parts) != 3 {
			return "__DATEDIFF__(" + args + ")"
		}
		part := unquoteDatePart(parts[0])
		startDate := strings.TrimSpace(parts[1])
		endDate := strings.TrimSpace(parts[2])
		// DATE_DIFF takes the part as a string, and it is quoted here, so an
		// already-quoted part has to lose its own quotes first.
		return fmt.Sprintf("DATE_DIFF('%s', CAST(%s AS DATE), CAST(%s AS DATE))", part, startDate, endDate)
	})
}

// unquoteDatePart strips the quotes Snowflake allows around a date part.
//
// Snowflake accepts DATEADD(day, ...) and DATEADD('day', ...) alike, but the
// two DuckDB forms need opposite things: interval wants a bare keyword and
// DATE_DIFF wants a string. Normalising to the bare word lets each transform
// add back whatever it needs. Anything that is not a plain identifier once
// unquoted is passed through untouched, since it is not a date part.
func unquoteDatePart(part string) string {
	trimmed := strings.TrimSpace(part)
	if len(trimmed) < 2 {
		return trimmed
	}

	first, last := trimmed[0], trimmed[len(trimmed)-1]
	if first != last || (first != '\'' && first != '"') {
		return trimmed
	}

	inner := trimmed[1 : len(trimmed)-1]
	if !identifierPattern.MatchString(inner) {
		return trimmed
	}
	return inner
}

// removeDualSuffix removes " from dual" suffix (case-insensitive) without regex.
func removeDualSuffix(sql string) string {
	// Trim trailing whitespace first
	trimmed := strings.TrimRight(sql, " \t\n\r")
	lower := strings.ToLower(trimmed)

	// Check for " from dual" at the end
	suffix := " from dual"
	if strings.HasSuffix(lower, suffix) {
		return trimmed[:len(trimmed)-len(suffix)]
	}
	return sql
}

// splitFunctionArgs splits function arguments respecting parentheses nesting.
// expectedCount is a hint for the expected number of arguments.
func splitFunctionArgs(args string, expectedCount int) []string {
	result := make([]string, 0, expectedCount)
	depth := 0
	start := 0

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				result = append(result, args[start:i])
				start = i + 1
			}
		}
	}

	// Add the last argument
	if start < len(args) {
		result = append(result, args[start:])
	}

	return result
}

// translateFunctionsLexically rewrites Snowflake function calls by scanning the
// statement instead of parsing it.
//
// The parser this translator is built on follows MySQL syntax, so it rejects
// Snowflake's "::" cast and misreads "||" as boolean OR. Both used to make
// Translate return the statement untouched, which is only graceful when DuckDB
// happens to know the function: IFF, NVL and the rest do not exist there, so
// the statement failed at execution with "Scalar Function ... does not exist".
//
// Scanning finds the same calls the AST walk does. What follows — the marker
// transformations in handleComplexTransformations — already worked on the
// serialized string, so it is shared by both paths.
func (t *Translator) translateFunctionsLexically(sql string) string {
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

		if replacement, ok := t.lexicalReplacement(name); ok && callFollows(sql, end) && !isQualified(sql, i) {
			out.WriteString(replacement)
		} else {
			out.WriteString(name)
		}
		i = end
	}

	return out.String()
}

// lexicalReplacement returns what a translated function call should be named:
// the DuckDB function for a simple rename, or the marker that
// handleComplexTransformations later expands.
func (t *Translator) lexicalReplacement(name string) (string, bool) {
	translator, exists := t.functionMap[strings.ToUpper(name)]
	if !exists {
		return "", false
	}
	if translator.Marker != "" {
		return translator.Marker, true
	}
	if translator.Name != "" {
		return translator.Name, true
	}
	return "", false
}

// callFollows reports whether an opening parenthesis comes next, so that a
// column happening to share a function's name is left alone.
func callFollows(sql string, from int) bool {
	for i := from; i < len(sql); i++ {
		switch sql[i] {
		case ' ', '\t', '\n', '\r':
		case '(':
			return true
		default:
			return false
		}
	}
	return false
}

// isQualified reports whether the identifier is preceded by a dot, as in
// my_schema.iff(...), where the name belongs to the caller and not to us.
func isQualified(sql string, start int) bool {
	for i := start - 1; i >= 0; i-- {
		switch sql[i] {
		case ' ', '\t', '\n', '\r':
		case '.':
			return true
		default:
			return false
		}
	}
	return false
}

// skipNonCode consumes a region where a function name cannot appear: a string
// literal, a quoted identifier, a dollar-quoted body, or a comment. It returns
// the index just past the region, and whether one was found at i.
func skipNonCode(sql string, i int) (end int, skipped bool) {
	switch {
	case sql[i] == '\'':
		return skipQuoted(sql, i, '\''), true
	case sql[i] == '"':
		return skipQuoted(sql, i, '"'), true
	case strings.HasPrefix(sql[i:], "$$"):
		if closing := strings.Index(sql[i+2:], "$$"); closing >= 0 {
			return i + 2 + closing + 2, true
		}
		return len(sql), true
	case strings.HasPrefix(sql[i:], "--"):
		if newline := strings.IndexByte(sql[i:], '\n'); newline >= 0 {
			return i + newline + 1, true
		}
		return len(sql), true
	case strings.HasPrefix(sql[i:], "/*"):
		if rest, ok := skipBlockComment(sql[i:]); ok {
			return len(sql) - len(rest), true
		}
		return len(sql), true
	}
	return i, false
}

// skipQuoted consumes a quoted region, treating a doubled quote as an escape.
func skipQuoted(sql string, i int, quote byte) int {
	for j := i + 1; j < len(sql); j++ {
		if sql[j] != quote {
			continue
		}
		if j+1 < len(sql) && sql[j+1] == quote {
			j++
			continue
		}
		return j + 1
	}
	return len(sql)
}

func isIdentifierStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isIdentifierPart(c byte) bool {
	return isIdentifierStart(c) || (c >= '0' && c <= '9') || c == '$'
}

// Rewrite records one substitution the translator will make.
type Rewrite struct {
	From string
	To   string
}

// FunctionRewrites reports the function substitutions a statement will undergo.
//
// It walks the statement with the same rules the lexical translation uses —
// skipping literals, comments and dollar-quoted bodies, requiring a call, and
// ignoring qualified names — rather than instrumenting the translation itself.
// Threading state through two translation paths to collect the same facts a
// second scan can derive would couple them for no gain.
func (t *Translator) FunctionRewrites(sql string) []Rewrite {
	seen := map[string]bool{}
	rewrites := make([]Rewrite, 0)

	for i := 0; i < len(sql); {
		if end, skipped := skipNonCode(sql, i); skipped {
			i = end
			continue
		}

		if !isIdentifierStart(sql[i]) {
			i++
			continue
		}

		end := i + 1
		for end < len(sql) && isIdentifierPart(sql[end]) {
			end++
		}
		name := sql[i:end]

		if replacement, ok := t.lexicalReplacement(name); ok && callFollows(sql, end) && !isQualified(sql, i) {
			upper := strings.ToUpper(name)
			if !seen[upper] {
				seen[upper] = true
				rewrites = append(rewrites, Rewrite{From: upper, To: describeReplacement(replacement)})
			}
		}
		i = end
	}

	return rewrites
}

// describeReplacement turns the marker the lexical path emits into the DuckDB
// form a reader recognizes. A marker is an internal placeholder that
// handleComplexTransformations later expands; showing it would be noise.
func describeReplacement(replacement string) string {
	switch replacement {
	case "__NVL2__":
		return "IF(a IS NOT NULL, b, c)"
	case "__TO_VARIANT__", "__PARSE_JSON__":
		return "CAST(… AS JSON)"
	case "__DATEADD__":
		return "date + INTERVAL n part"
	case "__DATEDIFF__":
		return "DATE_DIFF('part', …)"
	default:
		return replacement
	}
}
