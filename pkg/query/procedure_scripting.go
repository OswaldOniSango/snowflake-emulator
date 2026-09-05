package query

import (
	"fmt"
	"strings"
	"unicode"
)

// keywordEnd terminates every block the scripting parser understands.
const keywordEnd = "END"

type procedureScript struct {
	Declarations     []procedureDeclaration
	Statements       []procedureStatement
	ExceptionHandler []procedureStatement
}

type procedureDeclaration struct {
	Name       string
	DataType   string
	DefaultSQL string
}

type procedureStatement interface {
	isProcedureStatement()
}

type (
	procedureSQLStatement        struct{ SQL string }
	procedureAssignmentStatement struct{ Name, Expression string }
	procedureReturnStatement     struct{ Expression string }
	// procedureLetStatement declares and initializes a variable inline in the
	// executable body, unlike a DECLARE, which is a separate section ahead of
	// BEGIN. DataType is recorded but — the same as procedureDeclaration's —
	// not enforced; the value is whatever Expression evaluates to.
	procedureLetStatement struct {
		Name, DataType, Expression string
	}
	procedureIfStatement struct {
		Condition  string
		ThenBranch []procedureStatement
		ElseBranch []procedureStatement
	}
)

type procedureCaseBranch struct {
	Value      string
	Statements []procedureStatement
}
type procedureCaseStatement struct {
	Expression string
	Branches   []procedureCaseBranch
	ElseBranch []procedureStatement
}

func (procedureSQLStatement) isProcedureStatement()        {}
func (procedureAssignmentStatement) isProcedureStatement() {}
func (procedureReturnStatement) isProcedureStatement()     {}
func (procedureLetStatement) isProcedureStatement()        {}
func (procedureIfStatement) isProcedureStatement()         {}
func (procedureCaseStatement) isProcedureStatement()       {}

type procedureScriptParser struct {
	input string
	pos   int
}

func parseProcedureScript(body string) (*procedureScript, error) {
	p := &procedureScriptParser{input: strings.TrimSpace(body)}
	p.skipSpaceAndComments()
	script := &procedureScript{}
	if p.consumeKeyword("DECLARE") {
		declarations, err := p.parseDeclarations()
		if err != nil {
			return nil, err
		}
		script.Declarations = declarations
	}
	if !p.consumeKeyword("BEGIN") {
		return nil, fmt.Errorf("procedure body must contain BEGIN")
	}
	statements, terminator, err := p.parseStatements("EXCEPTION", keywordEnd)
	if err != nil {
		return nil, err
	}
	script.Statements = statements
	if terminator == "EXCEPTION" {
		p.consumeKeyword("EXCEPTION")
		if !p.consumeKeyword("WHEN") || !p.consumeKeyword("OTHER") || !p.consumeKeyword("THEN") {
			return nil, fmt.Errorf("EXCEPTION handler must use WHEN OTHER THEN")
		}
		script.ExceptionHandler = make([]procedureStatement, 0)
		script.ExceptionHandler, terminator, err = p.parseStatements(keywordEnd)
		if err != nil {
			return nil, err
		}
	}
	if terminator != keywordEnd {
		return nil, fmt.Errorf("procedure body is missing END")
	}
	p.consumeKeyword(keywordEnd)
	p.consumeSemicolon()
	p.skipSpaceAndComments()
	if !p.eof() {
		return nil, fmt.Errorf("unexpected content after procedure END near %q", p.remainingPreview())
	}
	return script, nil
}

func (p *procedureScriptParser) parseDeclarations() ([]procedureDeclaration, error) {
	var declarations []procedureDeclaration
	for {
		p.skipSpaceAndComments()
		if p.peekKeyword("BEGIN") {
			return declarations, nil
		}
		text, err := p.readUntilSemicolon()
		if err != nil {
			return nil, fmt.Errorf("invalid declaration: %w", err)
		}
		declaration, err := parseProcedureDeclaration(text)
		if err != nil {
			return nil, err
		}
		declarations = append(declarations, declaration)
	}
}

