package query

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/nnnkkk7/snowflake-emulator/pkg/identity"
)

var (
	readTablePattern   = regexp.MustCompile(`(?i)\b(?:FROM|JOIN)\s+([^\s,;()]+)`)
	insertTablePattern = regexp.MustCompile(`(?i)^\s*(?:INSERT|COPY)\s+INTO\s+([^\s(;]+)`)
	updateTablePattern = regexp.MustCompile(`(?i)^\s*UPDATE\s+([^\s;]+)`)
	deleteTablePattern = regexp.MustCompile(`(?i)^\s*DELETE\s+FROM\s+([^\s;]+)`)
	mergeTablePattern  = regexp.MustCompile(`(?i)^\s*MERGE\s+INTO\s+([^\s;]+)`)
	createTablePattern = regexp.MustCompile(`(?i)^\s*CREATE\s+(?:OR\s+REPLACE\s+)?(?:(?:TEMP|TEMPORARY|TRANSIENT)\s+)?TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?([^\s(;]+)`)
)

func (e *Executor) authorizeObjectStatement(ctx context.Context, executionContext ExecutionContext, sql string) error {
	if executionContext.Principal == nil || e.identityService == nil {
		return nil
	}
	authorizeTable := func(raw, privilege string) error {
		database, schema, table, err := resolveQualifiedObjectName(raw, "table", executionContext)
		if err != nil {
			return err
		}
		roleID := executionContext.Principal.RoleID
		if err := e.identityService.AuthorizeObject(ctx, roleID, "DATABASE", database, identity.PrivilegeUsage); err != nil {
			return err
		}
		if err := e.identityService.AuthorizeObject(ctx, roleID, "SCHEMA", database+"."+schema, identity.PrivilegeUsage); err != nil {
			return err
		}
		return e.identityService.AuthorizeObject(ctx, roleID, "TABLE", database+"."+schema+"."+table, privilege)
	}
	value := trimLeadingComments(sql)
	upper := strings.ToUpper(value)
	if strings.HasPrefix(strings.TrimSpace(upper), "MERGE ") && (strings.Contains(upper, "WHEN NOT MATCHED") || strings.Contains(upper, "THEN DELETE")) {
		return fmt.Errorf("authorization for MERGE INSERT/DELETE branches is not supported")
	}
	needsObjectResolution := readTablePattern.MatchString(value) || insertTablePattern.MatchString(value) || updateTablePattern.MatchString(value) || deleteTablePattern.MatchString(value) || mergeTablePattern.MatchString(value) || createTablePattern.MatchString(value)
	if needsObjectResolution && regexp.MustCompile(`"[^"]*\s+[^"]*"`).MatchString(value) {
		return fmt.Errorf("authorization for quoted identifiers containing whitespace is not supported")
	}
	if match := createTablePattern.FindStringSubmatch(value); match != nil {
		database, schema, _, err := resolveQualifiedObjectName(match[1], "table", executionContext)
		if err != nil {
			return err
		}
		roleID := executionContext.Principal.RoleID
		if err := e.identityService.AuthorizeObject(ctx, roleID, "DATABASE", database, identity.PrivilegeUsage); err != nil {
			return err
		}
		if err := e.identityService.AuthorizeObject(ctx, roleID, "SCHEMA", database+"."+schema, identity.PrivilegeUsage); err != nil {
			return err
		}
		if err := e.identityService.AuthorizeObject(ctx, roleID, "SCHEMA", database+"."+schema, identity.PrivilegeCreateTable); err != nil {
			return err
		}
		// CTAS also needs SELECT on every source discovered below. A plain CREATE
		// has no FROM/JOIN matches and therefore finishes after the CREATE check.
	}
	for _, target := range []struct {
		pattern   *regexp.Regexp
		privilege string
	}{
		{insertTablePattern, identity.PrivilegeInsert}, {updateTablePattern, identity.PrivilegeUpdate},
		{deleteTablePattern, identity.PrivilegeDelete}, {mergeTablePattern, identity.PrivilegeUpdate},
	} {
		if match := target.pattern.FindStringSubmatch(value); match != nil {
			if err := authorizeTable(match[1], target.privilege); err != nil {
				return err
			}
		}
	}
	for _, match := range readTablePattern.FindAllStringSubmatch(value, -1) {
		name := strings.Trim(match[1], `"`)
		if strings.HasPrefix(name, "@") {
			continue
		}
		if target := deleteTablePattern.FindStringSubmatch(value); target != nil && strings.EqualFold(name, target[1]) {
			continue
		}
		if err := authorizeTable(name, identity.PrivilegeSelect); err != nil {
			return err
		}
	}
	return nil
}
