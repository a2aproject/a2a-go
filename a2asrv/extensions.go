package a2asrv

import (
	"context"
	"slices"

	"github.com/a2aproject/a2a-go/a2a"
)

const ExtensionsMetaKey = "X-A2A-Extensions"

// Extensions provides utility methods for accessing extensions requested by the client and keeping track of extensions
// activated during request processing.
type Extensions struct {
	callCtx *CallContext
}

// ExtensionsFrom is a helper function for quick access to Extensions in the current CallContext.
func ExtensionsFrom(ctx context.Context) (*Extensions, bool) {
	serverCallCtx, ok := CallContextFrom(ctx)
	if !ok {
		return nil, false
	}
	return serverCallCtx.Extensions(), true
}

// Active returns true if an extension has already been activated in the current CallContext using ExtensionContext.Activate.
func (e *Extensions) Active(extension *a2a.AgentExtension) bool {
	return slices.Contains(e.callCtx.activatedExtensions, extension.URI)
}

// Activate marks extension as activated in the current CallContext. A list of activated extensions might be attached as
// response metadata by a transport implementation.
func (e *Extensions) Activate(extension *a2a.AgentExtension) {
	if e.Active(extension) {
		return
	}
	e.callCtx.activatedExtensions = append(e.callCtx.activatedExtensions, extension.URI)
}

// ActivatedURIs returns all URIs activated during call execution.
func (e *Extensions) ActivatedURIs() []string {
	return slices.Clone(e.callCtx.activatedExtensions)
}

// Requested returns true if the provided extension was requested by the client.
func (e *Extensions) Requested(extension *a2a.AgentExtension) bool {
	return slices.Contains(e.RequestedURIs(), extension.URI)
}

// RequestedURIs returns all URIs of extensions requested by the client.
func (e *Extensions) RequestedURIs() []string {
	requested, ok := e.callCtx.RequestMeta().Get(ExtensionsMetaKey)
	if !ok {
		return []string{}
	}
	return slices.Clone(requested)
}
