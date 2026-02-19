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

package taskstore

import (
	"strings"
	"testing"

	"github.com/a2aproject/a2a-go/a2a"
)

type forbiddenType struct{}

func TestValidateTask(t *testing.T) {
	invalidMeta := map[string]any{"hello": forbiddenType{}}
	invalidMsg := &a2a.Message{Metadata: invalidMeta}
	invalidArtifact := &a2a.Artifact{Metadata: invalidMeta}

	testCases := []struct {
		task  *a2a.Task
		valid bool
	}{
		{task: nil, valid: true},
		{task: &a2a.Task{}, valid: true},
		{task: &a2a.Task{Status: a2a.TaskStatus{Message: invalidMsg}}},
		{task: &a2a.Task{History: []*a2a.Message{invalidMsg}}},
		{task: &a2a.Task{Artifacts: []*a2a.Artifact{invalidArtifact}}},
		{task: &a2a.Task{Metadata: invalidMeta}},
	}
	for i, tc := range testCases {
		err := validateTask(tc.task)
		if tc.valid && err != nil {
			t.Fatalf("validateTask(tasks[%d]) = %v, want pass", i, err)
		}
		if !tc.valid && err == nil {
			t.Fatalf("validateTask(tasks[%d]) = pass, want error", i)
		}
	}
}

func TestValidateArtifact(t *testing.T) {
	invalidMeta := map[string]any{"hello": forbiddenType{}}

	testCases := []struct {
		artifact *a2a.Artifact
		valid    bool
	}{
		{artifact: nil, valid: true},
		{artifact: &a2a.Artifact{}, valid: true},
		{artifact: &a2a.Artifact{Metadata: invalidMeta}},
		{artifact: &a2a.Artifact{Parts: a2a.ContentParts{{Metadata: invalidMeta}}}},
	}
	for i, tc := range testCases {
		err := validateArtifact(tc.artifact)
		if tc.valid && err != nil {
			t.Fatalf("validateArtifact(artifacts[%d]) = %v, want pass", i, err)
		}
		if !tc.valid && err == nil {
			t.Fatalf("validateArtifact(artifacts[%d]) = pass, want error", i)
		}
	}
}

func TestValidateMessage(t *testing.T) {
	invalidMeta := map[string]any{"hello": forbiddenType{}}

	testCases := []struct {
		msg   *a2a.Message
		valid bool
	}{
		{msg: nil, valid: true},
		{msg: &a2a.Message{}, valid: true},
		{msg: &a2a.Message{Metadata: invalidMeta}},
		{msg: &a2a.Message{Parts: a2a.ContentParts{{Metadata: invalidMeta}}}},
	}
	for i, tc := range testCases {
		err := validateMessage(tc.msg)
		if tc.valid && err != nil {
			t.Fatalf("validateMessage(msgs[%d]) = %v, want pass", i, err)
		}
		if !tc.valid && err == nil {
			t.Fatalf("validateMessage(msgs[%d]) = pass, want error", i)
		}
	}
}

func TestValidateParts(t *testing.T) {
	invalidMeta := map[string]any{"hello": forbiddenType{}}

	testCases := []struct {
		parts a2a.ContentParts
		valid bool
	}{
		{parts: nil, valid: true},
		{parts: a2a.ContentParts{}, valid: true},
		{parts: a2a.ContentParts{
			a2a.NewTextPart("hello"),
			a2a.NewDataPart(map[string]any{}),
			a2a.NewFileURLPart(a2a.URL("ftp://..."), ""),
			a2a.NewRawPart([]byte{1, 2, 3}),
		}, valid: true},
		{parts: a2a.ContentParts{{Metadata: invalidMeta}}},
	}
	for i, tc := range testCases {
		err := validateParts(tc.parts)
		if tc.valid && err != nil {
			t.Fatalf("validateParts(parts[%d]) = %v, want pass", i, err)
		}
		if !tc.valid && err == nil {
			t.Fatalf("validateParts(parts[%d]) = pass, want error", i)
		}
	}
}

func TestValidateMetaRepeatedRefSuccess(t *testing.T) {
	arr := make([]any, 1)
	if err := validateMeta(map[string]any{"a": arr, "b": arr}); err != nil {
		t.Fatalf("validateMeta() = %v, want pass", err)
	}
}

func TestValidateMetaCircularRefFailure(t *testing.T) {
	arr := make([]any, 1)
	arr[0] = arr
	if err := validateMeta(map[string]any{"a": arr}); !isCircularRefErr(err) {
		t.Fatalf("validateMeta() = %v, want a circular ref error", err)
	}

	m := map[string]any{"foo": "bar"}
	m["self"] = m
	if err := validateMeta(map[string]any{"m": m}); !isCircularRefErr(err) {
		t.Fatalf("validateMeta() = %v, want a circular ref error", err)
	}

	deep := map[string]any{"nested": map[string]any{}}
	(deep["nested"].(map[string]any))["self"] = deep
	if err := validateMeta(map[string]any{"d": deep}); !isCircularRefErr(err) {
		t.Fatalf("validateMeta() = %v, want a circular ref error", err)
	}
}

func isCircularRefErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "circular")
}
