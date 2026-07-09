package a2acrypto

import (
	"crypto/elliptic"
	"encoding/asn1"
	"fmt"
	"math/big"
)

// ecdsaSignature is the ASN.1 structure used by Go's crypto/ecdsa.
type ecdsaSignature struct {
	R, S *big.Int
}

// marshalECDSASignature converts DER-encoded ECDSA signature to raw R||S (JWS format).
func marshalECDSASignature(der []byte, curve elliptic.Curve) ([]byte, error) {
	var sig ecdsaSignature
	if _, err := asn1.Unmarshal(der, &sig); err != nil {
		return nil, fmt.Errorf("failed to unmarshal DER signature: %w", err)
	}
	keySize := (curve.Params().BitSize + 7) / 8
	out := make([]byte, 2*keySize)
	sig.R.FillBytes(out[:keySize])
	sig.S.FillBytes(out[keySize:])
	return out, nil
}

// unmarshalECDSASignature converts raw R||S to r, s *big.Int for verification.
func unmarshalECDSASignature(raw []byte, curve elliptic.Curve) (r, s *big.Int, err error) {
	keySize := (curve.Params().BitSize + 7) / 8
	if len(raw) != 2*keySize {
		return nil, nil, fmt.Errorf("raw signature length %d, want %d", len(raw), 2*keySize)
	}
	r = new(big.Int).SetBytes(raw[:keySize])
	s = new(big.Int).SetBytes(raw[keySize:])
	return r, s, nil
}
