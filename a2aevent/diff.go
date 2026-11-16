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
	"reflect"
	"slices"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

// Recover attempts to return a list of events which when applied to the first argument bring
// it to the state of the second argument.
func Recover(prev *a2a.Task, curr *a2a.Task) []a2a.Event {
	var events []a2a.Event

	for _, artifact := range curr.Artifacts {
		prevI := slices.IndexFunc(prev.Artifacts, func(a *a2a.Artifact) bool { return a.ID == artifact.ID })
		if prevI < 0 {
			event := a2a.NewArtifactEvent(curr)
			event.Artifact = artifact
			events = append(events, event)
			continue
		}
		prevArtifact := prev.Artifacts[prevI]
		metaDiff := diffMetadata(prevArtifact.Metadata, artifact.Metadata)
		partDiff, partsChanged := diffParts(prevArtifact.Parts, artifact.Parts)
		canAppend := sameArtifactHeader(prevArtifact, artifact) && !hasRemovedKeys(prevArtifact.Metadata, artifact.Metadata)
		if partsChanged && partDiff.append && canAppend {
			event := a2a.NewArtifactUpdateEvent(curr, artifact.ID, partDiff.parts...)
			event.Artifact.Metadata = metaDiff
			events = append(events, event)
			continue
		}
		if partsChanged || len(metaDiff) > 0 || !canAppend {
			// A non-append update replaces the whole artifact, so it has to carry all of it.
			event := a2a.NewArtifactEvent(curr)
			event.Artifact = artifact
			events = append(events, event)
		}
	}

	metaDiff := diffMetadata(prev.Metadata, curr.Metadata)
	if hasStatusChanged(prev, curr) || len(metaDiff) > 0 {
		e := a2a.NewStatusUpdateEvent(curr, curr.Status.State, curr.Status.Message)
		e.Status.Timestamp = curr.Status.Timestamp
		e.Metadata = metaDiff
		events = append(events, e)
	}

	return events
}

func diffMetadata(prev map[string]any, curr map[string]any) map[string]any {
	diff := map[string]any{}
	for k, newVal := range curr {
		if prevVal, ok := prev[k]; !ok || !reflect.DeepEqual(newVal, prevVal) {
			diff[k] = newVal
		}
	}
	if len(diff) == 0 {
		return nil
	}
	return diff
}

func hasRemovedKeys(prev map[string]any, curr map[string]any) bool {
	for k := range prev {
		if _, ok := curr[k]; !ok {
			return true
		}
	}
	return false
}

func sameArtifactHeader(prev *a2a.Artifact, curr *a2a.Artifact) bool {
	return prev.Name == curr.Name &&
		prev.Description == curr.Description &&
		slices.Equal(prev.Extensions, curr.Extensions)
}

func diffParts(prev []*a2a.Part, curr []*a2a.Part) (*partsDiff, bool) {
	if len(curr) < len(prev) {
		return &partsDiff{parts: curr}, true
	}
	changed := false
	for i := range prev {
		if !reflect.DeepEqual(prev[i], curr[i]) {
			changed = true
			break
		}
	}
	if changed {
		return &partsDiff{parts: curr}, true
	}
	if len(curr) == len(prev) {
		return nil, false
	}
	return &partsDiff{parts: curr[len(prev):], append: true}, true
}

func hasStatusChanged(prevState *a2a.Task, state *a2a.Task) bool {
	s1, s2 := prevState.Status, state.Status
	return s1.State != s2.State ||
		((s1.Message != nil) != (s2.Message != nil)) ||
		(s1.Message != nil && s1.Message.ID != s2.Message.ID) ||
		!timestampsEqual(s1.Timestamp, s2.Timestamp)
}

func timestampsEqual(t1, t2 *time.Time) bool {
	if t1 == nil || t2 == nil {
		return t1 == t2
	}
	return t1.Equal(*t2)
}

type partsDiff struct {
	parts  []*a2a.Part
	append bool
}
