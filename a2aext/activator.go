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

package a2aext

import (
	"context"

	"github.com/a2aproject/a2a-go/a2aclient"
)

// NewActivator creates an [a2aclient.CallInterceptor] which requests extension activation
// when calls are made to the server supporting these extensions.
func NewActivator(extensionURIs ...string) a2aclient.CallInterceptor {
	return &activator{extensionURI: extensionURIs}
}

type activator struct {
	a2aclient.PassthroughInterceptor
	extensionURI []string
}

func (c *activator) Before(ctx context.Context, req *a2aclient.Request) (context.Context, any, error) {
	if req.Card == nil || len(req.Card.Capabilities.Extensions) == 0 {
		return ctx, nil, nil
	}

	var toAppend []string
	for _, ext := range c.extensionURI {
		if isExtensionSupported(req.Card, ext) {
			toAppend = append(toAppend, ext)
		}
	}
	if len(toAppend) > 0 {
		req.Meta.Append(CallMetaKey, toAppend...)
	}

	return ctx, nil, nil
}
