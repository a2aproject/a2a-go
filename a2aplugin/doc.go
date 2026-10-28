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

// Package a2aplugin provides a plugin framework for the A2A Go SDK, as
// proposed in go/a2a-plugins. A plugin implements one or more hook interfaces
// ([a2asrv.ServerPlugin], [a2aclient.ClientPlugin], [a2asrv.ExecutionPlugin], [a2asrv.TaskObserverPlugin],
// [push.Plugin]) and is dispatched by a [Manager] built with
// [Sequential]. A process-wide default manager can be installed with [SetGlobal].
package a2aplugin
