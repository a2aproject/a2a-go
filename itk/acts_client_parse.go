// The tck-client-parse behaviour: ACTS §10 client tests.
//
// Every other ACTS step drives the SUT as a server — send bytes, assert on
// what comes back. A client test inverts that: it supplies a canonical wire
// payload and asks whether this SDK's *client* parses it correctly, which no
// A2A operation can ask of a server. §10 defines the file format and says
// nothing about the mechanism, so without a convention like this one the
// runner cannot reach the client at all and skips the CLIENT-* tests.
//
// The runner sends an ordinary send_message naming this behaviour with
// {operation, wire_payload} in a data part; the agent builds a real client
// whose HTTP transport returns that payload verbatim, performs the operation,
// and hands back whatever its own client produced.
//
// A mock transport rather than a bare unmarshal: decoding the payload straight
// into a2a types would be a fraction of the code and would prove much less,
// skipping the JSON-RPC envelope, the error mapping and the response plumbing
// that are most of what a client test is about. CLIENT-PARSE-004 makes that
// concrete — it feeds an error envelope and expects {error: {code, message}}.

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aclient/agentcard"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"google.golang.org/protobuf/encoding/protojson"
)

const actsClientParseBehavior = "tck-client-parse"

// Nothing dials this — the fake transport answers before a socket is opened —
// but the client needs a syntactically valid base.
const actsParseBaseURL = "http://acts-client-parse.invalid"

const actsCardPath = "/.well-known/agent-card.json"
const actsExtendedCardPath = "/extendedAgentCard"

// The SDK maps a JSON-RPC code to a sentinel and drops the integer, and the
// mapping lives in an internal package. CLIENT-PARSE-004 asserts on the code,
// so it has to be re-derived here.
var actsErrorCodes = []struct {
	code int
	err  error
}{
	{-32700, a2a.ErrParseError},
	{-32600, a2a.ErrInvalidRequest},
	{-32601, a2a.ErrMethodNotFound},
	{-32602, a2a.ErrInvalidParams},
	{-32000, a2a.ErrServerError},
	{-32001, a2a.ErrTaskNotFound},
	{-32002, a2a.ErrTaskNotCancelable},
	{-32003, a2a.ErrPushNotificationNotSupported},
	{-32004, a2a.ErrUnsupportedOperation},
	{-32005, a2a.ErrUnsupportedContentType},
	{-32006, a2a.ErrInvalidAgentResponse},
	{-32007, a2a.ErrExtendedCardNotConfigured},
	{-32008, a2a.ErrExtensionSupportRequired},
	{-32009, a2a.ErrVersionNotSupported},
	{-31401, a2a.ErrUnauthenticated},
	{-31403, a2a.ErrUnauthorized},
	{-32603, a2a.ErrInternalError},
}

// actsFixedTransport answers every request with the payload under test.
//
// The status is always 200: the JSON-RPC transport checks the HTTP status
// before it looks at the body, so an error envelope served at 4xx is discarded
// as "unexpected HTTP status" and never parsed.
type actsFixedTransport struct{ payload []byte }

func (t actsFixedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	body := t.payload
	if echoed, err := actsEchoID(t.payload, r); err == nil {
		body = echoed
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(string(body))),
		Request:    r,
	}, nil
}

// actsEchoID rewrites the response's JSON-RPC id to the request's, which is
// what a real server does. The corpus's canned payloads carry a fixed id that
// cannot match one the client invented at call time, so a client validating
// the correlation rejects the payload before parsing any of it — leaving the
// test measuring correlation rather than parsing.
func actsEchoID(payload []byte, r *http.Request) ([]byte, error) {
	var envelope map[string]any
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return nil, err
	}
	if !actsIsEnveloped(envelope) || r.Body == nil {
		return nil, errors.New("not an envelope")
	}
	sent, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	var request map[string]any
	if err := json.Unmarshal(sent, &request); err != nil {
		return nil, err
	}
	id, ok := request["id"]
	if !ok {
		return nil, errors.New("request carried no id")
	}
	envelope["id"] = id
	return json.Marshal(envelope)
}

func actsIsEnveloped(payload map[string]any) bool {
	if payload == nil {
		return false
	}
	for _, key := range []string{"jsonrpc", "result", "error"} {
		if _, ok := payload[key]; ok {
			return true
		}
	}
	return false
}

func actsFixedClient(payload []byte) *http.Client {
	return &http.Client{Transport: actsFixedTransport{payload: payload}}
}

// actsParseClient builds a real client bound to the fake transport.
//
// From endpoints rather than from a card: a loaded card makes the client
// short-circuit on its own capability flags, so GetExtendedAgentCard would
// answer without ever reaching the payload.
func actsParseClient(ctx context.Context, payload []byte) (*a2aclient.Client, error) {
	return a2aclient.NewFromEndpoints(ctx,
		[]*a2a.AgentInterface{{
			URL:             actsParseBaseURL,
			ProtocolBinding: a2a.TransportProtocolJSONRPC,
			ProtocolVersion: a2a.Version,
		}},
		a2aclient.WithJSONRPCTransport(actsFixedClient(payload)),
	)
}

// actsParseCard runs a bare card payload through the SDK's own card handling.
// Both card operations land here when the payload is a bare card, which is how
// the corpus writes them — correctly, since a card is fetched over plain HTTP
// on every binding, so there is no envelope to unwrap.
func actsParseCard(ctx context.Context, payload []byte, path string) (any, error) {
	resolver := &agentcard.Resolver{Client: actsFixedClient(payload)}
	return resolver.Resolve(ctx, actsParseBaseURL, agentcard.WithPath(path))
}

