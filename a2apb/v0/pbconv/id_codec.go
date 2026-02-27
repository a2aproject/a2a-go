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
	"fmt"
	"regexp"

	"github.com/a2aproject/a2a-go/v1/a2a"
)

var (
	taskIDRegex   = regexp.MustCompile(`tasks/([^/]+)`)
	configIDRegex = regexp.MustCompile(`pushNotificationConfigs/([^/]*)`)
)

// ExtractTaskID extracts the task ID from a resource name.
func ExtractTaskID(name string) (a2a.TaskID, error) {
	matches := taskIDRegex.FindStringSubmatch(name)
	if len(matches) < 2 {
		return "", fmt.Errorf("invalid or missing task ID in name: %q", name)
	}
	return a2a.TaskID(matches[1]), nil
}

// MakeTaskName creates a task resource name from a task ID.
func MakeTaskName(taskID a2a.TaskID) string {
	return "tasks/" + string(taskID)
}

// ExtractConfigID extracts the config ID from a resource name.
func ExtractConfigID(name string) (string, error) {
	matches := configIDRegex.FindStringSubmatch(name)
	if len(matches) < 2 {
		return "", fmt.Errorf("invalid or missing config ID in name: %q", name)
	}
	return matches[1], nil
}

// MakeConfigName creates a config resource name from a task ID and a config ID.
func MakeConfigName(taskID a2a.TaskID, configID string) string {
	return MakeTaskName(taskID) + "/pushNotificationConfigs/" + configID
}
