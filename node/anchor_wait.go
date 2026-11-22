package node

import (
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// WAITING FOR THE ANCHOR
//
// A launch network starts when one particular Bitcoin block is mined (see
// genesis.go). A node started before then does not have to be restarted by
// hand at the right second: it waits, and starts by itself the moment the
// anchor is known. Everyone who is waiting starts within seconds of each
// other, which is the point of the anchor.
//
// While waiting, a node looks in two places:
//
//   - The anchor file, genesis_anchor_NAME.txt, in case the operator puts
//     the header there.
//   - Bitcoin itself. The node follows Bitcoin's headers from a block fixed
//     in this software up to the announced height, checking every one (see
//     bitcoin_headers.go), and takes the header it arrives at. Nobody's word
//     is taken for which block stands at that height: the node has counted.
//     `--no-anchor-lookup` switches this off, for an operator who prefers
//     to supply the header by hand.
//
// A header found this way is written to the anchor file, so the node starts
// from the same anchor every time afterwards without asking anyone.
//
// One case is still settled the older way. A height at or below the block
// fixed in this software cannot be counted up to, which only happens when a
// development network rehearses a launch from an old Bitcoin block. Public
// block explorers are then asked for the block at that height, and their
// answer is used only when two of them give the same header and it carries
// Bitcoin's proof of work. A launch network is never in this case: a test
// holds every scheduled network to a height after the fixed block.

// errAnchorNotYetKnown means the network is scheduled but its Bitcoin block
// has not been supplied yet: the node can wait for it.
var errAnchorNotYetKnown = errors.New("the anchor is not known yet")

// anchorSources are the public Bitcoin block explorers asked for the anchor.
// Each offers the same simple interface. A variable so tests can stand in.
var anchorSources = []string{
	"https://mempool.space/api",
	"https://blockstream.info/api",
	"https://mempool.emzy.de/api",
	"https://btcscan.org/api",
	"https://mempool.ninja/api",
}

// anchorSourcesNeeded is how many sources must give the same header.
const anchorSourcesNeeded = 2

// anchorFreshness is how far a Bitcoin block's own date may be from this
// node's clock for a node that watched Bitcoin reach the height to take it
// as the block it was waiting for. Bitcoin allows a block's date to be up to
// two hours ahead of true time and about an hour behind. A variable so tests
// can use older blocks.
var anchorFreshnessNanos = int64(3 * time.Hour) // read and written atomically; tests change it

func anchorFreshness() time.Duration {
	return time.Duration(atomic.LoadInt64(&anchorFreshnessNanos))
}

var anchorHTTPClient = &http.Client{Timeout: 10 * time.Second}

// anchorGet fetches a small text answer from an explorer.
func anchorGet(url string) (string, bool) { return anchorGetUpTo(url, 4096) }

// anchorGetUpTo fetches an answer of at most `limit` bytes from an explorer.
func anchorGetUpTo(url string, limit int64) (string, bool) {
	response, err := anchorHTTPClient.Get(url)
	if err != nil {
		return "", false
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", false
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit))
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(body)), true
}

// The follower of Bitcoin's headers for this network's anchor. One is kept
// for as long as the program runs, so that the watch for a replaced anchor
// (genesis_watch.go) carries on from the headers already checked.
var (
	anchorFollowerLock sync.Mutex
	anchorFollower     *bitcoinFollower
)

// followerForAnchor returns the follower, making it at first use. It is nil
// when this network's height cannot be counted up to.
func followerForAnchor() *bitcoinFollower {
	anchorFollowerLock.Lock()
	defer anchorFollowerLock.Unlock()
	if anchorFollower == nil {
		anchorFollower = newBitcoinFollower(Params.Anchor.BitcoinHeight)
	}
	return anchorFollower
}

