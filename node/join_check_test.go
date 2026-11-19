package node

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// In November 2026 a tester new to all this started the program to join the
// development network and it began a chain of its own, saying nothing. A node
// that has been told of other machines now asks them first.
func TestWhenANodeMayStartMining(t *testing.T) {
	cases := []struct {
		name          string
		height        int64
		agree, differ int
		want          int
	}{
		{"a seed has our first block", 0, 1, 0, joinGo},
		{"one seed agrees and one is on another chain", 0, 1, 1, joinGo},
		{"every seed that answers is on another chain", 0, 0, 2, joinRefuse},
		{"the same, with blocks of our own", 500, 0, 1, joinRefuse},
		{"nobody answers and we have no blocks", 0, 0, 0, joinWait},
		{"nobody answers and we have been on the network before", 500, 0, 0, joinGo},
	}
	for _, c := range cases {
		if got := decideJoin(c.height, c.agree, c.differ); got != c.want {
			t.Errorf("%s: decided %d, want %d", c.name, got, c.want)
		}
	}
}

// standInForSeeds makes the named hosts answer with the given first block
// ("" for no answer), and counts how often each was asked.
func standInForSeeds(t *testing.T, answers map[string]string) *sync.Map {
	t.Helper()
	asked := &sync.Map{}
	old, oldEvery := askFirstBlock, joinRetryEvery
	joinRetryEvery = time.Millisecond
	askFirstBlock = func(source string) (string, bool) {
		count, _ := asked.LoadOrStore(source, new(int))
		*count.(*int)++
		for host, hash := range answers {
			if strings.Contains(source, host) {
				return hash, hash != ""
			}
		}
		return "", false
	}
	t.Cleanup(func() { askFirstBlock, joinRetryEvery = old, oldEvery })
	return asked
}

func returnsSoon(t *testing.T, what string, run func()) {
	t.Helper()
	done := make(chan bool)
	go func() { run(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s: the node was still waiting after five seconds", what)
	}
}

func TestANodeAsksTheSeedsBeforeItMines(t *testing.T) {
	startTestChain(t, "devnet")
	ours := GetBlockByHeightFromDB(0).Hash
	args := []string{"cvn"}

	// With no seeds it is a network of one machine and nobody is asked.
	asked := standInForSeeds(t, nil)
	returnsSoon(t, "no seeds", func() { joinBeforeMining(args, true) })

	Params.SeedHosts = []string{"10.0.0.1", "10.0.0.2"}
	// A seed with our first block: carry on, even if another is silent.
	asked = standInForSeeds(t, map[string]string{"10.0.0.1": ours})
	returnsSoon(t, "a seed agrees", func() { joinBeforeMining(args, true) })
	if _, was := asked.Load("http://10.0.0.2:28081"); !was {
		t.Error("the second seed was not asked")
	}

	// A node that does not mine cannot start a chain and is not held up, and
	// --start-alone is the way to begin a network on purpose.
	asked = standInForSeeds(t, nil)
	returnsSoon(t, "a node that does not mine", func() { joinBeforeMining(args, false) })
	returnsSoon(t, "--start-alone", func() { joinBeforeMining([]string{"cvn", "--start-alone"}, true) })
	asked.Range(func(key, _ any) bool {
		t.Errorf("%v was asked although the check was off", key)
		return true
	})
}

func TestWithSilentSeedsANewNodeWaitsAndDoesNotMine(t *testing.T) {
	startTestChain(t, "devnet")
	ours := GetBlockByHeightFromDB(0).Hash
	Params.SeedHosts = []string{"10.0.0.1"}

	// Silent for the first rounds, then there.
	var mutex sync.Mutex
	rounds := 0
	old, oldEvery := askFirstBlock, joinRetryEvery
	joinRetryEvery = time.Millisecond
	askFirstBlock = func(string) (string, bool) {
		mutex.Lock()
		defer mutex.Unlock()
		rounds++
		return ours, rounds > 3
	}
	t.Cleanup(func() { askFirstBlock, joinRetryEvery = old, oldEvery })

	returnsSoon(t, "a seed that comes up later", func() { joinBeforeMining([]string{"cvn"}, true) })
	if rounds != 4 {
		t.Errorf("the seed was asked %d times; the node should have waited through three silences", rounds)
	}
	if GetLatestBlockInternal().Index != 0 {
		t.Error("blocks appeared while the node was waiting")
	}

	// A node that already has blocks is not kept waiting by silence: it has
	// been on the network, and the mining hold deals with it.
	extendTestChain(t, Params.GenesisTimestamp+100, testAddress("m"), nil)
	standInForSeeds(t, nil)
	returnsSoon(t, "silent seeds, with blocks of our own", func() { joinBeforeMining([]string{"cvn"}, true) })
}

func TestSeedsOnAnotherChainAreNamedAsSuch(t *testing.T) {
	startTestChain(t, "devnet")
	ours := GetBlockByHeightFromDB(0).Hash
	theirs := strings.Repeat("ab", 32)
	standInForSeeds(t, map[string]string{"10.0.0.1": theirs, "10.0.0.2": theirs, "10.0.0.3": ""})
	sources := []string{"http://10.0.0.1:28081", "http://10.0.0.2:28081", "http://10.0.0.3:28081"}

	agree, differ, got := compareFirstBlocks(sources, ours)
	if agree != 0 || differ != 2 || got != theirs {
		t.Fatalf("counted %d agreeing and %d differing (first block %q)", agree, differ, got)
	}
	if decideJoin(0, agree, differ) != joinRefuse {
		t.Fatal("seeds on another chain did not stop the node")
	}
	message := otherChainMessage(sources, ours, theirs)
	for _, want := range []string{"DIFFERENT CHAIN", "10.0.0.1, 10.0.0.2, 10.0.0.3", theirs[:16], ours[:16],
		"network devnet", "CVN_DEVNET_ANCHOR_HEIGHT", "Nothing was mined", "--start-alone"} {
		if !strings.Contains(message, want) {
			t.Errorf("the message does not mention %q:\n%s", want, message)
		}
	}
	// On a network started from a Bitcoin block, the height is shown.
	Params.Anchor.BitcoinHeight = 976283
	if !strings.Contains(otherChainMessage(sources, ours, theirs), "Bitcoin block 976283") {
		t.Error("the message does not say which Bitcoin block this program started from")
	}
}
