// Copyright 2025 The A2A Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/google/uuid"
)

type dbTaskStore struct {
	db *sql.DB
}

func newDBTaskStore(db *sql.DB) *dbTaskStore {
	return &dbTaskStore{db: db}
}

var _ a2asrv.TaskStore = (*dbTaskStore)(nil)

func (s *dbTaskStore) Save(ctx context.Context, task *a2a.Task, event a2a.Event, prev a2a.TaskVersion) (a2a.TaskVersion, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	newVersion := time.Now().UnixNano()
	taskJSON, err := json.Marshal(task)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal task: %w", err)
	}

	if prev == nil || prev == a2a.TaskVersionMissing {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO task (id, state, last_updated, task_json)
			VALUES (?, ?, ?, ?)
		`, task.ID, task.Status.State, newVersion, string(taskJSON))

		if err != nil {
			return nil, fmt.Errorf("failed to insert task: %w", err)
		}
	} else {
		prevVersion, ok := prev.(a2a.TaskVersionInt)
		if !ok {
			return nil, fmt.Errorf("invalid previous version type: %T", prev)
		}

		res, err := tx.ExecContext(ctx, `
			UPDATE task SET
				state = ?,
				last_updated = ?,
				task_json = ?
			WHERE id = ? AND last_updated = ?
		`, task.Status.State, newVersion, string(taskJSON), task.ID, int64(prevVersion))

		if err != nil {
			return nil, fmt.Errorf("failed to update task: %w", err)
		}

		rows, err := res.RowsAffected()
		if err != nil {
			return nil, fmt.Errorf("failed to get rows affected: %w", err)
		}
		if rows == 0 {
			return nil, fmt.Errorf("optimistic concurrency failure: task updated by another transaction")
		}
	}

	eventJSON, err := json.Marshal(event)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal event: %w", err)
	}

	eventID, eventType := uuid.Must(uuid.NewV7()).String(), getEventType(event)
	_, err = tx.ExecContext(ctx, `
		INSERT INTO task_event (id, task_id, type, task_version, event_json)
		VALUES (?, ?, ?, ?, ?)
	`, eventID, task.ID, eventType, newVersion, string(eventJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to insert event: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return a2a.TaskVersionInt(newVersion), nil
}

func (s *dbTaskStore) Get(ctx context.Context, taskID a2a.TaskID) (*a2a.Task, a2a.TaskVersion, error) {
	var taskJSON string
	var version int64
	err := s.db.QueryRowContext(ctx, "SELECT task_json, last_updated FROM task WHERE id = ?", taskID).Scan(&taskJSON, &version)
	if err == sql.ErrNoRows {
		return nil, nil, a2a.ErrTaskNotFound
	}
	if err != nil {
		return nil, nil, err
	}

	var task a2a.Task
	if err := json.Unmarshal([]byte(taskJSON), &task); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal task: %w", err)
	}

	return &task, a2a.TaskVersionInt(version), nil
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
