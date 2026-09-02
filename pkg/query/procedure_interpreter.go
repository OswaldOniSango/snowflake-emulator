package query

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

// sqlStateInternalError is the SQLSTATE the interpreter reports for a failure
// with no more specific mapping.
const sqlStateInternalError = "XX000"

var procedureTemporaryTablePattern = regexp.MustCompile(`(?i)^\s*CREATE\s+(?:OR\s+REPLACE\s+)?(?:TEMP|TEMPORARY)\s+TABLE(?:\s+IF\s+NOT\s+EXISTS)?\s+([A-Za-z_][A-Za-z0-9_$]*)`)

type procedureInterpreter struct {
	executor         *Executor
	executionContext ExecutionContext
	procedureName    string
	variables        map[string]any
	temporaryTables  map[string]string
	invocationID     string
}

type procedureExecution struct {
	result   *Result
	returned bool
}

func newProcedureInterpreter(executor *Executor, executionContext ExecutionContext, procedureName string) *procedureInterpreter {
	return &procedureInterpreter{
		executor:         executor,
		executionContext: executionContext,
		procedureName:    procedureName,
		variables:        make(map[string]any),
		temporaryTables:  make(map[string]string),
		invocationID:     strings.ReplaceAll(uuid.NewString(), "-", ""),
	}
}

func (i *procedureInterpreter) execute(ctx context.Context, script *procedureScript, arguments []ProcedureArgument, values []string) (result *Result, err error) {
	defer func() {
		cleanupErr := i.cleanupTemporaryTables(ctx)
		if err == nil && cleanupErr != nil {
			err = cleanupErr
			result = nil
		}
	}()

	for index, argument := range arguments {
		value, err := i.evaluateScalar(ctx, values[index], false)
		if err != nil {
			return nil, fmt.Errorf("invalid value for argument %s: %w", argument.Name, err)
		}
		i.variables[strings.ToUpper(argument.Name)] = value
	}
	for _, declaration := range script.Declarations {
		if _, exists := i.variables[declaration.Name]; exists {
			return nil, fmt.Errorf("variable %s is already defined", declaration.Name)
		}
		var value any
		if declaration.DefaultSQL != "" {
			var err error
			value, err = i.evaluateScalar(ctx, declaration.DefaultSQL, true)
			if err != nil {
				return nil, fmt.Errorf("failed to evaluate DEFAULT for %s: %w", declaration.Name, err)
			}
		}
		i.variables[declaration.Name] = value
	}
	execution, err := i.executeStatements(ctx, script.Statements)
	if err != nil {
		if script.ExceptionHandler == nil {
			return nil, err
		}
		i.variables["SQLCODE"] = int64(-1)
		i.variables["SQLSTATE"] = sqlStateInternalError
		i.variables["SQLERRM"] = err.Error()
		execution, err = i.executeStatements(ctx, script.ExceptionHandler)
		if err != nil {
			return nil, fmt.Errorf("procedure exception handler failed: %w", err)
		}
	}
	if execution.returned || execution.result != nil {
		return execution.result, nil
	}
	columns := []string{i.procedureName}
	return &Result{Columns: columns, ColumnTypes: textColumnMetadata(columns), Rows: [][]interface{}{{nil}}}, nil
}

func (i *procedureInterpreter) executeStatements(ctx context.Context, statements []procedureStatement) (procedureExecution, error) {
	var execution procedureExecution
	for _, statement := range statements {
		current, err := i.executeStatement(ctx, statement)
		if err != nil {
			return procedureExecution{}, err
		}
		if current.result != nil {
			execution.result = current.result
		}
		if current.returned {
			return current, nil
		}
	}
	return execution, nil
}

