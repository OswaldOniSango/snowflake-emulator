package query

import (
	"regexp"
	"strings"
)

const sqlKeywordFrom = "FROM"

// maskSQLLiterals preserves offsets and quoted identifiers, but prevents literal
// data and comments from contributing fictitious authorization targets.
func maskSQLLiterals(sql string) string {
	b := []byte(sql)
	for i := 0; i < len(sql); {
		if sql[i] == '"' {
			i = skipQuoted(sql, i, '"')
			continue
		}
		if end, ok := skipNonCode(sql, i); ok {
			for j := i; j < end; j++ {
				if b[j] != '\n' {
					b[j] = ' '
				}
			}
			i = end
		} else {
			i++
		}
	}
	return string(b)
}

func replaceSQLCodeMatches(sql string, pattern *regexp.Regexp, replace func(string) string) string {
	var out strings.Builder
	end := 0
	for _, match := range pattern.FindAllStringIndex(maskSQLLiterals(sql), -1) {
		out.WriteString(sql[end:match[0]])
		out.WriteString(replace(sql[match[0]:match[1]]))
		end = match[1]
	}
	out.WriteString(sql[end:])
	return out.String()
}
