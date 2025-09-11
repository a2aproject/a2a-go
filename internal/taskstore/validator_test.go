package taskstore

import (
	"github.com/a2aproject/a2a-go/a2a"
	"strings"
	"testing"
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
			t.Fatalf("expected Task to be valid for case %d, got %v", i, err)
		}
		if !tc.valid && err == nil {
			t.Fatalf("expected validate Task to fail for case %d", i)
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
		{artifact: &a2a.Artifact{Parts: a2a.ContentParts{a2a.TextPart{Metadata: invalidMeta}}}},
	}
	for i, tc := range testCases {
		err := validateArtifact(tc.artifact)
		if tc.valid && err != nil {
			t.Fatalf("expected Artifact to be valid for case %d, got %v", i, err)
		}
		if !tc.valid && err == nil {
			t.Fatalf("expected validate Artifact to fail for case %d", i)
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
		{msg: &a2a.Message{Parts: a2a.ContentParts{a2a.DataPart{Metadata: invalidMeta}}}},
	}
	for i, tc := range testCases {
		err := validateMessage(tc.msg)
		if tc.valid && err != nil {
			t.Fatalf("expected Message to be valid for case %d, got %v", i, err)
		}
		if !tc.valid && err == nil {
			t.Fatalf("expected validate Message to fail for case %d", i)
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
		{parts: a2a.ContentParts{a2a.TextPart{}, a2a.DataPart{}, a2a.FilePart{}}, valid: true},
		{parts: a2a.ContentParts{a2a.TextPart{Metadata: invalidMeta}}},
		{parts: a2a.ContentParts{a2a.DataPart{Metadata: invalidMeta}}},
		{parts: a2a.ContentParts{a2a.FilePart{Metadata: invalidMeta}}},
	}
	for i, tc := range testCases {
		err := validateParts(tc.parts)
		if tc.valid && err != nil {
			t.Fatalf("expected ContentParts to be valid for case %d, got %v", i, err)
		}
		if !tc.valid && err == nil {
			t.Fatalf("expected validate ContentParts to fail for case %d", i)
		}
	}
}

func TestValidateMetaCircularReference(t *testing.T) {
	arr := make([]any, 1)
	arr[0] = arr
	if err := validateMeta(map[string]any{"a": arr}); !isCircularRefErr(err) {
		t.Fatalf("expected a circular ref error, got %v", err)
	}

	m := map[string]any{"foo": "bar"}
	m["self"] = m
	if err := validateMeta(map[string]any{"m": m}); !isCircularRefErr(err) {
		t.Fatalf("expected a circular ref error, got %v", err)
	}

	deep := map[string]any{"nested": map[string]any{}}
	(deep["nested"].(map[string]any))["self"] = deep
	if err := validateMeta(map[string]any{"d": deep}); !isCircularRefErr(err) {
		t.Fatalf("expected a circular ref error, got %v", err)
	}
}

func isCircularRefErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "circular")
}
