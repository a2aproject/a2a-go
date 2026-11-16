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

package a2aacp

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/a2aproject/a2a-go/v2/internal/acp"
)

// SettingError is the error of a session setting the agent refused, for
// example an unknown model. A task fails with it, wrapped, before its message
// is sent to the agent; see [Config.Failure].
type SettingError struct {
	// ID is the setting's ID, for example "model".
	ID string
	// Value is the value the agent refused.
	Value any
	// Options are the values the agent offers for the setting, or nil if it
	// reports none. The agent may still accept a value which isn't listed.
	Options []SettingOption
	// Err is the agent's error.
	Err error
}

// SettingOption is a value the agent offers for a session setting.
type SettingOption struct {
	Value       string
	Name        string
	Description string
	// Group is the name of the group the agent lists the value in, if any.
	Group string
}

// Error implements error.
func (e *SettingError) Error() string { return fmt.Sprintf("setting %q: %v", e.ID, e.Err) }

// Unwrap returns the agent's error.
func (e *SettingError) Unwrap() error { return e.Err }

// configure applies session settings in a stable order. Config options which
// already have the value aren't sent.
func configure(ctx context.Context, ag *agent, sessionID string, settings map[string]any) error {
	for _, id := range slices.Sorted(maps.Keys(settings)) {
		value := settings[id]
		method, opt, ok := ag.configMethod(sessionID, id)
		if !ok {
			return fmt.Errorf("the agent has no session setting %q", id)
		}
		var err error
		switch method {
		case acp.MethodSetConfigOption:
			if sameValue(opt.CurrentValue, value) {
				continue
			}
			req := acp.SetConfigOptionRequest{SessionID: sessionID, ConfigID: id, Value: value}
			if opt.Type == "boolean" {
				req.Type = opt.Type
			}
			var resp *acp.SetConfigOptionResponse
			if resp, err = ag.client.SetConfigOption(ctx, req); err == nil {
				ag.setOptions(sessionID, resp.ConfigOptions)
			}
		case acp.MethodSetMode, acp.MethodSetModel:
			s, isText := value.(string)
			if !isText {
				return fmt.Errorf("the session setting %q must be a string", id)
			}
			if method == acp.MethodSetMode {
				err = ag.client.SetMode(ctx, acp.SetModeRequest{SessionID: sessionID, ModeID: s})
			} else {
				err = ag.client.SetModel(ctx, acp.SetModelRequest{SessionID: sessionID, ModelID: s})
			}
		}
		if err != nil {
			return &SettingError{ID: id, Value: value, Options: settingOptions(opt.Options, ""), Err: err}
		}
	}
	return nil
}

// sameValue reports whether a setting value equals the current value of a
// config option, which is a string or a bool.
func sameValue(current, value any) bool {
	switch c := current.(type) {
	case string:
		v, ok := value.(string)
		return ok && v == c
	case bool:
		v, ok := value.(bool)
		return ok && v == c
	}
	return false
}

// settingOptions flattens the values of a select option, which the agent may
// list in groups.
func settingOptions(opts []acp.SessionConfigSelectOption, group string) []SettingOption {
	var out []SettingOption
	for _, o := range opts {
		if o.Group != "" {
			out = append(out, settingOptions(o.Options, o.Name)...)
			continue
		}
		out = append(out, SettingOption{Value: o.Value, Name: o.Name, Description: o.Description, Group: group})
	}
	return out
}
