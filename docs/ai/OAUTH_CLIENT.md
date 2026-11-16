# OAuth client (`a2aclient/oauth`)

> **Staleness warning**: if anything described here contradicts observed behavior, re-read
> the source code. This document may be out of date.

`oauth.Manager` gets, refreshes and stores OAuth grants for an A2A client. It hooks into calls as an
`a2aclient.Plugin` (`plugin.go`), which wraps the client's interceptors and can repeat a call. It
learns of an agent's 401 from the error the client transport returned: `a2a.ErrUnauthenticated`,
with the response's `WWW-Authenticate` in `a2a.Error.ServiceParams`, which the JSON-RPC, REST and
gRPC client transports fill.

## Call flow (`plugin.WrapCall`)

```
Client call
  -> plugin.WrapCall                      caller Authorization set? pass through untouched
       prepare: card requirement (cached) or covering grant
         required + no grant -> SignInRequiredError, nothing sent
       attempt 1 with the grant on a copy of ServiceParams
         -> interceptors -> transport
       first result an error?
         grant attached: Bearer insufficient_scope (403, or 401) -> SignInRequiredError (wider scopes)
                         401 + Bearer, no error or invalid_token -> refreshRejected -> attempt 2
                         anything else, 401 without a Bearer challenge included -> unchanged
         no grant:       401 + Bearer -> requirementForChallenge(BaseURL)
                         -> stored grant? attempt 2 : SignInRequiredError
                         anything else -> unchanged
       attempt 2 refused with a Bearer challenge -> SignInRequiredError (no third attempt)
```

- `attempt` returns the first result instead of yielding it only when it is an error before any
  value, so a stream is repeated only when it failed before its first event; later errors pass
  through.
- `withGrant` copies `ServiceParams` (the `CallHandler` contract: a plugin may call `next` again
  with the original request).
- A refresh that fails for a reason other than `invalid_grant` returns a `FailureError` joining the
  refresh error and the agent's, so `errors.Is(err, a2a.ErrUnauthenticated)` still holds.
- The plugin sees only what it attached. An interceptor that replaces the token (e.g.
  `AuthInterceptor`) makes a 401 for its credential look like a rejected grant: one refresh, then
  `SignInRequiredError`.
- A 401 without a Bearer challenge is never followed by the plugin: probing well-known locations on
  every such call would cost five requests a call. `Discover(url, nil)` still probes them. Callers that want their credential to win put it in the context
  (`AttachServiceParams`) or in an earlier plugin.
- The plugin never sees HTTP: redirects are handled by the transport's `http.Client`, which keeps
  `Authorization` on same-host redirects. `checkTokenDestination` refuses to attach a token to a
  non-https, non-loopback `BaseURL` (gRPC targets excepted), and attached grants cover `BaseURL` by
  construction (`resolveGrant`).
- gRPC: tokens are attached; a rejected token is refreshed when the server's metadata carries a
  Bearer challenge. Discovery after a 401 needs an https or loopback `BaseURL`, so a gRPC address
  gets none.

## Entry points (manager.go, signin.go)

- `Discover(target, card)`: with a card, `requirementForCard` (cached); without, one unauthenticated
  GET to `target` (no redirects) and `requirementForChallenge` on a 401 with a Bearer challenge or
  none. Replaces `RequirementForCard/Challenge/Error/URL` of earlier designs; a protected agent card
  is a `Discover(cardURL, nil)`.
- `Token(target)`: `resolveGrant` over `Store.List` for the longest covering resource, refreshed by
  `grant`. `ErrGrantNotFound` when none; `SignInRequiredError` when the grant can't be refreshed.
- `SignIn`: device flow when only `ShowCode` is set, or both are and the requirement prefers it or
  the server has no authorization endpoint; otherwise auth code with a loopback listener on
  `127.0.0.1:0` at `RedirectPath`. The handler ignores requests whose state or iss don't match and
  ends on success or an OAuth `Error`. `BeginSignIn`/`CompleteSignIn` are the same steps for apps
  that receive the redirect; `PendingSignIn` keeps the unexported `authorizationRequest` and must
  stay in the process.
- `Revoke(g)`: reads the stored grant by `g.ID()` for its tokens, revokes at the server, then
  deletes under the lock even when revocation fails, returning the revocation error.

## Discovery limits and caches

- `fetcher.forAgent` refuses loopback URLs (metadata, redirects, authorization servers) unless the
  agent is on loopback (`isLoopbackURL`: loopback and unspecified addresses, `localhost` with a
  trailing dot or as a parent domain, and numeric hosts in shorthand forms such as `127.1`), and
  with them authorization server endpoints on loopback (`checkEndpoints`), so a remote agent can't
  aim token or device POSTs at local ports. At most `maxAuthorizationServers` (5) servers from PRM
  are tried, and the metadata of at most `maxCardMetadataDocuments` (5) card schemes is fetched.
