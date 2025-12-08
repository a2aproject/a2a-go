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
	"github.com/a2aproject/a2a-go/a2asrv/workqueue"
	"github.com/google/uuid"
)

var taskReclaimTimeout = 5 * time.Second

type dbWorkQueue struct {
	db       *sql.DB
	workerID string
}

func newDBWorkQueue(db *sql.DB, workerID string) *dbWorkQueue {
	return &dbWorkQueue{db: db, workerID: workerID}
}

var _ workqueue.Queue = (*dbWorkQueue)(nil)

func (q *dbWorkQueue) Read(ctx context.Context) (workqueue.Message, error) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()

		case <-ticker.C:
			var executionID, taskID, payloadJSON string

			tx, err := q.db.BeginTx(ctx, nil)
			if err != nil {
				return nil, err
			}

			reclaimCutoff := time.Now().Add(-taskReclaimTimeout)
			// TODO(yarolegovich): fetch in batches
			err = tx.QueryRowContext(ctx, `
				SELECT id, task_id, payload_json
				FROM task_execution
				WHERE state != 'completed' AND last_updated < ?
				LIMIT 1 
				FOR UPDATE SKIP LOCKED
			`, reclaimCutoff).Scan(&executionID, &taskID, &payloadJSON)

			if err == sql.ErrNoRows {
				tx.Rollback()
				continue
			}
			if err != nil {
				tx.Rollback()
				return nil, err
			}

			_, err = tx.ExecContext(ctx, `
				UPDATE task_execution
				SET state = 'working', worker_id = ?
				WHERE id = ?
			`, q.workerID, executionID)

			if err != nil {
				tx.Rollback()
				return nil, err
			}

			if err := tx.Commit(); err != nil {
				return nil, err
			}

			var payload workqueue.Payload
			if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
				return nil, fmt.Errorf("failed to unmarshal payload: %w", err)
			}

			return &dbWorkMessage{
				db:          q.db,
				executionID: executionID,
				taskID:      a2a.TaskID(taskID),
				payload:     &payload,
			}, nil
		}
	}
}

func (q *dbWorkQueue) Write(ctx context.Context, payload *workqueue.Payload) (a2a.TaskID, error) {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	taskID, executionID := payload.TaskID, uuid.New().String()

	if payload.Type == workqueue.PayloadTypeCancel {
		_, err = q.db.ExecContext(ctx, `
			INSERT INTO task_execution (id, task_id, state, work_type, payload_json)
			VALUES (?, ?, 'pending', ?, ?)
		`, executionID, payload.TaskID, payload.Type, string(payloadJSON))
		return taskID, err
	}

	// For other types (Execute), check for concurrent execution
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	var existingState string
	err = tx.QueryRowContext(ctx, `
		SELECT state FROM task_execution
		WHERE task_id = ? AND state != 'completed'
		FOR UPDATE
	`, taskID).Scan(&existingState)

	if err == nil {
		return "", fmt.Errorf("concurrent execution in progress for task %s (state: %s)", taskID, existingState)
	}
	if err != sql.ErrNoRows {
		return "", err
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO task_execution (id, task_id, state, work_type, payload_json)
		VALUES (?, ?, 'pending', ?, ?)
	`, executionID, taskID, payload.Type, string(payloadJSON))

	if err != nil {
		return "", err
	}

	return taskID, tx.Commit()
}

type dbWorkMessage struct {
	db          *sql.DB
	executionID string
	taskID      a2a.TaskID
	payload     *workqueue.Payload
}

var _ workqueue.Message = (*dbWorkMessage)(nil)
var _ workqueue.Heartbeater = (*dbWorkMessage)(nil)

func (m *dbWorkMessage) HeartbeatInterval() time.Duration {
	return 5 * time.Second
}

func (m *dbWorkMessage) Heartbeat(ctx context.Context) error {
	_, err := m.db.ExecContext(ctx, `
		UPDATE task_execution
		SET last_updated = NOW()
		WHERE id = ?
	`, m.executionID)
	return err
}

func (m *dbWorkMessage) Payload() *workqueue.Payload {
	return m.payload
}

func (m *dbWorkMessage) Complete(ctx context.Context, result a2a.SendMessageResult) error {
	return m.setCompleted(ctx, "")
}

func (m *dbWorkMessage) Return(ctx context.Context, cause error) error {
	return m.setCompleted(ctx, cause.Error())
}

func (m *dbWorkMessage) setCompleted(ctx context.Context, cause string) error {
	_, err := m.db.ExecContext(ctx, `
		UPDATE task_execution
		SET state = 'completed', cause = ?
		WHERE id = ?
	`, cause, m.executionID)
	return err
}
