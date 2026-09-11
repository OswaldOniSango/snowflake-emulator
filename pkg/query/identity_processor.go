package query

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/nnnkkk7/snowflake-emulator/pkg/identity"
	"github.com/nnnkkk7/snowflake-emulator/pkg/metadata"
)

const (
	identityIdentifierSQL = `(?:"(?:""|[^"])*"|[A-Za-z_][A-Za-z0-9_$]*)`
	columnComment         = "comment"
)

var (
	createUserSQL = regexp.MustCompile(`(?is)^CREATE\s+USER\s+(IF\s+NOT\s+EXISTS\s+)?(` + identityIdentifierSQL + `)\s+(.+?)\s*;?\s*$`)
	alterUserSQL  = regexp.MustCompile(`(?is)^ALTER\s+USER\s+(` + identityIdentifierSQL + `)\s+SET\s+(.+?)\s*;?\s*$`)
	dropUserSQL   = regexp.MustCompile(`(?is)^DROP\s+USER\s+(IF\s+EXISTS\s+)?(` + identityIdentifierSQL + `)\s*;?\s*$`)
	createRoleSQL = regexp.MustCompile(`(?is)^CREATE\s+ROLE\s+(IF\s+NOT\s+EXISTS\s+)?(` + identityIdentifierSQL + `)(?:\s+(.+?))?\s*;?\s*$`)
	dropRoleSQL   = regexp.MustCompile(`(?is)^DROP\s+ROLE\s+(IF\s+EXISTS\s+)?(` + identityIdentifierSQL + `)\s*;?\s*$`)
	grantRoleSQL  = regexp.MustCompile(`(?is)^GRANT\s+ROLE\s+(` + identityIdentifierSQL + `)\s+TO\s+(USER|ROLE)\s+(` + identityIdentifierSQL + `)\s*;?\s*$`)
	revokeRoleSQL = regexp.MustCompile(`(?is)^REVOKE\s+ROLE\s+(` + identityIdentifierSQL + `)\s+FROM\s+(USER|ROLE)\s+(` + identityIdentifierSQL + `)\s*;?\s*$`)
	showGrantsSQL = regexp.MustCompile(`(?is)^SHOW\s+GRANTS\s+(TO\s+(USER|ROLE)|OF\s+ROLE)\s+(` + identityIdentifierSQL + `)\s*;?\s*$`)
	passwordSQL   = regexp.MustCompile(`(?is)(\bPASSWORD\s*=\s*)'(?:''|[^'])*'`)
)

// RedactSensitiveSQL prevents identity secrets from entering query history.
func RedactSensitiveSQL(sql string) string {
	return passwordSQL.ReplaceAllString(sql, `${1}'********'`)
}

type identityOptions struct {
	password, defaultRole, comment string
	disabled                       bool
	hasPassword, hasDefaultRole    bool
	hasComment, hasDisabled        bool
}

func (e *Executor) executeIdentityStatement(ctx context.Context, sql string) (*ExecResult, bool, error) {
	statement := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(trimLeadingComments(sql)), ";"))
	switch {
	case createUserSQL.MatchString(statement):
		return &ExecResult{}, true, e.createIdentityUser(ctx, createUserSQL.FindStringSubmatch(statement))
	case alterUserSQL.MatchString(statement):
		return &ExecResult{}, true, e.alterIdentityUser(ctx, alterUserSQL.FindStringSubmatch(statement))
	case dropUserSQL.MatchString(statement):
		return &ExecResult{}, true, e.dropIdentityUser(ctx, dropUserSQL.FindStringSubmatch(statement))
	case createRoleSQL.MatchString(statement):
		return &ExecResult{}, true, e.createIdentityRole(ctx, createRoleSQL.FindStringSubmatch(statement))
	case dropRoleSQL.MatchString(statement):
		return &ExecResult{}, true, e.dropIdentityRole(ctx, dropRoleSQL.FindStringSubmatch(statement))
	case grantRoleSQL.MatchString(statement):
		return &ExecResult{}, true, e.grantIdentityRole(ctx, grantRoleSQL.FindStringSubmatch(statement))
	case revokeRoleSQL.MatchString(statement):
		return &ExecResult{}, true, e.revokeIdentityRole(ctx, revokeRoleSQL.FindStringSubmatch(statement))
	}
	return nil, false, nil
}