func (i *procedureInterpreter) executeStatement(ctx context.Context, statement procedureStatement) (procedureExecution, error) {
	switch statement := statement.(type) {
	case procedureSQLStatement:
		sql, err := i.bindVariables(statement.SQL, false)
		if err != nil {
			return procedureExecution{}, err
		}
		sql, droppedTemporaryTable := i.rewriteTemporaryTableReferences(sql)
		if IsQuery(sql) {
			result, err := i.executor.QueryWithContext(ctx, i.executionContext, sql)
			return procedureExecution{result: result}, err
		}
		_, err = i.executor.ExecuteWithContext(ctx, i.executionContext, sql)
		if err == nil && droppedTemporaryTable != "" {
			delete(i.temporaryTables, droppedTemporaryTable)
		}
		return procedureExecution{}, err
	case procedureAssignmentStatement:
		if _, exists := i.variables[statement.Name]; !exists {
			return procedureExecution{}, fmt.Errorf("variable %s is not declared", statement.Name)
		}
		value, err := i.evaluateScalar(ctx, statement.Expression, true)
		if err != nil {
			return procedureExecution{}, fmt.Errorf("failed to assign %s: %w", statement.Name, err)
		}
		i.variables[statement.Name] = value
		return procedureExecution{}, nil
	case procedureReturnStatement:
		expression, err := i.bindVariables(statement.Expression, true)
		if err != nil {
			return procedureExecution{}, err
		}
		expression, _ = i.rewriteTemporaryTableReferences(expression)
		result, err := i.executor.QueryWithContext(ctx, i.executionContext, "SELECT "+expression+" AS "+i.procedureName)
		return procedureExecution{result: result, returned: true}, err
	case procedureIfStatement:
		condition, err := i.evaluateBoolean(ctx, statement.Condition)
		if err != nil {
			return procedureExecution{}, fmt.Errorf("failed to evaluate IF condition: %w", err)
		}
		if condition {
			return i.executeStatements(ctx, statement.ThenBranch)
		}
		return i.executeStatements(ctx, statement.ElseBranch)
	case procedureCaseStatement:
		caseValue, err := i.evaluateScalar(ctx, statement.Expression, true)
		if err != nil {
			return procedureExecution{}, fmt.Errorf("failed to evaluate CASE expression: %w", err)
		}
		for _, branch := range statement.Branches {
			branchValue, err := i.evaluateScalar(ctx, branch.Value, true)
			if err != nil {
				return procedureExecution{}, fmt.Errorf("failed to evaluate CASE branch: %w", err)
			}
			if procedureValuesEqual(caseValue, branchValue) {
				return i.executeStatements(ctx, branch.Statements)
			}
		}
		return i.executeStatements(ctx, statement.ElseBranch)
	default:
		return procedureExecution{}, fmt.Errorf("unsupported procedure statement %T", statement)
	}
}

func (i *procedureInterpreter) rewriteTemporaryTableReferences(sql string) (string, string) {
	if match := procedureTemporaryTablePattern.FindStringSubmatch(sql); match != nil {
		logicalName := strings.ToUpper(match[1])
		if _, exists := i.temporaryTables[logicalName]; !exists {
			physicalName := fmt.Sprintf("__PROC_TEMP_%s_%s_%s_%s",
				i.invocationID,
				strings.ToUpper(i.executionContext.Database),
				strings.ToUpper(i.executionContext.Schema),
				logicalName,
			)
			i.temporaryTables[logicalName] = physicalName
		}
	}

	droppedTemporaryTable := ""
	result := sql
	for _, pattern := range contextualTablePatterns {
		result = pattern.ReplaceAllStringFunc(result, func(match string) string {
			parts := pattern.FindStringSubmatch(match)
			if len(parts) != 3 {
				return match
			}
			logicalName := strings.ToUpper(strings.TrimSpace(parts[2]))
			physicalName, exists := i.temporaryTables[logicalName]
			if !exists {
				return match
			}
			if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(parts[1])), "DROP TABLE") {
				droppedTemporaryTable = logicalName
			}
			return parts[1] + physicalName
		})
	}
	return result, droppedTemporaryTable
}

func (i *procedureInterpreter) cleanupTemporaryTables(ctx context.Context) error {
	for logicalName, physicalName := range i.temporaryTables {
		if _, err := i.executor.ExecuteWithContext(ctx, i.executionContext, "DROP TABLE IF EXISTS "+physicalName); err != nil {
			return fmt.Errorf("failed to clean temporary table %s: %w", logicalName, err)
		}
		delete(i.temporaryTables, logicalName)
	}
	return nil
}

func (i *procedureInterpreter) evaluateBoolean(ctx context.Context, expression string) (bool, error) {
	value, err := i.evaluateScalar(ctx, expression, true)
	if err != nil {
		return false, err
	}
	if value == nil {
		return false, nil
	}
	boolean, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("condition returned %T, expected BOOLEAN", value)
	}
	return boolean, nil
}

func (i *procedureInterpreter) evaluateScalar(ctx context.Context, expression string, replaceBareVariables bool) (any, error) {
	expression = trimOuterParentheses(strings.TrimSpace(expression))
	bound, err := i.bindVariables(expression, replaceBareVariables)
	if err != nil {
		return nil, err
	}
	bound, _ = i.rewriteTemporaryTableReferences(bound)
	query := "SELECT " + bound + " AS value"
	if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(bound)), "SELECT ") {
		query = bound
	}
	result, err := i.executor.QueryWithContext(ctx, i.executionContext, query)
	if err != nil {
		return nil, err
	}
	if len(result.Rows) != 1 || len(result.Rows[0]) != 1 {
		return nil, fmt.Errorf("scalar expression returned %d rows and %d columns", len(result.Rows), scalarColumnCount(result))
	}
	return result.Rows[0][0], nil
}

