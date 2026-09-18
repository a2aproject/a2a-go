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
