package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/google/uuid"
)

// MySQLDBAdapter implements utils.Database for MySQL.
type MySQLDBAdapter struct {
	db *sql.DB
}

// NewMySQLDBAdapter creates a new MySQLDBAdapter.
func NewMySQLDBAdapter(db *sql.DB) *MySQLDBAdapter {
	return &MySQLDBAdapter{db: db}
}

// Create persists a new task and event.
func (a *MySQLDBAdapter) Create(ctx context.Context, task *a2a.Task, event a2a.Event, newVersion int64, protocolVersion a2a.ProtocolVersion) error {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollbackTx(ctx, tx)

	taskJSON, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("failed to marshal task: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
			INSERT INTO task (id, state, last_updated, task_json, protocol_version)
			VALUES (?, ?, ?, ?, ?)
		`, task.ID, task.Status.State, newVersion, string(taskJSON), protocolVersion)

	if err != nil {
		return fmt.Errorf("failed to insert task: %w", err)
	}

	if err := a.insertEvent(ctx, tx, task.ID, event, newVersion); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}

// Update persists an updated task and event.
func (a *MySQLDBAdapter) Update(ctx context.Context, task *a2a.Task, event a2a.Event, newVersion int64, prevVersion int64, protocolVersion a2a.ProtocolVersion) (bool, error) {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer rollbackTx(ctx, tx)

	taskJSON, err := json.Marshal(task)
	if err != nil {
		return false, fmt.Errorf("failed to marshal task: %w", err)
	}

	res, err := tx.ExecContext(ctx, `
			UPDATE task SET
				state = ?,
				last_updated = ?,
				task_json = ?
			WHERE id = ? AND last_updated = ?
		`, task.Status.State, newVersion, string(taskJSON), task.ID, prevVersion)

	if err != nil {
		return false, fmt.Errorf("failed to update task: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		// Optimistic concurrency failure
		return false, nil
	}

	if err := a.insertEvent(ctx, tx, task.ID, event, newVersion); err != nil {
		return false, err
	}

	if err := tx.Commit(); err != nil {
		return false, err
	}

	return true, nil
}

func (a *MySQLDBAdapter) insertEvent(ctx context.Context, tx *sql.Tx, taskID a2a.TaskID, event a2a.Event, taskVersion int64) error {
	eventJSON, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	eventID := uuid.Must(uuid.NewV7()).String()
	eventType := getEventType(event)

	_, err = tx.ExecContext(ctx, `
		INSERT INTO task_event (id, task_id, type, task_version, event_json)
		VALUES (?, ?, ?, ?, ?)
	`, eventID, taskID, eventType, taskVersion, string(eventJSON))
	if err != nil {
		return fmt.Errorf("failed to insert event: %w", err)
	}
	return nil
}

// Get retrieves a task from the database.
func (a *MySQLDBAdapter) Get(ctx context.Context, taskID a2a.TaskID) (*a2a.Task, int64, error) {
	var taskJSON string
	var version int64
	err := a.db.QueryRowContext(ctx, "SELECT task_json, last_updated FROM task WHERE id = ?", taskID).Scan(&taskJSON, &version)
	if err == sql.ErrNoRows {
		return nil, 0, a2a.ErrTaskNotFound
	}
	if err != nil {
		return nil, 0, err
	}

	var task a2a.Task
	if err := json.Unmarshal([]byte(taskJSON), &task); err != nil {
		return nil, 0, fmt.Errorf("failed to unmarshal task: %w", err)
	}

	return &task, version, nil
}

func getEventType(e a2a.Event) string {
	switch e.(type) {
	case *a2a.Message:
		return "message"
	case *a2a.Task:
		return "task"
	case *a2a.TaskStatusUpdateEvent:
		return "status-update"
	case *a2a.TaskArtifactUpdateEvent:
		return "artifact-update"
	default:
		return "unknown"
	}
}
