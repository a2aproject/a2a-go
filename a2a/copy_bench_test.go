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

package a2a

import (
	"testing"
)

func benchmarkCopyTaskWithArtifacts(b *testing.B, numArtifacts, partsPerArtifact int) {
	task := &Task{
		ID:        "test-task-id",
		ContextID: "test-context-id",
		Status:    TaskStatus{State: TaskStateSubmitted},
	}
	for i := range numArtifacts {
		parts := make(ContentParts, partsPerArtifact)
		for j := range partsPerArtifact {
			parts[j] = NewTextPart("sample text content for part " + string(rune(j)))
		}
		task.Artifacts = append(task.Artifacts, &Artifact{
			ID:       ArtifactID("artifact-" + string(rune(i))),
			Name:     "artifact",
			Parts:    parts,
			Metadata: map[string]any{"key": "value"},
		})
	}

	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		_ = CopyTask(task)
	}
}

func BenchmarkCopyTask_1Artifact_1Part(b *testing.B) {
	benchmarkCopyTaskWithArtifacts(b, 1, 1)
}

func BenchmarkCopyTask_1Artifact_10Parts(b *testing.B) {
	benchmarkCopyTaskWithArtifacts(b, 1, 10)
}

func BenchmarkCopyTask_1Artifact_100Parts(b *testing.B) {
	benchmarkCopyTaskWithArtifacts(b, 1, 100)
}

func BenchmarkCopyTask_1Artifact_1000Parts(b *testing.B) {
	benchmarkCopyTaskWithArtifacts(b, 1, 1000)
}

func BenchmarkCopyTask_10Artifacts_10Parts(b *testing.B) {
	benchmarkCopyTaskWithArtifacts(b, 10, 10)
}

func BenchmarkCopyTask_WithHistory(b *testing.B) {
	task := &Task{
		ID:        "test-task-id",
		ContextID: "test-context-id",
		Status:    TaskStatus{State: TaskStateSubmitted},
	}
	for i := range 10 {
		task.History = append(task.History, &Message{
			ID:        "msg-" + string(rune(i)),
			Role:      MessageRoleUser,
			ContextID: "test-context-id",
			Parts:     ContentParts{NewTextPart("history message " + string(rune(i)))},
		})
	}

	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		_ = CopyTask(task)
	}
}

func BenchmarkCopyPushConfig(b *testing.B) {
	config := &PushConfig{
		TaskID: "test-task-id",
		ID:     "config-id",
		URL:    "https://example.com/callback",
		Auth: &PushAuthInfo{
			Scheme:      "Bearer",
			Credentials: "token",
		},
	}

	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		_ = CopyPushConfig(config)
	}
}
