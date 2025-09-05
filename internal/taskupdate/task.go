package taskupdate

import "github.com/a2aproject/a2a-go/a2a"

func NewSubmittedTask(msg *a2a.Message) *a2a.Task {
	history := make([]*a2a.Message, 1)
	history[0] = msg

	contextID := msg.ContextID
	if len(contextID) == 0 {
		contextID = a2a.NewContextID()
	}

	return &a2a.Task{
		ID:        a2a.NewTaskID(),
		ContextID: contextID,
		Status:    a2a.TaskStatus{State: a2a.TaskStateSubmitted},
		History:   history,
	}
}