- A PRM document a challenge names (`resource_metadata`) must describe the request URL itself or a
  resource within `metadataScope` of its location (the path before `/.well-known/`, else its
  directory), so an agent on a shared host can't claim the whole origin. Well-known probes are
  host-controlled and accept any covering resource.
- An inline card flow has no issuer. `requirementFromCard` resolves it per option through
  `resolveInlineIssuer`, so an unresolvable inline option is skipped like other broken ones; a
  `NoClientError` is returned only when no option works. `resolveInlineIssuer` adopts the metadata
  of the first registration whose `token_endpoint` is the card's. A card-only authorization or
  device endpoint is kept only on the origin of the server's issuer or token endpoint, under the
  issuer's path or the token endpoint's parent path (`serverEndpoint`), so a card can't aim a
  trusted server's flow, and the user's client ID, at another tenant or its own host. A
  registration whose authority is an origin and whose server publishes no metadata
  (`errNoServerMetadata`, as for GitHub) is used when every card endpoint is on that origin
  (`bareInline`). With `DefaultClientID` set and no match, it tries issuers at the token URL with
  path segments removed (`inlineIssuerCandidates`) and adopts one only if its metadata names the
  same token endpoint and supports CIMD.
- Server metadata counts as not published (`notPublished`) only for 404, 410, or a response that
  declares a non-JSON content type. Other statuses and malformed JSON are failures.
- The device flow checks `verification_uri` and `verification_uri_complete` with `checkSecureURL`.
- Metadata that lists `code_challenge_methods_supported` without `S256` loses its authorization
  endpoint (`usableEndpoints`), leaving the device flow, or is refused when it has none.
- `requirementForCard` caches per (card pointer, base URL) and resolved inline flows per declared
  `Endpoints`, both bounded (`boundedCache`); failures are cached for `failureCacheTTL`, except
  those of a call whose ctx was done. A card that also accepts other credentials requires nothing
  when all its OAuth options fail; that answer comes back as `skippedOptionsError` and is cached
  like a failure.

## Grants and resolution

`Grant` carries its identity (`Issuer`, `ClientID`, `Resource`, requested `Scopes`); `Grant.ID()`
hashes it normalized (`grantKey.id`) and is the `Store` key. Its format must not change: CLI grant
files and keychain entry names use it. `Grant.Covers` is `resourceCovers`: same origin, path prefix
on a `/` boundary, no dot segments.

`resolveGrant` is the only resolver (`lookup`, `grantForResource`, `Token`). Among the candidates
whose resource covers the target it picks the longest resource; several with the same resource are
ambiguous unless they share issuer and client and one's requested scopes include all the others'
(`widest`, the grant from a step-up). `lookup` first tries an exact `Store.Get` and uses that grant
whatever scopes the server listed (Entra leaves `offline_access` out). Otherwise it filters
`Store.List` by issuer, client and effective scopes (`Grant.scopes()`: granted if known, else
requested), or for the same resource its requested scopes. `save` deletes the grants for the same
issuer, client and resource whose requested scopes the new grant's include (`pruneNarrower`).
Refresh requests send no `scope`. `List` may omit tokens, so callers that need tokens `Get` by ID.
`Manager.get` treats a store's `(nil, nil)` from `Get` as not found.

## Locking and refresh

- `Manager.lock`: the in-process semaphore first, then `Manager.Locker` if set; a Store that
  implements `Locker` is not used implicitly. The wait ends with the caller's ctx or after
  `lockTimeout` (50 s), longer than a holder may keep the lock (`detachedTimeout` 30 s plus
  `storeTimeout` 10 s). The ctx given to `Locker.Lock` is canceled once `lock` returns.
- `refresh` re-reads the grant under the lock, so concurrent callers spend a rotating refresh token
  once, and starts no refresh for a caller that gave up. Work under the lock runs on
  `context.WithoutCancel(ctx)` with `detachedTimeout`; the `Put` of a refreshed grant gets its own
  `storeTimeout`. `save` after sign-in also waits for the lock detached.
- `invalid_grant` on refresh deletes the grant. A rejected token without a refresh token deletes
  the grant too (`dropRejected`): kept without a token, it would shadow a usable parent grant.
- `Revoke` waits for the lock detached from the caller's ctx, so a revocation that timed out
  still removes the grant.

## Tests

`a2aclient/oauth/oauthtest` provides an authorization server (`AuthServer`, with rotation and
invalidation switches) and a protected echo agent (`ProtectedAgent`: card variants, `BasePath`,
`ProtectCard`, `JSONRPC`). Plugin tests drive `WrapCall` with a fake `next` (`runPlugin`) and build
transport errors with `unauthenticated(challenge)`; end-to-end tests use `a2aclient.NewFromCard`
with `WithPlugins(m.Plugin())`. `Manager.sleep` skips device-flow polling waits in tests.
