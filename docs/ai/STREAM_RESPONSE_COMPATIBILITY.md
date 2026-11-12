# Streaming response compatibility

A v1 streaming client skips payload variants it cannot interpret and keeps
receiving known events through completion. This applies to both
`SendStreamingMessage` and `SubscribeToTask`; v0 transports are unchanged.

- REST first decodes its typed wrapper, including server errors. Only when no
  known payload is set does it inspect non-null JSON field names. Known payload
  decoding failures and multiple known variants remain errors.
- JSON-RPC first handles the RPC envelope and then uses the strict public
  `a2a.StreamResponse.UnmarshalJSON`. Its unknown-event error wraps an internal
  sentinel without changing the error text or accepting unknown events. Only
  the streaming client handles that sentinel by inspecting non-null fields;
  unary calls and server eventqueue decoding stay strict.
- JSON `null` fields are unset: `{"task":null,"futureEvent":{}}` is skipped,
  while `{}`, `null`, and null-only wrappers fail with
  `stream response has no payload` in the receiving clients.
- gRPC delegates classification to `pbconv.FromProtoStreamResponse`.
  `ErrUnsupportedStreamResponse` covers both unknown protobuf field numbers with no
  decoded payload and future generated oneof variants the converter does not
  support. Empty wrappers and conversion errors for supported variants remain
  errors. When no payload was decoded, wrong-wire records for message fields in
  the local descriptor are rejected: protobuf retains known fields with the
  wrong wire type as unknown bytes. This is an application-level check, not a
  protobuf parsing error. Responses with a decoded payload keep their existing
  behavior, including unsupported future generated variants.
  Protobuf oneofs cannot expose multiple simultaneous payloads or JSON
  null values, so the JSON validation cases do not apply to this transport.

Debug diagnostics contain JSON field names or protobuf field numbers and the
payload Go type, never full payloads. A protobuf field absent from the local
schema has no locally available name.

Regression tests must check the known events after skipped payloads and the
specific subsequent error. An assertion accepting the same unknown-event error
before and after the fix does not prove that reception continued.
