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

package a2aclient

// Plugin extends a [Client] by wrapping its components. Each method receives
// the next component and returns a wrapped one, or next itself to leave it unchanged.
// If multiple plugins are installed, the first one is the outermost wrapper.
type Plugin interface {
	// Name identifies the plugin.
	Name() string

	// WrapCall wraps every call. Plugins wrap outside the [CallInterceptor]s.
	WrapCall(next CallHandler) CallHandler
}

// WithPlugins adds plugins to the ones already set for created [Client]s. The first plugin is the outermost wrapper.
func WithPlugins(plugins ...Plugin) FactoryOption {
	return factoryOptionFn(func(f *Factory) {
		f.plugins = append(f.plugins, plugins...)
	})
}

// PassthroughPlugin can be embedded by [Plugin] implementers who don't need all methods.
// It returns every component unchanged, so adding a method to [Plugin] doesn't break plugins embedding it.
type PassthroughPlugin struct{}

// WrapCall implements [Plugin.WrapCall].
func (PassthroughPlugin) WrapCall(next CallHandler) CallHandler {
	return next
}
