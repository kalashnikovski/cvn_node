//go:build multinode || devtools

package node

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// FILLING BLOCKS ON PURPOSE
//
// A block may be up to MaxBlockBytes. Whether that figure is right is not a
// matter of opinion: a full block has to cross the real network and be
// checked by its smallest machine well inside one block interval. This tool
// makes full blocks and measures exactly that.
//
// It is left out of every ordinary build. Run it from the source, beside a
// node that is mining on a TEST network:
//
//	go run -tags devtools . --network devnet --fill-blocks 5 --seeds HOST,HOST,...
//
// What it does:
//
//  1. asks for the key of a wallet that holds coins (it is not shown or kept);
//  2. builds transfers from that wallet to itself, each one coin cut into the
//     most pieces a transfer may have, until there are enough to fill the
//     number of blocks asked for, and hands them to the node on this machine;
//  3. watches the next blocks: how big each is, and how long after this node
//     had it each seed had it too.
//
// The coins never leave the wallet. They end up in many small pieces, which
// the wallet's "Combine" puts back together. The transfers pay no fee: by
// rule the transfers that pay may fill only four fifths of a block, so a
// block is filled to its limit only with ones that do not.
// It refuses to run on a network that is not for testing.

func init() {
	devToolCommand = runFillBlocksCommand
}

// fillTransfers builds transfers from `address` to itself, one coin each cut
// into TxMaxOutputs pieces, until they add up to `wantBytes`. It returns them
// with their total size, and the coins it did not use.
func fillTransfers(privateKeyHex, publicKeyHex, address string, coins []SpendableCoin, wantBytes int, feeUnits int64) (transfers []Transaction, bytes int, unused []SpendableCoin) {
	pieces := int64(TxMaxOutputs)
	for i, coin := range coins {
		if bytes >= wantBytes {
			return transfers, bytes, coins[i:]
		}
		each := (coin.Amount.Units() - feeUnits) / pieces
		if !coin.Amount.Spendable() || each < minimumOutputUnits() {
			continue // too small to cut up
		}
		outputs := make([]UTXOOutput, pieces)
		for p := range outputs {
			outputs[p] = UTXOOutput{Recipient: address, Amount: UnitsAmount(each)}
		}
		// What does not divide evenly goes into the last piece, so that the
		// fee is exactly the one asked for and a transfer asked to be free
		// pays nothing.
		outputs[pieces-1].Amount = UnitsAmount(coin.Amount.Units() - feeUnits - each*(pieces-1))
		tx, err := signTransfer(privateKeyHex, publicKeyHex, address, []UTXOInput{walletInput(coin)}, outputs, feeUnits)
		if err != nil {
			continue
		}
		transfers = append(transfers, tx)
		bytes += transactionSizeBytes(tx)
	}
	return transfers, bytes, nil
}

// combineBack builds transfers that put the pieces made by `made` (the
// transfers fillTransfers built) back together, TxMaxInputs coins at a time.
// These are the other kind of large transfer: many coins in, one out.
func combineBack(privateKeyHex, publicKeyHex string, made []Transaction, coins []SpendableCoin, feeUnits int64) (transfers []Transaction, bytes int) {
	ours := make(map[string]bool, len(made))
	for _, tx := range made {
		ours[tx.ID] = true
	}
	var pieces []SpendableCoin
	for _, coin := range coins {
		if ours[coin.TxID] && coin.Amount.Spendable() {
			pieces = append(pieces, coin)
		}
	}
	for len(pieces) > 1 {
		group := pieces
		if len(group) > TxMaxInputs {
			group = group[:TxMaxInputs]
		}
		pieces = pieces[len(group):]
		tx, err := BuildSignedCombine(privateKeyHex, publicKeyHex, feeUnits, group)
		if err != nil {
			continue
		}
		transfers = append(transfers, tx)
		bytes += transactionSizeBytes(tx)
	}
	return transfers, bytes
}

// filledBlock is what was seen of one block.
type filledBlock struct {
	Height    int64
	Hash      string
	Bytes     int
	Transfers int
	// Delay is how long after the watched node had the block each other
	// node had it; a node that never showed it within the wait is missing.
	Delay map[string]time.Duration
}

func fetchBlockAt(source string, height int64) (Block, bool) {
	for _, block := range fetchBlockBatch(source, height) {
		if block.Index == height {
			return block, true
		}
	}
	return Block{}, false
}

