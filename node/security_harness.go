package node

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"os"
	"strings"
	"time"
)

// 👑 SYSTEM SCALING INFRASTRUCTURE: AUTHENTICATION SECRETS
// Used to cleanly discard raw packet flooding before unmarshaling JSON payloads.
func getNetworkAuthSecret() []byte {
	secret := os.Getenv("CVN_NETWORK_AUTH_TOKEN")
	if secret == "" {
		// Secure default baseline token for standard P2P mesh network communication
		return []byte(Params.AuthKey)
	}
	return []byte(secret)
}

// ComputePayloadHMAC signs an outbound packet string natively using SHA-256
func ComputePayloadHMAC(payload []byte) []byte {
	h := hmac.New(sha256.New, getNetworkAuthSecret())
	h.Write(payload)
	return h.Sum(nil)
}

// VerifyPayloadHMAC protects the JSON unmarshaling pipeline from unauthenticated flooding
func VerifyPayloadHMAC(payload []byte, receivedMac []byte) bool {
	expectedMac := ComputePayloadHMAC(payload)
	// Use constant-time comparison to prevent timing attacks
	return subtle.ConstantTimeCompare(receivedMac, expectedMac) == 1
}

func VerifyIncomingWireBlock(b Block) bool {
	if IsHeartbeatHeight(b.Index) {
		// Cheap pre-check only: the hash must carry the work the block claims.
		// Whether the claimed difficulty is the right one needs the parent and
		// is decided in ValidateBlockSignatures.
		if powAlgorithmAt(b.Index) == PowRandomX {
			// A RandomX proof is too costly to check for every block that
			// arrives. Here only the cheap part is checked; the proof itself
			// is verified in ValidateBlockSignatures, after the block has
			// been shown to attach and to claim the right difficulty.
			return b.Hash == CalculateHash(b) && int64(b.Difficulty) >= minDifficultyAt(b.Index)
		}
		if !HashMeetsDifficulty(b.Hash, b.Index, b.Difficulty) {
			fmt.Printf("🚨 WIRE BLOCK FAULT: Hash signature %s missing difficulty constraints.\n", b.Hash)
			return false
		}
		return true
	}

	var expectedDifficulty int
	if b.Index <= 10 {
		expectedDifficulty = 2
	} else {
		if b.Difficulty < 4 {
			return false
		}

		// Pull parent difficulty directly from disk to prevent cross-continental drift
		parentBlock := GetBlockByHeightFromDB(b.Index - 1)
		if parentBlock.Hash == "" {
			parentBlock = Block{
				Index:      b.Index - 1,
				Difficulty: b.Difficulty,
			}
		}
		expectedDifficulty = CalculateNextDifficulty(parentBlock, b.Timestamp)
	}

	if b.Difficulty != expectedDifficulty {
		fmt.Printf("🚨 REJECTION: Incoming block #%d uses invalid difficulty %d (Expected exactly: %d)\n", b.Index, b.Difficulty, expectedDifficulty)
		return false
	}

	targetPrefix := strings.Repeat("0", expectedDifficulty)
	if !strings.HasPrefix(b.Hash, targetPrefix) {
		fmt.Printf("🚨 WIRE BLOCK FAULT: Hash signature %s missing difficulty constraints.\n", b.Hash)
		return false
	}
	return true
}

// peerBanDuration is how long a peer that sent a block with invalid work is
// refused.
const peerBanDuration = 10 * time.Minute

// blockCarriesBadWork reports whether a block that claims to extend this
// node's own tip is certainly invalid in a way no honest node could relay:
// its identifier is not its own, it claims the wrong difficulty, or its
// proof of work does not meet that difficulty. All three follow from the
// block and its parent alone, never from this node's clock, and honest nodes
// only pass on blocks they have checked and stored. A block that does not
// attach to our tip cannot be judged here and is never held against a peer.
func blockCarriesBadWork(b Block) bool {
	tip := GetLatestBlockInternal()
	if tip.Hash == "" || b.Index != tip.Index+1 || b.PrevHash != tip.Hash || !IsHeartbeatHeight(b.Index) {
		return false
	}
	expected := CalculateNextDifficulty(tip, b.Timestamp)
	return b.Difficulty != expected || b.Hash != CalculateHash(b) || !BlockProofValid(b, expected)
}

// banPeer refuses a peer's connections for peerBanDuration.
func banPeer(ip, reason string) {
	if ip == "" {
		return
	}
	BlacklistMutex.Lock()
	MaliciousIPBlacklist[ip] = time.Now().Add(peerBanDuration)
	BlacklistMutex.Unlock()
	noteNetworkEvent(fmt.Sprintf("🚫 [PEERS] %s %s. Refusing it for %d minutes.", ip, reason, int(peerBanDuration/time.Minute)))
}

// peerIsBanned reports whether a peer is currently refused.
func peerIsBanned(ip string) bool {
	BlacklistMutex.Lock()
	defer BlacklistMutex.Unlock()
	until, banned := MaliciousIPBlacklist[ip]
	return banned && time.Now().Before(until)
}
