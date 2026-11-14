package node

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
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
//   - Public Bitcoin block explorers, asked for the block at the announced
//     height. Their answer is only used when two independent ones give the
//     same header, and the header must carry Bitcoin's proof of work like
//     any other. `--no-anchor-lookup` switches this off, for an operator who
//     prefers to supply the header by hand.
//
// A header found this way is written to the anchor file, so the node starts
// from the same anchor every time afterwards without asking anyone.
//
// What an explorer could do wrong: nothing that costs anyone coins. A false
// header is refused. A real header of the wrong block would start the node
// on a network of its own, which is why two sources have to agree.

// errAnchorNotYetKnown means the network is scheduled but its Bitcoin block
// has not been supplied yet: the node can wait for it.
var errAnchorNotYetKnown = errors.New("the anchor is not known yet")

// anchorSources are the public Bitcoin block explorers asked for the anchor.
// Each offers the same simple interface. A variable so tests can stand in.
var anchorSources = []string{
	"https://mempool.space/api",
	"https://blockstream.info/api",
}

// anchorSourcesNeeded is how many sources must give the same header.
const anchorSourcesNeeded = 2

var anchorHTTPClient = &http.Client{Timeout: 10 * time.Second}

// anchorGet fetches a small text answer from an explorer.
func anchorGet(url string) (string, bool) {
	response, err := anchorHTTPClient.Get(url)
	if err != nil {
		return "", false
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", false
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 4096))
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(body)), true
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

// lookUpAnchor asks every source and returns the header that enough of them
// agree on. tip is the highest Bitcoin height any of them reported.
func lookUpAnchor(height int64) (header string, tip int64, agreed bool) {
	votes := make(map[string]int)
	for _, source := range anchorSources {
		candidate, sourceTip, found := anchorFromSource(source, height)
		if sourceTip > tip {
			tip = sourceTip
		}
		if found {
			votes[candidate]++
		}
	}
	for candidate, count := range votes {
		if count >= anchorSourcesNeeded && len(votes) == 1 {
			return candidate, tip, true
		}
	}
	return "", tip, false
}

// anchorFromFile reads the header an operator has placed in the anchor file.
func anchorFromFile() (string, bool) {
	raw, err := os.ReadFile(genesisAnchorFileName())
	if err != nil || strings.TrimSpace(string(raw)) == "" {
		return "", false
	}
	return strings.TrimSpace(string(raw)), true
}

// waitForGenesisAnchor blocks until this network's anchor is known, then
// makes it the anchor. It looks every `nearInterval` once Bitcoin is within
// a few blocks of the height, and every `farInterval` before that.
func waitForGenesisAnchor(args []string, nearInterval, farInterval time.Duration) {
	height := Params.Anchor.BitcoinHeight
	lookup := true
	for _, arg := range args {
		if arg == "--no-anchor-lookup" {
			lookup = false
		}
	}
	fmt.Println("====================================================================")
	fmt.Printf(" WAITING FOR THE START OF %s\n", strings.ToUpper(Params.Name))
	fmt.Println("====================================================================")
	fmt.Printf(" This network starts when Bitcoin block %d is mined.\n", height)
	fmt.Println(" Leave this window open: the node will start by itself at that moment,")
	fmt.Println(" at the same time as everyone else who is waiting.")
	if lookup {
		fmt.Println(" It is watching public Bitcoin block explorers for that block.")
	} else {
		fmt.Printf(" Explorer look-ups are off. Put the block's header in %s\n", genesisAnchorFileName())
		fmt.Println(" when it exists, and the node will start.")
	}
	fmt.Println("====================================================================")

	lastReport := time.Time{}
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
		if lookup {
			header, tip, agreed := lookUpAnchor(height)
			if agreed {
				if err := useAnchorHeader(header); err == nil {
					// Kept, so that every later start uses the same anchor
					// without asking anyone.
					_ = os.WriteFile(genesisAnchorFileName(), []byte(header+"\n"), 0644)
					fmt.Printf("✅ Bitcoin block %d has been mined: %s\n   Starting.\n", height, Params.Anchor.BitcoinHash)
					return
				}
			}
			if tip > 0 && height-tip <= 3 {
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
		time.Sleep(interval)
	}
}
