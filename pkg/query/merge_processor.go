// Package query provides SQL query execution including MERGE INTO support.
package query

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// MergeAction represents the action to take in a WHEN clause.
type MergeAction int

const (
	// MergeActionUpdate represents WHEN MATCHED THEN UPDATE.
	MergeActionUpdate MergeAction = iota
	// MergeActionDelete represents WHEN MATCHED THEN DELETE.
	MergeActionDelete
	// MergeActionInsert represents WHEN NOT MATCHED THEN INSERT.
	MergeActionInsert
)

// SetClause represents a single SET column = value assignment.
type SetClause struct {
	Column string
	Value  string
}

// WhenClause represents a WHEN MATCHED or WHEN NOT MATCHED clause.
type WhenClause struct {
	IsMatched  bool        // true for WHEN MATCHED, false for WHEN NOT MATCHED
	Condition  string      // Additional AND condition (optional)
	Action     MergeAction // UPDATE, DELETE, or INSERT
	SetClauses []SetClause // For UPDATE SET
	InsertCols []string    // For INSERT (column list)
	InsertVals []string    // For INSERT (VALUES)
}

// MergeStatement represents a parsed MERGE INTO statement.
type MergeStatement struct {
	TargetTable string       // Target table name (may include db.schema.table)
	TargetAlias string       // Alias for target table
	SourceTable string       // Source table name or subquery
	SourceAlias string       // Alias for source table
	OnCondition string       // JOIN condition
	WhenClauses []WhenClause // List of WHEN clauses
}

// mergePatterns holds pre-compiled regex patterns for MERGE statement parsing.
type mergePatterns struct {
	mergeInto      *regexp.Regexp
	using          *regexp.Regexp
	onCondition    *regexp.Regexp
	whenMatched    *regexp.Regexp
	whenNotMatched *regexp.Regexp
	thenUpdate     *regexp.Regexp
	thenDelete     *regexp.Regexp
	thenInsert     *regexp.Regexp
	setClause      *regexp.Regexp
	insertValues   *regexp.Regexp
}

// newMergePatterns creates pre-compiled regex patterns for MERGE parsing.
// Note: Go regexp doesn't support lookahead, so we use simpler patterns
// and handle boundary detection in the parsing logic.
func newMergePatterns() *mergePatterns {
	return &mergePatterns{
		// MERGE INTO target [AS alias] - alias must not be USING
		mergeInto: regexp.MustCompile(`(?i)MERGE\s+INTO\s+(\S+)(?:\s+AS\s+(\w+)|\s+([a-zA-Z_][a-zA-Z0-9_]*))?(?:\s+USING)`),
		// USING source [AS alias] or USING (subquery) [AS alias] - alias must not be ON
		using: regexp.MustCompile(`(?i)USING\s+(\([^)]+\)|[^\s(]+)(?:\s+AS\s+(\w+)|\s+([a-zA-Z_][a-zA-Z0-9_]*))?(?:\s+ON)`),
		// ON condition - we'll extract until WHEN in the parsing logic
		onCondition: regexp.MustCompile(`(?i)\bON\s+(.+)`),
		// WHEN MATCHED [AND condition] THEN
		whenMatched: regexp.MustCompile(`(?i)WHEN\s+MATCHED(?:\s+AND\s+(.+?))?\s+THEN`),
		// WHEN NOT MATCHED [AND condition] THEN
		whenNotMatched: regexp.MustCompile(`(?i)WHEN\s+NOT\s+MATCHED(?:\s+AND\s+(.+?))?\s+THEN`),
		// THEN UPDATE SET ... - we'll handle boundary in parsing logic.
		// (?s) lets "." cross a newline: without it, a SET clause with each
		// assignment on its own line — ordinary, readable formatting — had
		// everything after the first line silently dropped from the capture.
		thenUpdate: regexp.MustCompile(`(?is)THEN\s+UPDATE\s+SET\s+(.+)`),
		// THEN DELETE
		thenDelete: regexp.MustCompile(`(?i)THEN\s+DELETE`),
		// THEN INSERT (cols) VALUES (vals) or THEN INSERT VALUES (vals)
		thenInsert: regexp.MustCompile(`(?i)THEN\s+INSERT\s*(?:\(([^)]*)\))?\s*VALUES\s*\(([^)]+)\)`),
		// SET column = value pattern
		setClause: regexp.MustCompile(`(?i)(\w+(?:\.\w+)?)\s*=\s*([^,]+)`),
		// INSERT (cols) VALUES (vals) capture
		insertValues: regexp.MustCompile(`(?i)\(([^)]+)\)`),
	}
}

