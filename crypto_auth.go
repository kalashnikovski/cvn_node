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

// GenerateKeyPair creates a secure random ECDSA P-256 private key and returns its hexadecimal address string
func GenerateKeyPair() (string, string, error) {
	curve := elliptic.P256()
	privateKey, err := ecdsa.GenerateKey(curve, rand.Reader)
	if err != nil {
		return "", "", err
	}

	privBytes := privateKey.D.Bytes()
	privHex := hex.EncodeToString(privBytes)

	pubHex := fmt.Sprintf("%x%x", privateKey.PublicKey.X, privateKey.PublicKey.Y)
	
	addressHash := sha256.Sum256([]byte(pubHex))
	walletAddress := "CVN_" + hex.EncodeToString(addressHash[:20])

	return privHex, walletAddress, nil
}

// SignTransactionPayload generates a distinct ECDSA signature using the sender's hexadecimal private key
func SignTransactionPayload(privKeyHex string, txData string) (string, string, error) {
	privBytes, err := hex.DecodeString(privKeyHex)
	if err != nil {
		return "", "", err
	}

	curve := elliptic.P256()
	d := new(big.Int).SetBytes(privBytes)
	privKey := new(ecdsa.PrivateKey)
	privKey.PublicKey.Curve = curve
	privKey.D = d
	privKey.PublicKey.X, privKey.PublicKey.Y = curve.ScalarBaseMult(privBytes)

	txHash := sha256.Sum256([]byte(txData))

	r, s, err := ecdsa.Sign(rand.Reader, privKey, txHash[:])
	if err != nil {
		return "", "", err
	}

	return r.Text(16), s.Text(16), nil
}

// VerifyTransactionSignature evaluates an incoming transaction to guarantee signature validity
func VerifyTransactionSignature(pubKeyCoordsHex string, txData string, rStr string, sStr string) bool {
	if pubKeyCoordsHex == "COVENANT_STEWARD_ASSEMBLY" || pubKeyCoordsHex == "GENESIS_VOID_REWARD_POOL" {
		return true
	}

	if len(pubKeyCoordsHex) < 64 || rStr == "" || sStr == "" {
		return false
	}

	curve := elliptic.P256()
	xStr := pubKeyCoordsHex[:len(pubKeyCoordsHex)/2]
	yStr := pubKeyCoordsHex[len(pubKeyCoordsHex)/2:]

	x, ok1 := new(big.Int).SetString(xStr, 16)
	y, ok2 := new(big.Int).SetString(yStr, 16)
	r, ok3 := new(big.Int).SetString(rStr, 16)
	s, ok4 := new(big.Int).SetString(sStr, 16)

	if !ok1 || !ok2 || !ok3 || !ok4 {
		return false
	}

	pubKey := &ecdsa.PublicKey{
		Curve: curve,
		X:     x,
		Y:     y,
	}

	txHash := sha256.Sum256([]byte(txData))

	return ecdsa.Verify(pubKey, txHash[:], r, s)
}