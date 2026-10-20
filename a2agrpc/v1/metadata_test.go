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
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"google.golang.org/grpc/metadata"
)

func TestWithGRPCMetadataPreservesCallerMetadata(t *testing.T) {
	ctx := metadata.AppendToOutgoingContext(context.Background(),
		"authorization", "Bearer token",
		"a2a-version", "stale",
		"x-request-id", "req-1",
	)
	original, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		t.Fatal("expected metadata in outgoing context")
	}

	out := withGRPCMetadata(ctx, a2aclient.ServiceParams{
		a2a.SvcParamVersion: []string{"1.0"},
		"X-Tenant":          []string{"acme"},
	})
	// Mutating the caller's map must not change the outgoing call.
	original["authorization"] = []string{"mutated"}
	original["x-request-id"] = []string{"mutated"}

	md, ok := metadata.FromOutgoingContext(out)
	if !ok {
		t.Fatal("expected metadata in outgoing context")
	}
	if got, want := md["authorization"], []string{"Bearer token"}; !reflect.DeepEqual(got, want) {
		t.Errorf("authorization = %v, want %v", got, want)
	}
	if got, want := md["x-request-id"], []string{"req-1"}; !reflect.DeepEqual(got, want) {
		t.Errorf("x-request-id = %v, want %v", got, want)
	}
	if got, want := md["a2a-version"], []string{"1.0"}; !reflect.DeepEqual(got, want) {
		t.Errorf("a2a-version = %v, want %v", got, want)
	}
	if got, want := md["x-tenant"], []string{"acme"}; !reflect.DeepEqual(got, want) {
		t.Errorf("x-tenant = %v, want %v", got, want)
	}
}

func TestWithGRPCMetadataEmptyParamsKeepsContext(t *testing.T) {
	ctx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer token")
	out := withGRPCMetadata(ctx, nil)
	if out != ctx {
		t.Fatal("empty service params should leave the context unchanged")
	}
}
