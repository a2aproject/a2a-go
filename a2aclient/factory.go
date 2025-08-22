// Copyright 2025 The A2A Authors
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

import (
	"context"

	"github.com/a2aproject/a2a-go/a2a"
)

// Factory provides an API for creating Clients compatible with the requested transports.
// Factory is immutable, but the configuration can be extended using WithAdditionalOptions(f, opts...) call.
// Additional configurations can be applied at the moment of Client creation.
type Factory struct {
	config       Config
	callbacks    []EventCallback
	interceptors []CallInterceptor
	transports   map[string]TransportFactory
}

// CreateFromCard returns a Client configured to communicate with the agent described by
// the provided AgentCard or fails if we couldn't establish a compatible transport.
func (f *Factory) CreateFromCard(ctx context.Context, card a2a.AgentCard, opts ...FactoryOption) (Client, error) {
	if len(opts) > 0 {
		extended := WithAdditionalOptions(*f, opts...)
		return extended.CreateFromCard(ctx, card)
	}

	return Client{}, ErrNotImplemented
}

// CreateFromURL returns a Client configured to communicate with provided URL using
// one of the provided protocols, or fails if we couldn't establish a compatible transport.
func (f *Factory) CreateFromURL(ctx context.Context, url string, protocols []string, opts ...FactoryOption) (Client, error) {
	if len(opts) > 0 {
		extended := WithAdditionalOptions(*f, opts...)
		return extended.CreateFromURL(ctx, url, protocols)
	}

	return Client{}, ErrNotImplemented
}

// FactoryOption represents a configuration applied to a Factory.
type FactoryOption interface {
	apply(f *Factory)
}

type factoryOptionFn func(f *Factory)

func (f factoryOptionFn) apply(factory *Factory) {
	f(factory)
}

// WithConfig makes the provided Config be used for all Clients created by the factory.
func WithConfig(c Config) FactoryOption {
	return factoryOptionFn(func(f *Factory) {
		f.config = c
	})
}

// WithTransport enables the factory to creates clients for the provided protocol.
func WithTransport(protocol string, factory TransportFactory) FactoryOption {
	return factoryOptionFn(func(f *Factory) {
		f.transports[protocol] = factory
	})
}

// WithEventCallbacks attaches callbacks to clients created by the factory.
func WithEventCallbacks(callbacks ...EventCallback) FactoryOption {
	return factoryOptionFn(func(f *Factory) {
		f.callbacks = append(f.callbacks, callbacks...)
	})
}

// WithInterceptors attaches call interceptors to clients created by the factory.
func WithInterceptors(interceptors ...CallInterceptor) FactoryOption {
	return factoryOptionFn(func(f *Factory) {
		f.interceptors = append(f.interceptors, interceptors...)
	})
}

// defaultsDisabledOpt is a marker for creating a Factory without any defaults set.
type defaultsDisabledOpt struct{}

func (defaultsDisabledOpt) apply(f *Factory) {}

// WithDefaultsDisabled attaches call interceptors to clients created by the factory.
func WithDefaultsDisabled() FactoryOption {
	return defaultsDisabledOpt{}
}

// defaultOptions is a set of default configurations applied to every Factory unless WithDefaultsDisabled was used.
var defaultOptions = []FactoryOption{WithGRPCTransport()}

// NewFactory creates a new Factory applying the provided configurations.
func NewFactory(options ...FactoryOption) *Factory {
	f := &Factory{
		transports:   make(map[string]TransportFactory),
		callbacks:    make([]EventCallback, 0),
		interceptors: make([]CallInterceptor, 0),
	}

	applyDefaults := true
	for _, o := range options {
		if _, ok := o.(defaultsDisabledOpt); ok {
			applyDefaults = false
			break
		}
	}

	if applyDefaults {
		for _, o := range defaultOptions {
			o.apply(f)
		}
	}

	for _, o := range options {
		o.apply(f)
	}

	return f
}

// WithAdditionalOptions creates a new Factory with the additionally provided options.
func WithAdditionalOptions(f Factory, opts ...FactoryOption) *Factory {
	options := []FactoryOption{
		WithDefaultsDisabled(),
		WithConfig(f.config),
		WithInterceptors(f.interceptors...),
		WithEventCallbacks(f.callbacks...),
	}
	for k, v := range f.transports {
		options = append(options, WithTransport(k, v))
	}
	return NewFactory(append(options, opts...)...)
}
