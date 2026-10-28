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

/*
Package a2aplugin provides a plugin framework for the A2A Go SDK. A plugin implements one or more hook interfaces
([a2asrv.ServerPlugin], [a2aclient.ClientPlugin], [a2asrv.ExecutionPlugin], [a2asrv.TaskObserverPlugin],
[push.Plugin]) and is dispatched by a [Manager] built with [Sequential]. Embed [Passthrough] to implement
only a subset of hooks.

# Ordering and errors

[Sequential] runs Before hooks in registration order and After hooks in reverse order. Managers implement
every hook interface, so they can be nested. An error returned from a Before hook rejects the call and the
After hooks of plugins that already ran are invoked with it. Errors returned from After and observer hooks
are logged.

# Wiring

A plugin can be configured in the following places:

  - [SetGlobal] installs the process-wide manager. It is read when components are constructed: by
    [a2asrv.NewHandler], [push.NewHTTPPushSender] and every time an [a2aclient.Factory] creates a client.
    Call it at startup, before building them.
  - [a2asrv.WithPlugins] sets the plugin of a request handler. It is also used for push hooks of a
    [push.HTTPPushSender] passed to [a2asrv.WithPushNotifications].
  - [a2aclient.WithPlugins] sets the plugin of clients created by a factory.
  - [push.HTTPSenderConfig.Plugin] sets the plugin of a push sender. It takes precedence over the plugin
    of the handler using the sender.

An explicitly configured plugin replaces the global one: hook interfaces it doesn't implement are
disabled for that component. To add plugins to the global ones, compose them with [Global]:

	handler := a2asrv.NewHandler(agent,
		a2asrv.WithPlugins(a2aplugin.Sequential(a2aplugin.Global(), newPlugin)),
	)
*/
package a2aplugin
