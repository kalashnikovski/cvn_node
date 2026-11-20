package node

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
)

// WHAT A NODE ANSWERS OVER HTTP
//
// A node answers two sets of requests.
//
// The first is what other nodes and wallets ask for: blocks in batches for a
// node that is catching up, the newest block and the total work behind it
// for a node comparing chains, the coins at an address for a wallet building
// a transfer, and a finished transfer handed in to be passed on. A browser
// asking for the first page gets the block explorer. Everything here is
// read from the chain or checked against it; a node never signs anything
// for whoever asks.
//
// The second is one small page that says the node is up and how far it has
// got, for a wallet on the same machine and for whoever keeps the node
// running.

// syncAPI is the first set.
func syncAPI() *http.ServeMux {
	winMux := http.NewServeMux()

	winMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.URL.Path == "/addresses" {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Access-Control-Allow-Origin", "*")

			// A full recount reads every coin. Anyone can ask for this
			// page, as often as they like, so it is recounted at most
			// every few seconds; the running totals are kept up to date
			// between recounts as blocks arrive.
			recountBalancesIfStale()

			BalanceCacheMutex.RLock()
			activeWallets := make(map[string]float64)
			for addr, bal := range StateBalanceCache {
				if bal > 0 {
					activeWallets[addr] = UnitsAmount(bal).CVN()
				}
			}
			BalanceCacheMutex.RUnlock()

			json.NewEncoder(w).Encode(activeWallets)
			return
		}
		if r.URL.Path == "/peers" {
			w.Header().Set("Content-Type", "application/json")
			PeerMapMutex.RLock()
			peerList := make([]string, 0)
			for peerIP := range ActivePeerMap {
				peerList = append(peerList, peerIP)
			}
			PeerMapMutex.RUnlock()
			if len(peerList) == 0 {
				peerList = seedGossipAddresses()
			}
			json.NewEncoder(w).Encode(peerList)
			return
		}

		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			htmlBytes, err := os.ReadFile("explorer.html")
			if err != nil {
				StateMutex.RLock()
				idx := GetLatestBlockInternal().Index
				StateMutex.RUnlock()
				fmt.Fprintf(w, "<h2>Covenant Standard node</h2><p>Height: #%d. (The explorer page, explorer.html, is not beside the program.)</p>", idx)
				return
			}
			w.Write(htmlBytes)
			return
		}

		http.NotFound(w, r)
	})

	winMux.HandleFunc("/blocks/batch", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")

		startStr := r.URL.Query().Get("start")
		startHeight, err := strconv.ParseInt(startStr, 10, 64)
		if err != nil || startHeight < 0 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		StateMutex.RLock()
		currentTip := GetLatestBlockInternal().Index
		StateMutex.RUnlock()
		blocks := blocksForBatch(startHeight, currentTip)
		json.NewEncoder(w).Encode(blocks)
	})

	// Height, identifier and total work of the newest block, without its
	// transactions: what a node comparing chains needs, at a fraction of
	// the size of /block.
	winMux.HandleFunc("/tip", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")
		latest := GetLatestBlockInternal()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"current_height": latest.Index,
			"block_hash":     latest.Hash,
			"timestamp":      latest.Timestamp,
			"total_work":     chainWorkThrough(latest.Index).String(),
		})
	})

	// The identifier of the block at one height: a small answer for a
	// node working out where its chain parts from this one.
	winMux.HandleFunc("/blockhash", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")
		height, err := strconv.ParseInt(r.URL.Query().Get("height"), 10, 64)
		if err != nil || height < 0 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		block := GetBlockByHeightFromDB(height)
		if block.Hash == "" || block.Index != height {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"height": height, "hash": block.Hash})
	})

	winMux.HandleFunc("/block", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		latest := GetLatestBlockInternal()
		races, racedOver, racesSince := blockRaceCount(latest.Index)
		payload := map[string]interface{}{
			"current_height": latest.Index,
			"block_hash":     latest.Hash,
			"timestamp":      latest.Timestamp,
			// Heights at which this node saw two blocks compete, out of
			// how many blocks, since it started (see block_races.go).
			"races":        races,
			"races_blocks": racedOver,
			"races_since":  racesSince.Unix(),
			"transactions": latest.Transactions,
			// Total mining work in this node's chain; peers compare chains by it.
			"total_work": chainWorkThrough(latest.Index).String(),
			// Units held by the vault (always 0 on a network without one).
			"vault_units": VaultBalanceUnits(),
		}
		json.NewEncoder(w).Encode(payload)
	})

	// A node only ever relays transfers that arrive already signed by the
	// owner of the coins. It never builds or signs one on a caller's behalf.
	winMux.HandleFunc("/transaction", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var tx Transaction
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256*1024)).Decode(&tx); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "transaction is not valid JSON"})
			return
		}
		if err := AdmitTransactionToMempool(tx); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		go BroadcastNewTransaction(tx)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"status": "accepted", "tx_id": tx.ID})
	})

	// Unspent coins of one address, for wallets building a transfer.
	winMux.HandleFunc("/utxos", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ListSpendableCoins(r.URL.Query().Get("address")))
	})
	// The balance of one address.
	winMux.HandleFunc("/audit", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "")
		w.Header().Set("Content-Type", "application/json")
		targetAddress := r.URL.Query().Get("address")
		if targetAddress == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		BalanceCacheMutex.RLock()
		balance, exists := StateBalanceCache[targetAddress]
		BalanceCacheMutex.RUnlock()
		payload := map[string]interface{}{
			"address": targetAddress,
			// balance is in CVN, for display; balance_units is the exact figure.
			"balance":        UnitsAmount(balance).CVN(),
			"balance_units":  balance,
			"exists":         exists,
			"account_status": "VALIDATED_ON_CHAIN",
		}
		json.NewEncoder(w).Encode(payload)
	})
	return winMux
}

// serveSyncAPI answers the first set for as long as the program runs.
func serveSyncAPI() {
	pages := syncAPI()
	fmt.Printf("📡 [PORT %d] Initializing HTTP Bulk Sync api channels...\n", Params.SyncPort)
	if err := serveHTTP(listenAddress(Params.SyncPort), pages); err != nil {
		if runtime.GOOS == "windows" {
			_ = serveHTTP(listenAddress(Params.SyncFallbackPort), pages)
		}
	}
}

// statusAPI is the second set.
func statusAPI() *http.ServeMux {
	mirrorMux := http.NewServeMux()
	mirrorMux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "")
		w.Header().Set("Content-Type", "application/json")
		StateMutex.RLock()
		tip := GetLatestBlockInternal()
		StateMutex.RUnlock()
		races, racedOver, racesSince := blockRaceCount(tip.Index)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":   "ONLINE",
			"height":   tip.Index,
			"tip_hash": tip.Hash,
			// Heights at which this node saw two blocks compete, out of how
			// many blocks, since it started (see block_races.go).
			"races":        races,
			"races_blocks": racedOver,
			"races_since":  racesSince.Unix(),
			// The address this node mines to, so a wallet on the same machine can
			// show the right balance by default.
			"miner_address": strings.TrimSpace(CustomMinerAddress),
		})
	})
	return mirrorMux
}
