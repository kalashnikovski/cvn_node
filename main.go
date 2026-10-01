package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

const BlockchainFile = "ledger_vault.json"
const ProfileConfigFile = "miner_config.json"
const TargetBlockTime = 10
const MaxTotalSupplyCap = 2100000000.0
const JubileeTimeWindow = 49 * 365 * 24 * 60 * 60 
const BurnAddress = "0x0000000000000000000000000000000000000000_BURN_VOID"
const CreatorTargetAddress = "CVN_c43b46f2506955b920b5981bf0a6375fc0bc0337"
const CityOfRefugeWindow = 72 * 60 * 60 

const RequiredStakingBond = 500.00 
const SlasherPenaltyRate  = 1.00   

type UTXOInput struct {
	TxID      string `json:"tx_id"`
	OutputIdx int    `json:"output_idx"`
	Signature string `json:"signature"`
}

type UTXOOutput struct {
	Recipient string  `json:"recipient"`
	Amount    float64 `json:"amount"`
}

type Transaction struct {
	ID               string       `json:"id"`
	Inputs           []UTXOInput  `json:"inputs"`
	Outputs          []UTXOOutput `json:"outputs"`
	FreeWillOffering float64      `json:"free_will_offering"`
	DataSizeKB       float64      `json:"data_size_kb"`
	Witness          string       `json:"witness"`
	SignatureR       string       `json:"signature_r,omitempty"`
	SignatureS       string       `json:"signature_s,omitempty"`
}

type Block struct {
	Index        int64         `json:"index"`
	Timestamp    int64         `json:"timestamp"`
	Transactions []Transaction `json:"transactions"`
	PrevHash     string        `json:"prev_hash"`
	Hash         string        `json:"hash"`
	Nonce        int64         `json:"nonce"`
	Difficulty   int64         `json:"difficulty"`
	GuardMatrix  []string      `json:"guard_matrix,omitempty"`
}

type MinerConfig struct {
	SavedMinerAddress string `json:"saved_miner_address"`
}

var (
	GlobalMempool []Transaction
	MempoolMutex  sync.Mutex
	ConnectTarget string 
	
	CustomMinerAddress string = "Nikola_Global_Network_Node" 
	
	ActivePeerRoster []string
	RosterMutex      sync.Mutex
	LocalListenerIP  string = "202.137.175.220" 
	
	ValidatorStakingPool map[string]float64
	StakingPoolMutex     sync.Mutex
)

var NetworkWitnessRoster = []string{
	"Peer_Witness_1", "Peer_Witness_2", "Peer_Witness_3", "Peer_Witness_4", "Peer_Witness_5",
	"Peer_Witness_6", "Communal_Peer_Witness_7", "Peer_Witness_8", "Peer_Witness_9", "Peer_Witness_10",
	"Witness_Alpha", "Witness_Beta", "Witness_Gamma", "Witness_Delta", "Witness_Epsilon",
	"Validator_Secure_A", "Validator_Secure_B", "Validator_Secure_C", "Validator_Secure_D", "Validator_Secure_E",
	"Node_Guardian_Prime", "Node_Guardian_Secure", "Root_Gateway_Echo", "Sovereign_State_Validator",
}

func CalculateHash(b Block) string {
	record := fmt.Sprintf("%d%d%v%s%d%d%v", b.Index, b.Timestamp, b.Transactions, b.PrevHash, b.Nonce, b.Difficulty, b.GuardMatrix)
	h := sha256.New()
	h.Write([]byte(record))
	return fmt.Sprintf("%x", h.Sum(nil))
}

