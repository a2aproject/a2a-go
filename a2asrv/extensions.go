package a2asrv

import (
	"context"
	"github.com/a2aproject/a2a-go/a2a"
	"slices"
)

// ExtensionContext provides utility methods for accessing extensions requested by the client and keeping track of extensions
// activated during request processing.
type ExtensionContext struct {
	callCtx *CallContext
}

// ExtensionContextFrom is a helper function for quick access to Extensions in the current CallContext.
func ExtensionContextFrom(ctx context.Context) (*ExtensionContext, bool) {
	serverCallCtx, ok := CallContextFrom(ctx)
	if !ok {
		return nil, false
	}
	return serverCallCtx.Extensions(), true
}

// Active returns true if an extension has already been activated in the current CallContext using ExtensionContext.Activate.
func (e *ExtensionContext) Active(extension *a2a.AgentExtension) bool {
	return slices.Contains(e.callCtx.activatedExtensions, extension.URI)
}

// Activate marks extension as activated in the current CallContext. A list of activated extensions might be attached as
// response metadata by a transport implementation.
func (e *ExtensionContext) Activate(extension *a2a.AgentExtension) {
	if e.Active(extension) {
		return
	}
	e.callCtx.activatedExtensions = append(e.callCtx.activatedExtensions, extension.URI)
}

// Requested returns true if the provided extension was requested by the client.
func (e *ExtensionContext) Requested(extension *a2a.AgentExtension) bool {
	return slices.Contains(e.RequestedURIs(), extension.URI)
}

// RequestedURIs returns all URIs of extensions requested by the client.
func (e *ExtensionContext) RequestedURIs() []string {
	requested, ok := e.callCtx.RequestMeta().Get("x-a2a-extensions")
	if !ok {
		return []string{}
	}
	return slices.Clone(requested)
}
