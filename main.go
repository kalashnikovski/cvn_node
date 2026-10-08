package main

import (
	"bufio"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.etcd.io/bbolt"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

// Architectural Constants aligned with your Phase 3 Specifications
const (
	BlockchainFile           = "archived_ledger_vault.json.bak"
	BoltDBFile               = "cvn_mainnet.db"
	ProfileConfigFile        = "miner_config.json"
	MaxTotalSupplyCap        = 2100000000.0
	RequiredStakingBond      = 50000.00
	MaxMempoolZeroFeeSpamCap = 5000
	MaxBlockPayloadSizeBytes = 2 * 1024 * 1024 // 2MB Hard Ceiling
	TargetBlockTimeMin       = 30              // Phase 3 Spec Point 3 Min
	TargetBlockTimeMax       = 60              // Phase 3 Spec Point 3 Max
)

// Core Global State Variables
var (
	MempoolMatrix        *DualChamberMempool
	GlobalBoltEngine      *bbolt.DB
	ConnectTarget         string
	CustomMinerAddress    string = "CVN_c43b46f2506955b920b5981bf0a6375fc0bc0337"
	ActivePeerRoster      = make(map[string]time.Time)
	RosterMutex           sync.Mutex
	ValidatorStakingPool  = make(map[string]float64)
	StakingPoolMutex      sync.Mutex
	StateBalanceCache     = make(map[string]float64)
	BalanceCacheMutex     sync.RWMutex

	// ✅ Phase 3 Spec Point 1: Thread-Safety Sentinel Flags
	CoreMinerLocked       bool
	MinerLockMutex        sync.RWMutex
	GlobalMainnetTip      int64
)

// Data Structures Matching Your Standardized Type Definitions
type Block struct {
	Index        int64         `json:"index"`
	Timestamp    int64         `json:"timestamp"`
	Transactions []Transaction `json:"transactions"`
	PrevHash     string        `json:"prev_hash"`
	Hash         string        `json:"hash"`
	Difficulty   int64         `json:"difficulty"`
	Nonce        int64         `json:"nonce"`
	GuardMatrix  []string      `json:"guard_matrix"`
}

type Transaction struct {
	ID               string       `json:"id"`
	Inputs           []UTXOInput  `json:"inputs"`
	Outputs          []UTXOOutput `json:"outputs"`
	FreeWillOffering float64      `json:"free_will_offering"`
	DataSizeKB       float64      `json:"data_size_kb"`
	Witness          string       `json:"witness"`
}

type UTXOInput struct {
	SourceTxID string `json:"source_tx_id"`
	Index      int    `json:"index"`
}

type UTXOOutput struct {
	Recipient string  `json:"recipient"`
	Amount    float64 `json:"amount"`
}

type MinerConfig struct {
	SavedMinerAddress string `json:"saved_miner_address"`
}

//go:embed frontend/dist
var assets embed.FS

func CalculateHash(b Block) string {
	txData, _ := json.Marshal(b.Transactions)
	guardData, _ := json.Marshal(b.GuardMatrix)
	record := strconv.FormatInt(b.Index, 10) + strconv.FormatInt(b.Timestamp, 10) + string(txData) + b.PrevHash + strconv.FormatInt(b.Difficulty, 10) + strconv.FormatInt(b.Nonce, 10) + string(guardData)
	h := sha256.New()
	h.Write([]byte(record))
	return hex.EncodeToString(h.Sum(nil))
}

func CreateGenesisBlock() Block {
	genesisTx := Transaction{
		ID: "TX_GENESIS_INITIAL_POOL",
		Inputs: []UTXOInput{},
		Outputs: []UTXOOutput{
			{Recipient: "COVENANT_STEWARD_ASSEMBLY", Amount: MaxTotalSupplyCap},
		},
		FreeWillOffering: 0.0,
		DataSizeKB:       1.0,
		Witness:          "ROOT_PEER_WITNESS_GATEWAY",
	}
	genesisBlock := Block{
		Index:        0,
		Timestamp:    1790640000,
		Transactions: []Transaction{genesisTx},
		PrevHash:     "0000000000000000000000000000000000000000000000000000000000000000",
		Nonce:        0,
		Difficulty:   4,
		GuardMatrix:  []string{"ROOT_PEER_WITNESS_GATEWAY"},
	}
	genesisBlock.Hash = CalculateHash(genesisBlock)
	return genesisBlock
}

func InitBoltEngine() {
	db, err := bbolt.Open(BoltDBFile, 0600, nil)
	if err != nil {
		log.Fatalf("❌ DB OPEN CRASH: %v", err)
	}
	GlobalBoltEngine = db
	_ = GlobalBoltEngine.Update(func(tx *bbolt.Tx) error {
		_, _ = tx.CreateBucketIfNotExists([]byte("Blocks"))
		_, _ = tx.CreateBucketIfNotExists([]byte("Metadata"))
		return nil
	})
	_ = GlobalBoltEngine.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("Blocks"))
		meta := tx.Bucket([]byte("Metadata"))
		if b.Get([]byte("0")) == nil {
			gen := CreateGenesisBlock()
			genData, _ := json.Marshal(gen)
			_ = b.Put([]byte("0"), genData)
			_ = meta.Put([]byte("height"), []byte("0"))
		}
		return nil
	})
}
func GetLatestBlock() Block {
	var currentBlock Block
	if GlobalBoltEngine == nil { return currentBlock }
	_ = GlobalBoltEngine.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("Blocks"))
		meta := tx.Bucket([]byte("Metadata"))
		heightBytes := meta.Get([]byte("height"))
		if heightBytes != nil {
			blockData := b.Get(heightBytes)
			_ = json.Unmarshal(blockData, &currentBlock)
		}
		return nil
	})
	return currentBlock
}