func CreateGenesisBlock() Block {
	genesisTx := Transaction{
		ID:     "TX_GENESIS_INITIAL_POOL",
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

func SaveChain(chain []Block) {
	data, _ := json.MarshalIndent(chain, "", "  ")
	_ = os.WriteFile(BlockchainFile, data, 0644)
}

func VerifyGenesisFreeze(chain []Block) bool {
	if len(chain) == 0 { return false }
	if len(chain[0].Transactions) == 0 { return false }
	return true
}

func LoadChain() []Block {
	if _, err := os.Stat(BlockchainFile); os.IsNotExist(err) {
		chain := []Block{CreateGenesisBlock()}
		SaveChain(chain)
		return chain
	}
	data, _ := os.ReadFile(BlockchainFile)
	var chain []Block
	_ = json.Unmarshal(data, &chain)
	if !VerifyGenesisFreeze(chain) {
		fmt.Println("🚨 GENESIS FREEZE LEAK")
		os.Exit(1)
	}
	return chain
}

func GetAddressBalance(chain []Block, address string) float64 {
	var totalReceived float64 = 0.0
	var totalSpent float64 = 0.0

	for _, block := range chain {
		for _, tx := range block.Transactions {
			// Track all inbound unspent clump receipts
			for _, out := range tx.Outputs {
				if out.Recipient == address {
					totalReceived += out.Amount
				}
			}
			// Track all outbound spent inputs
			for _, in := range tx.Inputs {
				// Resolve internal values from historical transactions
				for _, bHistory := range chain {
					for _, txHistory := range bHistory.Transactions {
						if txHistory.ID == in.TxID {
							if in.OutputIdx < len(txHistory.Outputs) {
								outTarget := txHistory.Outputs[in.OutputIdx]
								if outTarget.Recipient == address {
									totalSpent += outTarget.Amount
								}
							}
						}
					}
				}
			}
		}
	}
	balance := totalReceived - totalSpent
	if balance < 0 { return 0 }
	return balance
}

func CalculateAdaptiveDifficulty(chain []Block) int64 {
	if len(chain) < 2 { return 4 }
	latestBlock := chain[len(chain)-1]
	prevBlock := chain[len(chain)-2]
	actualTimeElapsed := latestBlock.Timestamp - prevBlock.Timestamp
	currentDiff := latestBlock.Difficulty

	if currentDiff > 6 { currentDiff = 6 }
	if currentDiff < 3 { currentDiff = 3 }

	if actualTimeElapsed < TargetBlockTime {
		if currentDiff < 6 { return currentDiff + 1 }
	} else if actualTimeElapsed > (TargetBlockTime * 3) {
		if currentDiff > 3 { return currentDiff - 1 }
	}
	return currentDiff
}

func RegisterGossipPeer(peerAddr string) {
	if peerAddr == "" || strings.HasPrefix(peerAddr, "127.0.0.1") || strings.HasPrefix(peerAddr, "0.0.0.0") {
		return
	}
	RosterMutex.Lock()
	defer RosterMutex.Unlock()
	for _, existing := range ActivePeerRoster {
		if existing == peerAddr { return }
	}
	ActivePeerRoster = append(ActivePeerRoster, peerAddr)
	fmt.Printf("🛰️  [Gossip Mesh Network] Connected new mesh node to routing tables: %s\n", peerAddr)
}

func ExecuteSabbaticalSlash(validatorAddress string, reason string) {
	StakingPoolMutex.Lock()
	stakedAmount := ValidatorStakingPool[validatorAddress]
	if stakedAmount > 0 {
		seizedBalance := stakedAmount * SlasherPenaltyRate
		ValidatorStakingPool[validatorAddress] = 0 
		StakingPoolMutex.Unlock()
		fmt.Printf("⚡ [Sabbatical Slasher] MALICIOUS ACT DETECTED (%s)! Slashing address %s. Seizing %.2f CVN bond to Burn Void!\n", reason, validatorAddress, seizedBalance)
	} else {
		StakingPoolMutex.Unlock()
	}
}

func HandleIncomingPeer(conn net.Conn) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		text := scanner.Text()
		if text == "REQ_CHAIN_SYNC" {
			chain := LoadChain()
			data, _ := json.Marshal(chain)
			fmt.Fprintln(conn, string(data))
			return
		}
		if strings.HasPrefix(text, "GOSSIP_PEER_DISCOVERY:") {
			incomingNodeAddress := strings.TrimPrefix(text, "GOSSIP_PEER_DISCOVERY:")
			RegisterGossipPeer(incomingNodeAddress)
			RosterMutex.Lock()
			rosterJSON, _ := json.Marshal(ActivePeerRoster)
			RosterMutex.Unlock()
			fmt.Fprintln(conn, string(rosterJSON))
			return
		}
		if strings.HasPrefix(text, "TX_BROADCAST:") {
			payload := strings.TrimPrefix(text, "TX_BROADCAST:")
			var tx Transaction
			if err := json.Unmarshal([]byte(payload), &tx); err == nil {
				MempoolMutex.Lock()
				GlobalMempool = append(GlobalMempool, tx)
				fmt.Printf("📥 [P2P Network Engine] Ingested verified cryptographic UTXO transaction! ID: %s\n", tx.ID)
				MempoolMutex.Unlock()
				fmt.Fprintln(conn, "TX_ACCEPTED")
			} else {
				fmt.Fprintln(conn, "TX_REJECTED_MALFORMED")
			}
			return
		}
	}
}

func StartTCPServer() {
	listener, err := net.Listen("tcp", "0.0.0.0:8080")
	if err != nil {
		fmt.Printf("🚨 TCP Server Bind Error: %v\n", err)
		return
	}
	defer listener.Close()
	fmt.Println("📡 Global TCP P2P Subnetwork Engine Online. Listening for incoming miners on Port :8080...")
	for {
		conn, err := listener.Accept()
		if err != nil { continue }
		go HandleIncomingPeer(conn)
	}
}