func scalarColumnCount(result *Result) int {
	if len(result.Rows) == 0 {
		return len(result.Columns)
	}
	return len(result.Rows[0])
}

func (i *procedureInterpreter) bindVariables(input string, replaceBareVariables bool) (string, error) {
	input, err := i.bindDynamicIdentifiers(input)
	if err != nil {
		return "", err
	}

	var output strings.Builder
	for position := 0; position < len(input); {
		if input[position] == '\'' {
			start := position
			position++
			for position < len(input) {
				if input[position] == '\'' {
					position++
					if position < len(input) && input[position] == '\'' {
						position++
						continue
					}
					break
				}
				position++
			}
			output.WriteString(input[start:position])
			continue
		}
		if input[position] == ':' {
			name, end := readProcedureVariableName(input, position+1)
			if name == "" {
				output.WriteByte(input[position])
				position++
				continue
			}
			value, exists := i.variables[strings.ToUpper(name)]
			if !exists {
				return "", fmt.Errorf("variable %s is not declared", name)
			}
			output.WriteString(procedureSQLLiteral(value))
			position = end
			continue
		}
		if replaceBareVariables && (unicode.IsLetter(rune(input[position])) || input[position] == '_') {
			name, end := readProcedureVariableName(input, position)
			if value, exists := i.variables[strings.ToUpper(name)]; exists {
				output.WriteString(procedureSQLLiteral(value))
			} else {
				output.WriteString(input[position:end])
			}
			position = end
			continue
		}
		output.WriteByte(input[position])
		position++
	}
	return output.String(), nil
}

func (i *procedureInterpreter) bindDynamicIdentifiers(input string) (string, error) {
	var output strings.Builder
	for position := 0; position < len(input); {
		if input[position] == '\'' {
			start := position
			position++
			for position < len(input) {
				if input[position] == '\'' {
					position++
					if position < len(input) && input[position] == '\'' {
						position++
						continue
					}
					break
				}
				position++
			}
			output.WriteString(input[start:position])
			continue
		}

		name, end, matched := readDynamicIdentifierReference(input, position)
		if !matched {
			output.WriteByte(input[position])
			position++
			continue
		}
		value, exists := i.variables[strings.ToUpper(name)]
		if !exists {
			return "", fmt.Errorf("variable %s is not declared", name)
		}
		identifier, ok := value.(string)
		if !ok {
			return "", fmt.Errorf("IDENTIFIER variable %s returned %T, expected VARCHAR", name, value)
		}
		if !identifierPattern.MatchString(identifier) {
			return "", fmt.Errorf("IDENTIFIER variable %s contains invalid object name %q", name, identifier)
		}
		output.WriteString(identifier)
		position = end
	}
	return output.String(), nil
}

func readDynamicIdentifierReference(input string, start int) (string, int, bool) {
	const keyword = "IDENTIFIER"
	if start > 0 && isIdentifierCharacter(rune(input[start-1])) {
		return "", start, false
	}
	if start+len(keyword) > len(input) || !strings.EqualFold(input[start:start+len(keyword)], keyword) {
		return "", start, false
	}
	position := start + len(keyword)
	if position < len(input) && isIdentifierCharacter(rune(input[position])) {
		return "", start, false
	}
	for position < len(input) && unicode.IsSpace(rune(input[position])) {
		position++
	}
	if position >= len(input) || input[position] != '(' {
		return "", start, false
	}
	position++
	for position < len(input) && unicode.IsSpace(rune(input[position])) {
		position++
	}
	if position >= len(input) || input[position] != ':' {
		return "", start, false
	}
	name, position := readProcedureVariableName(input, position+1)
	if name == "" {
		return "", start, false
	}
	for position < len(input) && unicode.IsSpace(rune(input[position])) {
		position++
	}
	if position >= len(input) || input[position] != ')' {
		return "", start, false
	}
	return name, position + 1, true
}

func readProcedureVariableName(input string, start int) (string, int) {
	end := start
	for end < len(input) && isIdentifierCharacter(rune(input[end])) {
		end++
	}
	return input[start:end], end
}

func procedureSQLLiteral(value any) string {
	switch value := value.(type) {
	case nil:
		return ValueNull
	case string:
		return "'" + strings.ReplaceAll(value, "'", "''") + "'"
	case []byte:
		return "'" + strings.ReplaceAll(string(value), "'", "''") + "'"
	case bool:
		return strconv.FormatBool(value)
	case time.Time:
		return "TIMESTAMP '" + value.Format("2006-01-02 15:04:05.999999999") + "'"
	default:
		return fmt.Sprint(value)
	}
}

func procedureValuesEqual(left, right any) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return fmt.Sprint(left) == fmt.Sprint(right)
}