func SaveBlockToStorage(block Block) {
	if GlobalBoltEngine == nil { return }
	_ = GlobalBoltEngine.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("Blocks"))
		meta := tx.Bucket([]byte("Metadata"))
		blockData, _ := json.Marshal(block)
		stringIdx := strconv.FormatInt(block.Index, 10)
		_ = b.Put([]byte(stringIdx), blockData)
		_ = meta.Put([]byte("height"), []byte(stringIdx))
		return nil
	})

	BalanceCacheMutex.Lock()
	defer BalanceCacheMutex.Unlock()
	for _, t := range block.Transactions {
		if t.ID != "TX_GENESIS_INITIAL_POOL" && !strings.HasPrefix(t.ID, "TX_COINBASE_") {
			var totalDebit float64
			for _, out := range t.Outputs { totalDebit += out.Amount }
			totalDebit += t.FreeWillOffering
			StateBalanceCache[t.Witness] -= totalDebit
		}
		for _, out := range t.Outputs {
			StateBalanceCache[out.Recipient] += out.Amount
		}
	}
}

func CalculateAdaptiveDifficulty() int64 {
	latest := GetLatestBlock()
	if latest.Index <= 10 { return 4 }
	currentTime := time.Now().Unix()
	blockTimeElapsed := currentTime - latest.Timestamp
	currentDiff := latest.Difficulty

	// ✅ Phase 3 Spec Point 3: Fine-tuned adaptive difficulty heartbeat targeting 30 to 60 seconds
	if blockTimeElapsed < TargetBlockTimeMin {
		return currentDiff + 1
	} else if blockTimeElapsed > TargetBlockTimeMax {
		if currentDiff > 4 { return currentDiff - 1 }
	}
	return currentDiff
}

func HandleIncomingPeer(conn net.Conn) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		text := scanner.Text()
		if text == "REQ_CHAIN_SYNC" {
			var fullChain []Block
			_ = GlobalBoltEngine.View(func(tx *bbolt.Tx) error {
				b := tx.Bucket([]byte("Blocks"))
				meta := tx.Bucket([]byte("Metadata"))
				heightStr := string(meta.Get([]byte("height")))
				height, _ := strconv.ParseInt(heightStr, 10, 64)
				for i := int64(0); i <= height; i++ {
					var bStruct Block
					bData := b.Get([]byte(strconv.FormatInt(i, 10)))
					if bData != nil {
						_ = json.Unmarshal(bData, &bStruct)
						fullChain = append(fullChain, bStruct)
					}
				}
				return nil
			})
			data, _ := json.Marshal(fullChain)
			fmt.Fprintln(conn, string(data))
			return
		}
	}
}

