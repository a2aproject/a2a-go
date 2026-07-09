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
