package node

import (
	"strings"
	"testing"
)

// A peer is cut off for a block that claims to extend our tip but carries
// invalid work, and never for a block we simply cannot judge.
func TestPeersAreJudgedOnlyOnWhatIsCertain(t *testing.T) {
	startTestChain(t, "devnet")
	timestamp := Params.GenesisTimestamp + 100
	for i := 0; i < 3; i++ {
		timestamp += 45
		extendTestChain(t, timestamp, "CVN_miner", nil)
	}
	good := mineTestBlock(t, timestamp+45, "CVN_miner", nil, 0)
	if blockCarriesBadWork(good) {
		t.Fatal("a valid next block was counted against its sender")
	}

	// The same block with a nonce that was never mined.
	unworked := good
	for unworked.Nonce = good.Nonce + 1; ; unworked.Nonce++ {
		unworked.Hash = CalculateHash(unworked)
		if !BlockProofValid(unworked, unworked.Difficulty) {
			break
		}
	}
	if !blockCarriesBadWork(unworked) {
		t.Error("a block with no valid proof of work was not noticed")
	}
	wrongDifficulty := good
	wrongDifficulty.Difficulty = good.Difficulty / 2
	wrongDifficulty.Hash = CalculateHash(wrongDifficulty)
	if !blockCarriesBadWork(wrongDifficulty) {
		t.Error("a block claiming an easier difficulty than the rules give was not noticed")
	}
	falseIdentifier := good
	falseIdentifier.Hash = strings.Repeat("0", 64)
	if !blockCarriesBadWork(falseIdentifier) {
		t.Error("a block whose identifier is not its own was not noticed")
	}

	// Blocks from another branch, or further ahead, prove nothing against a
	// peer: we may be the one that is behind.
	elsewhere := unworked
	elsewhere.PrevHash = strings.Repeat("ab", 32)
	ahead := unworked
	ahead.Index += 5
	for name, block := range map[string]Block{"from another branch": elsewhere, "further ahead": ahead} {
		if blockCarriesBadWork(block) {
			t.Errorf("a block %s was held against its sender", name)
		}
	}

	if peerIsBanned("203.0.113.7") {
		t.Fatal("a peer was refused before it did anything")
	}
	banPeer("203.0.113.7", "sent a block with invalid proof of work")
	if !peerIsBanned("203.0.113.7") || peerIsBanned("203.0.113.8") {
		t.Error("refusing one peer should refuse that peer and no other")
	}
	BlacklistMutex.Lock()
	delete(MaliciousIPBlacklist, "203.0.113.7")
	BlacklistMutex.Unlock()
}

// The engine that checks blocks under the next daily key is prepared before
// the first block that needs it, and is the one then used.
func TestNextKeyIsPreparedAhead(t *testing.T) {
	timestamp := startRandomXChain(t, []PowEra{{FromHeight: 1, Algorithm: PowRandomX, MinDifficulty: 4}})
	rxVerifier.Lock()
	for _, engine := range rxVerifier.engines {
		engine.close()
	}
	rxVerifier.engines = nil
	rxVerifier.Unlock()

	// With a key every 4 blocks, 2 blocks behind, block 7 is the first under
	// the key taken from block 4.
	for height := int64(1); height <= 4; height++ {
		*timestamp += 45
		extendTestChain(t, *timestamp, "CVN_miner", nil)
	}
	if warmNextVerifyKey() {
		t.Error("an engine was prepared while the key for the coming blocks is still the one in use")
	}
	*timestamp += 45
	extendTestChain(t, *timestamp, "CVN_miner", nil) // block 5
	nextKey, _ := powKeyFor(7)
	currentKey, _ := powKeyFor(6)
	if string(nextKey) == string(currentKey) {
		t.Fatal("the test expects the key to change at block 7")
	}
	if !warmNextVerifyKey() {
		t.Fatal("the engine for the next key was not prepared two blocks before it is needed")
	}
	if warmNextVerifyKey() {
		t.Error("the same engine was prepared twice")
	}
	find := func(key []byte) *rxLight {
		rxVerifier.Lock()
		defer rxVerifier.Unlock()
		for _, engine := range rxVerifier.engines {
			if engine.key == string(key) {
				return engine
			}
		}
		return nil
	}
	prepared := find(nextKey)
	if prepared == nil || find(currentKey) == nil {
		t.Fatal("after preparing, both the key in use and the next key should have an engine")
	}
	for height := int64(6); height <= 8; height++ {
		*timestamp += 45
		extendTestChain(t, *timestamp, "CVN_miner", nil)
	}
	if find(nextKey) != prepared {
		t.Error("the engine prepared ahead was not the one used when the key changed")
	}
}
