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

package a2aevent

import (
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/internal/utils"
)

func makeBenchTask(numArtifacts, partsPerArtifact int) *a2a.Task {
	task := &a2a.Task{
		ID:        "test-task-id",
		ContextID: "test-context-id",
		Status:    a2a.TaskStatus{State: a2a.TaskStateSubmitted},
		Metadata:  map[string]any{"key1": "value1", "key2": 42},
	}
	for i := range numArtifacts {
		parts := make(a2a.ContentParts, partsPerArtifact)
		for j := range partsPerArtifact {
			parts[j] = a2a.NewTextPart("sample text content for part")
		}
		task.Artifacts = append(task.Artifacts, &a2a.Artifact{
			ID:       a2a.ArtifactID("artifact-" + string(rune('a'+i))),
			Name:     "artifact",
			Parts:    parts,
			Metadata: map[string]any{"src": "agent"},
		})
	}
	return task
}

func benchmarkDeepCopyTask(b *testing.B, task *a2a.Task) {
	b.ReportAllocs()
	for range b.N {
		_, _ = utils.DeepCopy(task)
	}
}

func benchmarkManualCopyTask(b *testing.B, task *a2a.Task) {
	b.ReportAllocs()
	for range b.N {
		_ = a2a.CopyTask(task)
	}
}

func BenchmarkDeepCopyTask_1Art_10Parts(b *testing.B) {
	benchmarkDeepCopyTask(b, makeBenchTask(1, 10))
}
func BenchmarkManualCopyTask_1Art_10Parts(b *testing.B) {
	benchmarkManualCopyTask(b, makeBenchTask(1, 10))
}

func BenchmarkDeepCopyTask_1Art_100Parts(b *testing.B) {
	benchmarkDeepCopyTask(b, makeBenchTask(1, 100))
}
func BenchmarkManualCopyTask_1Art_100Parts(b *testing.B) {
	benchmarkManualCopyTask(b, makeBenchTask(1, 100))
}

func BenchmarkDeepCopyTask_1Art_1000Parts(b *testing.B) {
	benchmarkDeepCopyTask(b, makeBenchTask(1, 1000))
}
func BenchmarkManualCopyTask_1Art_1000Parts(b *testing.B) {
	benchmarkManualCopyTask(b, makeBenchTask(1, 1000))
}

func BenchmarkDeepCopyTask_10Art_100Parts(b *testing.B) {
	benchmarkDeepCopyTask(b, makeBenchTask(10, 100))
}
func BenchmarkManualCopyTask_10Art_100Parts(b *testing.B) {
	benchmarkManualCopyTask(b, makeBenchTask(10, 100))
}