func actsParse(ctx context.Context, operation string, payload []byte) map[string]any {
	parsed, err := actsParseOperation(ctx, operation, payload)
	if err != nil {
		return actsParseError(err, payload)
	}
	// Only send_message keeps its envelope: §4.2 makes the task/message
	// discriminator part of that operation's assertion root. get_task and the
	// card operations are asserted on their own fields.
	return actsWireMap(parsed, operation == "send_message")
}

func actsParseOperation(ctx context.Context, operation string, payload []byte) (any, error) {
	switch operation {
	case "get_agent_card":
		return actsParseCard(ctx, payload, actsCardPath)

	case "get_extended_agent_card":
		// The corpus writes this as a bare card, matching the wire: its own
		// payload names REST, where the extended card is a plain GET. Accept
		// an envelope too, since a JSON-RPC binding does wrap it.
		var envelope map[string]any
		if err := json.Unmarshal(payload, &envelope); err == nil && !actsIsEnveloped(envelope) {
			return actsParseCard(ctx, payload, actsExtendedCardPath)
		}
		client, err := actsParseClient(ctx, payload)
		if err != nil {
			return nil, err
		}
		defer func() { _ = client.Destroy() }()
		return client.GetExtendedAgentCard(ctx, &a2a.GetExtendedAgentCardRequest{})

	case "send_message":
		client, err := actsParseClient(ctx, payload)
		if err != nil {
			return nil, err
		}
		defer func() { _ = client.Destroy() }()
		return client.SendMessage(ctx, &a2a.SendMessageRequest{
			Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("acts")),
		})

	case "get_task":
		client, err := actsParseClient(ctx, payload)
		if err != nil {
			return nil, err
		}
		defer func() { _ = client.Destroy() }()
		return client.GetTask(ctx, &a2a.GetTaskRequest{ID: a2a.TaskID("acts")})
	}
	return nil, fmt.Errorf("unsupported client operation %q", operation)
}

// actsWireMap renders what the client produced as A2A wire JSON, wrapping it
// in the StreamResponse envelope when the operation's assertion root is the
// discriminated event rather than the object itself.
func actsWireMap(parsed any, enveloped bool) map[string]any {
	// A card goes out through the SDK's own protobuf conversion rather than
	// encoding/json: the Go struct tags are `omitempty`, so a capability the
	// client correctly parsed as false vanishes, and CLIENT-CAP-001 asserts on
	// exactly that. ProtoJSON with default values kept is the wire form the
	// assertions are written against.
	if card, ok := parsed.(*a2a.AgentCard); ok {
		if rendered, err := actsCardWire(card); err == nil {
			return rendered
		}
	}

	var encoded []byte
	var err error
	if event, ok := parsed.(a2a.Event); ok && enveloped {
		encoded, err = json.Marshal(a2a.StreamResponse{Event: event})
	} else {
		encoded, err = json.Marshal(parsed)
	}
	if err != nil {
		return map[string]any{"error": map[string]any{"message": err.Error()}}
	}

	var out map[string]any
	if err := json.Unmarshal(encoded, &out); err != nil {
		return map[string]any{"error": map[string]any{"message": err.Error()}}
	}
	return out
}

// actsParseError renders a client-raised error the way expect_parsed addresses
// it. The envelope's own error is preferred when the payload carried one: the
// assertion is about the client having surfaced *that* error, and inventing a
// code here would pass the test without the client having done anything.
func actsParseError(err error, payload []byte) map[string]any {
	var envelope map[string]any
	if jsonErr := json.Unmarshal(payload, &envelope); jsonErr == nil {
		if wire, ok := envelope["error"].(map[string]any); ok {
			return map[string]any{"error": wire, "raised": err.Error()}
		}
	}

	var typed *a2a.Error
	if errors.As(err, &typed) {
		return map[string]any{"error": map[string]any{
			"code":    actsErrorCode(err),
			"message": typed.Message,
		}}
	}
	return map[string]any{"error": map[string]any{"message": err.Error()}}
}

func actsCardWire(card *a2a.AgentCard) (map[string]any, error) {
	proto, err := pbconv.ToProtoAgentCard(card)
	if err != nil {
		return nil, err
	}
	encoded, err := protojson.MarshalOptions{EmitDefaultValues: true}.Marshal(proto)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(encoded, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func actsErrorCode(err error) int {
	for _, mapping := range actsErrorCodes {
		if errors.Is(err, mapping.err) {
			return mapping.code
		}
	}
	return -32603
}

func actsClientParseRequest(msg *a2a.Message) (string, []byte, bool) {
	if msg == nil {
		return "", nil, false
	}
	for _, part := range msg.Parts {
		data, ok := part.Data().(map[string]any)
		if !ok {
			continue
		}
		operation, ok := data["operation"].(string)
		if !ok {
			continue
		}
		payload, err := json.Marshal(data["wire_payload"])
		if err != nil {
			return "", nil, false
		}
		return operation, payload, true
	}
	return "", nil, false
}

func actsClientParse(ctx context.Context, execCtx *a2asrv.ExecutorContext, yield func(a2a.Event, error) bool) {
	operation, payload, ok := actsClientParseRequest(execCtx.Message)
	if !ok {
		yield(actsStatus(execCtx, a2a.TaskStateFailed, actsClientParseBehavior+" needs {operation, wire_payload}"), nil)
		return
	}

	parsed := actsParse(ctx, operation, payload)
	if !yield(actsArtifact(execCtx, actsClientParseBehavior, a2a.NewDataPart(parsed)), nil) {
		return
	}
	yield(actsStatus(execCtx, a2a.TaskStateCompleted, operation+" parsed"), nil)
}
