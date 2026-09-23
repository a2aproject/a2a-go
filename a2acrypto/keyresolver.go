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

package a2acrypto

import (
	"context"
	"crypto"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
)

// KeyResolverFunc simplifies creating a [PublicKeyResolver] backed by a set of known keys.
type KeyResolverFunc func(ctx context.Context, kid string) (crypto.PublicKey, error)

// ResolveKey implements [PublicKeyResolver].
func (f KeyResolverFunc) ResolveKey(ctx context.Context, kid, _ string) (crypto.PublicKey, error) {
	key, err := f(ctx, kid)
	if err != nil {
		return nil, fmt.Errorf("no trusted key for kid %q: %w", kid, err)
	}
	return key, nil
}

// JWKSKeyResolver resolves verification keys from a fixed allowlist of JWKS
// URLs. Keys are fetched only from the URLs the verifier lists here, and the
// signer-supplied jku is honored only when it is on that list.
//
// A jku pointing anywhere else is rejected, so a signer can never steer verification
// at key material the verifier did not already choose to trust.
type JWKSKeyResolver struct {
	client    *http.Client
	allowlist []string
}

// NewJWKSKeyResolver returns a resolver that trusts keys served by the given
// JWKS URLs. A nil client uses http.DefaultClient.
func NewJWKSKeyResolver(client *http.Client, trustedJWKSURLs []string) *JWKSKeyResolver {
	if client == nil {
		client = http.DefaultClient
	}
	return &JWKSKeyResolver{
		client:    client,
		allowlist: append([]string(nil), trustedJWKSURLs...),
	}
}

// ResolveKey returns the key enrolled for kid at jku, provided jku is on the
// allowlist and has a matching key.
func (r *JWKSKeyResolver) ResolveKey(ctx context.Context, kid, jku string) (crypto.PublicKey, error) {
	if !slices.Contains(r.allowlist, jku) {
		return nil, fmt.Errorf("jku %q is not on the trusted allowlist", jku)
	}
	jwks, err := r.fetch(ctx, jku)
	if err != nil {
		return nil, fmt.Errorf("failed to refresh JWKS from %q: %w", jku, err)
	}
	key, ok := jwks[kid]
	if !ok {
		return nil, fmt.Errorf("no trusted key for kid %q at %q", kid, jku)
	}
	return key, nil
}

func (r *JWKSKeyResolver) fetch(ctx context.Context, url string) (_ map[string]crypto.PublicKey, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var set struct {
		Keys []jwk `json:"keys"`
	}
	if err := json.Unmarshal(body, &set); err != nil {
		return nil, fmt.Errorf("invalid JWKS: %w", err)
	}

	keys := make(map[string]crypto.PublicKey)
	for _, k := range set.Keys {
		if k.Kid == "" {
			continue
		}
		pub, err := k.publicKey()
		if err != nil {
			continue
		}
		keys[k.Kid] = pub
	}
	return keys, nil
}
