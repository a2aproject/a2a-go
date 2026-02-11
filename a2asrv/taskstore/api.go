package taskstore

import (
	"context"

	"github.com/a2aproject/a2a-go/a2a"
)

// TaskVersion is a version of the task stored on the server.
type TaskVersion int64

// TaskVersionMissing is a special value used to denote that task version is not being tracked.
var TaskVersionMissing TaskVersion = 0

// After returns true if the version is greater than the other version.
// The methods consider every state "latest" if the version is not tracked (TaskVersionMissing).
// It is expected that:
// v1 := TaskVersionMissing
// v2 := TaskVersionMissing
// v1.After(v2) == v2.After(v1)
func (v TaskVersion) After(another TaskVersion) bool {
	if another == TaskVersionMissing {
		return true
	}
	if v == TaskVersionMissing {
		return false
	}
	return another < v
}

type StoredTask struct {
	Task    *a2a.Task
	Version TaskVersion
}

type Store interface {
	// Save stores a task. Implementations might choose to store event and use the previous known TaskVersion
	// for optimistic concurrency control during updates.
	Save(ctx context.Context, task *a2a.Task, event a2a.Event, prev TaskVersion) (TaskVersion, error)

	// Get retrieves a task by ID. If a Task doesn't exist the method should return [a2a.ErrTaskNotFound].
	Get(ctx context.Context, taskID a2a.TaskID) (*StoredTask, error)

	// List retrieves a list of tasks based on the provided request.
	List(ctx context.Context, req *a2a.ListTasksRequest) (*a2a.ListTasksResponse, error)
}
