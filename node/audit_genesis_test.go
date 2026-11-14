package node

import (
	"strings"
	"testing"
)

// Audit finding: a peer could replace the genesis block. A block at height 0
// passed the old leading-zeros check, was treated as a sibling of the genesis
// block, won on a lower identifier, and was stored with every output of every
// transaction in it, unchecked. That creates coins from nothing.
func TestGenesisBlockCannotBeReplaced(t *testing.T) {
	for _, network := range []string{"devnet", "testnet-1"} {
		t.Run(network, func(t *testing.T) {
			startTestChain(t, network)
			genesis := GetBlockByHeightFromDB(0)

			// A rival "genesis" that pays its maker a fortune in a second
			// transaction, ground until its identifier is lower than the
			// real one and starts with the two zeros the old check wanted.
			rival := Block{
				Version: 1, Index: 0, Timestamp: genesis.Timestamp - 1000000,
				PrevHash: strings.Repeat("0", 64), Difficulty: 2,
				UncleHash: strings.Repeat("0", 64), UncleMiner: "NONE",
				Transactions: []Transaction{
					{Version: 1, ID: "TX_GENESIS_FAIR_LAUNCH_POOL", Inputs: []UTXOInput{}, Outputs: []UTXOOutput{}},
					{Version: 1, ID: "TX_FORGED", Inputs: []UTXOInput{}, Outputs: []UTXOOutput{{Recipient: "CVN_thief", Amount: UnitsAmount(cvn("1000000"))}}},
				},
			}
			for nonce := int64(0); ; nonce++ {
				rival.Nonce = nonce
				rival.Hash = CalculateHash(rival)
				if strings.HasPrefix(rival.Hash, "00") && rival.Hash < genesis.Hash {
					break
				}
			}

			if VerifyIncomingWireBlock(rival) {
				t.Error("a block at height 0 passed the check on arriving blocks")
			}
			if ValidateBlockSignatures(rival) {
				t.Error("a second genesis block was judged valid")
			}
			ProcessIncomingWireBlockWithDelay(rival)
			if SaveBlockToStorageInternal(rival) {
				t.Error("a second genesis block was stored")
			}
			if now := GetBlockByHeightFromDB(0); now.Hash != genesis.Hash {
				t.Fatalf("the genesis block was replaced: %.16s became %.16s", genesis.Hash, now.Hash)
			}
			if got := balanceOf("CVN_thief"); got != 0 {
				t.Fatalf("%s CVN were created from nothing", FormatUnits(got))
			}
			// The real genesis block is still accepted as itself.
			if !ValidateBlockSignatures(genesis) {
				t.Error("the real genesis block is no longer judged valid")
			}
		})
	}
}
