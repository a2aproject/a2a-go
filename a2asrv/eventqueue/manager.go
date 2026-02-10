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

package eventqueue

import (
	"context"

	"github.com/a2aproject/a2a-go/a2a"
)

// Manager manages event queues on a per-task basis.
type Manager interface {
	// CreateReader creates a new reader for the specified task.
	CreateReader(ctx context.Context, taskID a2a.TaskID) (Reader, error)

	// CreateWriter creates a new writer for the specified task.
	CreateWriter(ctx context.Context, taskID a2a.TaskID) (Writer, error)

	// Destroy closes the queue for the specified task and frees all associates resources.
	Destroy(ctx context.Context, taskID a2a.TaskID) error
}
