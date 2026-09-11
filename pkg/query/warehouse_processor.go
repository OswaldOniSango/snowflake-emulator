package query

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/nnnkkk7/snowflake-emulator/pkg/warehouse"
)

var (
	createWarehouseSQL = regexp.MustCompile(`(?is)^CREATE\s+WAREHOUSE\s+([^\s;]+)(.*?);?\s*$`)
	alterWarehouseSQL  = regexp.MustCompile(`(?is)^ALTER\s+WAREHOUSE\s+([^\s;]+)\s+(RESUME|SUSPEND|SET\s+.+?)\s*;?\s*$`)
	dropWarehouseSQL   = regexp.MustCompile(`(?is)^DROP\s+WAREHOUSE\s+(IF\s+EXISTS\s+)?([^\s;]+)\s*;?\s*$`)
	warehouseSizeOpt   = regexp.MustCompile(`(?i)WAREHOUSE_SIZE\s*=\s*'?([\w-]+)'?`)
	autoResumeOpt      = regexp.MustCompile(`(?i)AUTO_RESUME\s*=\s*(TRUE|FALSE)`)
	autoSuspendOpt     = regexp.MustCompile(`(?i)AUTO_SUSPEND\s*=\s*(\d+)`)
)

func isShowWarehouses(sql string) bool {
	return strings.EqualFold(strings.TrimSpace(strings.TrimSuffix(trimLeadingComments(sql), ";")), "SHOW WAREHOUSES")
}

func (e *Executor) executeWarehouseStatement(ctx context.Context, sql string) (*ExecResult, bool, error) {
	value := strings.TrimSpace(trimLeadingComments(sql))
	if match := createWarehouseSQL.FindStringSubmatch(value); match != nil {
		settings, err := parseWarehouseSettings(match[2], warehouse.Settings{Size: "X-SMALL", AutoResume: true, AutoSuspend: 600})
		if err != nil {
			return nil, true, err
		}
		_, err = e.warehouseManager.CreateWarehouseWithSettings(ctx, strings.Trim(match[1], `"`), "", settings)
		return &ExecResult{}, true, err
	}
	if match := alterWarehouseSQL.FindStringSubmatch(value); match != nil {
		name, action := strings.Trim(match[1], `"`), strings.TrimSpace(match[2])
		switch strings.ToUpper(action) {
		case "RESUME":
			return &ExecResult{}, true, e.warehouseManager.ResumeWarehouse(ctx, name)
		case "SUSPEND":
			return &ExecResult{}, true, e.warehouseManager.SuspendWarehouse(ctx, name)
		}
		current, err := e.warehouseManager.GetWarehouse(ctx, name)
		if err != nil {
			return nil, true, err
		}
		settings, err := parseWarehouseSettings(strings.TrimSpace(action[3:]), warehouse.Settings{Size: current.Size, AutoResume: current.AutoResume, AutoSuspend: current.AutoSuspend})
		if err != nil {
			return nil, true, err
		}
		return &ExecResult{}, true, e.warehouseManager.AlterWarehouse(ctx, name, settings)
	}
	if match := dropWarehouseSQL.FindStringSubmatch(value); match != nil {
		err := e.warehouseManager.DropWarehouse(ctx, strings.Trim(match[2], `"`))
		if err != nil && match[1] != "" && strings.Contains(err.Error(), "not found") {
			err = nil
		}
		return &ExecResult{}, true, err
	}
	return nil, false, nil
}

func parseWarehouseSettings(options string, defaults warehouse.Settings) (warehouse.Settings, error) {
	if match := warehouseSizeOpt.FindStringSubmatch(options); match != nil {
		defaults.Size = match[1]
	}
	if match := autoResumeOpt.FindStringSubmatch(options); match != nil {
		defaults.AutoResume = strings.EqualFold(match[1], "TRUE")
	}
	if match := autoSuspendOpt.FindStringSubmatch(options); match != nil {
		seconds, err := strconv.Atoi(match[1])
		if err != nil {
			return defaults, err
		}
		defaults.AutoSuspend = seconds
	}
	remaining := warehouseSizeOpt.ReplaceAllString(options, "")
	remaining = autoResumeOpt.ReplaceAllString(remaining, "")
	remaining = autoSuspendOpt.ReplaceAllString(remaining, "")
	if strings.Trim(strings.TrimSpace(remaining), ";") != "" {
		return defaults, fmt.Errorf("unsupported warehouse options: %s", strings.TrimSpace(remaining))
	}
	return defaults, nil
}

func (e *Executor) showWarehouses(ctx context.Context) (*Result, error) {
	values, err := e.warehouseManager.ListWarehouses(ctx)
	if err != nil {
		return nil, err
	}
	columns := []string{columnName, "state", "size", "auto_suspend", "auto_resume", "running", "queued", "last_resumed_on", "last_suspended_on", "last_activity_on"}
	rows := make([][]interface{}, 0, len(values))
	for _, v := range values {
		rows = append(rows, []interface{}{v.Name, string(v.State), v.Size, v.AutoSuspend, v.AutoResume, v.Running, v.Queued, v.LastResumedAt, v.LastSuspendedAt, v.LastActivityAt})
	}
	return &Result{Columns: columns, ColumnTypes: textColumnMetadata(columns), Rows: rows, TotalRows: len(rows)}, nil
}
