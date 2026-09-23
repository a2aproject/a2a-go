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
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"fmt"
)

func inferAlgorithm(key crypto.Signer) string {
	switch pub := key.Public().(type) {
	case *ecdsa.PublicKey:
		switch pub.Curve {
		case elliptic.P256():
			return "ES256"
		case elliptic.P384():
			return "ES384"
		case elliptic.P521():
			return "ES512"
		default:
			return ""
		}
	case ed25519.PublicKey:
		return "EdDSA"
	case *rsa.PublicKey:
		return "RS256"
	default:
		return ""
	}
}

func algToHash(alg string) (crypto.Hash, error) {
	switch alg {
	case "ES256", "RS256":
		return crypto.SHA256, nil
	case "ES384", "RS384":
		return crypto.SHA384, nil
	case "ES512", "RS512":
		return crypto.SHA512, nil
	case "EdDSA":
		return crypto.Hash(0), nil
	default:
		return 0, fmt.Errorf("unsupported algorithm: %s", alg)
	}
}
