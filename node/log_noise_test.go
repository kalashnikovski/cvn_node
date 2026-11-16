package node

import (
	"strings"
	"testing"

	"go.etcd.io/bbolt"
)

// Seen in the seeds' logs on devnet (16 November 2026): "reward transaction
// ID already used" for blocks that were perfectly good. The same block
// arrives from several peers; one copy is stored, and another, checked a
// moment later, is judged against a coin set that already holds its own
// coins. This shows that mechanism, and that such a copy is recognised as a
// duplicate so that it is not reported as a bad block.
func TestACopyOfAStoredBlockIsNotReportedAsBad(t *testing.T) {
	t.Chdir(t.TempDir())
	startTestChain(t, "devnet")
	blocks := buildTestChain(t, 3)
	stored := blocks[len(blocks)-1]

	_ = GlobalBoltEngine.View(func(dbTx *bbolt.Tx) error {
		// The second copy, judged as if it still extended the tip.
		vaultBefore, err := readVaultUnits(dbTx, stored.Index-1, stored.PrevHash)
		if err != nil {
			t.Fatalf("vault before the block: %v", err)
		}
		_, _, err = ValidateBlockTransactions(stored, bucketLookup(dbTx.Bucket(utxoBucket)), vaultBefore)
		if err == nil || !strings.Contains(err.Error(), "reward transaction ID already used") {
			t.Fatalf("a second copy of a stored block gave %v, want the already-used refusal", err)
		}
		if !blockAlreadyStored(dbTx, stored) {
			t.Error("the stored block was not recognised as already stored")
		}

		// A different block at that height is not a duplicate and is still reported.
		other := stored
		other.Hash = strings.Repeat("ab", 32)
		if blockAlreadyStored(dbTx, other) {
			t.Error("a different block at the same height was taken for the stored one")
		}
		ahead := stored
		ahead.Index += 5
		if blockAlreadyStored(dbTx, ahead) {
			t.Error("a block at a height with nothing stored was taken for a stored one")
		}
		return nil
	})
}
