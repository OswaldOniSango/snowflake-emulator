package query

import (
	"fmt"
	"strconv"
	"strings"
)

// translateNumberDeclarations only visits column types in CREATE TABLE's
// definition list. Identifiers, expressions, literals and comments stay intact.
func translateNumberDeclarations(sql string) (string, error) {
	statement := trimLeadingComments(sql)
	match := createTablePattern.FindStringIndex(statement)
	if match == nil {
		return sql, nil
	}
	start := len(sql) - len(statement)
	open := skipSpaceAndComments(sql, start+match[1])
	if open >= len(sql) || sql[open] != '(' {
		return sql, nil
	}
	closeAt := matchingParen(sql, open)
	if closeAt < 0 {
		return sql, nil
	}
	columns := splitSQL(sql[open+1:closeAt], ',')
	for i, column := range columns {
		translated, err := translateNumberColumn(column)
		if err != nil {
			return "", err
		}
		columns[i] = translated
	}

	return sql[:open+1] + strings.Join(columns, ",") + sql[closeAt:], nil
}

func translateNumberColumn(column string) (string, error) {
	pos := skipSpaceAndComments(column, 0)
	var end int
	if pos < len(column) && column[pos] == '"' {
		end = skipQuoted(column, pos, '"')
	} else {
		_, end = readIdentifierAt(column, pos)
	}
	pos = skipSpaceAndComments(column, end)
	name, typeEnd := readIdentifierAt(column, pos)
	if !strings.EqualFold(name, "NUMBER") {
		return column, nil
	}
	precision, scale := 38, 0
	after := skipSpaceAndComments(column, typeEnd)
	if after < len(column) && column[after] == '(' {
		last := matchingParen(column, after)
		if last < 0 {
			return "", fmt.Errorf("invalid NUMBER declaration")
		}
		args := splitSQL(column[after+1:last], ',')
		if len(args) < 1 || len(args) > 2 {
			return "", fmt.Errorf("NUMBER expects precision and optional scale")
		}
		var err error
		precision, err = strconv.Atoi(strings.TrimSpace(numberArgument(args[0])))
		if err != nil {
			return "", fmt.Errorf("invalid NUMBER precision %q", args[0])
		}
		if len(args) == 2 {
			scale, err = strconv.Atoi(strings.TrimSpace(numberArgument(args[1])))
			if err != nil {
				return "", fmt.Errorf("invalid NUMBER scale %q", args[1])
			}
		}
		typeEnd = last + 1
	}
	if precision < 1 || precision > 38 || scale < 0 || scale > 37 || scale > precision {
		return "", fmt.Errorf("invalid NUMBER(%d,%d): precision must be 1..38; scale 0..min(precision,37)", precision, scale)
	}
	return column[:pos] + fmt.Sprintf("DECIMAL(%d,%d)", precision, scale) + numberTypeComments(column[pos:typeEnd]) + column[typeEnd:], nil
}

func numberTypeComments(fragment string) string {
	var out strings.Builder
	for i := 0; i < len(fragment); {
		if end, ok := skipNonCode(fragment, i); ok {
			if strings.HasPrefix(fragment[i:], "--") || strings.HasPrefix(fragment[i:], "/*") {
				out.WriteByte(' ')
				out.WriteString(fragment[i:end])
				out.WriteByte('\n')
			}
			i = end
		} else {
			i++
		}
	}
	return out.String()
}

// Remove only comments from numeric arguments; other SQL tokens still fail Atoi.
func numberArgument(value string) string {
	var out strings.Builder
	for i := 0; i < len(value); {
		if strings.HasPrefix(value[i:], "/*") || strings.HasPrefix(value[i:], "--") {
			end, _ := skipNonCode(value, i)
			out.WriteByte(' ')
			i = end
		} else {
			out.WriteByte(value[i])
			i++
		}
	}
	return out.String()
}
