package main

import (
	"context"
	"crypto/elliptic"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// App is the desktop wallet. It never opens the chain database: everything it
// shows comes from a node over HTTP, so it can run beside the miner.
type App struct {
	ctx context.Context
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{}
}

// startup is called when the wallet window opens.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

// walletAddressFromConfig returns the address this machine mines to. It asks,
// in order: a `--miner-address` given on the command line, the miner running
// on this machine (which knows the address it was started with), and last the
// miner_config.json file. The file alone is not reliable: a miner started
// with `--miner-address` ignores it.
func walletAddressFromConfig() string {
	for i, arg := range os.Args {
		if arg == "--miner-address" && i+1 < len(os.Args) {
			if address := strings.TrimSpace(os.Args[i+1]); address != "" {
				return address
			}
		}
	}
	client := &http.Client{Timeout: 2 * time.Second}
	if resp, err := client.Get("http://localhost:8082/status"); err == nil {
		var status map[string]interface{}
		decodeErr := json.NewDecoder(resp.Body).Decode(&status)
		resp.Body.Close()
		if decodeErr == nil {
			if address, _ := status["miner_address"].(string); strings.HasPrefix(address, "CVN_") {
				return address
			}
		}
	}
	configFileBytes, err := os.ReadFile("miner_config.json")
	if err != nil {
		return ""
	}
	var parsedConfig map[string]string
	if json.Unmarshal(configFileBytes, &parsedConfig) != nil {
		return ""
	}
	address := strings.TrimSpace(parsedConfig["miner_address"])
	if strings.Contains(address, "UNCONFIGURED") {
		return ""
	}
	return address
}

// walletGetJSON asks each node in turn (this machine first, then the public
// seeds) and returns the first JSON answer.
func walletGetJSON(path string) (map[string]interface{}, bool) {
	client := &http.Client{Timeout: 4 * time.Second}
	for _, gateway := range walletNodeGateways() {
		resp, err := client.Get(gateway + path)
		if err != nil {
			continue
		}
		var data map[string]interface{}
		decodeErr := json.NewDecoder(resp.Body).Decode(&data)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || decodeErr != nil {
			continue
		}
		return data, true
	}
	return nil, false
}

// GetLocalNodeTelemetry reports the chain tip and this machine's wallet address.
func (a *App) GetLocalNodeTelemetry() string {
	telemetry := map[string]interface{}{
		"current_height": 0,
		"block_hash":     "",
		"miner_address":  walletAddressFromConfig(),
		"difficulty":     0,
		"is_synced":      false,
	}
	if data, ok := walletGetJSON("/block"); ok {
		heightVal, _ := data["current_height"].(float64)
		hashVal, _ := data["block_hash"].(string)
		telemetry["current_height"] = int64(heightVal)
		telemetry["block_hash"] = hashVal
		telemetry["is_synced"] = heightVal > 0
	}
	finalBytes, _ := json.Marshal(telemetry)
	return string(finalBytes)
}

// FetchWalletBalanceQuery returns an address's confirmed balance.
func (a *App) FetchWalletBalanceQuery(address string) float64 {
	address = strings.TrimSpace(address)
	if address == "" {
		return 0.0
	}
	data, ok := walletGetJSON("/audit?address=" + url.QueryEscape(address))
	if !ok {
		return 0.0
	}
	balance, _ := data["balance"].(float64)
	return balance
}

// publicKeyFromPrivateKey rebuilds the 128-character public key that belongs
// to a private key, so the user only ever has to supply the private key.
func publicKeyFromPrivateKey(privateKeyHex string) (string, error) {
	privateKeyHex = strings.TrimSpace(privateKeyHex)
	if len(privateKeyHex)%2 == 1 {
		privateKeyHex = "0" + privateKeyHex
	}
	privBytes, err := hex.DecodeString(privateKeyHex)
	if err != nil || len(privBytes) == 0 || len(privBytes) > 32 {
		return "", fmt.Errorf("that is not a valid private key")
	}
	x, y := elliptic.P256().ScalarBaseMult(privBytes)
	if x == nil || y == nil || (x.Sign() == 0 && y.Sign() == 0) {
		return "", fmt.Errorf("that is not a valid private key")
	}
	return fmt.Sprintf("%064x%064x", x, y), nil
}

// BroadcastLocalTransactionSubmit builds and signs a transfer on this machine
// and hands the finished transfer to a node. The private key is used for this
// one transfer only: it is not saved, logged or sent anywhere.
func (a *App) BroadcastLocalTransactionSubmit(recipient, amountStr, feeStr, privateKeyHex string) string {
	amount, err := strconv.ParseFloat(strings.TrimSpace(amountStr), 64)
	if err != nil || amount <= 0 {
		return "REJECTED: Enter an amount greater than zero."
	}
	fee := 0.0
	if strings.TrimSpace(feeStr) != "" {
		fee, err = strconv.ParseFloat(strings.TrimSpace(feeStr), 64)
		if err != nil || fee < 0 {
			return "REJECTED: The fee must be zero or a positive number."
		}
	}
	recipient = strings.TrimSpace(recipient)
	if recipient == "" {
		return "REJECTED: Enter a recipient address."
	}
	privateKeyHex = strings.TrimSpace(privateKeyHex)
	if privateKeyHex == "" {
		return "REJECTED: Enter the private key of the wallet you are sending from."
	}
	publicKeyHex, err := publicKeyFromPrivateKey(privateKeyHex)
	if err != nil {
		return "REJECTED: " + err.Error()
	}
	if len(privateKeyHex)%2 == 1 {
		privateKeyHex = "0" + privateKeyHex
	}
	senderAddress, err := DeriveAddressFromPublicKey(publicKeyHex)
	if err != nil {
		return "REJECTED: " + err.Error()
	}

	tx, err := SendTransferFromWallet(privateKeyHex, publicKeyHex, recipient, amount, fee)
	if err != nil {
		return "REJECTED: " + err.Error() + " (sending from " + senderAddress + ")"
	}
	return "SUCCESS: Transfer " + tx.ID[:19] + "... sent from " + senderAddress + ". It will confirm in one of the next blocks."
}