func parseProcedureDeclaration(text string) (procedureDeclaration, error) {
	text = strings.TrimSpace(text)
	name, rest := consumeIdentifier(text)
	if name == "" {
		return procedureDeclaration{}, fmt.Errorf("invalid declaration %q: variable name is required", text)
	}
	defaultIndex := keywordIndexAtTopLevel(rest, "DEFAULT")
	dataType := strings.TrimSpace(rest)
	defaultSQL := ""
	if defaultIndex >= 0 {
		dataType = strings.TrimSpace(rest[:defaultIndex])
		defaultSQL = strings.TrimSpace(rest[defaultIndex+len("DEFAULT"):])
	}
	if dataType == "" {
		return procedureDeclaration{}, fmt.Errorf("invalid declaration for %s: data type is required", name)
	}
	if defaultIndex >= 0 && defaultSQL == "" {
		return procedureDeclaration{}, fmt.Errorf("invalid declaration for %s: DEFAULT expression is required", name)
	}
	return procedureDeclaration{Name: strings.ToUpper(name), DataType: strings.ToUpper(dataType), DefaultSQL: defaultSQL}, nil
}

func (p *procedureScriptParser) parseStatements(terminators ...string) ([]procedureStatement, string, error) {
	var statements []procedureStatement
	for {
		p.skipSpaceAndComments()
		for _, terminator := range terminators {
			if p.peekKeyword(terminator) {
				return statements, terminator, nil
			}
		}
		if p.eof() {
			return nil, "", fmt.Errorf("unexpected end of procedure body")
		}
		statement, err := p.parseStatement()
		if err != nil {
			return nil, "", err
		}
		statements = append(statements, statement)
	}
}

func (p *procedureScriptParser) parseStatement() (procedureStatement, error) {
	p.skipSpaceAndComments()
	switch {
	case p.consumeKeyword("CASE"):
		return p.parseCase()
	case p.consumeKeyword("IF"):
		return p.parseIf()
	case p.consumeKeyword("LET"):
		return p.parseLet()
	case p.consumeKeyword("RETURN"):
		expression, err := p.readUntilSemicolon()
		if err != nil {
			return nil, fmt.Errorf("invalid RETURN: %w", err)
		}
		return procedureReturnStatement{Expression: strings.TrimSpace(expression)}, nil
	}

	start := p.pos
	name := p.readIdentifier()
	if name != "" {
		p.skipSpaceAndComments()
		if strings.HasPrefix(p.input[p.pos:], ":=") {
			p.pos += 2
			expression, err := p.readUntilSemicolon()
			if err != nil {
				return nil, fmt.Errorf("invalid assignment to %s: %w", name, err)
			}
			return procedureAssignmentStatement{Name: strings.ToUpper(name), Expression: strings.TrimSpace(expression)}, nil
		}
	}
	p.pos = start
	sql, err := p.readUntilSemicolon()
	if err != nil {
		return nil, fmt.Errorf("invalid SQL statement: %w", err)
	}
	return procedureSQLStatement{SQL: strings.TrimSpace(sql)}, nil
}

func (p *procedureScriptParser) parseLet() (procedureStatement, error) {
	text, err := p.readUntilSemicolon()
	if err != nil {
		return nil, fmt.Errorf("invalid LET: %w", err)
	}
	return parseProcedureLet(text)
}

// parseProcedureLet parses a LET statement's body, one of:
//
//	LET name := expression
//	LET name type := expression
//	LET name type DEFAULT expression
//
// Snowflake requires a value for LET — unlike DECLARE, there is no bare
// "LET name type" form — so, unlike parseProcedureDeclaration, a missing
// separator is itself the error rather than an accepted no-default case.
func parseProcedureLet(text string) (procedureStatement, error) {
	text = strings.TrimSpace(text)
	name, rest := consumeIdentifier(text)
	if name == "" {
		return nil, fmt.Errorf("invalid LET %q: variable name is required", text)
	}

	assignIndex := topLevelIndexOf(rest, ":=")
	defaultIndex := keywordIndexAtTopLevel(rest, "DEFAULT")
	separatorIndex, separatorLen := assignIndex, len(":=")
	if defaultIndex >= 0 && (assignIndex < 0 || defaultIndex < assignIndex) {
		separatorIndex, separatorLen = defaultIndex, len("DEFAULT")
	}
	if separatorIndex < 0 {
		return nil, fmt.Errorf("invalid LET %s: expected := or DEFAULT with a value", name)
	}

	dataType := strings.TrimSpace(rest[:separatorIndex])
	expression := strings.TrimSpace(rest[separatorIndex+separatorLen:])
	if expression == "" {
		return nil, fmt.Errorf("invalid LET %s: expression is required", name)
	}

	return procedureLetStatement{Name: strings.ToUpper(name), DataType: strings.ToUpper(dataType), Expression: expression}, nil
}