func tipOf(source string) (int64, string, bool) {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(strings.TrimSuffix(source, "/") + "/block")
	if err != nil {
		return 0, "", false
	}
	defer resp.Body.Close()
	var reply struct {
		Height int64  `json:"current_height"`
		Hash   string `json:"block_hash"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&reply) != nil {
		return 0, "", false
	}
	return reply.Height, reply.Hash, true
}

// watchBlocks follows `local` for `count` new blocks after `fromHeight` and
// times how long each takes to appear on every node in `others`. It gives up
// on a block's stragglers after `patience`.
func watchBlocks(local string, others []string, fromHeight int64, count int, patience, limit time.Duration, report func(filledBlock)) []filledBlock {
	var seen []filledBlock
	next := fromHeight + 1
	deadline := time.Now().Add(limit)
	for len(seen) < count && time.Now().Before(deadline) {
		height, _, ok := tipOf(local)
		if !ok || height < next {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		arrived := time.Now()
		block, ok := fetchBlockAt(local, next)
		if !ok {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		entry := filledBlock{Height: next, Hash: block.Hash, Bytes: blockSizeBytes(block), Transfers: len(block.Transactions) - 1, Delay: map[string]time.Duration{}}
		waiting := append([]string{}, others...)
		for len(waiting) > 0 && time.Since(arrived) < patience {
			var still []string
			for _, other := range waiting {
				if theirs, known := fetchBlockHash(other, next); known && theirs == block.Hash {
					entry.Delay[other] = time.Since(arrived)
				} else {
					still = append(still, other)
				}
			}
			waiting = still
			if len(waiting) > 0 {
				time.Sleep(150 * time.Millisecond)
			}
		}
		seen = append(seen, entry)
		if report != nil {
			report(entry)
		}
		next++
	}
	return seen
}

// watchAndReport follows the next `count` blocks, prints one line for each,
// and says whether every full block reached every seed in good time.
func watchAndReport(local string, seeds []string, fromHeight int64, count int) {
	fmt.Printf(" %-8s %-9s %-10s %s\n", "BLOCK", "BYTES", "TRANSFERS", "SECONDS UNTIL EACH SEED HAD IT")
	blocks := watchBlocks(local, seeds, fromHeight, count, 40*time.Second, time.Duration(count+4)*4*time.Minute, func(b filledBlock) {
		var parts []string
		for _, seed := range seeds {
			host := strings.TrimSuffix(strings.TrimPrefix(seed, "http://"), ":"+Params.syncPortText())
			if delay, ok := b.Delay[seed]; ok {
				parts = append(parts, fmt.Sprintf("%s %.1f", host, delay.Seconds()))
			} else {
				parts = append(parts, host+" NOT IN 40 s")
			}
		}
		if len(parts) == 0 {
			parts = []string{"(no seeds were named with --seeds)"}
		}
		fmt.Printf(" #%-7d %-9d %-10d %s\n", b.Height, b.Bytes, b.Transfers, strings.Join(parts, " | "))
	})
	var slowest time.Duration
	full, late := 0, 0
	for _, b := range blocks {
		if b.Bytes < Params.MaxBlockBytes*9/10 {
			continue
		}
		full++
		for _, seed := range seeds {
			delay, ok := b.Delay[seed]
			if !ok {
				late++
			} else if delay > slowest {
				slowest = delay
			}
		}
	}
	fmt.Println()
	switch {
	case full == 0:
		fmt.Println(" No full block was seen. Is this node mining, and was it the one that mined the next blocks?")
	case late > 0:
		fmt.Printf(" ❌ %d full block(s) seen. A seed did not have a full block within 40 seconds %d time(s). The block size is too large for this network as it is.\n", full, late)
	default:
		fmt.Printf(" ✅ %d full block(s) seen. The slowest seed had a full block %.1f seconds after this node (a block interval is %d seconds).\n", full, slowest.Seconds(), HeartbeatTargetSeconds)
	}
}

func runFillBlocksCommand(args []string) bool {
	count, err := strconv.Atoi(argumentValue(args, "--fill-blocks"))
	if err != nil || count < 1 {
		return false
	}
	if count > 20 {
		count = 20
	}
	if Params.Name == cvnTestnetParams.Name && Params.Anchor.BitcoinHeight <= 0 {
		fmt.Println("🚨 That network has not started.")
		return true
	}
	local := localSyncURLs()[0]
	if Params.ListenHost != "" {
		local = Params.syncURL(Params.ListenHost)
	}
	if given := argumentValue(args, "--node"); given != "" {
		local = strings.TrimSuffix(given, "/")
	}
	startHeight, _, ok := tipOf(local)
	if !ok {
		fmt.Printf("🚨 No node answered at %s. Start the miner on this machine first, on the same network.\n", local)
		return true
	}
	budget := blockTransferBudgetBytes()
	fmt.Println("====================================================================")
	fmt.Printf(" FILL %d BLOCK(S) ON %s\n", count, strings.ToUpper(Params.Name))
	fmt.Println("====================================================================")
	fmt.Printf(" A block holds up to %d bytes. The node at %s is at block #%d.\n", Params.MaxBlockBytes, local, startHeight)
	fmt.Println(" This sends coins from your wallet back to your own wallet, cut into")
	fmt.Println(" many small pieces, with no fee. Use the wallet's \"Combine\"")
	fmt.Println(" afterwards to put them back together.")
	fmt.Println()
	input := bufio.NewReader(os.Stdin)
	typed := readSecretLine(input, " Private key, 24 recovery words, or the saved key's password (not shown while typing): ")
	privateKeyHex, publicKeyHex, err := signingKeyFrom(typed)
	if err != nil {
		fmt.Println("❌ " + err.Error())
		return true
	}
	address, err := DeriveAddressFromPublicKey(publicKeyHex)
	if err != nil {
		fmt.Println("❌ " + err.Error())
		return true
	}
	address = strings.TrimSpace(address)

	var coins []SpendableCoin
	client := &http.Client{Timeout: 20 * time.Second}
	if resp, err := client.Get(local + "/utxos?address=" + address); err == nil {
		_ = json.NewDecoder(resp.Body).Decode(&coins)
		resp.Body.Close()
	}
	fmt.Printf(" Wallet %s holds %d coin(s) ready to spend.\n", address, len(coins))
	// No fee: transfers that pay may fill only four fifths of a block.
	const feeUnits = 0
	transfers, bytes, _ := fillTransfers(privateKeyHex, publicKeyHex, address, coins, budget*count, feeUnits)
	if bytes < budget {
		fmt.Printf("❌ These coins make only %d bytes of transfers, less than one full block (%d). Mine more blocks first: about %d coins are needed for each block to be filled.\n",
			bytes, budget, budget/5500+1)
		return true
	}
	fmt.Printf(" Built %d transfers, %d bytes: enough for %d full block(s).\n", len(transfers), bytes, bytes/budget)
	fmt.Print(" Type YES to send them: ")
	if answer, _ := input.ReadString('\n'); strings.TrimSpace(answer) != "YES" {
		fmt.Println(" Nothing was sent.")
		return true
	}
	sent := 0
	for _, tx := range transfers {
		if err := SubmitTransferOverHTTP(local, tx); err != nil {
			fmt.Printf(" ⚠️ A transfer was refused: %v\n", err)
			continue
		}
		sent++
	}
	fmt.Printf(" Sent %d transfers. Watching the next blocks (Ctrl+C stops watching; the transfers stay sent).\n\n", sent)

	seeds := seedSyncURLs()
	watchAndReport(local, seeds, startHeight, bytes/budget+2)

	// The wallet now holds its coins in many small pieces. Putting them back
	// together makes the other kind of full block: few transfers, each
	// spending the most coins a transfer may.
	fmt.Println()
	fmt.Print(" Put the pieces back together now? This fills a few more blocks. Type YES: ")
	if answer, _ := input.ReadString('\n'); strings.TrimSpace(answer) != "YES" {
		fmt.Println(" Left as they are. The wallet's \"Combine\" does the same at any time.")
		return true
	}
	coins = nil
	if resp, err := client.Get(local + "/utxos?address=" + address); err == nil {
		_ = json.NewDecoder(resp.Body).Decode(&coins)
		resp.Body.Close()
	}
	combines, combineBytes := combineBack(privateKeyHex, publicKeyHex, transfers, coins, feeUnits)
	privateKeyHex = ""
	if len(combines) == 0 {
		fmt.Println(" None of the pieces are confirmed yet. Use the wallet's \"Combine\" in a few blocks.")
		return true
	}
	againFrom, _, _ := tipOf(local)
	sent = 0
	for _, tx := range combines {
		if err := SubmitTransferOverHTTP(local, tx); err != nil {
			fmt.Printf(" ⚠️ A transfer was refused: %v\n", err)
			continue
		}
		sent++
	}
	fmt.Printf(" Sent %d transfers (%d bytes), each spending up to %d coins.\n\n", sent, combineBytes, TxMaxInputs)
	watchAndReport(local, seeds, againFrom, combineBytes/budget+2)
	return true
}
