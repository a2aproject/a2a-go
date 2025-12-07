package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
	"github.com/a2aproject/a2a-go/log"
)

type dbEventQueueManager struct {
	db *sql.DB

	mu     sync.Mutex
	queues map[a2a.TaskID][]*dbEventQueue
}

func newDBEventQueueManager(db *sql.DB) *dbEventQueueManager {
	return &dbEventQueueManager{
		db:     db,
		queues: make(map[a2a.TaskID][]*dbEventQueue),
	}
}

var _ eventqueue.Manager = (*dbEventQueueManager)(nil)

func (m *dbEventQueueManager) GetOrCreate(ctx context.Context, taskID a2a.TaskID) (eventqueue.Queue, error) {
	var pollFromID string
	err := m.db.QueryRowContext(ctx, `SELECT MAX(id) FROM task_event WHERE task_id = ?`, taskID).Scan(&pollFromID)

	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("failed to query latest event version: %w", err)
	}

	q := newDBEventQueue(m.db, taskID, pollFromID)

	m.mu.Lock()
	m.queues[taskID] = append(m.queues[taskID], q)
	m.mu.Unlock()

	return q, nil
}

func (m *dbEventQueueManager) Get(ctx context.Context, taskID a2a.TaskID) (eventqueue.Queue, bool) {
	q, err := m.GetOrCreate(ctx, taskID)
	return q, err == nil
}

func (m *dbEventQueueManager) Destroy(ctx context.Context, taskID a2a.TaskID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, queue := range m.queues[taskID] {
		if err := queue.Close(); err != nil {
			return err
		}
	}
	delete(m.queues, taskID)
	return nil
}

type versionedEvent struct {
	event   a2a.Event
	version a2a.TaskVersion
}

type dbEventQueue struct {
	closeSignal chan struct{}
	closed      chan struct{}
	eventsCh    chan *versionedEvent
}

var _ eventqueue.Queue = (*dbEventQueue)(nil)

func newDBEventQueue(db *sql.DB, taskID a2a.TaskID, pollFromID string) *dbEventQueue {
	queue := &dbEventQueue{
		closeSignal: make(chan struct{}),
		closed:      make(chan struct{}),
		eventsCh:    make(chan *versionedEvent),
	}
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)

		defer func() {
			ticker.Stop()
			close(queue.closed)
			close(queue.eventsCh)
		}()

		ctx := context.Background()
		for {
			select {
			case <-queue.closeSignal:
				return
			case <-ticker.C:
				rows, err := db.QueryContext(ctx, `
				SELECT event_json, task_version, id
				FROM task_event
				WHERE task_id = ? AND id > ?
				ORDER BY id ASC
				LIMIT 10
			`, taskID, pollFromID)

				if err != nil {
					// TODO: circuit breaker if not ErrNoRows
					continue
				}

				for rows.Next() {
					var version int64
					var eventJSON, id string
					if err := rows.Scan(&eventJSON, &version, &id); err != nil {
						rows.Close()
						continue
					}
					event, err := a2a.UnmarshalEventJSON([]byte(eventJSON))
					if err != nil {
						log.Error(ctx, "failed to unmarshal event", err)
						continue
					}
					select {
					case queue.eventsCh <- &versionedEvent{
						event:   event,
						version: a2a.TaskVersionInt(version),
					}:
					case <-queue.closeSignal:
						rows.Close()
						return
					}
					pollFromID = id
				}
				rows.Close()
			}
		}
	}()
	return queue
}

func (q *dbEventQueue) Read(ctx context.Context) (a2a.Event, a2a.TaskVersion, error) {
	select {
	case <-ctx.Done():
		return nil, nil, ctx.Err()

	case res, ok := <-q.eventsCh:
		if !ok {
			return nil, nil, eventqueue.ErrQueueClosed
		}
		return res.event, res.version, nil

	case <-q.closed:
		return nil, nil, eventqueue.ErrQueueClosed
	}
}

func (q *dbEventQueue) Close() error {
	select {
	case <-q.closed:
		return nil
	case q.closeSignal <- struct{}{}:
	}
	<-q.closed
	return nil
}

func (q *dbEventQueue) Write(ctx context.Context, event a2a.Event) error {
	return nil // events are written through TaskStore
}

func (q *dbEventQueue) WriteVersioned(ctx context.Context, event a2a.Event, version a2a.TaskVersion) error {
	return nil // events are written through TaskStore
}
