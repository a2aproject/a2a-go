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

package a2aotel

import (
	"context"
	"slices"
	"strings"

	"go.opentelemetry.io/otel/propagation"

	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

// injectServiceParams returns a copy of params carrying the trace context of ctx. The caller's params
// must stay unchanged so that a middleware can retry the call with the original request.
func injectServiceParams(ctx context.Context, propagators propagation.TextMapPropagator, params a2aclient.ServiceParams) a2aclient.ServiceParams {
	carrier := propagation.MapCarrier{}
	propagators.Inject(ctx, carrier)
	injected := make(a2aclient.ServiceParams, len(params)+len(carrier))
	for k, v := range params {
		injected[k] = slices.Clone(v)
	}
	for k, v := range carrier {
		injected[strings.ToLower(k)] = []string{v}
	}
	return injected
}

type serverParamsCarrier struct {
	params *a2asrv.ServiceParams
}

var _ propagation.TextMapCarrier = serverParamsCarrier{}

func (c serverParamsCarrier) Get(key string) string {
	values, ok := c.params.Get(key)
	if !ok || len(values) == 0 {
		return ""
	}
	return values[0]
}

func (c serverParamsCarrier) Set(string, string) {}

func (c serverParamsCarrier) Keys() []string {
	var keys []string
	for k := range c.params.List() {
		keys = append(keys, k)
	}
	return keys
}
