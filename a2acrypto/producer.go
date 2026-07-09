package a2acrypto

import (
	"context"
	"fmt"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

// NewSignedCardProducer wraps an AgentCardProducer to automatically sign every
// produced AgentCard before returning it.
func NewSignedCardProducer(signer *Signer, wrapped a2asrv.AgentCardProducer) a2asrv.AgentCardProducer {
	return &signedCardProducer{signer: signer, wrapped: wrapped}
}

type signedCardProducer struct {
	signer  *Signer
	wrapped a2asrv.AgentCardProducer
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
