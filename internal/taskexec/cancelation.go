// Copyright 2025 The A2A Authors
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

package taskexec

import (
	"context"
	"fmt"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/log"
)

type cancelation struct {
	params *a2a.TaskIDParams
	result *promise
}

func newCancelation(params *a2a.TaskIDParams) *cancelation {
	return &cancelation{
		params: params,
		result: newPromise(),
	}
}

func (c *cancelation) wait(ctx context.Context) (*a2a.Task, error) {
	result, err := c.result.wait(ctx)

	if err != nil {
		return nil, fmt.Errorf("cancelation failed: %w", err)
	}

	task, ok := result.(*a2a.Task)
	if !ok { // a2a.Message was the result of the execution
		log.Info(ctx, "failed to cancel, because execution resolved to a Message")
		return nil, a2a.ErrTaskNotCancelable
	}

	if task.Status.State != a2a.TaskStateCanceled {
		log.Info(ctx, "task in non-cancelable state", "state", task.Status.State)
		return nil, a2a.ErrTaskNotCancelable
	}

	return task, nil
}
