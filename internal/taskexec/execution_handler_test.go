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
	"testing"

	"github.com/a2aproject/a2a-go/a2a"
)

func TestRunProducerConsumer(t *testing.T) {
	panicFn := func(str string) error { panic(str) }
	msg := a2a.NewMessage(a2a.MessageRoleUser)

	testCases := []struct {
		name     string
		producer eventProducerFn
		consumer eventConsumerFn
		wantErr  error
	}{
		{
			name:     "success",
			producer: func(ctx context.Context) error { return nil },
			consumer: func(ctx context.Context) (a2a.SendMessageResult, error) { return msg, nil },
		},
		{
			name:     "producer panic",
			producer: func(ctx context.Context) error { return panicFn("panic!") },
			consumer: func(ctx context.Context) (a2a.SendMessageResult, error) {
				<-ctx.Done()
				return nil, nil
			},
			wantErr: fmt.Errorf("event producer panic: panic!"),
		},
		{
			name:     "consumer panic",
			producer: func(ctx context.Context) error { return nil },
			consumer: func(ctx context.Context) (a2a.SendMessageResult, error) { return nil, panicFn("panic!") },
			wantErr:  fmt.Errorf("event consumer panic: panic!"),
		},
		{
			name:     "producer error",
			producer: func(ctx context.Context) error { return fmt.Errorf("error") },
			consumer: func(ctx context.Context) (a2a.SendMessageResult, error) {
				<-ctx.Done()
				return nil, nil
			},
			wantErr: fmt.Errorf("error"),
		},
		{
			name:     "producer error override by consumer result",
			producer: func(ctx context.Context) error { return fmt.Errorf("error") },
			consumer: func(ctx context.Context) (a2a.SendMessageResult, error) {
				<-ctx.Done()
				return &a2a.Task{Status: a2a.TaskStatus{State: a2a.TaskStateFailed}}, nil
			},
		},
		{
			name:     "consumer error",
			producer: func(ctx context.Context) error { return nil },
			consumer: func(ctx context.Context) (a2a.SendMessageResult, error) { return nil, fmt.Errorf("error") },
			wantErr:  fmt.Errorf("error"),
		},
		{
			name:     "nil consumer result",
			producer: func(ctx context.Context) error { return nil },
			consumer: func(ctx context.Context) (a2a.SendMessageResult, error) { return nil, nil },
			wantErr:  fmt.Errorf("bug: consumer stopped, but result unset: consumer stopped"),
		},
		{
			name: "producer context canceled on consumer non-nil result",
			producer: func(ctx context.Context) error {
				<-ctx.Done()
				return ctx.Err()
			},
			consumer: func(ctx context.Context) (a2a.SendMessageResult, error) { return msg, nil },
		},
		{
			name: "producer context canceled on consumer error result",
			producer: func(ctx context.Context) error {
				<-ctx.Done()
				return ctx.Err()
			},
			consumer: func(ctx context.Context) (a2a.SendMessageResult, error) { return nil, fmt.Errorf("error") },
			wantErr:  fmt.Errorf("error"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := runProducerConsumer(t.Context(), tc.producer, tc.consumer, nil)
			if tc.wantErr != nil && err == nil {
				t.Fatalf("expected error, got %v", result)
			}
			if tc.wantErr == nil && err != nil {
				t.Fatalf("expected result, got %v, %v", result, err)
			}
			if tc.wantErr != nil && tc.wantErr.Error() != err.Error() {
				t.Fatalf("expected error = %s, got %s", tc.wantErr.Error(), err.Error())
			}
			if result == nil && err == nil {
				t.Fatalf("expected non-nil error when result is nil")
			}
		})
	}

}
