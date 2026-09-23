// Copyright 2025 The A2A Authors
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
	"encoding/json"
	"fmt"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2acrypto"
)

// NewSignedCardProducer wraps an AgentCardProducer to automatically sign every
// produced AgentCard before returning it.
//
// The signer resolves its signing keys on every call, so keys can be rotated
// without restarting the server: a card is signed once per currently-resolved
// key, and each signature is added to the produced card. The card returned by
// the wrapped producer is left unmodified.
func NewSignedCardProducer(signer *a2acrypto.Signer, wrapped AgentCardProducer) AgentCardProducer {
	return &signedCardProducer{signer: signer, wrapped: wrapped}
}

type signedCardProducer struct {
	signer  *a2acrypto.Signer
	wrapped AgentCardProducer
}

// Card implements [AgentCardProducer].
func (p *signedCardProducer) Card(ctx context.Context) (*a2a.AgentCard, error) {
	card, err := p.wrapped.Card(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(card)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal agent card: %w", err)
	}
	sigs, err := p.signer.Sign(ctx, raw)
	if err != nil {
		return nil, fmt.Errorf("failed to sign agent card: %w", err)
	}
	cardCopy := *card
	cardCopy.Signatures = append([]a2a.AgentCardSignature(nil), card.Signatures...)
	for _, sig := range sigs {
		cardCopy.Signatures = append(cardCopy.Signatures, *sig)
	}
	return &cardCopy, nil
}
