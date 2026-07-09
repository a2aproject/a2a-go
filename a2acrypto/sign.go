package a2acrypto

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

// Sign computes a JWS signature for the given AgentCard.
// It serializes the card to JSON, builds the protected header, and signs.
func (s *Signer) Sign(card *a2a.AgentCard) (*a2a.AgentCardSignature, error) {
	payload, err := json.Marshal(card)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal agent card: %w", err)
	}

	protected := map[string]any{
		"alg": s.algorithm,
		"kid": s.kid,
	}
	if s.jwksURL != "" {
		protected["jku"] = s.jwksURL
	}

	protectedJSON, err := json.Marshal(protected)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal protected header: %w", err)
	}

	protectedB64 := base64.RawURLEncoding.EncodeToString(protectedJSON)
	payloadB64 := base64.RawURLEncoding.EncodeToString(payload)
	signingInput := protectedB64 + "." + payloadB64

	hash, err := algToHash(s.algorithm)
	if err != nil {
		return nil, err
	}

	var signature []byte
	if hash == 0 {
		signature, err = s.key.Sign(rand.Reader, []byte(signingInput), crypto.Hash(0))
	} else {
		h := hash.New()
		h.Write([]byte(signingInput))
		signature, err = s.key.Sign(rand.Reader, h.Sum(nil), hash)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to sign: %w", err)
	}

	// Convert ECDSA DER output to raw R||S for JWS compatibility.
	if _, ok := s.key.Public().(*ecdsa.PublicKey); ok {
		signature, err = marshalECDSASignature(signature, s.key.Public().(*ecdsa.PublicKey).Curve)
		if err != nil {
			return nil, fmt.Errorf("failed to convert ECDSA signature: %w", err)
		}
	}

	return &a2a.AgentCardSignature{
		Protected: protectedB64,
		Signature: base64.RawURLEncoding.EncodeToString(signature),
	}, nil
}
