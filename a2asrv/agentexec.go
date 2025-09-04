package a2asrv

import (
	"context"
	"fmt"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
	"github.com/a2aproject/a2a-go/internal/taskupdate"
)

type executor struct {
	*processor
	agent  AgentExecutor
	reqCtx RequestContext
}

func (e *executor) Execute(ctx context.Context, q eventqueue.Queue) error {
	return e.agent.Execute(ctx, e.reqCtx, q)
}

type canceler struct {
	*processor
	agent AgentExecutor
	task  *a2a.Task
}

func (c *canceler) Cancel(ctx context.Context, q eventqueue.Queue) error {
	reqCtx := RequestContext{
		TaskID:    c.task.ID,
		ContextID: c.task.ContextID,
		Task:      c.task,
	}
	return c.agent.Cancel(ctx, reqCtx, q)
}

type processor struct {
	updateManager *taskupdate.Manager
}

func (p *processor) Process(ctx context.Context, q a2a.Event) (*a2a.SendMessageResult, error) {
	// TODO(yarolegovich): handle invalid event sequence where a Message is produced after a Task was created
	if msg, ok := q.(*a2a.Message); ok {
		var result a2a.SendMessageResult = msg
		return &result, nil
	}

	if err := p.updateManager.Process(ctx, q); err != nil {
		return nil, err
	}

	task := p.updateManager.Task

	// TODO(yarolegovich): handle pushes

	if task.Status.State == a2a.TaskStateUnknown {
		return nil, fmt.Errorf("unknown state state")
	}

	if task.Status.State.Terminal() || task.Status.State == a2a.TaskStateInputRequired {
		var result a2a.SendMessageResult = task
		return &result, nil
	}

	return nil, nil
}