// anchorFromSource asks one explorer for the header of the Bitcoin block at
// `height`. found is false while that block does not exist yet. tip is the
// height Bitcoin has reached according to this explorer, or zero if it did
// not say.
func anchorFromSource(source string, height int64) (header string, tip int64, found bool) {
	if text, ok := anchorGet(source + "/blocks/tip/height"); ok {
		tip, _ = strconv.ParseInt(text, 10, 64)
	}
	hash, ok := anchorGet(fmt.Sprintf("%s/block-height/%d", source, height))
	if !ok || !isLowerHex(hash, 64) {
		return "", tip, false
	}
	header, ok = anchorGet(source + "/block/" + hash + "/header")
	if !ok {
		return "", tip, false
	}
	header = strings.ToLower(header)
	// The header must be a real Bitcoin header, and the one this explorer
	// itself says sits at that height.
	if headerHash, _, err := parseBitcoinHeader(header); err != nil || headerHash != hash {
		return "", tip, false
	}
	return header, tip, true
}

// lookUpAnchor asks every source at once and returns the header that enough
// of them agree on. tip is the highest Bitcoin height any of them reported.
func lookUpAnchor(height int64) (header string, tip int64, agreed bool) {
	type answer struct {
		header string
		tip    int64
		found  bool
	}
	answers := make(chan answer, len(anchorSources))
	for _, source := range anchorSources {
		go func(source string) {
			candidate, sourceTip, found := anchorFromSource(source, height)
			answers <- answer{candidate, sourceTip, found}
		}(source)
	}
	// One header must have the votes it needs, and no other may. If Bitcoin
	// itself is undecided between two blocks at that height, the sources
	// will be too, and the node waits for them to settle.
	votes := make(map[string]int)
	withEnough := 0
	for waiting := len(anchorSources); waiting > 0; {
		got := <-answers
		waiting--
		if got.tip > tip {
			tip = got.tip
		}
		if got.found {
			votes[got.header]++
		}
		withEnough = 0
		rivalCouldStillReach := false
		for candidate, count := range votes {
			if count >= anchorSourcesNeeded {
				header = candidate
				withEnough++
			} else if count+waiting >= anchorSourcesNeeded {
				rivalCouldStillReach = true
			}
		}
		// Settled early: one header has its votes, and the sources still to
		// answer could not give any other header as many. A slow source then
		// delays nobody.
		if withEnough == 1 && !rivalCouldStillReach && waiting < anchorSourcesNeeded {
			break
		}
	}
	if withEnough != 1 {
		return "", tip, false
	}
	return header, tip, true
}

// anchorFromFile reads the header an operator has placed in the anchor file.
func anchorFromFile() (string, bool) {
	raw, err := os.ReadFile(genesisAnchorFileName())
	if err != nil || strings.TrimSpace(string(raw)) == "" {
		return "", false
	}
	return strings.TrimSpace(string(raw)), true
}

// anchorLookupAllowed reports whether this node may ask Bitcoin block
// explorers about the anchor: it may unless started with --no-anchor-lookup.
func anchorLookupAllowed(args []string) bool {
	for _, arg := range args {
		if arg == "--no-anchor-lookup" {
			return false
		}
	}
	return true
}

