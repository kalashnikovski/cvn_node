//go:build multinode

package node

import (
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"
)

// work is the total work of the chain a node holds, or nil if it did not say.
func (n *localNode) work() *big.Int {
	var state struct {
		Work string `json:"total_work"`
	}
	if !n.get("/block", &state) {
		return nil
	}
	work, ok := new(big.Int).SetString(state.Work, 10)
	if !ok {
		return nil
	}
	return work
}

// A network that has come apart into two halves, each mining its own chain
// from the same first block, and then comes back together. Nobody decides
// which half was right: every node ends on the chain with more work in it,
// the blocks of the other half are given up, and the coins they paid are
// gone on every node. Both halves go on mining the whole time.
func TestMultiNodeTwoHalvesRejoin(t *testing.T) {
	c := newLocalCluster(t)
	const wait = 4 * time.Minute
	_, _, strongOne := newTestWallet(t)
	_, _, strongTwo := newTestWallet(t)
	_, _, weakAddress := newTestWallet(t)

	// The strong half: a seed and two miners. The weak half: a seed and one
	// miner. Neither half has ever heard of the other.
	strongSeed := c.seed("strong-seed", 1, []int{1}).startNode()
	weakSeed := c.seed("weak-seed", 2, []int{2}).startNode()
	c.waitForHeight(30*time.Second, 0, strongSeed, weakSeed)
	strongMinerA := c.miner("strong-miner-a", 11, []int{1}, strongOne).startNode()
	strongMinerB := c.miner("strong-miner-b", 12, []int{1}, strongTwo).startNode()
	weakMiner := c.miner("weak-miner", 21, []int{2}, weakAddress).startNode()
	strong := []*localNode{strongSeed, strongMinerA, strongMinerB}
	weak := []*localNode{weakSeed, weakMiner}
	everyone := append(append([]*localNode{}, strong...), weak...)

	// Each half builds a chain of its own, the strong one clearly ahead.
	c.waitForHeight(wait, 5, weak...)
	c.waitForHeight(wait, 8, strong...)
	c.eventually(wait, "the half with two miners did not get clearly ahead of the half with one", func() bool {
		ahead, behind := strongSeed.work(), weakSeed.work()
		if ahead == nil || behind == nil {
			return false
		}
		// Half as much again, so that the weak half cannot overtake while
		// the two are finding each other.
		return new(big.Int).Mul(ahead, big.NewInt(2)).Cmp(new(big.Int).Mul(behind, big.NewInt(3))) > 0
	})
	strongFirst, weakFirst := strongSeed.hashAt(1), weakSeed.hashAt(1)
	if strongFirst == "" || weakFirst == "" || strongFirst == weakFirst {
		t.Fatalf("the two halves should hold different blocks at height 1: %.16s and %.16s", strongFirst, weakFirst)
	}
	weakHeight := weakSeed.height()
	if paid := weakSeed.balances()[weakAddress]; paid != float64(weakHeight)*50 {
		t.Logf("the weak half counts %v CVN for its miner at block #%d", paid, weakHeight)
	}
	t.Logf("before they meet: the strong half is at block #%d, the weak half at block #%d", strongSeed.height(), weakHeight)

	// The link comes back: the weak half's seed is restarted knowing of the
	// strong half's. Every miner keeps mining throughout.
	weakSeed.stopNode()
	weakSeed.args = []string{"--network", localnetName, "--no-backups", "--no-mining", "--seeds", c.host(1) + "," + c.host(2)}
	weakSeed.startNode()

	c.eventually(wait, "the weak half never took up the strong half's chain", func() bool {
		return weakSeed.hashAt(1) == strongFirst && weakMiner.hashAt(1) == strongFirst
	})
	target := strongSeed.height()
	c.waitForAgreement(wait, target, everyone...)
	for _, n := range everyone {
		if got := n.hashAt(1); got != strongFirst {
			t.Errorf("%s holds %.16s at height 1; the chain with more work has %.16s there", n.name, got, strongFirst)
		}
	}
	// Two miners in one half race each other now and then, and the loser
	// gives up a block for the other's: that is ordinary. What must never
	// happen is a node of the strong half leaving its chain for the weak
	// half's.
	for _, n := range strong {
		for _, weakNode := range weak {
			if strings.Contains(n.log(), "Switching to the branch from http://"+weakNode.host+":") {
				t.Errorf("%s, in the half with more work, gave up blocks for the chain of %s", n.name, weakNode.name)
			}
		}
	}
	if !strings.Contains(weakSeed.log()+weakMiner.log(), "Switching to the branch") {
		t.Error("no node of the weak half shows it switching to the other branch")
	}
	// The weak miner goes on mining after the two meet, now on the shared
	// chain: it must win blocks there like anyone else.
	joinedAt := strongSeed.height()
	c.eventually(wait, "the weak half's miner won no block on the shared chain", func() bool {
		return strongSeed.balances()[weakAddress] > 0
	})
	t.Logf("joined by block #%d; the weak half's miner has since won blocks on the shared chain", joinedAt)

	// At rest, every node counts the same coins, and they add up to what the
	// one remaining chain issued: the blocks the weak half gave up paid
	// nothing that is still counted anywhere.
	for _, miner := range []*localNode{strongMinerA, strongMinerB, weakMiner} {
		miner.stopNode()
	}
	height := c.restAndCheckAccounts(wait, strongSeed, weakSeed)
	t.Logf("came to rest at block #%d, with %v CVN counted for the weak half's miner", height, strongSeed.balances()[weakAddress])
	for _, n := range c.nodes {
		if log := n.log(); strings.Contains(log, "panic:") || strings.Contains(log, "is not valid") || strings.Contains(log, "did not deliver one") {
			t.Errorf("%s's log shows a panic, a refused block or a neighbour set aside", n.name)
		}
	}
}

