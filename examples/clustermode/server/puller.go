// Copyright 2026 The A2A Authors
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

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv/eventqueue"
	"github.com/a2aproject/a2a-go/v2/log"
)

var _ eventqueue.Puller = (*dbPuller)(nil)

type dbPuller struct {
	db        *sql.DB
	startFrom string
}

func (p *dbPuller) Pull(ctx context.Context, taskID a2a.TaskID, pullCursor eventqueue.PullCursor) (*eventqueue.PullResponse, error) {
	cursor, ok := pullCursor.(string)
	if !ok {
		return nil, fmt.Errorf("failed to convert pullCursor to string")
	}
	var (
		rows *sql.Rows
		err  error
	)

	if cursor == "" {
		cursor = p.startFrom
	}
	rows, err = p.db.QueryContext(ctx, `
		SELECT event_json, task_version, id
		FROM task_event
		WHERE task_id = ? AND id > ?
		ORDER BY id ASC
		LIMIT 10
	`, taskID, cursor)
	if err != nil {
		return nil, fmt.Errorf("failed to query events: %w", err)
	}
	defer closeSQLRows(ctx, rows)

	nextCursor := cursor
	var messages []*eventqueue.Message
	for rows.Next() {
		var version int64
		var eventJSON, id string
		if err := rows.Scan(&eventJSON, &version, &id); err != nil {
			return nil, fmt.Errorf("failed to scan events row: %w", err)
		}
		var msg eventqueue.Message
		if err := json.Unmarshal([]byte(eventJSON), &msg); err != nil {
			log.Error(ctx, "failed to unmarshal event", err)
			continue
		}
		messages = append(messages, &msg)
		nextCursor = id
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to query events: %w", err)
	}
	return &eventqueue.PullResponse{
		Messages: messages,
		Cursor:   nextCursor,
	}, nil
}

func (p *dbPuller) Close(ctx context.Context) error {
	return nil
}

func newDBPuller(db *sql.DB, startFrom string) *dbPuller {
	return &dbPuller{db: db, startFrom: startFrom}
}

func newPullQueueManager(db *sql.DB) eventqueue.Manager {
	pp := newPullerProvider(db)
	cfg := eventqueue.PullConfig{
		PollInterval: 500 * time.Millisecond,
	}
	return eventqueue.NewPullQueueManager(pp, cfg)
}

func newPullerProvider(db *sql.DB) eventqueue.PullerProvider {
	return func(ctx context.Context, taskID a2a.TaskID) (eventqueue.Puller, error) {
		var startFrom sql.NullString
		if err := db.QueryRowContext(ctx, `
			SELECT COALESCE(MAX(id), "")
			FROM task_event
			WHERE task_id = ?
		`, taskID).Scan(&startFrom); err != nil {
			return nil, fmt.Errorf("failed to query task: %w", err)
		}
		return newDBPuller(db, startFrom.String), nil
	}
}