// MergeProcessor handles MERGE INTO operations.
type MergeProcessor struct {
	executor   *Executor
	translator *Translator
	patterns   *mergePatterns
}

// NewMergeProcessor creates a new MERGE handler.
func NewMergeProcessor(executor *Executor) *MergeProcessor {
	return &MergeProcessor{
		executor:   executor,
		translator: NewTranslator(),
		patterns:   newMergePatterns(),
	}
}

// ParseMergeStatement parses a MERGE INTO SQL statement.
func (h *MergeProcessor) ParseMergeStatement(sql string) (*MergeStatement, error) {
	sql = strings.TrimSpace(sql)

	stmt := &MergeStatement{}

	// Parse MERGE INTO target [AS alias]
	mergeMatch := h.patterns.mergeInto.FindStringSubmatch(sql)
	if len(mergeMatch) < 2 {
		return nil, fmt.Errorf("invalid MERGE INTO syntax: missing target table")
	}
	stmt.TargetTable = mergeMatch[1]
	// Check for alias (either with AS or without)
	if len(mergeMatch) > 2 && mergeMatch[2] != "" {
		stmt.TargetAlias = mergeMatch[2]
	} else if len(mergeMatch) > 3 && mergeMatch[3] != "" {
		stmt.TargetAlias = mergeMatch[3]
	}

	// Parse USING source [AS alias]
	usingMatch := h.patterns.using.FindStringSubmatch(sql)
	if len(usingMatch) < 2 {
		return nil, fmt.Errorf("invalid MERGE syntax: missing USING clause")
	}
	stmt.SourceTable = usingMatch[1]
	// Check for alias (either with AS or without)
	if len(usingMatch) > 2 && usingMatch[2] != "" {
		stmt.SourceAlias = usingMatch[2]
	} else if len(usingMatch) > 3 && usingMatch[3] != "" {
		stmt.SourceAlias = usingMatch[3]
	}

	// Parse ON condition - extract until first WHEN keyword
	onMatch := h.patterns.onCondition.FindStringSubmatch(sql)
	if len(onMatch) < 2 {
		return nil, fmt.Errorf("invalid MERGE syntax: missing ON condition")
	}
	onCondition := onMatch[1]
	// Truncate at WHEN keyword (case-insensitive)
	whenIdx := strings.Index(strings.ToUpper(onCondition), " WHEN")
	if whenIdx == -1 {
		whenIdx = strings.Index(strings.ToUpper(onCondition), "\nWHEN")
	}
	if whenIdx == -1 {
		whenIdx = strings.Index(strings.ToUpper(onCondition), "\tWHEN")
	}
	if whenIdx != -1 {
		onCondition = onCondition[:whenIdx]
	}
	stmt.OnCondition = strings.TrimSpace(onCondition)

	// Parse WHEN clauses
	whenClauses, err := h.parseWhenClauses(sql)
	if err != nil {
		return nil, fmt.Errorf("error parsing WHEN clauses: %w", err)
	}
	if len(whenClauses) == 0 {
		return nil, fmt.Errorf("invalid MERGE syntax: at least one WHEN clause required")
	}
	stmt.WhenClauses = whenClauses

	return stmt, nil
}