// waitForGenesisAnchor blocks until this network's anchor is known, then
// makes it the anchor. It looks every `nearInterval` once Bitcoin is within
// a few blocks of the height, and every `farInterval` before that.
func waitForGenesisAnchor(args []string, nearInterval, farInterval time.Duration) {
	height := Params.Anchor.BitcoinHeight
	lookup := anchorLookupAllowed(args)
	fmt.Println("====================================================================")
	fmt.Printf(" WAITING FOR THE START OF %s\n", strings.ToUpper(Params.Name))
	fmt.Println("====================================================================")
	fmt.Printf(" This network starts when Bitcoin block %d is mined.\n", height)
	fmt.Println(" Leave this window open: the node will start by itself at that moment,")
	fmt.Println(" at the same time as everyone else who is waiting.")
	follower := followerForAnchor()
	switch {
	case lookup && follower != nil:
		fmt.Printf(" It follows Bitcoin's own headers to that block from block %d,\n", bitcoinStart.Height)
		fmt.Println(" and checks every one of them itself.")
	case lookup:
		fmt.Println(" It is watching public Bitcoin block explorers for that block.")
	default:
		fmt.Printf(" Look-ups are off. Put the block's header in %s\n", genesisAnchorFileName())
		fmt.Println(" when it exists, and the node will start.")
	}
	fmt.Println("====================================================================")

	lastReport := time.Time{}
	// sawItMissing is set once this node has itself seen Bitcoin short of
	// the height. From then on the block it is waiting for must be new.
	sawItMissing := false
	near := false
	for {
		if supplied, ok := anchorFromFile(); ok {
			if err := useAnchorHeader(supplied); err == nil {
				fmt.Printf("✅ Anchor read from %s. Starting.\n", genesisAnchorFileName())
				return
			} else if time.Since(lastReport) > time.Minute {
				fmt.Printf("⚠️  %s does not hold a usable header: %v\n", genesisAnchorFileName(), err)
			}
		}
		interval := farInterval
		if lookup && follower != nil {
			tip := follower.advance()
			if header, reached := follower.chain.headerAt(height); reached {
				if err := useAnchorHeader(header); err == nil {
					follower.done()
					_ = os.WriteFile(genesisAnchorFileName(), []byte(header+"\n"), 0644)
					fmt.Printf("✅ Bitcoin block %d has been mined: %s\n", height, Params.Anchor.BitcoinHash)
					fmt.Printf("   This node followed Bitcoin's headers to it from block %d and checked each one.\n   Starting.\n", bitcoinStart.Height)
					return
				} else if time.Since(lastReport) > time.Minute {
					fmt.Printf("⚠️  Bitcoin block %d cannot be used: %v\n", height, err)
				}
			}
			if height-tip <= 3 {
				interval = nearInterval
			}
			if time.Since(lastReport) > time.Minute {
				lastReport = time.Now()
				switch source := follower.source(); {
				case source == "":
					fmt.Printf("[%s] ⏳ Waiting for Bitcoin block %d. Neither the Bitcoin network nor the block explorers answered just now; still trying. Followed so far: block %d.\n", time.Now().Format("15:04:05"), height, tip)
				case tip < height:
					fmt.Printf("[%s] ⏳ Waiting for Bitcoin block %d. Bitcoin's headers followed to block %d (from %s): %d to go, roughly %d minutes.\n", time.Now().Format("15:04:05"), height, tip, source, height-tip, (height-tip)*10)
				}
			}
		} else if lookup {
			header, tip, agreed := lookUpAnchor(height)
			if agreed && sawItMissing {
				if _, dated, err := parseBitcoinHeader(header); err == nil {
					if age := time.Since(time.Unix(dated, 0)); age > anchorFreshness() || age < -anchorFreshness() {
						agreed = false
						if time.Since(lastReport) > time.Minute {
							fmt.Printf("⚠️  The explorers gave a block for height %d dated %s, which cannot be the block this node has been waiting for. Not using it.\n", height, time.Unix(dated, 0).UTC().Format("2 January 2006 15:04 UTC"))
						}
					}
				}
			}
			if tip > 0 && tip < height {
				sawItMissing = true
			}
			if agreed {
				if err := useAnchorHeader(header); err == nil {
					// Kept, so that every later start uses the same anchor
					// without asking anyone.
					_ = os.WriteFile(genesisAnchorFileName(), []byte(header+"\n"), 0644)
					fmt.Printf("✅ Bitcoin block %d has been mined: %s\n   Starting.\n", height, Params.Anchor.BitcoinHash)
					return
				}
			}
			// Once Bitcoin is close, look often, and keep looking often even
			// if the explorers stop saying how far Bitcoin has got.
			if tip > 0 && height-tip <= 3 {
				near = true
			}
			if near {
				interval = nearInterval
			}
			if time.Since(lastReport) > time.Minute {
				lastReport = time.Now()
				switch {
				case tip <= 0:
					fmt.Printf("[%s] ⏳ Waiting for Bitcoin block %d. The explorers did not answer just now; still trying.\n", time.Now().Format("15:04:05"), height)
				case tip >= height:
					fmt.Printf("[%s] ⏳ Bitcoin has reached block %d. Waiting for two explorers to agree on its header.\n", time.Now().Format("15:04:05"), height)
				default:
					fmt.Printf("[%s] ⏳ Waiting for Bitcoin block %d. Bitcoin is at block %d: %d to go, roughly %d minutes.\n", time.Now().Format("15:04:05"), height, tip, height-tip, (height-tip)*10)
				}
			}
		}
		// A little unevenness, so that thousands of waiting nodes do not all
		// ask at the same instant.
		time.Sleep(interval + time.Duration(rand.Int63n(int64(interval)/5+1)))
	}
}
