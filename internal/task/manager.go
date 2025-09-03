package task

import (
	"context"
	"github.com/a2aproject/a2a-go/a2a"
)

type Store interface {
	Save(ctx context.Context, task *a2a.Task) error
	Get(ctx context.Context, taskId a2a.TaskID) (*a2a.Task, error)
}

type Manager struct {
	TaskID    a2a.TaskID
	ContextID string
	task      *a2a.Task

	store Store
}

func NewManager(store Store, taskID a2a.TaskID, contextID string) *Manager {
	return &Manager{
		TaskID:    taskID,
		ContextID: contextID,
		store:     store,
	}
}

func GetTask(ctx context.Context) (*a2a.Task, error) {

}

func (m *Manager) Process(ctx context.Context, event a2a.Event) error {
	if _, ok := event.(*a2a.Message); ok {
		return nil
	}
	
}
