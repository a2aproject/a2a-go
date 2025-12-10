package tasksearchext

import (
	"context"
	"fmt"
	"strings"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2aext"
	"github.com/a2aproject/a2a-go/a2asrv"
)

type ServerExt struct {
	Method a2aext.ServerMethod
}

func NewForServer(store a2asrv.TaskStore) *ServerExt {
	method := a2aext.NewUnaryServerMethod(
		MethodName,
		func(ctx context.Context, req *Request) (*Response, error) {
			tasks, err := store.List(ctx, &a2a.ListTasksRequest{})
			if err != nil {
				return nil, fmt.Errorf("failed to list tasks: %w", err)
			}

			var filteredTasks []*a2a.Task

		tasksScan:
			for _, task := range tasks.Tasks {
				if task.Status.Message != nil && partsContain(task.Status.Message.Parts, req.Query) {
					filteredTasks = append(filteredTasks, task)
					continue
				}
				for _, artifact := range task.Artifacts {
					if partsContain(artifact.Parts, req.Query) {
						filteredTasks = append(filteredTasks, task)
						continue tasksScan
					}
				}
			}

			return &Response{Tasks: filteredTasks}, nil
		},
		a2asrv.NewJSONRPCMethodBinding[Request](JSONRPCMethodName),
	)
	return &ServerExt{Method: method}
}

func partsContain(parts []a2a.Part, text string) bool {
	lowerText := strings.ToLower(text)
	for _, part := range parts {
		if tp, ok := part.(a2a.TextPart); ok {
			if strings.Contains(strings.ToLower(tp.Text), lowerText) {
				return true
			}
		}
	}
	return false
}