func StartPublicExplorerServer() {
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "" && r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		http.ServeFile(w, r, "explorer.html")
	})

	mux.HandleFunc("/audit", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		http.ServeFile(w, r, "audit.html")
	})

	mux.HandleFunc("/req_chain", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")
		chain := LoadChain()
		var totalMined float64 = 0.0
		var burnedTokens float64 = 0.0
		
		if len(chain) > 1 { totalMined = float64(len(chain)-1) * 50.0 }
		for _, block := range chain {
			for _, tx := range block.Transactions {
				for _, out := range tx.Outputs {
					if out.Recipient == BurnAddress {
						burnedTokens += out.Amount
					}
				}
			}
		}
		circulatingSupply := totalMined - burnedTokens
		if circulatingSupply < 0 { circulatingSupply = 0 }

		var totalRealEscrow float64 = 0.0
		StakingPoolMutex.Lock()
		for _, bond := range ValidatorStakingPool { totalRealEscrow += bond }
		StakingPoolMutex.Unlock()

		responseData := map[string]interface{}{
			"circulating_supply": circulatingSupply,
			"blocks":             chain,
			"escrow_balance":     totalRealEscrow,
		}
		data, _ := json.Marshal(responseData)
		w.Write(data)
	})

	fmt.Println("🌐 Public Block Explorer Server Online. Hosting dashboard live on http://localhost:8081...")
