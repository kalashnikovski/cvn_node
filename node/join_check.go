package node

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// JOINING A NETWORK THAT IS ALREADY RUNNING
//
// A node with no chain of its own and nobody to follow mines block #1. That
// is how a network begins, and it is also how a newcomer ends up alone: with
// the seeds mistyped, or the program started for a different first block,
// the node used to begin a chain of its own and say nothing. Every coin mined
// on it exists nowhere else.
//
// So a node that mines, and that has been told of other nodes, asks them for
// their first block before it does anything else:
//
//   - one of them has this node's first block: carry on;
//   - some answer and none has it: they are another chain. The program stops
//     and says so, whatever height this node is at;
//   - none answers and this node has no blocks yet: it waits and keeps
//     asking, and does not mine;
//   - none answers and this node has blocks: carry on. The node has been on
//     the network before, and mining_readiness.go holds it back while it
//     cannot confirm it is up to date.
//
// `--start-alone` skips all of it, for whoever means to mine the first blocks
// of a network before its other machines are up. A node that does not mine
// (--no-mining) is not asked: it cannot start a chain.

const (
	joinGo = iota
	joinWait
	joinRefuse
)

// decideJoin is the rule itself, kept free of the network.
func decideJoin(ourHeight int64, agree, differ int) int {
	switch {
	case agree > 0:
		return joinGo
	case differ > 0:
		return joinRefuse
	case ourHeight == 0:
		return joinWait
	}
	return joinGo
}

// askFirstBlock asks a node for the identifier of its first block. A variable
// so tests can stand in for the network.
var askFirstBlock = func(source string) (string, bool) { return fetchBlockHash(source, 0) }

// joinRetryEvery is how long a waiting node leaves between rounds of asking.
var joinRetryEvery = 10 * time.Second

// nodesToAskBeforeJoining lists the seeds and the nodes the operator named,
// then every other node this one knows of.
func nodesToAskBeforeJoining() (named, others []string) {
	tiers := syncSourceTiers()
	seen := make(map[string]bool)
	for i, tier := range tiers {
		for _, source := range tier {
			if seen[source] {
				continue
			}
			seen[source] = true
			if i == 0 {
				named = append(named, source)
			} else {
				others = append(others, source)
			}
		}
	}
	return named, others
}

// compareFirstBlocks asks each node for its first block and counts how many
// have ours, how many have another, and remembers one that differs.
func compareFirstBlocks(sources []string, ours string) (agree, differ int, theirs string) {
	type answer struct {
		hash string
		ok   bool
	}
	answers := make(chan answer, len(sources))
	for _, source := range sources {
		go func(source string) {
			hash, ok := askFirstBlock(source)
			answers <- answer{hash, ok}
		}(source)
	}
	for range sources {
		switch a := <-answers; {
		case !a.ok:
		case a.hash == ours:
			agree++
		default:
			differ++
			theirs = a.hash
		}
	}
	return agree, differ, theirs
}

func hostsOf(sources []string) string {
	hosts := make([]string, len(sources))
	for i, source := range sources {
		host := strings.TrimPrefix(source, "http://")
		hosts[i] = strings.TrimSuffix(host, ":"+Params.syncPortText())
	}
	return strings.Join(hosts, ", ")
}

func shortHash(hash string) string {
	if len(hash) > 16 {
		return hash[:16] + "..."
	}
	return hash
}

// otherChainMessage is what a person is told when the nodes they named are on
// another chain.
func otherChainMessage(named []string, ours, theirs string) string {
	started := "its built-in first block"
	if Params.Anchor.BitcoinHeight > 0 {
		started = fmt.Sprintf("Bitcoin block %d", Params.Anchor.BitcoinHeight)
	}
	text := "🚨 The other machines are on a DIFFERENT CHAIN from the one this program would run.\n" +
		fmt.Sprintf("   Asked: %s\n", hostsOf(named)) +
		fmt.Sprintf("   Their first block: %s\n", shortHash(theirs)) +
		fmt.Sprintf("   This program's:    %s  (network %s, started from %s)\n", shortHash(ours), Params.Name, started) +
		"   Nothing was mined. Check three things with whoever runs the network:\n" +
		"     1. the network name given with --network;\n"
	if Params.Name == "devnet" {
		text += "     2. the Bitcoin block it started from. It is set before the command, and must be\n" +
			"        set again in every new window:\n" +
			"            PowerShell:      $env:CVN_DEVNET_ANCHOR_HEIGHT=\"HEIGHT\"\n" +
			"            Command Prompt:  set CVN_DEVNET_ANCHOR_HEIGHT=HEIGHT\n" +
			"            Linux:           export CVN_DEVNET_ANCHOR_HEIGHT=HEIGHT\n"
	} else {
		text += "     2. that the addresses given are machines of this network;\n"
	}
	return text + "     3. that this is the release they are running.\n" +
		"   To start a separate chain on purpose, add --start-alone."
}

// joinBeforeMining is run once, before the start-up download. It returns when
// the node may carry on, and ends the program if the nodes it was told of are
// on another chain.
func joinBeforeMining(args []string, mines bool) {
	if !mines {
		return
	}
	for _, arg := range args {
		if arg == "--start-alone" {
			fmt.Println("🌐 --start-alone: not checking the other machines' chain before mining.")
			return
		}
	}
	operatorSyncSources = operatorSourcesFrom(argumentValue(args, "--connect"))
	named, _ := nodesToAskBeforeJoining()
	if len(named) == 0 {
		return // a network of one machine, as before
	}
	ours := GetBlockByHeightFromDB(0).Hash
	said := false
	for {
		named, others := nodesToAskBeforeJoining()
		agree, differ, theirs := compareFirstBlocks(named, ours)
		if agree == 0 && differ == 0 {
			// The named machines are all silent. Any other node that holds our
			// first block shows the network is there.
			agree, _, _ = compareFirstBlocks(others, ours)
		}
		switch decideJoin(GetLatestBlockInternal().Index, agree, differ) {
		case joinGo:
			if said {
				fmt.Println("✅ Another machine answered. Carrying on.")
			}
			return
		case joinRefuse:
			fmt.Println(otherChainMessage(named, ours, theirs))
			os.Exit(1)
		}
		if !said {
			said = true
			fmt.Println("⏳ NOT MINING YET: none of the other machines answered, and this one has no blocks.")
			fmt.Printf("   Asked: %s\n", hostsOf(named))
			fmt.Println("   Mining now would start a separate chain that nobody else has, so this node waits")
			fmt.Printf("   and asks again every %d seconds. If it stays like this, check:\n", int(joinRetryEvery.Seconds()))
			fmt.Println("     1. the addresses above are the ones you were given (numbers and dots only,")
			fmt.Println("        with commas between them, no spaces and no port numbers);")
			fmt.Println("     2. this computer is online, and its firewall or antivirus lets this program")
			fmt.Println("        connect to the internet.")
			fmt.Println("   To mine the first blocks of a network on purpose, add --start-alone.")
		}
		time.Sleep(joinRetryEvery)
	}
}
