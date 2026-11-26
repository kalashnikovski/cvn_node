package node

import (
	"context"
	"crypto/elliptic"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
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
	if resp, err := client.Get(localStatusURL()); err == nil {
		var status map[string]interface{}
		decodeErr := decodeAnswerWithin(resp.Body, nodeAnswerLimitBytes, &status)
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
		decodeErr := decodeAnswerWithin(resp.Body, nodeAnswerLimitBytes, &data)
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
	amountUnits, err := ParseCVN(amountStr)
	if err != nil || amountUnits < 1 {
		return "REJECTED: Enter an amount greater than zero, with at most 8 decimal places."
	}
	feeUnits := int64(0)
	if strings.TrimSpace(feeStr) != "" {
		if feeUnits, err = ParseCVN(feeStr); err != nil {
			return "REJECTED: The fee must be zero or a positive number, with at most 8 decimal places."
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
	privateKeyHex, publicKeyHex, err := signingKeyFrom(privateKeyHex)
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

	tx, err := SendTransferFromWallet(privateKeyHex, publicKeyHex, recipient, amountUnits, feeUnits)
	if err != nil {
		return "REJECTED: " + err.Error() + " (sending from " + senderAddress + ")"
	}
	return "SUCCESS: Transfer " + tx.ID[:19] + "... sent from " + senderAddress + ". It will confirm in one of the next blocks."
}

// SendWithGiftSubmit sends a transfer like BroadcastLocalTransactionSubmit
// and, in the same transfer, gives something to a second address the sender
// has named: giftStr is an amount in CVN, or a share of the amount sent such
// as "10%". The gift is in addition to the amount. Giving is the sender's
// choice each time and no rule of the chain (giving.go); with both gift
// fields empty this is an ordinary transfer, and with only one filled
// nothing is sent.
func (a *App) SendWithGiftSubmit(recipient, amountStr, feeStr, giftTo, giftStr, privateKeyHex string) string {
	if strings.TrimSpace(giftTo) == "" && strings.TrimSpace(giftStr) == "" {
		return a.BroadcastLocalTransactionSubmit(recipient, amountStr, feeStr, privateKeyHex)
	}
	amountUnits, err := ParseCVN(amountStr)
	if err != nil || amountUnits < 1 {
		return "REJECTED: Enter an amount greater than zero, with at most 8 decimal places."
	}
	feeUnits := int64(0)
	if strings.TrimSpace(feeStr) != "" {
		if feeUnits, err = ParseCVN(feeStr); err != nil {
			return "REJECTED: The fee must be zero or a positive number, with at most 8 decimal places."
		}
	}
	recipient = strings.TrimSpace(recipient)
	if recipient == "" {
		return "REJECTED: Enter a recipient address."
	}
	giftTo, giftUnits, err := giftFromWhatWasTyped(giftTo, giftStr, recipient, amountUnits)
	if err != nil {
		return "REJECTED: " + err.Error() + ". Nothing was sent."
	}
	privateKeyHex = strings.TrimSpace(privateKeyHex)
	if privateKeyHex == "" {
		return "REJECTED: Enter the private key of the wallet you are sending from."
	}
	privateKeyHex, publicKeyHex, err := signingKeyFrom(privateKeyHex)
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

	tx, err := SendTransferWithGiftFromWallet(privateKeyHex, publicKeyHex, recipient, amountUnits, giftTo, giftUnits, feeUnits)
	if err != nil {
		return "REJECTED: " + err.Error() + " (sending from " + senderAddress + ")"
	}
	return "SUCCESS: Transfer " + tx.ID[:19] + "... sent from " + senderAddress + ": " + FormatUnits(amountUnits) + " CVN to the recipient and " +
		FormatUnits(giftUnits) + " CVN given to " + giftTo + ". It will confirm in one of the next blocks."
}

// GiftPreview works out, without sending anything and without any key, what
// a transfer with a gift would give and what would leave the wallet in all,
// so that the screen can show the sender those figures before asking them to
// agree. The figures come from the same code that builds the transfer, so
// what is shown is what is sent. The answer begins "GIFT: " and is followed
// by the sentence to show, or begins "REJECTED: " with the reason nothing
// can be given as typed.
func (a *App) GiftPreview(recipient, amountStr, feeStr, giftTo, giftStr string) string {
	amountUnits, err := ParseCVN(amountStr)
	if err != nil || amountUnits < 1 {
		return "REJECTED: Enter an amount greater than zero, with at most 8 decimal places."
	}
	feeUnits := int64(0)
	if strings.TrimSpace(feeStr) != "" {
		if feeUnits, err = ParseCVN(feeStr); err != nil {
			return "REJECTED: The fee must be zero or a positive number, with at most 8 decimal places."
		}
	}
	giftTo, giftUnits, err := giftFromWhatWasTyped(giftTo, giftStr, recipient, amountUnits)
	if err != nil {
		return "REJECTED: " + err.Error() + ". Nothing was sent."
	}
	if giftUnits == 0 {
		return "REJECTED: No gift was named. Nothing was sent."
	}
	return "GIFT: " + giftInWords(amountUnits, giftUnits, feeUnits, giftTo)
}

// RenewCoinsSubmit sends the coins of a wallet that are within a year of
// their seven-year date, and those with a fifth set aside at such a date,
// back to the wallet's own address. Each becomes a new coin with a new
// seven-year count, and anything set aside is taken back. With `everything`
// set to "all", every coin is renewed. The screen sends no fee with it
// unless the user has asked to pay one. Like a transfer, it is signed on this machine and the private
// key is not kept.
//
// If a renewal this wallet sent with no fee has not gone through after some
// days, the answer begins "NOT THROUGH:" and nothing is sent. The screen
// then asks, and calls again with "again" (send that renewal again, with a
// fee) or "skip" (leave it and renew as usual).
func (a *App) RenewCoinsSubmit(feeStr, privateKeyHex, everything string) string {
	feeUnits := int64(0)
	var err error
	if strings.TrimSpace(feeStr) != "" {
		if feeUnits, err = ParseCVN(feeStr); err != nil {
			return "REJECTED: The fee must be zero or a positive number, with at most 8 decimal places."
		}
	}
	privateKeyHex = strings.TrimSpace(privateKeyHex)
	if privateKeyHex == "" {
		return "REJECTED: Enter the private key of the wallet whose coins you want to renew."
	}
	privateKeyHex, publicKeyHex, err := signingKeyFrom(privateKeyHex)
	if err != nil {
		return "REJECTED: " + err.Error()
	}
	if len(privateKeyHex)%2 == 1 {
		privateKeyHex = "0" + privateKeyHex
	}
	// A renewal sent some days ago that has not gone through
	// (renew_record.go). Asked plainly, the wallet first says so, with how
	// many transfers a second try would be and what it would cost in all,
	// and sends nothing. Asked with "again", it sends that renewal again
	// with the fee the wallet proposed in its question. The screen sends no
	// fee of its own with "again", so what is paid is what was shown.
	// With "skip" or "all" it goes on to an ordinary renewal.
	switch mode := strings.TrimSpace(everything); mode {
	case "again":
		result, err := RenewAgainFromWallet(privateKeyHex, publicKeyHex, feeUnits, time.Now())
		if err != nil && len(result.Transfers) == 0 {
			return "REJECTED: " + err.Error()
		}
		message := "SUCCESS: " + result.Summary()
		if err != nil {
			message += " Then it stopped: " + err.Error()
		}
		return message
	case "":
		if address, err := DeriveAddressFromPublicKey(publicKeyHex); err == nil {
			if coins, _, err := FetchSpendableCoinsOverHTTP(strings.TrimSpace(address)); err == nil {
				if waiting := renewalsNotGoneThrough(address, coins, time.Now()); waiting.any() {
					return "NOT THROUGH: " + waiting.describe(time.Now()) + ". " + waiting.question(waiting.proposedFeeUnits())
				}
			}
		}
	}
	result, err := RenewCoinsFromWallet(privateKeyHex, publicKeyHex, feeUnits, strings.TrimSpace(everything) == "all")
	if err != nil && len(result.Transfers) == 0 {
		// Nothing being near its date is not a failure; say so plainly.
		if strings.Contains(err.Error(), "Nothing needs renewing yet") {
			return "NOTHING DUE: " + err.Error()
		}
		return "REJECTED: " + err.Error()
	}
	message := "SUCCESS: " + result.Summary()
	if err != nil {
		message += " Then it stopped: " + err.Error()
	}
	return message
}

// CombineCoinsSubmit merges the many coins of a wallet into a few large ones
// by sending them back to the wallet's own address. Like a transfer, it is
// signed on this machine and the private key is not kept.
func (a *App) CombineCoinsSubmit(feeStr, privateKeyHex string) string {
	feeUnits := int64(0)
	var err error
	if strings.TrimSpace(feeStr) != "" {
		if feeUnits, err = ParseCVN(feeStr); err != nil {
			return "REJECTED: The fee must be zero or a positive number, with at most 8 decimal places."
		}
	}
	privateKeyHex = strings.TrimSpace(privateKeyHex)
	if privateKeyHex == "" {
		return "REJECTED: Enter the private key of the wallet whose coins you want to combine."
	}
	privateKeyHex, publicKeyHex, err := signingKeyFrom(privateKeyHex)
	if err != nil {
		return "REJECTED: " + err.Error()
	}
	if len(privateKeyHex)%2 == 1 {
		privateKeyHex = "0" + privateKeyHex
	}
	result, err := CombineCoinsFromWallet(privateKeyHex, publicKeyHex, feeUnits)
	if err != nil && len(result.Transfers) == 0 {
		return "REJECTED: " + err.Error()
	}
	message := "SUCCESS: " + result.Summary()
	if err != nil {
		message += " Then it stopped: " + err.Error()
	}
	return message
}
