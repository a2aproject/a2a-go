// Copyright 2026 The A2A Authors
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

package workqueue

import (
	"errors"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

func TestInMemoryLeaseManager_ConflictMatrix(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		held    PayloadType
		request PayloadType
		wantErr bool
	}{
		{
			name:    "execute blocks execute",
			held:    PayloadTypeExecute,
			request: PayloadTypeExecute,
			wantErr: true,
		},
		{
			name:    "cancel blocks execute",
			held:    PayloadTypeCancel,
			request: PayloadTypeExecute,
			wantErr: true,
		},
		{
			name:    "cancel blocks cancel",
			held:    PayloadTypeCancel,
			request: PayloadTypeCancel,
			wantErr: true,
		},
		{
			name:    "execute allows cancel",
			held:    PayloadTypeExecute,
			request: PayloadTypeCancel,
			wantErr: false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			lm := NewInMemoryLeaseManager()
			tid := a2a.NewTaskID()

			_, err := lm.Acquire(t.Context(), &Payload{Type: tc.held, TaskID: tid})
			if err != nil {
				t.Fatalf("Acquire() first error = %v, want nil", err)
			}

			_, err = lm.Acquire(t.Context(), &Payload{Type: tc.request, TaskID: tid})
			if tc.wantErr && !errors.Is(err, ErrLeaseAlreadyTaken) {
				t.Fatalf("Acquire() error = %v, want %v", err, ErrLeaseAlreadyTaken)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("Acquire() error = %v, want nil", err)
			}
		})
	}
}

func TestInMemoryLeaseManager_DisjointTaskIDs(t *testing.T) {
	t.Parallel()

	lm := NewInMemoryLeaseManager()

	_, err := lm.Acquire(t.Context(), &Payload{Type: PayloadTypeExecute, TaskID: a2a.NewTaskID()})
	if err != nil {
		t.Fatalf("Acquire() first error = %v, want nil", err)
	}

	_, err = lm.Acquire(t.Context(), &Payload{Type: PayloadTypeExecute, TaskID: a2a.NewTaskID()})
	if err != nil {
		t.Fatalf("Acquire() second error = %v, want nil (different TaskIDs should not conflict)", err)
	}
}

func TestInMemoryLeaseManager_ReleaseAllowsReacquire(t *testing.T) {
	t.Parallel()

	lm := NewInMemoryLeaseManager()
	tid := a2a.NewTaskID()
	payload := &Payload{Type: PayloadTypeExecute, TaskID: tid}

	lease, err := lm.Acquire(t.Context(), payload)
	if err != nil {
		t.Fatalf("Acquire() error = %v, want nil", err)
	}

	lease.Release(t.Context())

	if _, err := lm.Acquire(t.Context(), payload); err != nil {
		t.Fatalf("Acquire() after Release() error = %v, want nil", err)
	}
}

func TestInMemoryLeaseManager_ReleaseIdempotent(t *testing.T) {
	t.Parallel()

	lm := NewInMemoryLeaseManager()
	tid := a2a.NewTaskID()

	lease, err := lm.Acquire(t.Context(), &Payload{Type: PayloadTypeExecute, TaskID: tid})
	if err != nil {
		t.Fatalf("Acquire() error = %v, want nil", err)
	}

	lease.Release(t.Context())
	lease.Release(t.Context()) // must not panic or corrupt state

	if _, err := lm.Acquire(t.Context(), &Payload{Type: PayloadTypeExecute, TaskID: tid}); err != nil {
		t.Fatalf("Acquire() after double Release() error = %v, want nil", err)
	}
}

func TestInMemoryLeaseManager_TaskIDPassthrough(t *testing.T) {
	t.Parallel()

	lm := NewInMemoryLeaseManager()
	tid := a2a.NewTaskID()

	lease, err := lm.Acquire(t.Context(), &Payload{Type: PayloadTypeExecute, TaskID: tid})
	if err != nil {
		t.Fatalf("Acquire() error = %v, want nil", err)
	}

	if got := lease.TaskID(); got != tid {
		t.Fatalf("lease.TaskID() = %v, want %v", got, tid)
	}
}
