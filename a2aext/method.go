package a2aext

import (
	"context"
	"errors"
	"iter"

	"github.com/a2aproject/a2a-go/a2a"
)

type Binding interface {
	Protocol() a2a.TransportProtocol
}

type Method interface {
	Name() string
	Binding(a2a.TransportProtocol) (Binding, bool)
	Streaming() bool
}

type ServerMethod interface {
	Method

	InvokeUnary(ctx context.Context, arg any) (any, error)
	InvokeStreaming(ctx context.Context, arg any) iter.Seq2[any, error]
}

type methodDescriptor struct {
	name      string
	bindings  map[a2a.TransportProtocol]Binding
	streaming bool
}

func (d *methodDescriptor) Name() string { return d.name }

func (d *methodDescriptor) Binding(protocol a2a.TransportProtocol) (Binding, bool) {
	b, ok := d.bindings[protocol]
	return b, ok
}

func (d *methodDescriptor) Streaming() bool { return d.streaming }

type UnaryClientMethod[Req, Resp any] struct {
	methodDescriptor
}

var _ Method = (*UnaryClientMethod[any, any])(nil)

// NewUnaryClientMethod creates a new unary extension method definition.
func NewUnaryClientMethod[Arg, Res any](
	name string,
	bindings ...Binding,
) *UnaryClientMethod[Arg, Res] {
	bmap := map[a2a.TransportProtocol]Binding{}
	for _, b := range bindings {
		bmap[b.Protocol()] = b
	}
	return &UnaryClientMethod[Arg, Res]{
		methodDescriptor: methodDescriptor{name: name, bindings: bmap, streaming: false},
	}
}

type unaryServerMethod[Arg, Res any] struct {
	methodDescriptor
	call func(context.Context, *Arg) (*Res, error)
}

// NewUnaryMethod creates a new unary extension method definition.
func NewUnaryServerMethod[Arg, Res any](
	name string,
	call func(context.Context, *Arg) (*Res, error),
	bindings ...Binding,
) ServerMethod {
	bmap := map[a2a.TransportProtocol]Binding{}
	for _, b := range bindings {
		bmap[b.Protocol()] = b
	}
	return &unaryServerMethod[Arg, Res]{
		methodDescriptor: methodDescriptor{name: name, bindings: bmap, streaming: false},
		call:             call,
	}
}

func (m *unaryServerMethod[Arg, Res]) InvokeUnary(ctx context.Context, arg any) (any, error) {
	return m.call(ctx, arg.(*Arg))
}

func (m *unaryServerMethod[Arg, Res]) InvokeStreaming(ctx context.Context, arg any) iter.Seq2[any, error] {
	return func(yield func(any, error) bool) {
		yield(nil, errors.New("unary method invoked as streaming"))
	}
}
