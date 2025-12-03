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

package a2a

// TaskVersion is a version of the task stored on the server.
type TaskVersion interface {
	After(another TaskVersion) bool
}

type taskVersionMissingType struct{}

// TaskVersionMissing is a special value used to denote that task version is not being tracked.
var TaskVersionMissing TaskVersion = taskVersionMissingType{}

func (taskVersionMissingType) After(another TaskVersion) bool {
	// Consider every state "latest" if the version is not tracked.
	return true
}

// TaskVersionInt is a version of the task stored on the server represented as an int64.
type TaskVersionInt int64

var _ TaskVersion = TaskVersionInt(0)

func (i TaskVersionInt) After(another TaskVersion) bool {
	if ai, ok := another.(TaskVersionInt); ok {
		return i > ai
	}
	return false
}
