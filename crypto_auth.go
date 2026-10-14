package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
)

// GenerateKeyPair creates a cryptographically secure random ECDSA P-256 key pair
// using Go's native CSPRNG engine ("crypto/rand"), completely isolated from system clocks.
func GenerateKeyPair() (string, string, string, error) {
	// 🟩 CRITERIA 1 & 2: Rely exclusively on crypto/rand to enforce true high-entropy CSPRNG paths
	curve := elliptic.P256()
	privateKey, err := ecdsa.GenerateKey(curve, rand.Reader)
	if err != nil {
		return "", "", "", fmt.Errorf("cryptographic entropy path failure: %w", err)
	}

	// Extract private key scalar parameter D securely into a fixed hexadecimal representation
	privBytes := privateKey.D.Bytes()
	privHex := hex.EncodeToString(privBytes)

	// Format public key by serializing coordinate markers cleanly into a uniform 128-character hex string
	pubHex := fmt.Sprintf("%064x%064x", privateKey.PublicKey.X, privateKey.PublicKey.Y)

	// Derive the unique public ledger address wallet handle (CVN_...) using a SHA-256 digest
	addressHash := sha256.Sum256([]byte(pubHex))
	walletAddress := "CVN_" + hex.EncodeToString(addressHash[:20])

	return privHex, pubHex, walletAddress, nil
}

// SignTransactionPayload generates an ECDSA signature string using the sender's private key hex token.
func SignTransactionPayload(privKeyHex string, txData string) (string, string, error) {
	privBytes, err := hex.DecodeString(privKeyHex)
	if err != nil {
		return "", "", fmt.Errorf("failed to decode private key hexadecimal string: %w", err)
	}

	curve := elliptic.P256()
	d := new(big.Int).SetBytes(privBytes)
	
	// Reconstruct the structural ecdsa.PrivateKey parameters securely
	privKey := &ecdsa.PrivateKey{
		PublicKey: ecdsa.PublicKey{
			Curve: curve,
		},
		D: d,
	}
	privKey.PublicKey.X, privKey.PublicKey.Y = curve.ScalarBaseMult(privBytes)

	// Compute message digest hash to generate the cryptographic signature vectors
	txHash := sha256.Sum256([]byte(txData))

	// Generate r and s signature vectors utilizing the cryptographically secure random reader
	r, s, err := ecdsa.Sign(rand.Reader, privKey, txHash[:])
	if err != nil {
		return "", "", fmt.Errorf("failed to compute asymmetric ecdsa mathematical signatures: %w", err)
	}

	return r.Text(16), s.Text(16), nil
}

// VerifyTransactionSignature evaluates an incoming transaction signature using the explicit public key coordinates.
func VerifyTransactionSignature(pubKeyHex string, txData string, rStr string, sStr string) bool {
	// Early validation fence to drop empty parameters or truncated public keys
	if rStr == "" || sStr == "" || len(pubKeyHex) < 128 {
		return false
	}

	// 1. Reconstruct big.Int coordinate parameters from the hexadecimal signature strings
	rSign := new(big.Int)
	sSign := new(big.Int)
	if _, ok := rSign.SetString(rStr, 16); !ok {
		return false
	}
	if _, ok := sSign.SetString(sStr, 16); !ok {
		return false
	}

	// 2. Reconstruct the un-truncated public key coordinate points safely out of the hex data track
	pubKeyX := new(big.Int)
	pubKeyY := new(big.Int)
	
	if _, ok := pubKeyX.SetString(pubKeyHex[:64], 16); !ok {
		return false
	}
	if _, ok := pubKeyY.SetString(pubKeyHex[64:], 16); !ok {
		return false
	}

	curve := elliptic.P256()
	rawPubKey := &ecdsa.PublicKey{
		Curve: curve,
		X:     pubKeyX,
		Y:     pubKeyY,
	}
	
	// 3. Compute message digest hash to execute signature validation
	txHash := sha256.Sum256([]byte(txData))

	return ecdsa.Verify(rawPubKey, txHash[:], rSign, sSign)
}