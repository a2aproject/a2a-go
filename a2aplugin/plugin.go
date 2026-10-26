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

package a2aplugin

import (
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aplugin/core"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

// Plugin is the common base of every plugin. Concrete plugins additionally
// implement one or more hook interfaces, such as [a2asrv.ServerPlugin] or
// [a2aclient.ClientPlugin].
type Plugin = core.Plugin

// CompletePlugin is a plugin that implements both the client and the server hooks.
type CompletePlugin interface {
	a2asrv.ServerPlugin
	a2aclient.ClientPlugin
}
