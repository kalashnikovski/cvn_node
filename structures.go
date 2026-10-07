package main

// Core UTXO and Block Engine Structures
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