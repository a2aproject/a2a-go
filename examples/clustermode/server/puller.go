// Copyright 2026 The A2A Authors

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     http://www.apache.org/licenses/LICENSE-2.0

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
	"github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"
	"github.com/a2aproject/a2a-go/v2/log"
)

var _ eventqueue.Puller = (*dbPuller)(nil)

type dbPuller struct {
	db    *sql.DB
	store taskstore.Store
}

func (p *dbPuller) Pull(ctx context.Context, taskID a2a.TaskID, cursor eventqueue.PullCursor) (*eventqueue.PullResponse, error) {
	if cursor == nil {
		storedTask, err := p.store.Get(ctx, taskID)
		if err == a2a.ErrTaskNotFound {
			// In cluster mode the frontend creates the reader before the worker has had a chance to INSERT
			// the initial task row. Tolerate ErrTaskNotFound by returning a synthetic SUBMITTED snapshot
			// so the reader can register
			return &eventqueue.PullResponse{
				Messages: []*eventqueue.Message{
					{
						Event:       &a2a.Task{ID: taskID, Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}},
						TaskVersion: taskstore.TaskVersionMissing,
						Protocol:    a2a.Version,
					},
				},
				Cursor: "",
			}, nil
		}
		if err != nil {
			return nil, err
		}
		var nextCursor sql.NullString
		err = p.db.QueryRowContext(ctx, `SELECT MAX(id) FROM task_event WHERE task_id = ?`, taskID).Scan(&nextCursor)
		if err != nil {
			return nil, fmt.Errorf("failed to query latest event version: %w", err)
		}
		return &eventqueue.PullResponse{
			Messages: []*eventqueue.Message{{Event: storedTask.Task, TaskVersion: storedTask.Version, Protocol: a2a.Version}},
			Cursor:   nextCursor.String,
		}, nil
	}
	rows, err := p.db.QueryContext(ctx, `
   SELECT event_json, task_version, id
   FROM task_event
   WHERE task_id = ? AND id > ?
   ORDER BY id ASC
   LIMIT 10
 `, taskID, cursor)
	var messages []*eventqueue.Message
	if err != nil {
		return nil, err
	}
	defer closeSQLRows(ctx, rows)
	for rows.Next() {
		var version int64
		var eventJSON, id string
		if err := rows.Scan(&eventJSON, &version, &id); err != nil {
			continue
		}
		var msg eventqueue.Message
		if err := json.Unmarshal([]byte(eventJSON), &msg); err != nil {
			log.Error(ctx, "failed to unmarshal event", err)
			continue
		}
		messages = append(messages, &msg)
		cursor = id
	}
	return &eventqueue.PullResponse{
		Messages: messages,
		Cursor:   cursor,
	}, nil
}

func (p *dbPuller) Close(ctx context.Context) error {
	return nil
}

func newDBPuller(db *sql.DB, store taskstore.Store) *dbPuller {
	return &dbPuller{db: db, store: store}
}

func newPullQueueManager(db *sql.DB, store taskstore.Store) eventqueue.Manager {
	pp := eventqueue.NewStaticPullerProvider(newDBPuller(db, store))
	cfg := eventqueue.PullConfig{
		PollInterval: 500 * time.Millisecond,
	}
	return eventqueue.NewPullQueueManager(pp, cfg)
}
