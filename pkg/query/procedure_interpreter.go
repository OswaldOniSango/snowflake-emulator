package query

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type procedureInterpreter struct {
	executor         *Executor
	executionContext ExecutionContext
	procedureName    string
	variables        map[string]any
}

type procedureExecution struct {
	result   *Result
	returned bool
}

func newProcedureInterpreter(executor *Executor, executionContext ExecutionContext, procedureName string) *procedureInterpreter {
	return &procedureInterpreter{executor: executor, executionContext: executionContext, procedureName: procedureName, variables: make(map[string]any)}
}

func (i *procedureInterpreter) execute(ctx context.Context, script *procedureScript, arguments []ProcedureArgument, values []string) (*Result, error) {
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
		return nil, err
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
		if IsQuery(sql) {
			result, err := i.executor.QueryWithContext(ctx, i.executionContext, sql)
			return procedureExecution{result: result}, err
		}
		_, err = i.executor.ExecuteWithContext(ctx, i.executionContext, sql)
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
		return "NULL"
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