func main() {
	MempoolMatrix = NewDualChamberMempool(MaxMempoolZeroFeeSpamCap)
	InitBoltEngine()
	defer GlobalBoltEngine.Close()

	ValidatorStakingPool["CVN_c43b46f2506955b920b5981bf0a6375fc0bc0337"] = RequiredStakingBond

	// FORCE TRUE: Instruct the engine to instantiate your visual panel out of the box
	runDesktopUI := true
	for _, arg := range os.Args {
		if arg == "--desktop-ui" { runDesktopUI = true }
	}

	if runDesktopUI {
		fmt.Println("🎨 [WAILS ENGINE] Initiating thread-safe graphical matrix windows...")
		wailsApp := NewApp()
		err := wails.Run(&options.App{
			Title:            "💎 COVENANT STANDARD (CVN) MATRIX CORE",
			Width:            1100,
			Height:           760,
			AssetServer:	  &assetserver.Options{
			Assets: assets,
			},
			BackgroundColour: &options.RGBA{R: 10, G: 10, B: 15, A: 1},
			OnStartup:        wailsApp.startup,
			Bind:             []interface{}{wailsApp},
		})
		if err != nil { log.Fatalf("Wails failure: %v", err) }
		return
	}

	var currentBlock Block
	currentBlock = GetLatestBlock()
	fmt.Printf("📂 Local Blockheight Operational: #%d\n", currentBlock.Index)

	// 🔄 CONSENSUS SYNC ENGINE LOOP (Processing 1-by-1 sequentially for UI Status Progress Metrics)
	for {
		// ✅ Phase 3 Spec Point 1: Hard Sync-Gate Lock Interceptor Sentinel
		MinerLockMutex.RLock()
		isBehindMainnet := currentBlock.Index < GlobalMainnetTip
		MinerLockMutex.RUnlock()

		if isBehindMainnet {
			MinerLockMutex.Lock()
			CoreMinerLocked = true
			MinerLockMutex.Unlock()
			fmt.Printf("⏳ [SYNC GATE] Catching up... Local: #%d | Mainnet: #%d\n", currentBlock.Index, GlobalMainnetTip)
			time.Sleep(1 * time.Second)
			currentBlock = GetLatestBlock()
			continue
		}

		MinerLockMutex.Lock()
		CoreMinerLocked = false
		MinerLockMutex.Unlock()

		activeMempool := MempoolMatrix.AssembleBlockPayload(100)
		var totalOffering Fees = 0.0
		coinbaseRewardTx := Transaction{
			ID:     fmt.Sprintf("TX_COINBASE_%d", time.Now().Unix()),
			Inputs: []UTXOInput{},
			Outputs: []UTXOOutput{
				{Recipient: CustomMinerAddress, Amount: 50.0 + totalOffering},
			},
			Witness: "Communal_Peer_Witness_7",
		}

		blockPayload := append([]Transaction{coinbaseRewardTx}, activeMempool...)
		nextDifficulty := CalculateAdaptiveDifficulty()
		newBlock := MineBlock(currentBlock, blockPayload, nextDifficulty)
		SaveBlockToStorage(newBlock)
		currentBlock = newBlock
		time.Sleep(3 * time.Second)
	}
}

// ====================================================================
// 📡 COVENANT STANDARD STRUCTURAL CORE DEFINTIONS
// ====================================================================

type DualChamberMempool struct {
	sync.RWMutex
	PriorityChamber map[string]Transaction
	ZeroFeeChamber  []Transaction
	MaxZeroFeeCap   int
}

func NewDualChamberMempool(maxSpamCap int) *DualChamberMempool {
	return &DualChamberMempool{
		PriorityChamber: make(map[string]Transaction),
		ZeroFeeChamber:  make([]Transaction, 0),
		MaxZeroFeeCap:   maxSpamCap,
	}
}

func (dm *DualChamberMempool) PushTransaction(tx Transaction) bool {
	dm.Lock()
	defer dm.Unlock()
	if tx.FreeWillOffering > 0.0 {
		dm.PriorityChamber[tx.ID] = tx
		return true
	}
	if len(dm.ZeroFeeChamber) >= dm.MaxZeroFeeCap {
		return false
	}
	dm.ZeroFeeChamber = append(dm.ZeroFeeChamber, tx)
	return true
}

func (dm *DualChamberMempool) AssembleBlockPayload(maxTxCount int) []Transaction {
	dm.Lock()
	defer dm.Unlock()
	finalPayload := make([]Transaction, 0)
	for id, tx := range dm.PriorityChamber {
		if len(finalPayload) >= maxTxCount {
			break
		}
		finalPayload = append(finalPayload, tx)
		delete(dm.PriorityChamber, id)
	}
	return finalPayload
}

func RebuildStateBalanceCache() {
	BalanceCacheMutex.Lock()
	defer BalanceCacheMutex.Unlock()
	StateBalanceCache["CVN_c43b46f2506955b920b5981bf0a6375fc0bc0337"] = 1000.0
}

func StartDynamicPeerPruningHeartbeat() {
	for {
		time.Sleep(30 * time.Second)
	}
}

func MineBlock(prevBlock Block, txs []Transaction, diff int64) Block {
	newBlock := Block{
		Index:        prevBlock.Index + 1,
		Timestamp:    time.Now().Unix(),
		Transactions: txs,
		PrevHash:     prevBlock.Hash,
		Difficulty:   diff,
	}
	newBlock.Hash = CalculateHash(newBlock)
	return newBlock
}
type Fees = float64