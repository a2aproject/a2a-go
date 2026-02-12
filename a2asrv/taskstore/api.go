package taskstore

import (
	"context"
	"errors"

	"github.com/a2aproject/a2a-go/a2a"
)

// ErrTaskAlreadyExists indicates that a task with the provided ID already exists.
var ErrTaskAlreadyExists = errors.New("task already exists")

// ErrConcurrentModification indicates that optimistic concurrency control failed.
var ErrConcurrentModification = errors.New("concurrent modification")

// TaskVersion is a version of the task stored on the server.
type TaskVersion int64

// TaskVersionMissing is a special value used to denote that task version is not being tracked.
var TaskVersionMissing TaskVersion = 0

// After returns true if the version is greater than the other version.
// The methods consider every state "latest" if the version is not tracked (TaskVersionMissing).
// It is expected that:
//
//	v1 := TaskVersionMissing
//	v2 := TaskVersionMissing
//	v1.After(v2) == v2.After(v1)
func (v TaskVersion) After(another TaskVersion) bool {
	if another == TaskVersionMissing {
		return true
	}
	if v == TaskVersionMissing {
		return false
	}
	return another < v
}

// StoredTask represents a task stored in the task store.
type StoredTask struct {
	// Task is the stored data.
	Task *a2a.Task
	// Version is the task store version used for tracking task modifications.
	Version TaskVersion
}

// UpdateRequest represents a request to update a task.
type UpdateRequest struct {
	// Task represents the desired state of the task in the store.
	Task *a2a.Task
	// Event is the event that triggered the update.
	Event a2a.Event
	// PrevVersion is the version of the task before the update. It is passed for detecting concurrent udpates.
	// If the provided version does not match the latest task version the update request must be rejected with [ErrConcurrentModification].
	PrevVersion TaskVersion
}

type Store interface {
	// Create creates a new task. It should return [ErrTaskAlreadyExists] if a task with the provided ID already exists.
	Create(ctx context.Context, task *a2a.Task) (TaskVersion, error)

	// Update updates the stored task. It should return [a2a.ErrTaskNotFound] if a task with the provided ID doesn't exist.
	Update(ctx context.Context, update *UpdateRequest) (TaskVersion, error)

	// Get retrieves a task by ID. If a Task doesn't exist the method should return [a2a.ErrTaskNotFound].
	Get(ctx context.Context, taskID a2a.TaskID) (*StoredTask, error)

	// List retrieves a list of tasks based on the provided request.
	List(ctx context.Context, req *a2a.ListTasksRequest) (*a2a.ListTasksResponse, error)
}