func (p *procedureScriptParser) parseIf() (procedureStatement, error) {
	condition, err := p.readUntilKeyword("THEN")
	if err != nil {
		return nil, fmt.Errorf("invalid IF: %w", err)
	}
	p.consumeKeyword("THEN")
	thenBranch, terminator, err := p.parseStatements("ELSE", keywordEnd)
	if err != nil {
		return nil, err
	}
	var elseBranch []procedureStatement
	if terminator == "ELSE" {
		p.consumeKeyword("ELSE")
		elseBranch, terminator, err = p.parseStatements(keywordEnd)
		if err != nil {
			return nil, err
		}
	}
	if terminator != keywordEnd || !p.consumeKeyword(keywordEnd) || !p.consumeKeyword("IF") {
		return nil, fmt.Errorf("IF statement is missing END IF")
	}
	p.consumeSemicolon()
	return procedureIfStatement{Condition: trimOuterParentheses(condition), ThenBranch: thenBranch, ElseBranch: elseBranch}, nil
}

func (p *procedureScriptParser) parseCase() (procedureStatement, error) {
	expression, err := p.readUntilKeyword("WHEN")
	if err != nil {
		return nil, fmt.Errorf("invalid CASE: %w", err)
	}
	caseStatement := procedureCaseStatement{Expression: trimOuterParentheses(expression)}
	for p.consumeKeyword("WHEN") {
		value, err := p.readUntilKeyword("THEN")
		if err != nil {
			return nil, fmt.Errorf("invalid CASE WHEN: %w", err)
		}
		p.consumeKeyword("THEN")
		statements, terminator, err := p.parseStatements("WHEN", "ELSE", keywordEnd)
		if err != nil {
			return nil, err
		}
		caseStatement.Branches = append(caseStatement.Branches, procedureCaseBranch{Value: strings.TrimSpace(value), Statements: statements})
		if terminator != "WHEN" {
			break
		}
	}
	if p.consumeKeyword("ELSE") {
		caseStatement.ElseBranch, _, err = p.parseStatements(keywordEnd)
		if err != nil {
			return nil, err
		}
	}
	if !p.consumeKeyword(keywordEnd) || !p.consumeKeyword("CASE") {
		return nil, fmt.Errorf("CASE statement is missing END CASE")
	}
	p.consumeSemicolon()
	return caseStatement, nil
}

func (p *procedureScriptParser) readUntilSemicolon() (string, error) {
	start := p.pos
	depth, inQuote := 0, false
	for p.pos < len(p.input) {
		current := p.input[p.pos]
		if current == '\'' {
			if inQuote && p.pos+1 < len(p.input) && p.input[p.pos+1] == '\'' {
				p.pos += 2
				continue
			}
			inQuote = !inQuote
			p.pos++
			continue
		}
		if !inQuote {
			switch current {
			case '(':
				depth++
			case ')':
				depth--
			case ';':
				if depth == 0 {
					value := p.input[start:p.pos]
					p.pos++
					return value, nil
				}
			}
		}
		p.pos++
	}
	return "", fmt.Errorf("missing semicolon")
}

func (p *procedureScriptParser) readUntilKeyword(keyword string) (string, error) {
	start := p.pos
	depth, inQuote := 0, false
	for p.pos < len(p.input) {
		current := p.input[p.pos]
		if current == '\'' {
			if inQuote && p.pos+1 < len(p.input) && p.input[p.pos+1] == '\'' {
				p.pos += 2
				continue
			}
			inQuote = !inQuote
			p.pos++
			continue
		}
		if !inQuote {
			switch {
			case current == '(':
				depth++
			case current == ')':
				depth--
			case depth == 0 && p.peekKeyword(keyword):
				return p.input[start:p.pos], nil
			}
		}
		p.pos++
	}
	return "", fmt.Errorf("missing %s", keyword)
}

