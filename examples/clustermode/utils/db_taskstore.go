package utils

import (
	"context"
	"fmt"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"
)

// Database defines the interface for persistence operations required by DBTaskStore.
type Database interface {
	// Create persists a new task and event.
	Create(ctx context.Context, task *a2a.Task, event a2a.Event, newVersion int64, protocolVersion a2a.ProtocolVersion) error
	// Update persists an updated task and event.
	// It returns true if the operation was successful (or false if optimistic locking failed).
	Update(ctx context.Context, task *a2a.Task, event a2a.Event, newVersion int64, prevVersion int64, protocolVersion a2a.ProtocolVersion) (bool, error)
	// Get retrieves a task by ID.
	Get(ctx context.Context, taskID a2a.TaskID) (*a2a.Task, int64, error)
}

// DBTaskStore implements a2asrv.TaskStore using a generic Database backend.
type DBTaskStore struct {
	db      Database
	version a2a.ProtocolVersion
}

// NewDBTaskStore creates a new DBTaskStore.
func NewDBTaskStore(db Database, version a2a.ProtocolVersion) *DBTaskStore {
	return &DBTaskStore{db: db, version: version}
}

var _ a2asrv.TaskStore = (*DBTaskStore)(nil)

// Save persists the task and event to the database.
func (s *DBTaskStore) Save(ctx context.Context, task *a2a.Task, event a2a.Event, prevVersion a2a.TaskVersion) (a2a.TaskVersion, error) {
	newVersion := time.Now().UnixNano()

	if prevVersion == a2a.TaskVersionMissing {
		err := s.db.Create(ctx, task, event, newVersion, s.version)
		if err != nil {
			return a2a.TaskVersionMissing, err
		}
	} else {
		success, err := s.db.Update(ctx, task, event, newVersion, int64(prevVersion), s.version)
		if err != nil {
			return a2a.TaskVersionMissing, err
		}
		if !success {
			return a2a.TaskVersionMissing, fmt.Errorf("optimistic concurrency failure: task updated by another transaction")
		}
	}

	return a2a.TaskVersion(newVersion), nil
}

// Get retrieves a task from the database.
func (s *DBTaskStore) Get(ctx context.Context, taskID a2a.TaskID) (*a2a.Task, a2a.TaskVersion, error) {
	task, version, err := s.db.Get(ctx, taskID)
	if err != nil {
		return nil, a2a.TaskVersionMissing, err
	}
	return task, a2a.TaskVersion(version), nil
}

// List is not implemented.
func (s *DBTaskStore) List(ctx context.Context, req *a2a.ListTasksRequest) (*a2a.ListTasksResponse, error) {
	return nil, fmt.Errorf("not implemented")
}
