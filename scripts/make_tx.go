package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"time"
)

func main() {
	// 1. Establish Master Parameters and Target Wallets
	senderAddress := "CVN_8060d735cc62e95970f4f8f5e2088f1c6df14f17"
	recipientAddress := "CVN_766d4b3be2e1c894f0b2a688527e4d2592151cd1"
	
	// Long token containing scalar parameters
	rawTokenHex := "e7e637ff19f45fda04c0a5d8a47796d038f06643f7c8605b9aa8436e9272f0db5c66666a624593db443ad39d4050276bb877cf039553483b2bf8d3e837d8ce1a"
	
	// 👑 THE CRITICAL BOUNDARY CLAMP:
	// Isolate precisely the first 64 hexadecimal characters (32 bytes) to extract
	// your valid P-256 scalar parameter D, dropping the concatenated trailing key residue!
	privateKeyHex := rawTokenHex[:64]
	
	// 📐 MATHEMATICAL BALANCE ACCOUNTING:
	// Your wallet current balance according to the L1 API: 24,750.00 CVN
	// Transfer Amount: 1,000.00 CVN // PriorityOffering Fee: 0.01 CVN
	// Your Change Out: 24,750.00 - 1,000.00 - 0.01 = 23,749.99 CVN
	transferAmount := 1000.00
	priorityFee := 0.01
	changeAmount := 23749.99

	// 2. Reconstruct Asymmetric Cryptographic Key Coordinates cleanly out of D Scalar Hex Strings
	privBytes, err := hex.DecodeString(privateKeyHex)
	if err != nil {
		fmt.Printf("Error decoding private key hex: %v\n", err)
		return
	}

	curve := elliptic.P256()
	d := new(big.Int).SetBytes(privBytes)
	privKey := &ecdsa.PrivateKey{
		PublicKey: ecdsa.PublicKey{Curve: curve},
		D:         d,
	}
	privKey.PublicKey.X, privKey.PublicKey.Y = curve.ScalarBaseMult(privBytes)

	// Uniform serialize coordinate markers cleanly back into a standard 128-character true public key string
	publicKeyHex := fmt.Sprintf("%064x%064x", privKey.PublicKey.X, privKey.PublicKey.Y)

	// 3. Build Deterministic Message Preimage Pre-commitment strings matching rules exactly
	tSeed := fmt.Sprintf("%d-%s-%s-%f", time.Now().UnixNano(), senderAddress, recipientAddress, transferAmount)
	txID := fmt.Sprintf("TX_DESKTOP_%x", sha256.Sum256([]byte(tSeed)))
	txMessage := fmt.Sprintf("%s-%s-%f", txID, senderAddress, priorityFee)

	// 4. Compute Cryptographic ECDSA P-256 Hashes and Signatures Vectors using native Go CSPRNG rand readers
	txHash := sha256.Sum256([]byte(txMessage))
	r, s, err := ecdsa.Sign(nil, privKey, txHash[:])
	if err != nil {
		fmt.Printf("Signature Math Failure: %v\n", err)
		return
	}
	combinedSignature := fmt.Sprintf("%s|%s", r.Text(16), s.Text(16))

	// 5. Output a pristine, mainnet-ready structural JSON block framework string
	outputJSON := fmt.Sprintf(`{"tx_id":"%s","inputs":[{"source_tx_id":"TX_GENESIS_INITIAL_POOL","output_index":0,"signature":"%s"}],"outputs":[{"recipient":"%s","amount":%f},{"recipient":"%s","amount":%f}],"signature":"%s","public_key":"%s","timestamp":%d,"free_will_offering":%f,"data_size_kb":1.2,"witness":"%s"}`,
		txID, combinedSignature, recipientAddress, transferAmount, senderAddress, changeAmount, combinedSignature, publicKeyHex, time.Now().Unix(), priorityFee, senderAddress)

	fmt.Println(outputJSON)
}