func (p *procedureScriptParser) skipSpaceAndComments() {
	for {
		for p.pos < len(p.input) && unicode.IsSpace(rune(p.input[p.pos])) {
			p.pos++
		}
		if strings.HasPrefix(p.input[p.pos:], "--") {
			if newline := strings.IndexByte(p.input[p.pos:], '\n'); newline >= 0 {
				p.pos += newline + 1
				continue
			}
			p.pos = len(p.input)
		}
		return
	}
}

func (p *procedureScriptParser) peekKeyword(keyword string) bool {
	originalPosition := p.pos
	p.skipSpaceAndComments()
	end := p.pos + len(keyword)
	if end > len(p.input) || !strings.EqualFold(p.input[p.pos:end], keyword) {
		p.pos = originalPosition
		return false
	}
	matches := (p.pos == 0 || !isIdentifierCharacter(rune(p.input[p.pos-1]))) && (end == len(p.input) || !isIdentifierCharacter(rune(p.input[end])))
	if !matches {
		p.pos = originalPosition
	}
	return matches
}

func (p *procedureScriptParser) consumeKeyword(keyword string) bool {
	if !p.peekKeyword(keyword) {
		return false
	}
	p.pos += len(keyword)
	return true
}

func (p *procedureScriptParser) consumeSemicolon() {
	p.skipSpaceAndComments()
	if p.pos < len(p.input) && p.input[p.pos] == ';' {
		p.pos++
	}
}

func (p *procedureScriptParser) readIdentifier() string {
	p.skipSpaceAndComments()
	start := p.pos
	if start >= len(p.input) || (!unicode.IsLetter(rune(p.input[start])) && p.input[start] != '_') {
		return ""
	}
	p.pos++
	for p.pos < len(p.input) && isIdentifierCharacter(rune(p.input[p.pos])) {
		p.pos++
	}
	return p.input[start:p.pos]
}

func (p *procedureScriptParser) eof() bool { return p.pos >= len(p.input) }

func (p *procedureScriptParser) remainingPreview() string {
	end := min(p.pos+30, len(p.input))
	return p.input[p.pos:end]
}

func consumeIdentifier(input string) (string, string) {
	input = strings.TrimSpace(input)
	end := 0
	for end < len(input) && isIdentifierCharacter(rune(input[end])) {
		end++
	}
	return input[:end], input[end:]
}

func keywordIndexAtTopLevel(input, keyword string) int {
	upper := strings.ToUpper(input)
	depth, inQuote := 0, false
	for i := 0; i+len(keyword) <= len(input); i++ {
		switch input[i] {
		case '\'':
			inQuote = !inQuote
		case '(':
			if !inQuote {
				depth++
			}
		case ')':
			if !inQuote {
				depth--
			}
		}
		if !inQuote && depth == 0 && strings.HasPrefix(upper[i:], keyword) &&
			(i == 0 || !isIdentifierCharacter(rune(input[i-1]))) &&
			(i+len(keyword) == len(input) || !isIdentifierCharacter(rune(input[i+len(keyword)]))) {
			return i
		}
	}
	return -1
}

// topLevelIndexOf returns the index of the first occurrence of substr outside
// any parentheses or quoted string, or -1 if there is none — the punctuation
// counterpart to keywordIndexAtTopLevel, for an operator like ":=" that is
// not a word and so has no identifier boundary to check.
func topLevelIndexOf(input, substr string) int {
	depth, inQuote := 0, false
	for i := 0; i+len(substr) <= len(input); i++ {
		switch input[i] {
		case '\'':
			inQuote = !inQuote
		case '(':
			if !inQuote {
				depth++
			}
		case ')':
			if !inQuote {
				depth--
			}
		}
		if !inQuote && depth == 0 && input[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func trimOuterParentheses(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && value[0] == '(' && value[len(value)-1] == ')' {
		return strings.TrimSpace(value[1 : len(value)-1])
	}
	return value
}

func isIdentifierCharacter(value rune) bool {
	return unicode.IsLetter(value) || unicode.IsDigit(value) || value == '_' || value == '$'
}