// parseWhenClauses extracts all WHEN clauses from the SQL.
func (h *MergeProcessor) parseWhenClauses(sql string) ([]WhenClause, error) {
	var clauses []WhenClause

	// Find all WHEN MATCHED clauses
	// We need to find the positions of all WHEN clauses and parse them in order
	upperSQL := strings.ToUpper(sql)

	// Split by WHEN keyword and process each section
	whenPattern := regexp.MustCompile(`(?i)\bWHEN\s+`)
	whenIndices := whenPattern.FindAllStringIndex(sql, -1)

	for i, idx := range whenIndices {
		start := idx[0]
		var end int
		if i < len(whenIndices)-1 {
			end = whenIndices[i+1][0]
		} else {
			end = len(sql)
		}

		whenSection := sql[start:end]
		upperWhenSection := upperSQL[start:end]

		clause, err := h.parseWhenClause(whenSection, upperWhenSection)
		if err != nil {
			return nil, err
		}
		clauses = append(clauses, clause)
	}

	return clauses, nil
}

// parseWhenClause parses a single WHEN clause.
func (h *MergeProcessor) parseWhenClause(section, upperSection string) (WhenClause, error) {
	clause := WhenClause{}

	// Determine if MATCHED or NOT MATCHED
	switch {
	case strings.Contains(upperSection, "NOT MATCHED"):
		clause.IsMatched = false
		// Check for additional AND condition
		notMatchedMatch := h.patterns.whenNotMatched.FindStringSubmatch(section)
		if len(notMatchedMatch) > 1 && notMatchedMatch[1] != "" {
			clause.Condition = strings.TrimSpace(notMatchedMatch[1])
		}
	case strings.Contains(upperSection, "MATCHED"):
		clause.IsMatched = true
		// Check for additional AND condition
		matchedMatch := h.patterns.whenMatched.FindStringSubmatch(section)
		if len(matchedMatch) > 1 && matchedMatch[1] != "" {
			clause.Condition = strings.TrimSpace(matchedMatch[1])
		}
	default:
		return clause, fmt.Errorf("invalid WHEN clause: %s", section)
	}

	// Determine action (UPDATE, DELETE, or INSERT). Matched against the
	// compiled patterns rather than a literal "THEN UPDATE" substring: SQL
	// formatted across several lines — "THEN\n    UPDATE SET", entirely
	// ordinary style — has more than the one space Contains required, and
	// fell through to every case as an "invalid WHEN clause action".
	switch {
	case h.patterns.thenDelete.MatchString(section):
		clause.Action = MergeActionDelete
	case h.patterns.thenUpdate.MatchString(section):
		clause.Action = MergeActionUpdate
		// Parse SET clauses
		updateMatch := h.patterns.thenUpdate.FindStringSubmatch(section)
		if len(updateMatch) > 1 {
			setStr := updateMatch[1]
			// Truncate at a following WHEN clause, if the (now dotall) capture
			// reached one — section is already bounded to this one clause by
			// parseWhenClauses, so this is a safety net rather than the usual
			// case. Matched with whitespace generally, not a literal single
			// space, for the same reason the SET capture itself needed (?s).
			if loc := nextWhenClausePattern.FindStringIndex(setStr); loc != nil {
				setStr = setStr[:loc[0]]
			}
			setClauses, err := h.parseSetClauses(setStr)
			if err != nil {
				return clause, err
			}
			clause.SetClauses = setClauses
		}
	case h.patterns.thenInsert.MatchString(section):
		clause.Action = MergeActionInsert
		cols, vals, err := parseInsertClause(section)
		if err != nil {
			return clause, err
		}
		clause.InsertCols = cols
		clause.InsertVals = vals
	default:
		return clause, fmt.Errorf("invalid WHEN clause action: %s", section)
	}

	return clause, nil
}

// parseSetClauses parses UPDATE SET assignments.
func (h *MergeProcessor) parseSetClauses(setStr string) ([]SetClause, error) {
	var clauses []SetClause

	// Split by comma, but be careful of commas inside function calls
	parts := splitByCommaRespectingParens(setStr)

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Match column = value
		eqIdx := strings.Index(part, "=")
		if eqIdx == -1 {
			return nil, fmt.Errorf("invalid SET clause: %s", part)
		}

		clauses = append(clauses, SetClause{
			Column: strings.TrimSpace(part[:eqIdx]),
			Value:  strings.TrimSpace(part[eqIdx+1:]),
		})
	}

	return clauses, nil
}

