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

package pbconv

import (
	"testing"

	"github.com/a2aproject/a2a-go/a2a"
)

func TestPathExtractors(t *testing.T) {
	t.Run("extractTaskID", func(t *testing.T) {
		tests := []struct {
			name    string
			path    string
			want    a2a.TaskID
			wantErr bool
		}{
			{
				name: "simple path",
				path: "tasks/12345",
				want: "12345",
			},
			{
				name: "complex path",
				path: "projects/p/locations/l/tasks/abc-def",
				want: "abc-def",
			},
			{
				name:    "missing value",
				path:    "tasks/",
				wantErr: true,
			},
			{
				name:    "missing keyword in path",
				path:    "configs/123",
				wantErr: true,
			},
			{
				name:    "empty path",
				wantErr: true,
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got, err := ExtractTaskID(tt.path)
				if (err != nil) != tt.wantErr {
					t.Errorf("extractTaskID() error = %v, wantErr %v", err, tt.wantErr)
					return
				}
				if got != tt.want {
					t.Errorf("extractTaskID() = %v, want %v", got, tt.want)
				}
			})
		}
	})

	t.Run("extractConfigID", func(t *testing.T) {
		tests := []struct {
			name    string
			path    string
			want    string
			wantErr bool
		}{
			{
				name: "simple path",
				path: "pushNotificationConfigs/abc-123",
				want: "abc-123",
			},
			{
				name: "complex path",
				path: "tasks/12345/pushNotificationConfigs/abc-123",
				want: "abc-123",
			},
			{
				name: "missing value", // push notification config ID is optional
				path: "pushNotificationConfigs/",
				want: "",
			},
			{
				name:    "missing keyword in path",
				path:    "tasks/123",
				wantErr: true,
			},
			{
				name:    "empty path",
				wantErr: true,
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got, err := ExtractConfigID(tt.path)
				if (err != nil) != tt.wantErr {
					t.Errorf("extractConfigID() error = %v, wantErr %v", err, tt.wantErr)
					return
				}
				if got != tt.want {
					t.Errorf("extractConfigID() = %v, want %v", got, tt.want)
				}
			})
		}
	})
}
