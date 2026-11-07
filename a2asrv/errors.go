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

var callerErrors = []error{
	a2a.ErrParseError,
	a2a.ErrInvalidRequest,
	a2a.ErrMethodNotFound,
	a2a.ErrInvalidParams,
	a2a.ErrTaskNotFound,
	a2a.ErrTaskNotCancelable,
	a2a.ErrPushNotificationNotSupported,
	a2a.ErrUnsupportedOperation,
	a2a.ErrUnsupportedContentType,
	a2a.ErrInvalidAgentResponse,
	a2a.ErrExtendedCardNotConfigured,
	a2a.ErrExtensionSupportRequired,
	a2a.ErrVersionNotSupported,
	a2a.ErrUnauthenticated,
	a2a.ErrUnauthorized,
	context.Canceled,
	context.DeadlineExceeded,
}

// NewErrorSanitizer returns a [CallInterceptor] which logs every request error in full and replaces
// it with an error that tells clients what went wrong without the server's internal details:
//   - the [a2a.Error] in the error chain, without the context added while the error was returned;
//   - otherwise the A2A error or context error the chain wraps, other than [a2a.ErrInternalError]
//     and [a2a.ErrServerError], e.g. [a2a.ErrTaskNotFound];
//   - otherwise [a2a.ErrInternalError].
//
// Attach it with [WithCallInterceptors] before other interceptors, so that it also handles the errors
// they return from Before. Errors other interceptors return from After skip it.
func NewErrorSanitizer() CallInterceptor {
	return errorSanitizer{}
}

type errorSanitizer struct {
	PassthroughCallInterceptor
}

func (errorSanitizer) After(ctx context.Context, callCtx *CallContext, resp *Response) error {
	err := resp.Err
	if err == nil {
		return nil
	}
	var a2aErr *a2a.Error
	if errors.As(err, &a2aErr) {
		log.Info(ctx, "request failed", "method", callCtx.Method(), "error", err)
		resp.Err = a2aErr
		return nil
	}
	for _, callerErr := range callerErrors {
		if errors.Is(err, callerErr) {
			log.Info(ctx, "request failed", "method", callCtx.Method(), "error", err)
			resp.Err = callerErr
			return nil
		}
	}
	log.Error(ctx, "request failed", err, "method", callCtx.Method())
	resp.Err = a2a.ErrInternalError
	return nil
}