func (e *Executor) createIdentityUser(ctx context.Context, match []string) error {
	options, err := parseIdentityOptions(match[3])
	if err != nil {
		return err
	}
	if !options.hasPassword {
		return fmt.Errorf("CREATE USER requires PASSWORD")
	}
	defaultRole := options.defaultRole
	if !options.hasDefaultRole {
		defaultRole = identity.RolePublic
	}
	_, err = e.identityService.CreateUserConfigured(ctx, parseIdentityIdentifier(match[2]), options.password, defaultRole, options.disabled, options.comment)
	if match[1] != "" && isDuplicateCatalogError(err) {
		return nil
	}
	return err
}

func (e *Executor) alterIdentityUser(ctx context.Context, match []string) error {
	options, err := parseIdentityOptions(match[2])
	if err != nil {
		return err
	}
	changes := identity.UserChanges{}
	if options.hasPassword {
		changes.Password = &options.password
	}
	if options.hasDefaultRole {
		changes.DefaultRole = &options.defaultRole
	}
	if options.hasDisabled {
		changes.Disabled = &options.disabled
	}
	if options.hasComment {
		changes.Comment = &options.comment
	}
	return e.identityService.AlterUser(ctx, parseIdentityIdentifier(match[1]), changes)
}

func (e *Executor) dropIdentityUser(ctx context.Context, match []string) error {
	err := e.identityService.DeleteUser(ctx, parseIdentityIdentifier(match[2]))
	if match[1] != "" && errors.Is(err, metadata.ErrIdentityNotFound) {
		return nil
	}
	return err
}

func (e *Executor) createIdentityRole(ctx context.Context, match []string) error {
	options, err := parseIdentityOptions(match[3])
	if err != nil {
		return err
	}
	_, err = e.identityService.CreateRole(ctx, parseIdentityIdentifier(match[2]), options.comment)
	if match[1] != "" && isDuplicateCatalogError(err) {
		return nil
	}
	return err
}

func (e *Executor) dropIdentityRole(ctx context.Context, match []string) error {
	err := e.identityService.DeleteRole(ctx, parseIdentityIdentifier(match[2]))
	if match[1] != "" && errors.Is(err, metadata.ErrIdentityNotFound) {
		return nil
	}
	return err
}

func (e *Executor) grantIdentityRole(ctx context.Context, match []string) error {
	child, target := parseIdentityIdentifier(match[1]), parseIdentityIdentifier(match[3])
	if strings.EqualFold(match[2], "USER") {
		return e.identityService.GrantRoleToUser(ctx, child, target)
	}
	return e.identityService.GrantRoleToRole(ctx, child, target)
}

func (e *Executor) revokeIdentityRole(ctx context.Context, match []string) error {
	child, target := parseIdentityIdentifier(match[1]), parseIdentityIdentifier(match[3])
	if strings.EqualFold(match[2], "USER") {
		return e.identityService.RevokeRoleFromUser(ctx, child, target)
	}
	return e.identityService.RevokeRoleFromRole(ctx, child, target)
}

func (e *Executor) queryIdentityStatement(ctx context.Context, sql string) (*Result, bool, error) {
	statement := strings.TrimSpace(strings.TrimSuffix(trimLeadingComments(sql), ";"))
	switch {
	case strings.EqualFold(statement, "SHOW USERS"):
		users, err := e.identityService.ListUsers(ctx)
		if err != nil {
			return nil, true, err
		}
		roles, err := e.identityService.ListRoles(ctx)
		if err != nil {
			return nil, true, err
		}
		roleNames := make(map[string]string, len(roles))
		for _, role := range roles {
			roleNames[role.ID] = role.Name
		}
		columns := []string{columnName, "disabled", "default_role", columnComment}
		rows := make([][]interface{}, 0, len(users))
		for _, user := range users {
			rows = append(rows, []interface{}{user.Name, user.Disabled, roleNames[user.DefaultRoleID], user.Comment})
		}
		return identityResult(columns, rows), true, nil
	case strings.EqualFold(statement, "SHOW ROLES"):
		roles, err := e.identityService.ListRoles(ctx)
		if err != nil {
			return nil, true, err
		}
		columns := []string{columnCreatedOn, columnName, columnComment, "is_system"}
		rows := make([][]interface{}, 0, len(roles))
		for _, role := range roles {
			rows = append(rows, []interface{}{role.CreatedAt, role.Name, role.Comment, role.SystemRole})
		}
		return identityResult(columns, rows), true, nil
	case showGrantsSQL.MatchString(statement):
		match := showGrantsSQL.FindStringSubmatch(statement)
		name := parseIdentityIdentifier(match[3])
		var assignments []identity.RoleAssignment
		var err error
		direction := strings.ToUpper(strings.Join(strings.Fields(match[1]), " "))
		switch direction {
		case "TO USER":
			assignments, err = e.identityService.DirectGrantsToUser(ctx, name)
		case "TO ROLE":
			assignments, err = e.identityService.DirectGrantsToRole(ctx, name)
		default:
			assignments, err = e.identityService.DirectGrantsOfRole(ctx, name)
		}
		if err != nil {
			return nil, true, err
		}
		columns := []string{"role", "granted_to", "grantee_name"}
		rows := make([][]interface{}, 0, len(assignments))
		for _, assignment := range assignments {
			rows = append(rows, []interface{}{assignment.RoleName, assignment.GrantedTo, assignment.Grantee})
		}
		return identityResult(columns, rows), true, nil
	}
	return nil, false, nil
}

