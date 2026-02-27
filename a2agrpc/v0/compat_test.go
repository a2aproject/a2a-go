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

package a2agrpc

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/a2aproject/a2a-go/v1/a2a"
	"github.com/a2aproject/a2a-go/v1/a2aclient"
	"github.com/a2aproject/a2a-go/v1/a2asrv"
	"google.golang.org/grpc/metadata"
)

func TestCompat_WithGRPCMetadata(t *testing.T) {
	modernKey := strings.ToLower(a2a.SvcParamExtensions)
	legacyKey := "x-" + modernKey

	ctx := withGRPCMetadata(context.Background(), a2aclient.ServiceParams{
		modernKey: []string{"uri1"},
		"other":   []string{"val"},
	})
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		t.Fatal("expected metadata in outgoing context")
	}

	if !reflect.DeepEqual(md[legacyKey], []string{"uri1"}) {
		t.Errorf("expected %s: %v, got %v", legacyKey, []string{"uri1"}, md[legacyKey])
	}
	if !reflect.DeepEqual(md["other"], []string{"val"}) {
		t.Errorf("expected other: %v, got %v", []string{"val"}, md["other"])
	}
}

func TestCompat_ToTrailer(t *testing.T) {
	legacyKey := "x-" + strings.ToLower(a2a.SvcParamExtensions)

	ctx := context.Background()
	svcParams := a2asrv.NewServiceParams(map[string][]string{
		strings.ToLower(a2a.SvcParamExtensions): {"uri1"},
	})
	_, callCtx := a2asrv.NewCallContext(ctx, svcParams)

	// Manually activate an extension to see it in trailers
	callCtx.Extensions().Activate(&a2a.AgentExtension{URI: "uri1"})

	got := toTrailer(callCtx)
	if !reflect.DeepEqual(got[legacyKey], []string{"uri1"}) {
		t.Errorf("expected trailer %s: %v, got %v", legacyKey, []string{"uri1"}, got[legacyKey])
	}
}
