package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// WailsWalletState represents the unified UI model transmitted directly to the frontend window view layer.
type WailsWalletState struct {
	WalletAddress     string               `json:"wallet_address"`
	WalletBalance     float64              `json:"wallet_balance"`
	CurrentBlockHeight int64               `json:"current_block_height"`
	CirculatingSupply float64              `json:"circulating_supply"`
	ActivePeerCount   int                  `json:"active_peer_count"`
	PeerRoster        []string             `json:"peer_roster"`
	MempoolQueueCount int                  `json:"mempool_queue_count"`
	NetworkDifficulty int64                `json:"network_difficulty"`
	RecentTransactions []WailsTxSnapshot   `json:"recent_transactions"`
}

// WailsTxSnapshot structures individual historical actions cleanly for visual UI list rendering.
type WailsTxSnapshot struct {
	TxID       string    `json:"tx_id"`
	Type       string    `json:"type"` // "COINBASE", "SPEND", "RECEIVE"
	Address    string    `json:"address"`
	Amount     float64   `json:"amount"`
	Offering   float64   `json:"offering"`
	Timestamp  int64     `json:"timestamp"`
}

// WalletDashboardContext coordinates Wails frontend lifecycle actions safely across threads.
type WalletDashboardContext struct {
	sync.RWMutex
	SelectedAddress string
}

// NewWalletDashboardContext instantiates a clean dashboard app routing context frame.
func NewWalletDashboardContext(defaultAddress string) *WalletDashboardContext {
	return &WalletDashboardContext{
		SelectedAddress: defaultAddress,
	}
}

// SetActiveWallet updates the active tracking focus whenever a user toggles profiles in the UI.
func (wd *WalletDashboardContext) SetActiveWallet(newAddress string) {
	wd.Lock()
	defer wd.Unlock()
	wd.SelectedAddress = strings.TrimSpace(newAddress)
}

// GetLiveDashboardTelemetry pulls states across our storage backplane and packs them into a single UI data payload.
func (wd *WalletDashboardContext) GetLiveDashboardTelemetry() (string, error) {
	wd.RLock()
	targetAddress := wd.SelectedAddress
	wd.RUnlock()

	// 1. Fetch live metrics directly from your microsecond memory cache matrix
	currentBalance := GetAddressBalance(targetAddress)

	// 2. Read latest tail tip info out of the binary BoltDB allocations
	latestBlock := GetLatestBlock()
	currentHeight := latestBlock.Index
	currentDifficulty := latestBlock.Difficulty

	// 3. Compute network-wide operational state summaries
	fullChain := LoadFullChainSlice()
	var totalMined float64 = 0.0
	var totalBurned float64 = 0.0
	var visualTxSnapshots []WailsTxSnapshot

	if len(fullChain) > 1 {
		totalMined = float64(len(fullChain)-1) * 50.0
	}

	for _, block := range fullChain {
		for _, tx := range block.Transactions {
			// Process global burn void track states
			for _, out := range tx.Outputs {
				if out.Recipient == BurnAddress {
					totalBurned += out.Amount
				}
			}

			// Filter out and capture the 10 most recent transaction paths involving our active address profile
			isCoinbase := strings.HasPrefix(tx.ID, "TX_COINBASE") || strings.HasPrefix(tx.ID, "COINBASE_")
			
			if isCoinbase {
				for _, out := range tx.Outputs {
					if out.Recipient == targetAddress && len(visualTxSnapshots) < 10 {
						visualTxSnapshots = append(visualTxSnapshots, WailsTxSnapshot{
							TxID:      tx.ID,
							Type:      "COINBASE",
							Address:   "NETWORK_MINING_POOL",
							Amount:    out.Amount,
							Offering:  0.0,
							Timestamp: block.Timestamp,
						})
					}
				}
			} else {
				// Process standard user spend and receive delta actions
				if tx.Witness == targetAddress && len(visualTxSnapshots) < 10 {
					var totalSent float64
					var recipientList []string
					for _, out := range tx.Outputs {
						totalSent += out.Amount
						recipientList = append(recipientList, out.Recipient)
					}
					visualTxSnapshots = append(visualTxSnapshots, WailsTxSnapshot{
						TxID:      tx.ID,
						Type:      "SPEND",
						Address:   strings.Join(recipientList, ", "),
						Amount:    totalSent,
						Offering:  tx.FreeWillOffering,
						Timestamp: block.Timestamp,
					})
				} else {
					for _, out := range tx.Outputs {
						if out.Recipient == targetAddress && len(visualTxSnapshots) < 10 {
							visualTxSnapshots = append(visualTxSnapshots, WailsTxSnapshot{
								TxID:      tx.ID,
								Type:      "RECEIVE",
								Address:   tx.Witness,
								Amount:    out.Amount,
								Offering:  tx.FreeWillOffering,
								Timestamp: block.Timestamp,
							})
						}
					}
				}
			}
		}
	}

	circulatingSupply := totalMined - totalBurned
	if circulatingSupply < 0 {
		circulatingSupply = 0
	}

	// 4. Capture current P2P Gossip Mesh routing statistics
	RosterMutex.Lock()
	peerCount := len(ActivePeerRoster)
	peerList := make([]string, peerCount)
	copy(peerList, ActivePeerRoster)
	RosterMutex.Unlock()

	// 5. Query active memory pool queue streams
	var mempoolCount int
	if MempoolMatrix != nil {
		MempoolMatrix.RLock()
		mempoolCount = len(MempoolMatrix.PriorityChamber) + len(MempoolMatrix.ZeroFeeChamber)
		MempoolMatrix.RUnlock()
	}

	// 6. Build the unified dashboard payload state model
	statePayload := WailsWalletState{
		WalletAddress:     targetAddress,
		WalletBalance:     currentBalance,
		CurrentBlockHeight: currentHeight,
		CirculatingSupply: circulatingSupply,
		ActivePeerCount:   peerCount,
		PeerRoster:        peerList,
		MempoolQueueCount: mempoolCount,
		NetworkDifficulty: currentDifficulty,
		RecentTransactions: visualTxSnapshots,
	}

	// Serialize model cleanly into a JSON string stream for Wails front-end runtime binding consumption
	jsonBytes, err := json.Marshal(statePayload)
	if err != nil {
		return "", fmt.Errorf("failed to serialize dashboard telemetry model data layout: %v", err)
	}

	return string(jsonBytes), nil
}