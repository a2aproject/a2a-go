package pbconv

import (
	"github.com/a2aproject/a2a-go/a2a"
	"testing"
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
				path: "pushConfigs/abc-123",
				want: "abc-123",
			},
			{
				name: "complex path",
				path: "tasks/12345/pushConfigs/abc-123",
				want: "abc-123",
			},
			{
				name:    "missing value",
				path:    "pushConfigs/",
				wantErr: true,
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
