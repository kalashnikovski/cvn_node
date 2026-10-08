package main

// Block represents a single verified ledger block segment on the mainnet wire
type Block struct {
	Index        int64         `json:"current_height"`
	Timestamp    int64         `json:"timestamp"`
	Transactions []Transaction `json:"transactions"`
	PrevHash     string        `json:"prev_hash"`
	Hash         string        `json:"block_hash"`
	Difficulty   int           `json:"difficulty"`
	Nonce        int64         `json:"nonce"`
}

// Transaction maps the cryptographic dual-chamber cash value transfer payload
type Transaction struct {
	ID               string       `json:"tx_id"`
	Inputs           []UTXOInput  `json:"inputs"`
	Outputs          []UTXOOutput `json:"outputs"`
	Signature        string       `json:"signature"`
	Timestamp        int64        `json:"timestamp"`
	FreeWillOffering float64      `json:"free_will_offering"`
	DataSizeKB       float64      `json:"data_size_kb"`
	Witness          string       `json:"witness"`
}

// UTXOInput tracks a reference link targeting an existing spent balance output cell
type UTXOInput struct {
	SourceTxID string `json:"source_tx_id"`
	Index      int    `json:"output_index"`
	Signature  string `json:"signature"`
}

// UTXOOutput maps the recipient target destination address hash and allocated balance
type UTXOOutput struct {
	Recipient string  `json:"recipient"`
	Amount    float64 `json:"amount"`
}

// Global Ledger Tracking Storage Parameters
var BlockchainFile = "cvn_mainnet.db"
var CustomMinerAddress string