go func() { _ = http.ListenAndServe("0.0.0.0:8081", mux) }()
}
func DialAndGossipWithSeedPeer(seedAddr string) {
conn, err := net.DialTimeout("tcp", seedAddr, 5*time.Second)
if err != nil { return }
defer conn.Close()
client := &http.Client{Timeout: 3 * time.Second}
resp, httpErr := client.Get("ipify.org")
myExtIP := LocalListenerIP
if httpErr == nil {
defer resp.Body.Close()
ipBytes, _ := io.ReadAll(resp.Body)
myExtIP = strings.TrimSpace(string(ipBytes))
}
fmt.Fprintln(conn, "GOSSIP_PEER_DISCOVERY:"+myExtIP+":8080")
respLine, err := bufio.NewReader(conn).ReadString('\n')
if err == nil {
var sharedRoster []string
if json.Unmarshal([]byte(strings.TrimSpace(respLine)), &sharedRoster) == nil {
for _, externalNode := range sharedRoster { RegisterGossipPeer(externalNode) }
}
}
}
func SyncChainFromSeedPeer(seedAddr string) {
conn, err := net.DialTimeout("tcp", seedAddr, 5*time.Second)
if err != nil { return }
defer conn.Close()
fmt.Fprintln(conn, "REQ_CHAIN_SYNC")
respBytes, err := bufio.NewReader(conn).ReadBytes('\n')
if err != nil { return }
var remoteChain []Block
if err := json.Unmarshal(respBytes, &remoteChain); err == nil {
localChain := LoadChain()
if len(remoteChain) > len(localChain) { SaveChain(remoteChain) }
}
}
func Assemble21WitnessGuardMatrix() []string {
rSource := rand.NewSource(time.Now().UnixNano())
rEngine := rand.New(rSource)
shuffled := make([]string, len(NetworkWitnessRoster))
copy(shuffled, NetworkWitnessRoster)
rEngine.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
limit := 21
if len(shuffled) < limit { limit = len(shuffled) }
return shuffled[:limit]
}
func MineBlock(prevBlock Block, txs []Transaction, currentDifficulty int64) Block {
var newBlock Block
newBlock.Index = prevBlock.Index + 1
newBlock.Timestamp = time.Now().Unix()
newBlock.Transactions = txs
newBlock.PrevHash = prevBlock.Hash
newBlock.Difficulty = currentDifficulty
newBlock.Nonce = 0
newBlock.GuardMatrix = Assemble21WitnessGuardMatrix()
targetPrefix := strings.Repeat("0", int(newBlock.Difficulty))
fmt.Printf("\n⚒️  PoD Active: Mining Block %d (Target Pattern: Starting with %d Zeros)...\n", newBlock.Index, newBlock.Difficulty)
startTime := time.Now()
for {
newBlock.Hash = CalculateHash(newBlock)
if newBlock.Nonce > 0 && newBlock.Nonce%500000 == 0 {
elapsed := time.Since(startTime).Seconds()
if elapsed == 0 { elapsed = 0.001 }
hashRate := float64(newBlock.Nonce) / elapsed / 1000.0
fmt.Printf("   ⏳ Nonce: %d | Throughput: %.2f kH/s...\n", newBlock.Nonce, hashRate)
}
if int(newBlock.Difficulty) <= len(newBlock.Hash) && newBlock.Hash[:int(newBlock.Difficulty)] == targetPrefix {
totalElapsed := time.Since(startTime).Seconds()
if totalElapsed == 0 { totalElapsed = 0.001 }
finalHashRate := float64(newBlock.Nonce) / totalElapsed / 1000.0
fmt.Printf("🎉 BLOCK SOLVED! Nonce: %d | Time: %.2fs | Speed: %.2f kH/s | Hash: %s\n", newBlock.Nonce, totalElapsed, finalHashRate, newBlock.Hash)
break
}
newBlock.Nonce++
}
return newBlock
}
func main() {
ValidatorStakingPool = make(map[string]float64)
ValidatorStakingPool["CVN_c43b46f2506955b920b5981bf0a6375fc0bc0337"] = RequiredStakingBond
ValidatorStakingPool["Peer_Alpha_Stake_Rig"] = RequiredStakingBond
CustomMinerAddress := "CVN_c43b46f2506955b920b5981bf0a6375fc0bc0337"
ConnectTarget := ""
for i := 1; i < len(os.Args); i++ {
arg := os.Args[i]
if arg == "--wallet" {
RunWalletGUI()
return
}
if arg == "--miner-address" && i+1 < len(os.Args) {
inputAddress := strings.TrimSpace(os.Args[i+1])
if inputAddress != "" && inputAddress != "=" {
CustomMinerAddress = inputAddress
}
i++
}
if arg == "--connect" && i+1 < len(os.Args) {
ConnectTarget = os.Args[i+1]
i++
}
}
if data, err := os.ReadFile(ProfileConfigFile); err == nil {
var savedCfg MinerConfig
if json.Unmarshal(data, &savedCfg) == nil && savedCfg.SavedMinerAddress != "" {
CustomMinerAddress = savedCfg.SavedMinerAddress
}
}
fmt.Println("====================================================")
fmt.Println("💎 COVENANT STANDARD (CVN) GOSSIP MESH CORE ENGAGED")
fmt.Printf("💰 BLOCK REWARDS ROUTED TO TARGET ID: %s\n", CustomMinerAddress)
fmt.Println("====================================================\n")
go StartTCPServer()
go StartPublicExplorerServer()
time.Sleep(200 * time.Millisecond)
if ConnectTarget != "" {
SyncChainFromSeedPeer(ConnectTarget)
go DialAndGossipWithSeedPeer(ConnectTarget)
}
blockchain := LoadChain()
currentBlock := blockchain[len(blockchain)-1]
fmt.Printf("📂 Local Ledger Loaded. Active Block Height: %d\n", currentBlock.Index)
for {
MempoolMutex.Lock()
activeMempool := make([]Transaction, len(GlobalMempool))
copy(activeMempool, GlobalMempool)
GlobalMempool = []Transaction{}
MempoolMutex.Unlock()
sort.Slice(activeMempool, func(i, j int) bool {
if activeMempool[i].DataSizeKB == 0 { activeMempool[i].DataSizeKB = 1.0 }
if activeMempool[j].DataSizeKB == 0 { activeMempool[j].DataSizeKB = 1.0 }
return (activeMempool[i].FreeWillOffering / activeMempool[i].DataSizeKB) > (activeMempool[j].FreeWillOffering / activeMempool[j].DataSizeKB)
})
var totalBountyOfferings float64 = 0.0
for _, tx := range activeMempool { totalBountyOfferings += tx.FreeWillOffering }
coinbaseRewardTx := Transaction{
ID:     fmt.Sprintf("TX_COINBASE_%d", time.Now().Unix()),
Inputs: []UTXOInput{},
Outputs: []UTXOOutput{
{Recipient: CustomMinerAddress, Amount: 50.0 + totalBountyOfferings},
},
FreeWillOffering: 0.0,
DataSizeKB:       0.1,
Witness:          "Communal_Peer_Witness_7",
}
blockPayload := append([]Transaction{coinbaseRewardTx}, activeMempool...)
nextDifficulty := CalculateAdaptiveDifficulty(blockchain)
newBlock := MineBlock(currentBlock, blockPayload, nextDifficulty)
blockchain = LoadChain()
blockchain = append(blockchain, newBlock)
currentBlock = newBlock
SaveChain(blockchain)
fmt.Printf("💰 LOCAL NODE REWARD AUDIT: Current Balance of %s: %.2f CVN\n", CustomMinerAddress, GetAddressBalance(blockchain, CustomMinerAddress))
fmt.Println("-----------------------------------------------------")
time.Sleep(3 * time.Second)
}
}