package node

import (
	"encoding/hex"
	"os"
	"sync"
	"sync/atomic"

	"cvn_node/internal/randomx"
)

// CHECKING DOWNLOADED BLOCKS ON SEVERAL CORES
//
// Every block's proof of work is one RandomX hash in the low-memory mode,
// which takes a desktop core about a hundredth of a second and a small
// rented core about a tenth. A node downloading the chain checked them one
// after another on one core, so its first download ran at the speed of that
// one core: nine blocks a second on the small server this was found on,
// which is sixteen hours for a year of blocks.
//
// The hashes do not depend on each other. So when a batch of blocks has
// arrived, their proofs are worked out here first, on as many cores as
// mining may use, and kept. Nothing about how a block is checked changes:
// the checking that follows runs exactly as before, one block after another,
// and when it asks for a block's proof it finds the answer ready.
//
// What is kept is only ever the RandomX hash of a key and a blob, filed
// under that key and that blob. It is not a verdict. If the key guessed here
// is not the one the checking uses (the chain changed in between, or the
// batch is not what it seemed), the answer is simply not found and the proof
// is worked out the old way.
//
// A batch comes from another node and may be rubbish. Work is done on it a
// few blocks at a time, and stops at the first block that does not join on
// to the one before, does not carry its own identifier, or whose proof falls
// short of the difficulty the block itself claims. Someone sending rubbish
// costs this node a handful of hashes, as it did before, not a batch full.

// proofCheckThreads is how many cores may work on proofs at once: as many as
// mining may use, so that `--threads` limits this too.
var proofCheckThreads = func() int { return miningThreads(os.Args) }

// proofsFoundReady counts the proofs the checking found already worked out.
var proofsFoundReady int64

// proofsAtOncePerThread is how many proofs each core is given in one go.
// The checking engine is held for that long, so it is kept short.
const proofsAtOncePerThread = 4

type proofWanted struct {
	blob   []byte
	target string
}

// prepareProofs works out, on several cores, the proofs of a batch of blocks
// that is about to be checked and added, and reports how many it prepared.
func prepareProofs(batch []Block) int {
	threads := proofCheckThreads()
	if threads < 2 || len(batch) < 2 || !networkUsesRandomX() {
		return 0
	}
	// What was prepared for the batch before this one is no longer needed.
	rxVerifier.Lock()
	rxVerifier.prepared = make(map[string]string, len(batch))
	rxVerifier.Unlock()

	prepared := 0
	var key []byte
	var wanted []proofWanted
	// workOut does the proofs gathered so far, and reports whether every one
	// of them met the difficulty its block claims.
	workOut := func() bool {
		if len(wanted) == 0 {
			return true
		}
		blobs := make([][]byte, len(wanted))
		for i := range wanted {
			blobs[i] = wanted[i].blob
		}
		works, ok := randomXVerifyTogether(key, blobs, threads)
		good := ok
		for i := range works {
			if works[i] > wanted[i].target {
				good = false
			}
		}
		if ok {
			prepared += len(wanted)
		}
		wanted = wanted[:0]
		return good
	}

	first := batch[0].Index
	for i, block := range batch {
		if block.Index != first+int64(i) || (i > 0 && block.PrevHash != batch[i-1].Hash) {
			break
		}
		if powAlgorithmAt(block.Index) != PowRandomX {
			continue
		}
		if int64(block.Difficulty) < minDifficultyAt(block.Index) || block.Nonce < 0 || block.Nonce >= powNonceLimit || block.Hash != CalculateHash(block) {
			break
		}
		// The key is the identifier of an earlier block of the same chain:
		// one of the blocks in this batch, or one this node already holds.
		keyBlockHash := ""
		if keyHeight := powKeyHeight(block.Index); keyHeight >= first {
			keyBlockHash = batch[keyHeight-first].Hash
		} else {
			keyBlockHash = GetBlockByHeightFromDB(keyHeight).Hash
		}
		blockKey, err := hex.DecodeString(keyBlockHash)
		if err != nil || len(blockKey) == 0 {
			break
		}
		blob, ok := powBlob(powCommitment(block), uint32(block.Nonce))
		if !ok {
			break
		}
		if string(blockKey) != string(key) || len(wanted) >= threads*proofsAtOncePerThread {
			if !workOut() {
				return prepared
			}
			key = blockKey
		}
		wanted = append(wanted, proofWanted{blob: blob, target: HeartbeatTargetHex(block.Difficulty)})
	}
	workOut()
	return prepared
}

// randomXVerifyTogether returns the work values of several mining blobs
// under one key, worked out on up to `threads` cores at once, and keeps them
// for the checking that follows.
func randomXVerifyTogether(key []byte, blobs [][]byte, threads int) ([]string, bool) {
	if len(key) == 0 || len(blobs) == 0 {
		return nil, false
	}
	rxVerifier.Lock()
	defer rxVerifier.Unlock()
	engine := verifyEngineLocked(key)
	if engine == nil {
		return nil, false
	}
	if threads > len(blobs) {
		threads = len(blobs)
	}
	machines := engine.machines(threads)

	works := make([]string, len(blobs))
	var next int64 = -1
	var working sync.WaitGroup
	for _, machine := range machines {
		working.Add(1)
		go func(machine *randomx.VM) {
			defer working.Done()
			for {
				i := int(atomic.AddInt64(&next, 1))
				if i >= len(blobs) {
					return
				}
				var output [randomx.RANDOMX_HASH_SIZE]byte
				machine.CalculateHash(blobs[i], &output)
				works[i] = powWorkValue(output)
			}
		}(machine)
	}
	working.Wait()

	if rxVerifier.prepared == nil {
		rxVerifier.prepared = make(map[string]string, len(blobs))
	}
	for i, blob := range blobs {
		rxVerifier.prepared[string(key)+string(blob)] = works[i]
	}
	return works, true
}

// machines returns up to `count` machines that work from this engine's
// cache, making the spare ones it lacks. There is always at least one.
func (e *rxLight) machines(count int) []*randomx.VM {
	for len(e.spare) < count-1 {
		machine, err := randomx.NewVM(randomx.GetFlags(), e.cache, nil)
		if err != nil {
			break
		}
		e.spare = append(e.spare, machine)
	}
	all := append([]*randomx.VM{e.vm}, e.spare...)
	if len(all) > count && count >= 1 {
		all = all[:count]
	}
	return all
}

func (e *rxLight) closeSpare() {
	for _, machine := range e.spare {
		machine.Close()
	}
	e.spare = nil
}

// releaseProofHelp gives back what a download used to check proofs on
// several cores: the spare machines and the answers kept for the last batch.
func releaseProofHelp() {
	rxVerifier.Lock()
	defer rxVerifier.Unlock()
	for _, engine := range rxVerifier.engines {
		engine.closeSpare()
	}
	rxVerifier.prepared = nil
}
