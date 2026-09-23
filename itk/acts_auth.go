// ACTS authentication support.
//
// Five ACTS tests assert that an agent requiring a credential rejects a request
// that lacks one. A2A conditions that obligation on the agent's own declared
// requirements, so those tests gate on a card declaring securitySchemes and
// securityRequirements — and an agent declaring neither is not violating
// anything by serving an unauthenticated request. Every ITK agent declares
// neither by default, because traversal peers dial it with no credential.
//
// So enforcement is opt-in, and the ACTS runner turns it on for a separate pass
// over just those tests. It cannot be on for the main pass: the runner sends
// raw steps exactly as written, so an absent Authorization header means "reject
// me" in SEC-AUTH-001 and "serve me" in JSONRPC-ENV-001, and no server can tell
// those two requests apart.
//
// The extended card is the exception, guarded whatever the mode — A2A §13.3
// makes its authentication unconditional, and no traversal scenario fetches
// one.

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Credentials the ACTS runner presents. Not secrets: the runner attaches the
// valid one to every abstract operation and offers the insufficient one from
// SEC-AUTH-002 and SEC-EXTCARD-002, so a fixture has to recognise both to
// answer 200 / 403 / 401 as those tests require.
const (
	actsValidToken        = "itk-valid-token"
	actsInsufficientToken = "itk-insufficient-token"
	actsSecuritySchemeID  = "bearerAuth"
)

func actsAuthEnforced() bool {
	return os.Getenv("ITK_ACTS_AUTH") != ""
}

// actsSecuritySchemes declares a scheme only when the agent actually enforces
// it: a card claiming one it does not check would be a lie, and this is what
// the ACTS authentication precondition reads to decide whether the SEC-AUTH
// tests apply at all.
func actsSecuritySchemes() a2a.NamedSecuritySchemes {
	if !actsAuthEnforced() {
		return nil
	}
	return a2a.NamedSecuritySchemes{
		actsSecuritySchemeID: a2a.HTTPAuthSecurityScheme{
			Description:  "Bearer token presented by the ACTS runner.",
			Scheme:       "Bearer",
			BearerFormat: "opaque",
		},
	}
}

// actsSecurityRequirements is separate from the schemes because the two mean
// different things: schemes are what a client may use, requirements are what it
// must. An agent publishing the first and not the second requires nothing.
func actsSecurityRequirements() a2a.SecurityRequirementsOptions {
	if !actsAuthEnforced() {
		return nil
	}
	return a2a.SecurityRequirementsOptions{
		a2a.SecurityRequirements{actsSecuritySchemeID: a2a.SecuritySchemeScopes{}},
	}
}

func actsPresentedToken(header string) string {
	scheme, value, found := strings.Cut(header, " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return strings.TrimSpace(value)
}

// actsRejection reports how to refuse a credential, or ok when it passes. Three
// outcomes, because the tests distinguish them: the valid token authorizes, the
// insufficient one authenticates but does not, and anything else — including
// nothing at all — fails authentication.
func actsRejection(header string) (ok bool, httpStatus int, rpcCode codes.Code, reason, message string) {
	switch actsPresentedToken(header) {
	case actsValidToken:
		return true, 0, codes.OK, "", ""
	case actsInsufficientToken:
		return false, http.StatusForbidden, codes.PermissionDenied,
			"PERMISSION_DENIED", "Token lacks the required scope."
	default:
		return false, http.StatusUnauthorized, codes.Unauthenticated,
			"UNAUTHENTICATED", "A bearer token is required."
	}
}

// actsStatusBody is the google.rpc.Status shape A2A §11.6 requires of an error.
func actsStatusBody(code int, reason, message string) map[string]any {
	return map[string]any{
		"error": map[string]any{
			"code":    code,
			"status":  reason,
			"message": message,
			"details": []any{map[string]any{
				"@type":  "type.googleapis.com/google.rpc.ErrorInfo",
				"reason": reason,
				"domain": "a2a-protocol.org",
			}},
		},
	}
}

// actsCredentialMiddleware guards the operation endpoints when enforcement is
// on, and the extended card always.
//
// The public agent card stays reachable without a credential in either mode:
// A2A §8.2 makes the well-known URL the discovery mechanism and §7.3 has the
// client learn which schemes it needs from that card, so requiring one to read
// it would be circular — and the ITK readiness probe fetches it too.
func actsCredentialMiddleware(next http.Handler) http.Handler {
	enforced := actsAuthEnforced()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == a2asrv.WellKnownAgentCardPath {
			next.ServeHTTP(w, r)
			return
		}
		// Suffix, not equality: the REST handlers are mounted under prefixes,
		// so the extended card answers on more than one path.
		if !enforced && !strings.HasSuffix(r.URL.Path, actsExtendedCardPath) {
			next.ServeHTTP(w, r)
			return
		}
		ok, httpStatus, _, reason, message := actsRejection(r.Header.Get("Authorization"))
		if ok {
			next.ServeHTTP(w, r)
			return
		}
		if httpStatus == http.StatusUnauthorized {
			w.Header().Set("WWW-Authenticate", `Bearer realm="a2a", scheme="`+actsSecuritySchemeID+`"`)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(httpStatus)
		if err := json.NewEncoder(w).Encode(actsStatusBody(httpStatus, reason, message)); err != nil {
			log.Error(r.Context(), "Failed to write credential rejection", err)
		}
	})
}

// actsGRPCRejection enforces the same rule over gRPC metadata, so that a card
// claiming an agent-wide requirement is not contradicted by one binding that
// serves anyone. gRPC keys are lowercase by protocol.
func actsGRPCRejection(ctx context.Context) error {
	md, _ := metadata.FromIncomingContext(ctx)
	var header string
	if values := md.Get("authorization"); len(values) > 0 {
		header = values[0]
	}
	ok, _, rpcCode, _, message := actsRejection(header)
	if ok {
		return nil
	}
	return status.Error(rpcCode, message)
}

func actsUnaryAuthInterceptor() grpc.UnaryServerInterceptor {
	enforced := actsAuthEnforced()
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if enforced {
			if err := actsGRPCRejection(ctx); err != nil {
				return nil, err
			}
		}
		return handler(ctx, req)
	}
}

func actsStreamAuthInterceptor() grpc.StreamServerInterceptor {
	enforced := actsAuthEnforced()
	return func(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if enforced {
			if err := actsGRPCRejection(ss.Context()); err != nil {
				return err
			}
		}
		return handler(srv, ss)
	}
}
