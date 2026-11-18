# a2aotel

OpenTelemetry instrumentation for the [A2A Go SDK](../../../README.md). It follows the
[OpenTelemetry semantic conventions for the Agent2Agent protocol](https://github.com/open-telemetry/semantic-conventions-genai/blob/main/docs/gen-ai/a2a.md).

```sh
go get github.com/a2aproject/a2a-go/v2/integrations/otel/a2aotel
```

## Usage

```go
// Server: reports a span for every call handled by the request handler.
handler := a2asrv.NewHandler(executor, a2asrv.WithPlugins(
	a2aotel.NewServerPlugin(a2aotel.WithAgentCard(card)),
))

// Client: reports a span for every call and propagates the trace context to the server.
client, err := a2aclient.NewFromCard(ctx, card, a2aclient.WithPlugins(a2aotel.NewClientPlugin()))
```

The plugins use the global `TracerProvider` and `TextMapPropagator` by default. To use others, pass
`WithTracerProvider` and `WithPropagators`.

## Telemetry

| Span | Kind | Name |
| --- | --- | --- |
| `a2a.client` | `CLIENT` | `{a2a.method.name}` |
| `a2a.server` | `SERVER` | `{a2a.method.name}` |

When the server handles a call inside an otelhttp or otelgrpc server span, the plugin adds the A2A
attributes to that span instead of creating a nested one. It renames HTTP spans to the A2A method name.

Attributes: `a2a.method.name`, `a2a.protocol.version`, `a2a.tenant`, `a2a.message.id`,
`a2a.message.reference_task_ids`, `a2a.operation.return_immediately`, `a2a.task.id`, `a2a.task.state`,
`gen_ai.conversation.id`, `gen_ai.agent.{name,description,version}`, `error.type`, and
`server.{address,port}` on client spans.

## Configuration

| Environment variable | Default | Description |
| --- | --- | --- |
| `OTEL_INSTRUMENTATION_A2A_SDK_ENABLED` | `true` | Set to `false` to disable the plugins. Disabled plugins pass calls through without telemetry. |
