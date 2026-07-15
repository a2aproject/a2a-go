package a2asrv

import (
	"context"
	"fmt"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2acrypto"
)

// NewSignedCardProducer wraps an AgentCardProducer to automatically sign every
// produced AgentCard before returning it.
func NewSignedCardProducer(signer *a2acrypto.Signer, wrapped AgentCardProducer) AgentCardProducer {
	return &signedCardProducer{signer: signer, wrapped: wrapped}
}

type signedCardProducer struct {
	signer  *a2acrypto.Signer
	wrapped AgentCardProducer
}

func (p *signedCardProducer) Card(ctx context.Context) (*a2a.AgentCard, error) {
	card, err := p.wrapped.Card(ctx)
	if err != nil {
		return nil, err
	}
	sig, err := p.signer.Sign(card)
	if err != nil {
		return nil, fmt.Errorf("failed to sign agent card: %w", err)
	}
	card.Signatures = append(card.Signatures, *sig)
	return card, nil
}
