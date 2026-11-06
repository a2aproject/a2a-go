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

package a2asrv

import (
	"context"
	"errors"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/log"
)

// VerboseErrorsEnv is the environment variable which enables [WithVerboseErrors] for every handler
// created by [NewHandler] when set to a value [strconv.ParseBool] accepts as true.
const VerboseErrorsEnv = "A2A_VERBOSE_ERRORS"

// WithVerboseErrors makes the handler send clients errors as they are, instead of the errors
// [NewErrorSanitizer] would send. Errors are logged either way.
func WithVerboseErrors() RequestHandlerOption {
	return func(ih *InterceptedHandler, h *defaultRequestHandler) {
		ih.verboseErrors = true
	}
}

// NewErrorSanitizer returns a [CallInterceptor] which logs every request error in full and replaces it
// with what clients see:
//   - the [a2a.Error] in the error chain, without the context added while the error was returned;
//   - the error as it is, if it wraps an A2A error other than [a2a.ErrInternalError] and
//     [a2a.ErrServerError], or a context error;
//   - [a2a.ErrInternalError] otherwise.
//
// [NewHandler] attaches it before other interceptors, so it sees the errors other interceptors return
// from Before, but not those they return from After. With [WithVerboseErrors] it only logs.
func NewErrorSanitizer() CallInterceptor {
	return &errorSanitizer{}
}

type errorSanitizer struct {
	PassthroughCallInterceptor
	verbose bool
}

func (s *errorSanitizer) After(ctx context.Context, callCtx *CallContext, resp *Response) error {
	err := resp.Err
	if err == nil {
		return nil
	}
	var a2aErr *a2a.Error
	switch {
	case errors.As(err, &a2aErr):
		log.Info(ctx, "request failed", "method", callCtx.Method(), "error", err)
		if !s.verbose {
			resp.Err = a2aErr
		}
	case isCallerError(err):
		log.Info(ctx, "request failed", "method", callCtx.Method(), "error", err)
	default:
		log.Error(ctx, "request failed", err, "method", callCtx.Method())
		if !s.verbose {
			resp.Err = a2a.ErrInternalError
		}
	}
	return nil
}

func isCallerError(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	reason := a2a.ErrorReason(err)
	return reason != a2a.ErrorReason(a2a.ErrInternalError) && reason != a2a.ErrorReason(a2a.ErrServerError)
}