// The limit on replacing one's own blocks, between real nodes. A node that
// mined more blocks cut off than the limit allows keeps them when it comes
// back, however much more work the network shows: settled history is not
// rewritten. But blocks a newcomer merely took on a seed's word are not its
// own in that sense, and it leaves them for the chain with more work,
// however far back the two part and across a restart.
//
// The network's limit is 100 blocks. Here it is set to 6, on the test
// network only, so that the test takes a minute and not half an hour.
func TestMultiNodeLimitOnReplacingBlocks(t *testing.T) {
	c := newLocalCluster(t)
	c.env = append(c.env, "CVN_LOCALNET_REORG_LIMIT=6")
	const wait = 4 * time.Minute
	const limit = 6
	_, _, minerAddress := newTestWallet(t)
	_, _, secondAddress := newTestWallet(t)
	_, _, strayAddress := newTestWallet(t)

	seed := c.seed("seed", 1, []int{1}).startNode()
	c.waitForHeight(30*time.Second, 0, seed)
	miner := c.miner("miner", 11, []int{1}, minerAddress).startNode()
	second := c.miner("second-miner", 12, []int{1}, secondAddress).startNode()

	// Cut off, and mining a chain of its own that is longer than the limit.
	stray := c.miner("stray", 13, nil, strayAddress).startNode()
	c.waitForHeight(wait, limit+4, stray)
	stray.stopNode()
	stray.args = []string{"--network", localnetName, "--no-backups", "--no-mining", "--seeds", c.host(1)}
	stray.startNode()
	c.eventually(30*time.Second, "the stray node did not answer after it was restarted", func() bool {
		return stray.height() >= 0
	})
	strayHeight, strayFirst := stray.height(), stray.hashAt(1)
	if strayHeight < limit+4 {
		t.Fatalf("the stray node holds %d blocks; it should hold more than the limit of %d", strayHeight, limit)
	}

	// The network gets well ahead of it in work.
	c.eventually(wait, "the network did not get clearly ahead of the stray node", func() bool {
		ahead, behind := seed.work(), stray.work()
		return ahead != nil && behind != nil && ahead.Cmp(new(big.Int).Mul(behind, big.NewInt(2))) > 0
	})
	networkFirst := seed.hashAt(1)
	if networkFirst == strayFirst {
		t.Fatal("the stray node was never on a chain of its own")
	}

	// It has seen the network's chain and says why it will not take it.
	c.eventually(wait, "the stray node never said that the network's chain parts from its own beyond the limit", func() bool {
		return strings.Contains(stray.log(), "[REORG LIMIT]")
	})
	// And it goes on refusing: nothing it holds changes while the network
	// mines on.
	stillAt := seed.height()
	c.waitForHeight(wait, stillAt+4, seed, miner, second)
	if got := stray.hashAt(1); got != strayFirst {
		t.Errorf("the stray node gave up its own block #1 (%.16s) for %.16s, replacing more than %d of its own blocks", strayFirst, got, limit)
	}
	if got := stray.height(); got != strayHeight {
		t.Errorf("the stray node was at block #%d and is now at #%d", strayHeight, got)
	}
	if strings.Contains(stray.log(), "Switching to the branch") {
		t.Error("the stray node's log shows it switching branch")
	}

	// A newcomer whose only seed is the stray node takes the stray chain: it
	// knows no better.
	newcomer := c.seed("newcomer", 14, []int{13}).startNode()
	// (The stray node tells the newcomer of the network's nodes, and now and
	// then the newcomer has compared chains with one of them and moved on
	// before this test looks. Its log then shows that it held the stray
	// chain first and left it.)
	c.eventually(wait, "the newcomer did not download the stray node's chain", func() bool {
		if newcomer.height() == strayHeight && newcomer.hashAt(1) == strayFirst {
			return true
		}
		log := newcomer.log()
		return strings.Contains(log, fmt.Sprintf("Starting at block #%d.", strayHeight)) &&
			strings.Contains(log, fmt.Sprintf("it proves more work than our %d block(s) there", strayHeight))
	})
	// Restarted knowing of the network's seed as well, it leaves that chain
	// for the one with more work, although the two part further back than
	// the limit: it never made those blocks, it was only told of them.
	newcomer.stopNode()
	newcomer.args = []string{"--network", localnetName, "--no-backups", "--no-mining", "--seeds", c.host(13) + "," + c.host(1)}
	newcomer.startNode()
	c.eventually(wait, "the newcomer stayed on the stray node's chain after it was shown the network's", func() bool {
		return newcomer.hashAt(1) == networkFirst
	})
	target := seed.height()
	c.waitForAgreement(wait, target, seed, miner, second, newcomer)
	if !strings.Contains(newcomer.log(), "Switching to the branch") {
		t.Error("the newcomer's log does not show it switching to the network's branch")
	}
	if strings.Contains(newcomer.log(), "[REORG LIMIT]") {
		t.Error("the newcomer was held by the limit, on blocks it had only taken on a seed's word")
	}

	// The network itself was never disturbed by any of it.
	miner.stopNode()
	second.stopNode()
	height := c.restAndCheckAccounts(wait, seed, newcomer)
	if got := seed.balances()[strayAddress]; got != 0 {
		t.Errorf("the network counts %v CVN for blocks the stray node mined cut off from it", got)
	}
	if got := stray.balances()[strayAddress]; got != float64(strayHeight)*50 {
		t.Errorf("the stray node counts %v CVN for its %d blocks", got, strayHeight)
	}
	t.Logf("the network came to rest at block #%d; the stray node still holds its own %d blocks", height, strayHeight)
	for _, n := range c.nodes {
		if log := n.log(); strings.Contains(log, "panic:") || strings.Contains(log, "is not valid") {
			t.Errorf("%s's log shows a panic or a refused block", n.name)
		}
	}
}
