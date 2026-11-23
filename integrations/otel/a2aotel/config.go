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
	"os"
	"runtime/debug"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/log"
)

const (
	// ScopeName is the instrumentation scope name of the telemetry emitted by the plugins.
	ScopeName = "github.com/a2aproject/a2a-go/v2/integrations/otel/a2aotel"

	// EnvEnabled is the environment variable which disables the plugins when set to "false".
	EnvEnabled = "OTEL_INSTRUMENTATION_A2A_SDK_ENABLED"

	pluginName    = "a2aotel"
	sdkModulePath = "github.com/a2aproject/a2a-go/v2"
)

// Option configures both the client and the server plugin.
type Option interface {
	ServerOption
	apply(*config)
}

// ServerOption configures the server plugin. Every [Option] is also a ServerOption.
type ServerOption interface {
	applyServer(*serverConfig)
}

type optionFunc func(*config)

func (fn optionFunc) apply(c *config) {
	fn(c)
}

func (fn optionFunc) applyServer(c *serverConfig) {
	fn(&c.config)
}

type serverOptionFunc func(*serverConfig)

func (fn serverOptionFunc) applyServer(c *serverConfig) {
	fn(c)
}

// WithTracerProvider sets the provider of the tracer used to create spans.
// Defaults to the global provider returned by [otel.GetTracerProvider].
func WithTracerProvider(provider trace.TracerProvider) Option {
	return optionFunc(func(c *config) {
		if provider != nil {
			c.tracerProvider = provider
		}
	})
}

// WithPropagators sets the propagators used to send and receive the trace context in A2A service parameters.
// Defaults to the global propagators returned by [otel.GetTextMapPropagator].
func WithPropagators(propagators propagation.TextMapPropagator) Option {
	return optionFunc(func(c *config) {
		if propagators != nil {
			c.propagators = propagators
		}
	})
}

// WithAgentCard sets the card of the agent served by the server plugin. Its name, description and version
// are reported on server spans.
func WithAgentCard(card *a2a.AgentCard) ServerOption {
	return serverOptionFunc(func(c *serverConfig) {
		c.agentCard = card
	})
}

// WithPublicEndpoint configures the server plugin to start a new root trace instead of parenting
// server spans to the trace context received in call service parameters. Any valid incoming span
// context is attached to the server span as a link.
func WithPublicEndpoint() ServerOption {
	return serverOptionFunc(func(c *serverConfig) {
		c.publicEndpoint = true
	})
}

type config struct {
	enabled        bool
	tracerProvider trace.TracerProvider
	propagators    propagation.TextMapPropagator
}

type serverConfig struct {
	config
	agentCard      *a2a.AgentCard
	publicEndpoint bool
}

func defaultConfig() config {
	return config{
		enabled:        enabledFromEnv(),
		tracerProvider: otel.GetTracerProvider(),
		propagators:    otel.GetTextMapPropagator(),
	}
}

func newConfig(opts []Option) *config {
	c := defaultConfig()
	for _, opt := range opts {
		opt.apply(&c)
	}
	return &c
}

func newServerConfig(opts []ServerOption) *serverConfig {
	c := &serverConfig{config: defaultConfig()}
	for _, opt := range opts {
		opt.applyServer(c)
	}
	return c
}

func (c *config) tracer() trace.Tracer {
	return c.tracerProvider.Tracer(ScopeName, trace.WithInstrumentationVersion(instrumentationVersion()))
}

// enabledFromEnv treats only "true" and "false" as valid values, matching the OpenTelemetry
// specification for boolean environment variables, but defaults to enabled.
func enabledFromEnv() bool {
	value := strings.TrimSpace(os.Getenv(EnvEnabled))
	switch {
	case value == "" || strings.EqualFold(value, "true"):
		return true
	case strings.EqualFold(value, "false"):
		return false
	default:
		log.Warn(context.Background(), "invalid boolean environment variable value, using the default", "name", EnvEnabled, "value", value, "default", true)
		return true
	}
}

// instrumentationVersion returns the version of the SDK module, which the plugins are part of.
func instrumentationVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	if info.Main.Path == sdkModulePath {
		return info.Main.Version
	}
	for _, dep := range info.Deps {
		if dep.Path != sdkModulePath {
			continue
		}
		if dep.Replace != nil {
			return dep.Replace.Version
		}
		return dep.Version
	}
	return ""
}
