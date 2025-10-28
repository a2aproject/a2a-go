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
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/log"
)

// Factory provides an API for creating Clients compatible with the requested transports.
// Factory is immutable, but the configuration can be extended using WithAdditionalOptions(f, opts...) call.
// Additional configurations can be applied at the moment of Client creation.
type Factory struct {
	config       Config
	interceptors []CallInterceptor
	transports   map[a2a.TransportProtocol]TransportFactory
}

// transportCandidate represents an Agent endpoint with the protocol supported by the Client
// and is used during the best compatible transport selection.
type transportCandidate struct {
	factory  TransportFactory
	endpoint a2a.AgentInterface
	// priority if determined by the index of endpoint.Transport in Config.PreferredTransports
	// or is set to len(Config.PreferredTransports) if Transport is not present in the config
	priority int
}

// defaultOptions is a set of default configurations applied to every Factory unless WithDefaultsDisabled was used.
var defaultOptions = []FactoryOption{WithGRPCTransport()}

// NewFromCard is a helper for creating a Client configured without creating a factory.
// It is equivalent to calling NewFromCard on a Factory created without any options.
func NewFromCard(ctx context.Context, card *a2a.AgentCard, opts ...FactoryOption) (*Client, error) {
	return NewFactory(opts...).CreateFromCard(ctx, card)
}

// NewFromEndpoints is a helper for creating a Client configured without creating a factory.
// It is equivalent to calling NewFromEndpoints on a Factory created without any options.
func NewFromEndpoints(ctx context.Context, endpoints []a2a.AgentInterface, opts ...FactoryOption) (*Client, error) {
	return NewFactory(opts...).CreateFromEndpoints(ctx, endpoints)
}

// CreateFromCard returns a Client configured to communicate with the agent described by
// the provided AgentCard or fails if we couldn't establish a compatible transport.
// Config PreferredTransports will be used to determine the order of connection attempts.
// If PreferredTransports were not provided, we start from the PreferredTransport specified in the AgentCard
// and proceed in the order specified by the AdditionalInterfaces.
// The method fails if we couldn't establish a compatible transport.
func (f *Factory) CreateFromCard(ctx context.Context, card *a2a.AgentCard) (*Client, error) {
	serverPrefs := make([]a2a.AgentInterface, 1+len(card.AdditionalInterfaces))
	serverPrefs[0] = a2a.AgentInterface{Transport: card.PreferredTransport, URL: card.URL}
	copy(serverPrefs[1:], card.AdditionalInterfaces)

	candidates, err := f.selectTransport(serverPrefs)
	if err != nil {
		return nil, err
	}

	conn, err := createTransport(ctx, candidates, card)
	if err != nil {
		return nil, fmt.Errorf("failed to open a connection: %w", err)
	}

	return &Client{
		Config:       f.config,
		transport:    conn,
		interceptors: f.interceptors,
	}, nil
}

// CreateFromEndpoints returns a Client configured to communicate with one of the provided endpoints.
// Config PreferredTransports will be used to determine the order of connection attempts.
// If PreferredTransports were not provided, we attempt to establish using the provided endpoint order.
// The method fails if we couldn't establish a compatible transport.
func (f *Factory) CreateFromEndpoints(ctx context.Context, endpoints []a2a.AgentInterface) (*Client, error) {
	candidates, err := f.selectTransport(endpoints)
	if err != nil {
		return nil, err
	}

	conn, err := createTransport(ctx, candidates, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to open a connection: %w", err)
	}

	return &Client{
		Config:       f.config,
		transport:    conn,
		interceptors: f.interceptors,
	}, nil
}

// createTransport attempts to connect using the provided transports, returning the first
// one that succeeds. If all transports fail, it returns an error.
func createTransport(ctx context.Context, candidates []transportCandidate, card *a2a.AgentCard) (Transport, error) {
	if len(candidates) == 0 {
		return nil, fmt.Errorf("empty list of transport candidates was provided")
	}
	var transport Transport
	var failures []error
	for _, tc := range candidates {
		conn, err := tc.factory.Create(ctx, tc.endpoint.URL, card)
		if err == nil {
			transport = conn
			break
		}
		err = fmt.Errorf("failed to connect to %s: %w", tc.endpoint.URL, err)
		failures = append(failures, err)
	}
	if transport == nil {
		return nil, errors.Join(failures...)
	}
	if len(failures) > 0 {
		log.Info(ctx, "some transports failed to connect", "failures", failures)
	}
	return transport, nil
}

// selectTransport filters the list of available endpoints leaving only those with
// compatible transport protocols. If config.PreferredTransports is set the result is ordered
// based on the provided client preferences.
func (f *Factory) selectTransport(available []a2a.AgentInterface) ([]transportCandidate, error) {
	candidates := make([]transportCandidate, 0, len(available))

	for _, opt := range available {
		if tf, ok := f.transports[opt.Transport]; ok {
			priority := len(f.config.PreferredTransports)
			for i, clientPref := range f.config.PreferredTransports {
				if clientPref == opt.Transport {
					priority = i
					break
				}
			}
			candidates = append(candidates, transportCandidate{tf, opt, priority})
		}
	}

	if len(candidates) == 0 {
		protocols := make([]string, len(available))
		for i, a := range available {
			protocols[i] = string(a.Transport)
		}
		return nil, fmt.Errorf("no compatible transports found: available transports - [%s]", strings.Join(protocols, ","))
	}

	if len(f.config.PreferredTransports) > 0 {
		slices.SortFunc(candidates, func(c1, c2 transportCandidate) int {
			return c1.priority - c2.priority
		})
	}

	return candidates, nil
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
func WithTransport(protocol a2a.TransportProtocol, factory TransportFactory) FactoryOption {
	return factoryOptionFn(func(f *Factory) {
		f.transports[protocol] = factory
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

// NewFactory creates a new Factory applying the provided configurations.
func NewFactory(options ...FactoryOption) *Factory {
	f := &Factory{
		transports:   make(map[a2a.TransportProtocol]TransportFactory),
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
func WithAdditionalOptions(f *Factory, opts ...FactoryOption) *Factory {
	options := []FactoryOption{
		WithDefaultsDisabled(),
		WithConfig(f.config),
		WithInterceptors(f.interceptors...),
	}
	for k, v := range f.transports {
		options = append(options, WithTransport(k, v))
	}
	return NewFactory(append(options, opts...)...)
}
