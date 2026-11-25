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
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"fmt"
)

func inferAlgorithm(key crypto.Signer) (string, error) {
	switch pub := key.Public().(type) {
	case *ecdsa.PublicKey:
		switch pub.Curve {
		case elliptic.P256():
			return "ES256", nil
		case elliptic.P384():
			return "ES384", nil
		case elliptic.P521():
			return "ES512", nil
		}
	case ed25519.PublicKey:
		return "EdDSA", nil
	case *rsa.PublicKey:
		return "RS256", nil
	}
	return "", fmt.Errorf("cannot infer algorithm from key type %T", key.Public())
}

func validateKeyAlgorithm(pub crypto.PublicKey, alg string) (crypto.Hash, error) {
	switch alg {
	case "ES256":
		return validateECDSAKey(pub, alg, elliptic.P256(), crypto.SHA256)
	case "ES384":
		return validateECDSAKey(pub, alg, elliptic.P384(), crypto.SHA384)
	case "ES512":
		return validateECDSAKey(pub, alg, elliptic.P521(), crypto.SHA512)
	case "RS256":
		return validateRSAKey(pub, alg, crypto.SHA256)
	case "RS384":
		return validateRSAKey(pub, alg, crypto.SHA384)
	case "RS512":
		return validateRSAKey(pub, alg, crypto.SHA512)
	case "EdDSA":
		key, ok := pub.(ed25519.PublicKey)
		if !ok || len(key) != ed25519.PublicKeySize {
			return 0, fmt.Errorf("algorithm %s requires an Ed25519 key, got %T", alg, pub)
		}
		return crypto.Hash(0), nil
	default:
		return 0, fmt.Errorf("unsupported algorithm: %s", alg)
	}
}

func validateECDSAKey(pub crypto.PublicKey, alg string, wantCurve elliptic.Curve, hash crypto.Hash) (crypto.Hash, error) {
	key, ok := pub.(*ecdsa.PublicKey)
	if !ok || key == nil {
		return 0, fmt.Errorf("algorithm %s requires an ECDSA %s key, got %T", alg, wantCurve.Params().Name, pub)
	}
	if key.Curve != wantCurve {
		gotCurve := "<nil>"
		if key.Curve != nil {
			gotCurve = key.Curve.Params().Name
		}
		return 0, fmt.Errorf("algorithm %s requires curve %s, got %s", alg, wantCurve.Params().Name, gotCurve)
	}
	return hash, nil
}

func validateRSAKey(pub crypto.PublicKey, alg string, hash crypto.Hash) (crypto.Hash, error) {
	key, ok := pub.(*rsa.PublicKey)
	if !ok || key == nil || key.N == nil {
		return 0, fmt.Errorf("algorithm %s requires an RSA key, got %T", alg, pub)
	}
	return hash, nil
}