func identityResult(columns []string, rows [][]interface{}) *Result {
	return &Result{Columns: columns, ColumnTypes: textColumnMetadata(columns), Rows: rows, TotalRows: len(rows)}
}

func parseIdentityIdentifier(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`) {
		return strings.ReplaceAll(value[1:len(value)-1], `""`, `"`)
	}
	return strings.ToUpper(value)
}

func parseIdentityOptions(input string) (identityOptions, error) {
	var result identityOptions
	input = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(input), ";"))
	for input != "" {
		input = strings.TrimLeft(input, " \t\r\n,")
		keyEnd := strings.IndexAny(input, "= \t\r\n")
		if keyEnd <= 0 {
			return result, fmt.Errorf("invalid identity option near %q", input)
		}
		key := strings.ToUpper(input[:keyEnd])
		input = strings.TrimSpace(input[keyEnd:])
		if !strings.HasPrefix(input, "=") {
			return result, fmt.Errorf("identity option %s requires =", key)
		}
		input = strings.TrimSpace(input[1:])
		value, rest, err := readIdentityOptionValue(input)
		if err != nil {
			return result, err
		}
		input = rest
		switch key {
		case "PASSWORD":
			if !strings.HasPrefix(value, "'") {
				return result, fmt.Errorf("PASSWORD must be a string literal")
			}
			result.password, result.hasPassword = unquoteSQLString(value), true
		case "DEFAULT_ROLE":
			result.defaultRole, result.hasDefaultRole = parseIdentityIdentifier(value), true
		case "DISABLED":
			if !strings.EqualFold(value, "TRUE") && !strings.EqualFold(value, "FALSE") {
				return result, fmt.Errorf("DISABLED must be TRUE or FALSE")
			}
			result.disabled, result.hasDisabled = strings.EqualFold(value, "TRUE"), true
		case "COMMENT":
			if !strings.HasPrefix(value, "'") {
				return result, fmt.Errorf("COMMENT must be a string literal")
			}
			result.comment, result.hasComment = unquoteSQLString(value), true
		default:
			return result, fmt.Errorf("unsupported identity option %s", key)
		}
	}
	return result, nil
}

func readIdentityOptionValue(input string) (string, string, error) {
	if input == "" {
		return "", "", fmt.Errorf("identity option value is required")
	}
	if input[0] == '\'' {
		end := skipQuoted(input, 0, '\'')
		if end == len(input) && input[end-1] != '\'' {
			return "", "", fmt.Errorf("unterminated string literal")
		}
		return input[:end], input[end:], nil
	}
	if input[0] == '"' {
		end := skipQuoted(input, 0, '"')
		if end == len(input) && input[end-1] != '"' {
			return "", "", fmt.Errorf("unterminated quoted identifier")
		}
		return input[:end], input[end:], nil
	}
	end := strings.IndexAny(input, " ,\t\r\n")
	if end < 0 {
		return input, "", nil
	}
	return input[:end], input[end:], nil
}

func unquoteSQLString(value string) string {
	return strings.ReplaceAll(value[1:len(value)-1], "''", "'")
}

func isDuplicateCatalogError(err error) bool {
	return err != nil && (strings.Contains(strings.ToLower(err.Error()), "duplicate") || strings.Contains(strings.ToLower(err.Error()), "constraint"))
}
