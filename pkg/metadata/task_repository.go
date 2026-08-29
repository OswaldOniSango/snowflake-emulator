package metadata

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

func (r *Repository) CreateTask(ctx context.Context, schemaID, name, warehouse, schedule, definition string, replace bool) (*Task, error) {
	name = strings.ToUpper(strings.TrimSpace(name))
	warehouse = strings.ToUpper(strings.TrimSpace(warehouse))
	if name == "" || warehouse == "" || strings.TrimSpace(schedule) == "" || strings.TrimSpace(definition) == "" {
		return nil, fmt.Errorf("task name, warehouse, schedule, and definition are required")
	}
	id := uuid.New().String()
	err := r.mgr.ExecTx(ctx, func(tx *sql.Tx) error {
		if replace {
			if _, err := tx.ExecContext(ctx, `DELETE FROM _metadata_tasks WHERE schema_id = ? AND name = ?`, schemaID, name); err != nil {
				return fmt.Errorf("failed to replace task: %w", err)
			}
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO _metadata_tasks
			(id, schema_id, name, warehouse, schedule, definition, state, created_at, owner)
			VALUES (?, ?, ?, ?, ?, ?, 'SUSPENDED', CURRENT_TIMESTAMP, '')`,
			id, schemaID, name, warehouse, strings.TrimSpace(schedule), strings.TrimSpace(definition))
		if err != nil {
			return fmt.Errorf("failed to create task %s: %w", name, err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return r.GetTask(ctx, id)
}

func (r *Repository) GetTask(ctx context.Context, id string) (*Task, error) {
	return scanTask(r.mgr.QueryRow(ctx, taskSelect+` WHERE id = ?`, id), "task with ID %s not found", id)
}

func (r *Repository) GetTaskByName(ctx context.Context, schemaID, name string) (*Task, error) {
	return scanTask(r.mgr.QueryRow(ctx, taskSelect+` WHERE schema_id = ? AND name = ?`, schemaID, strings.ToUpper(name)), "task %s not found", name)
}

const taskSelect = `SELECT id, schema_id, name, warehouse, schedule, definition, state, created_at,
	last_executed_at, last_completed_at, last_error, owner FROM _metadata_tasks`

func scanTask(row *sql.Row, notFoundFormat, value string) (*Task, error) {
	var task Task
	var createdAt, lastExecutedAt, lastCompletedAt sql.NullTime
	var lastError, owner sql.NullString
	if err := row.Scan(&task.ID, &task.SchemaID, &task.Name, &task.Warehouse, &task.Schedule, &task.Definition,
		&task.State, &createdAt, &lastExecutedAt, &lastCompletedAt, &lastError, &owner); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf(notFoundFormat, value)
		}
		return nil, fmt.Errorf("failed to get task: %w", err)
	}
	if createdAt.Valid {
		task.CreatedAt = createdAt.Time
	}
	if lastExecutedAt.Valid {
		task.LastExecutedAt = &lastExecutedAt.Time
	}
	if lastCompletedAt.Valid {
		task.LastCompletedAt = &lastCompletedAt.Time
	}
	task.LastError, task.Owner = lastError.String, owner.String
	return &task, nil
}

func (r *Repository) ListTasks(ctx context.Context, schemaID string) ([]*Task, error) {
	query := taskSelect
	var args []any
	if schemaID != "" {
		query += ` WHERE schema_id = ?`
		args = append(args, schemaID)
	}
	query += ` ORDER BY name`
	rows, err := r.mgr.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list tasks: %w", err)
	}
	defer func() { _ = rows.Close() }()
	tasks := make([]*Task, 0)
	for rows.Next() {
		var task Task
		var createdAt, lastExecutedAt, lastCompletedAt sql.NullTime
		var lastError, owner sql.NullString
		if err := rows.Scan(&task.ID, &task.SchemaID, &task.Name, &task.Warehouse, &task.Schedule, &task.Definition,
			&task.State, &createdAt, &lastExecutedAt, &lastCompletedAt, &lastError, &owner); err != nil {
			return nil, fmt.Errorf("failed to scan task: %w", err)
		}
		if createdAt.Valid {
			task.CreatedAt = createdAt.Time
		}
		if lastExecutedAt.Valid {
			task.LastExecutedAt = &lastExecutedAt.Time
		}
		if lastCompletedAt.Valid {
			task.LastCompletedAt = &lastCompletedAt.Time
		}
		task.LastError, task.Owner = lastError.String, owner.String
		tasks = append(tasks, &task)
	}
	return tasks, rows.Err()
}

func (r *Repository) SetTaskState(ctx context.Context, id, state string) error {
	state = strings.ToUpper(state)
	if state != "STARTED" && state != "SUSPENDED" {
		return fmt.Errorf("invalid task state %s", state)
	}
	result, err := r.mgr.Exec(ctx, `UPDATE _metadata_tasks SET state = ? WHERE id = ?`, state, id)
	if err != nil {
		return fmt.Errorf("failed to update task state: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("task with ID %s not found", id)
	}
	return nil
}

func (r *Repository) RecordTaskExecution(ctx context.Context, id, executionError string) error {
	_, err := r.mgr.Exec(ctx, `UPDATE _metadata_tasks SET last_executed_at = CURRENT_TIMESTAMP,
		last_completed_at = CURRENT_TIMESTAMP, last_error = ? WHERE id = ?`, executionError, id)
	if err != nil {
		return fmt.Errorf("failed to record task execution: %w", err)
	}
	return nil
}

func (r *Repository) DropTask(ctx context.Context, schemaID, name string, ifExists bool) error {
	result, err := r.mgr.Exec(ctx, `DELETE FROM _metadata_tasks WHERE schema_id = ? AND name = ?`, schemaID, strings.ToUpper(name))
	if err != nil {
		return fmt.Errorf("failed to drop task: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 && !ifExists {
		return fmt.Errorf("task %s not found", name)
	}
	return nil
}
