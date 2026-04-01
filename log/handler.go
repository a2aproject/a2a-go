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

package log

import (
	"context"
	"log/slog"
)

// TypeFormatter inspects a value and returns a replacement [slog.Value] when
// it recognises the type. Returning false leaves the original value unchanged.
type TypeFormatter func(val any) (slog.Value, bool)

// handler is an [slog.handler] wrapper that applies a [TypeFormatter] to every
// attribute before forwarding the record to the inner handler. This allows
// domain types (such as A2A Tasks and Events) to be logged with only select
// fields instead of a full serialization dump.
type handler struct {
	inner     slog.Handler
	formatter TypeFormatter
}

// AttachFormatter wraps inner with a [TypeFormatter] that is applied to every
// attribute value of kind [slog.KindAny] (and recursively inside groups).
func AttachFormatter(inner slog.Handler, formatter TypeFormatter) slog.Handler {
	return &handler{inner: inner, formatter: formatter}
}

// Enabled delegates to the inner handler.
func (h *handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

// Handle creates a copy of the record with formatted attributes and forwards it to the inner handler.
func (h *handler) Handle(ctx context.Context, r slog.Record) error {
	nr := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	r.Attrs(func(a slog.Attr) bool {
		nr.AddAttrs(h.formatAttr(a))
		return true
	})
	return h.inner.Handle(ctx, nr)
}

// WithAttrs formats the attrs and delegates to the inner handler.
func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	formatted := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		formatted[i] = h.formatAttr(a)
	}
	return &handler{inner: h.inner.WithAttrs(formatted), formatter: h.formatter}
}

// WithGroup delegates to the inner handler.
func (h *handler) WithGroup(name string) slog.Handler {
	return &handler{inner: h.inner.WithGroup(name), formatter: h.formatter}
}

func (h *handler) formatAttr(a slog.Attr) slog.Attr {
	v := a.Value.Resolve()
	switch v.Kind() {
	case slog.KindAny:
		if newVal, ok := h.formatter(v.Any()); ok {
			return slog.Attr{Key: a.Key, Value: newVal}
		}
	case slog.KindGroup:
		attrs := v.Group()
		formatted := make([]slog.Attr, len(attrs))
		for i, ga := range attrs {
			formatted[i] = h.formatAttr(ga)
		}
		return slog.Attr{Key: a.Key, Value: slog.GroupValue(formatted...)}
	}
	return a
}
