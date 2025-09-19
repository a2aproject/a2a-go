package pbconv

import (
	"fmt"
	"regexp"

	"github.com/a2aproject/a2a-go/a2a"
)

var (
	taskIDRegex   = regexp.MustCompile(`tasks/([^/]+)`)
	configIDRegex = regexp.MustCompile(`pushConfigs/([^/]+)`)
)

func ExtractTaskID(name string) (a2a.TaskID, error) {
	matches := taskIDRegex.FindStringSubmatch(name)
	if len(matches) < 2 {
		return "", fmt.Errorf("invalid or missing task ID in name: %q", name)
	}
	return a2a.TaskID(matches[1]), nil
}

func MakeTaskName(taskID a2a.TaskID) string {
	return "tasks/" + string(taskID)
}

func ExtractConfigID(name string) (string, error) {
	matches := configIDRegex.FindStringSubmatch(name)
	if len(matches) < 2 {
		return "", fmt.Errorf("invalid or missing config ID in name: %q", name)
	}
	return matches[1], nil
}

func MakeConfigName(taskID a2a.TaskID, configID string) string {
	return MakeTaskName(taskID) + "/pushConfigs/" + configID
}