// insertKeywordPattern and valuesKeywordPattern locate INSERT and VALUES as
// standalone words, so parseInsertClause can find the parenthesized lists that
// follow each one without trusting a regex to also capture their contents.
var (
	insertKeywordPattern = regexp.MustCompile(`(?i)\bINSERT\b`)
	valuesKeywordPattern = regexp.MustCompile(`(?i)\bVALUES\b`)

	// nextWhenClausePattern finds a WHEN clause possibly separated from the
	// text before it by a newline rather than a plain space.
	nextWhenClausePattern = regexp.MustCompile(`(?i)\bWHEN\b`)
)

// parseInsertClause extracts the optional column list and the VALUES list
// from a WHEN NOT MATCHED THEN INSERT section.
//
// Regex alone cannot capture either parenthesized list correctly: a pattern
// built on [^)]+ between the two parens has no way to look past a value that
// is itself a function call — CURRENT_TIMESTAMP(), say — since that call's own
// closing paren satisfies the character class first and truncates the whole
// capture right there, silently dropping the closing paren of the list
// itself along with anything meant to follow it. Finding the true matching
// paren the same way a CTE's body is found avoids that.
func parseInsertClause(section string) (cols, vals []string, err error) {
	insertLoc := insertKeywordPattern.FindStringIndex(section)
	if insertLoc == nil {
		return nil, nil, fmt.Errorf("invalid WHEN clause action: %s", section)
	}

	pos := skipSpaceAndComments(section, insertLoc[1])
	if pos < len(section) && section[pos] == '(' {
		closeAt := matchingParen(section, pos)
		if closeAt < 0 {
			return nil, nil, fmt.Errorf("unterminated INSERT column list: %s", section)
		}
		cols = parseCommaSeparated(section[pos+1 : closeAt])
		pos = skipSpaceAndComments(section, closeAt+1)
	}

	valuesLoc := valuesKeywordPattern.FindStringIndex(section[pos:])
	if valuesLoc == nil {
		return nil, nil, fmt.Errorf("INSERT is missing VALUES: %s", section)
	}
	pos = skipSpaceAndComments(section, pos+valuesLoc[1])
	if pos >= len(section) || section[pos] != '(' {
		return nil, nil, fmt.Errorf("INSERT VALUES must be followed by (...): %s", section)
	}
	closeAt := matchingParen(section, pos)
	if closeAt < 0 {
		return nil, nil, fmt.Errorf("unterminated INSERT VALUES list: %s", section)
	}
	vals = parseCommaSeparated(section[pos+1 : closeAt])

	return cols, vals, nil
}

