package query

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

var variableTypePattern = regexp.MustCompile(`^(BOOLEAN|TINYINT|SMALLINT|INTEGER|BIGINT|HUGEINT|UTINYINT|USMALLINT|UINTEGER|UBIGINT|UHUGEINT|FLOAT|DOUBLE|VARCHAR|DATE|TIME|TIMESTAMP|TIMESTAMP WITH TIME ZONE|DECIMAL\([0-9]+,[0-9]+\))$`)

// expandSessionVariables leaves literals, comments, dollar-quoted procedure
// bodies and positional bind placeholders alone. Values are typed SQL literals,
// not executable user-provided fragments.
func expandSessionVariables(sql string, ec ExecutionContext) (string, error) {
	var out strings.Builder
	for i := 0; i < len(sql); {
		if end, skip := skipNonCode(sql, i); skip {
			out.WriteString(sql[i:end])
			i = end
			continue
		}
		if sql[i] != '$' || (i > 0 && isIdentifierPart(sql[i-1])) {
			out.WriteByte(sql[i])
			i++
			continue
		}
		name, end := readIdentifierAt(sql, i+1)
		if name == "" {
			out.WriteByte(sql[i])
			i++
			continue
		}
		if ec.Variables == nil {
			return "", fmt.Errorf("session variable %s is not defined", name)
		}
		value, ok := ec.Variables.Load(strings.ToUpper(name))
		if !ok {
			return "", fmt.Errorf("session variable %s is not defined", name)
		}
		out.WriteString("(" + value.(string) + ")")
		i = end
	}
	return out.String(), nil
}

func (e *Executor) executeSessionVariable(ctx context.Context, ec ExecutionContext, sql string) (*ExecResult, bool, error) {
	statement := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(trimLeadingComments(sql)), ";"))
	keyword, end := readIdentifierAt(statement, 0)
	if !strings.EqualFold(keyword, "SET") && !strings.EqualFold(keyword, "UNSET") {
		return nil, false, nil
	}
	if ec.Variables == nil || ec.SessionID == "" {
		return nil, true, fmt.Errorf("SET/UNSET requires an active session")
	}
	pos := skipSpaceAndComments(statement, end)
	name, end := readIdentifierAt(statement, pos)
	if name == "" {
		return nil, true, fmt.Errorf("expected one session variable name")
	}
	name = strings.ToUpper(name)
	pos = skipSpaceAndComments(statement, end)
	if strings.EqualFold(keyword, "UNSET") {
		if pos != len(statement) {
			return nil, true, fmt.Errorf("UNSET supports one variable name")
		}
		ec.Variables.Delete(name)
		return &ExecResult{}, true, nil
	}
	if pos >= len(statement) || statement[pos] != '=' {
		return nil, true, fmt.Errorf("SET supports SET name = expression")
	}
	expression := strings.TrimSpace(statement[pos+1:])
	if expression == "" {
		return nil, true, fmt.Errorf("SET requires an expression")
	}
	result, err := e.QueryWithContext(ctx, ec, "SELECT typeof(__variable_value), CAST(__variable_value AS VARCHAR) FROM (SELECT "+expression+" AS __variable_value)")
	if err != nil {
		return nil, true, err
	}
	if len(result.Rows) != 1 {
		return nil, true, fmt.Errorf("SET expression must produce one value")
	}
	value := ValueNull
	if result.Rows[0][1] != nil {
		kind := fmt.Sprint(result.Rows[0][0])
		text := fmt.Sprint(result.Rows[0][1])
		if !variableTypePattern.MatchString(kind) {
			return nil, true, fmt.Errorf("unsupported session variable type %s", kind)
		}
		if len(text) > 16*1024 {
			return nil, true, fmt.Errorf("session variable value exceeds 16 KB")
		}
		value = "CAST(" + sqlStringLiteral(text) + " AS " + kind + ")"
	}
	ec.Variables.Store(name, value)
	return &ExecResult{}, true, nil
}