// parseCommaSeparated splits a comma-separated string into parts.
func parseCommaSeparated(s string) []string {
	parts := splitByCommaRespectingParens(s)
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// splitByCommaRespectingParens splits by comma while respecting parentheses nesting.
func splitByCommaRespectingParens(s string) []string {
	var parts []string
	var current strings.Builder
	depth := 0

	for _, r := range s {
		switch r {
		case '(':
			depth++
			current.WriteRune(r)
		case ')':
			depth--
			current.WriteRune(r)
		case ',':
			if depth == 0 {
				parts = append(parts, current.String())
				current.Reset()
			} else {
				current.WriteRune(r)
			}
		default:
			current.WriteRune(r)
		}
	}

	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

	return parts
}

// ExecuteMerge executes a parsed MERGE statement.
// Strategy: Try native DuckDB MERGE first. If unsupported, decompose into UPDATE/DELETE/INSERT.
func (h *MergeProcessor) ExecuteMerge(ctx context.Context, executionContext ExecutionContext, stmt *MergeStatement) (*MergeResult, error) {
	result := &MergeResult{}

	// Resolve table names once, up front. The decomposed fallback substitutes
	// the target's alias with stmt.TargetTable inside SET and WHERE clauses,
	// where the contextual rewriter cannot reach it, so both paths must start
	// from the physical name.
	stmt, err := h.resolveMergeTables(ctx, stmt, executionContext)
	if err != nil {
		return nil, err
	}

	// Build the native MERGE SQL
	mergeSQL := h.buildMergeSQL(stmt)

	// Try native execution first (DuckDB 1.4+ supports MERGE)
	execResult, err := h.executor.executeRawWithContext(ctx, executionContext, mergeSQL)
	if err == nil {
		// Native MERGE succeeded
		// DuckDB returns total rows affected; we can't distinguish insert/update/delete
		result.RowsUpdated = execResult.RowsAffected
		return result, nil
	}

	// If native MERGE fails (older DuckDB version), decompose into separate statements
	return h.executeDecomposedMerge(ctx, executionContext, stmt)
}

// resolveMergeTables returns a copy of stmt whose unqualified table names are
// mapped to their physical DuckDB names. Without an execution context, or for
// names already qualified or shaped like a subquery, the statement is unchanged.
//
// A name that is already a temporary table is left bare rather than
// qualified: this runs after the calling statement has already been rewritten
// once — inside a procedure body, a temp table's logical name has already
// become its mangled physical one, and inside a plain MERGE its physical name
// has already been resolved by the same rules a CREATE would go through — so
// qualifying it again here would double it (TEST_DB.PUBLIC___PROC_TEMP_...),
// a name that was never created and could never resolve to the real table.
func (h *MergeProcessor) resolveMergeTables(
	ctx context.Context,
	stmt *MergeStatement,
	executionContext ExecutionContext,
) (*MergeStatement, error) {
	if executionContext.Database == "" || executionContext.Schema == "" {
		return stmt, nil
	}

	resolve := func(name string) (string, error) {
		if strings.Contains(name, ".") || !identifierPattern.MatchString(name) {
			return name, nil
		}
		temp, err := h.executor.repo.TableIsTemporary(ctx, name)
		if err != nil {
			return "", err
		}
		if temp {
			return name, nil
		}
		return BuildTableName(executionContext.Database, executionContext.Schema, name), nil
	}

	resolved := *stmt
	var err error
	if resolved.TargetTable, err = resolve(stmt.TargetTable); err != nil {
		return nil, err
	}
	if resolved.SourceTable, err = resolve(stmt.SourceTable); err != nil {
		return nil, err
	}
	return &resolved, nil
}

// buildMergeSQL constructs the MERGE SQL statement for native execution.
func (h *MergeProcessor) buildMergeSQL(stmt *MergeStatement) string {
	var sb strings.Builder

	// MERGE INTO target [alias]
	sb.WriteString("MERGE INTO ")
	sb.WriteString(stmt.TargetTable)
	if stmt.TargetAlias != "" {
		sb.WriteString(" AS ")
		sb.WriteString(stmt.TargetAlias)
	}

	// USING source [alias]
	sb.WriteString(" USING ")
	sb.WriteString(stmt.SourceTable)
	if stmt.SourceAlias != "" {
		sb.WriteString(" AS ")
		sb.WriteString(stmt.SourceAlias)
	}

	// ON condition
	sb.WriteString(" ON ")
	sb.WriteString(stmt.OnCondition)

	// WHEN clauses
	for i := range stmt.WhenClauses {
		sb.WriteString(" ")
		sb.WriteString(h.buildWhenClause(&stmt.WhenClauses[i]))
	}

	return sb.String()
}

// buildWhenClause builds a single WHEN clause.
func (h *MergeProcessor) buildWhenClause(when *WhenClause) string {
	var sb strings.Builder

	if when.IsMatched {
		sb.WriteString("WHEN MATCHED")
	} else {
		sb.WriteString("WHEN NOT MATCHED")
	}

	if when.Condition != "" {
		sb.WriteString(" AND ")
		sb.WriteString(when.Condition)
	}

	sb.WriteString(" THEN ")

	switch when.Action {
	case MergeActionDelete:
		sb.WriteString("DELETE")
	case MergeActionUpdate:
		sb.WriteString("UPDATE SET ")
		var sets []string
		for _, sc := range when.SetClauses {
			sets = append(sets, sc.Column+" = "+sc.Value)
		}
		sb.WriteString(strings.Join(sets, ", "))
	case MergeActionInsert:
		sb.WriteString("INSERT")
		if len(when.InsertCols) > 0 {
			sb.WriteString(" (")
			sb.WriteString(strings.Join(when.InsertCols, ", "))
			sb.WriteString(")")
		}
		sb.WriteString(" VALUES (")
		sb.WriteString(strings.Join(when.InsertVals, ", "))
		sb.WriteString(")")
	}

	return sb.String()
}

// executeDecomposedMerge executes MERGE as separate UPDATE/DELETE/INSERT statements.
// This fallback is used when native MERGE is not supported.
func (h *MergeProcessor) executeDecomposedMerge(ctx context.Context, executionContext ExecutionContext, stmt *MergeStatement) (*MergeResult, error) {
	result := &MergeResult{}

	// Process WHEN MATCHED clauses first (UPDATE/DELETE)
	for i := range stmt.WhenClauses {
		when := &stmt.WhenClauses[i]
		if !when.IsMatched {
			continue
		}

		switch when.Action {
		case MergeActionUpdate:
			rows, err := h.executeMatchedUpdate(ctx, executionContext, stmt, when)
			if err != nil {
				return result, fmt.Errorf("MERGE UPDATE failed: %w", err)
			}
			result.RowsUpdated += rows

		case MergeActionDelete:
			rows, err := h.executeMatchedDelete(ctx, executionContext, stmt, when)
			if err != nil {
				return result, fmt.Errorf("MERGE DELETE failed: %w", err)
			}
			result.RowsDeleted += rows
		}
	}

	// Process WHEN NOT MATCHED clauses (INSERT)
	for i := range stmt.WhenClauses {
		when := &stmt.WhenClauses[i]
		if when.IsMatched {
			continue
		}

		if when.Action == MergeActionInsert {
			rows, err := h.executeNotMatchedInsert(ctx, executionContext, stmt, when)
			if err != nil {
				return result, fmt.Errorf("MERGE INSERT failed: %w", err)
			}
			result.RowsInserted += rows
		}
	}

	return result, nil
}

// executeMatchedUpdate executes UPDATE for WHEN MATCHED THEN UPDATE.
func (h *MergeProcessor) executeMatchedUpdate(ctx context.Context, executionContext ExecutionContext, stmt *MergeStatement, when *WhenClause) (int64, error) {
	// Build: UPDATE target SET ... FROM source WHERE join_condition [AND when_condition]
	// DuckDB requires the table name (not alias) in UPDATE clause
	var sb strings.Builder

	sb.WriteString("UPDATE ")
	sb.WriteString(stmt.TargetTable)
	sb.WriteString(" SET ")

	// DuckDB's UPDATE ... SET accepts only a bare column name on the left —
	// "Qualified column names in UPDATE .. SET not supported" — even though
	// the same alias is fine in FROM, WHERE, and on the right-hand side of
	// the assignment. "target.name = source.name" is ordinary Snowflake
	// MERGE syntax, so the alias (or the table name itself, had the author
	// written that instead) has to come off the column being assigned to.
	var sets []string
	for _, sc := range when.SetClauses {
		col := sc.Column
		val := sc.Value
		if stmt.TargetAlias != "" {
			col = strings.TrimPrefix(col, stmt.TargetAlias+".")
		}
		col = strings.TrimPrefix(col, stmt.TargetTable+".")
		sets = append(sets, col+" = "+val)
	}
	sb.WriteString(strings.Join(sets, ", "))

	// FROM clause for the source
	sb.WriteString(" FROM ")
	sb.WriteString(stmt.SourceTable)
	if stmt.SourceAlias != "" {
		sb.WriteString(" AS ")
		sb.WriteString(stmt.SourceAlias)
	}

	// WHERE clause with join condition
	// Replace target alias with table name in condition
	onCondition := stmt.OnCondition
	if stmt.TargetAlias != "" {
		onCondition = strings.ReplaceAll(onCondition, stmt.TargetAlias+".", stmt.TargetTable+".")
	}
	sb.WriteString(" WHERE ")
	sb.WriteString(onCondition)

	// Additional AND condition
	if when.Condition != "" {
		condition := when.Condition
		if stmt.TargetAlias != "" {
			condition = strings.ReplaceAll(condition, stmt.TargetAlias+".", stmt.TargetTable+".")
		}
		sb.WriteString(" AND ")
		sb.WriteString(condition)
	}

	execResult, err := h.executor.executeRawWithContext(ctx, executionContext, sb.String())
	if err != nil {
		return 0, err
	}

	return execResult.RowsAffected, nil
}

// executeMatchedDelete executes DELETE for WHEN MATCHED THEN DELETE.
func (h *MergeProcessor) executeMatchedDelete(ctx context.Context, executionContext ExecutionContext, stmt *MergeStatement, when *WhenClause) (int64, error) {
	// Build: DELETE FROM target USING source WHERE join_condition [AND when_condition]
	var sb strings.Builder

	sb.WriteString("DELETE FROM ")
	sb.WriteString(stmt.TargetTable)

	// USING clause for the source (DuckDB syntax)
	sb.WriteString(" USING ")
	sb.WriteString(stmt.SourceTable)
	if stmt.SourceAlias != "" {
		sb.WriteString(" AS ")
		sb.WriteString(stmt.SourceAlias)
	}

	// WHERE clause with join condition
	sb.WriteString(" WHERE ")
	sb.WriteString(stmt.OnCondition)

	// Additional AND condition
	if when.Condition != "" {
		sb.WriteString(" AND ")
		sb.WriteString(when.Condition)
	}

	execResult, err := h.executor.executeRawWithContext(ctx, executionContext, sb.String())
	if err != nil {
		return 0, err
	}

	return execResult.RowsAffected, nil
}

// executeNotMatchedInsert executes INSERT for WHEN NOT MATCHED THEN INSERT.
func (h *MergeProcessor) executeNotMatchedInsert(ctx context.Context, executionContext ExecutionContext, stmt *MergeStatement, when *WhenClause) (int64, error) {
	// Build: INSERT INTO target (cols) SELECT vals FROM source WHERE NOT EXISTS (...)
	var sb strings.Builder

	sb.WriteString("INSERT INTO ")
	sb.WriteString(stmt.TargetTable)

	if len(when.InsertCols) > 0 {
		sb.WriteString(" (")
		sb.WriteString(strings.Join(when.InsertCols, ", "))
		sb.WriteString(")")
	}

	// SELECT from source where no match exists
	sb.WriteString(" SELECT ")
	sb.WriteString(strings.Join(when.InsertVals, ", "))
	sb.WriteString(" FROM ")
	sb.WriteString(stmt.SourceTable)
	if stmt.SourceAlias != "" {
		sb.WriteString(" AS ")
		sb.WriteString(stmt.SourceAlias)
	}

	// WHERE NOT EXISTS to find non-matching rows
	sb.WriteString(" WHERE NOT EXISTS (SELECT 1 FROM ")
	sb.WriteString(stmt.TargetTable)
	if stmt.TargetAlias != "" {
		sb.WriteString(" AS ")
		sb.WriteString(stmt.TargetAlias)
	}
	sb.WriteString(" WHERE ")
	sb.WriteString(stmt.OnCondition)
	sb.WriteString(")")

	// Additional AND condition for the source
	if when.Condition != "" {
		sb.WriteString(" AND ")
		sb.WriteString(when.Condition)
	}

	execResult, err := h.executor.executeRawWithContext(ctx, executionContext, sb.String())
	if err != nil {
		return 0, err
	}

	return execResult.RowsAffected, nil
}
