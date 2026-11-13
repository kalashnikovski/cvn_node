package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// TEXT WALLET
//
// Started with `--wallet`. It works in any terminal on Linux and Windows and
// never opens the chain database, so it can run in one terminal while the
// miner runs in another. Everything it shows comes from a node over HTTP: the
// node on this machine first, then the public seeds.

// RunTextWallet is the menu loop. It returns when the user chooses to exit.
func RunTextWallet() {
	reader := bufio.NewReader(os.Stdin)
	ask := func(prompt string) string {
		fmt.Print(prompt)
		line, _ := reader.ReadString('\n')
		return strings.TrimSpace(line)
	}

	for {
		ownAddress := walletAddressFromConfig()
		fmt.Println()
		fmt.Println("====================================================================")
		fmt.Println("💎 COVENANT STANDARD (CVN) TEXT WALLET")
		fmt.Println("====================================================================")
		if ownAddress != "" {
			fmt.Printf(" This machine's address: %s\n", ownAddress)
		} else {
			fmt.Println(" This machine has no address yet (no miner_config.json here).")
		}
		if height, ok := walletChainHeight(); ok {
			fmt.Printf(" Network height: #%d\n", height)
		} else {
			fmt.Println(" ⚠️  No node could be reached. Start your miner or check your connection.")
		}
		fmt.Println("--------------------------------------------------------------------")
		fmt.Println(" 1. Show a balance")
		fmt.Println(" 2. Send CVN")
		fmt.Println(" 3. Create a new wallet")
		fmt.Println(" 4. Exit")
		fmt.Println("====================================================================")

		switch ask("Choose 1-4: ") {
		case "1":
			textWalletShowBalance(ask, ownAddress)
		case "2":
			textWalletSend(ask, reader)
		case "3":
			textWalletCreate(ask, ownAddress)
		case "4", "q", "exit":
			fmt.Println("Wallet closed.")
			return
		}
	}
}

func walletChainHeight() (int64, bool) {
	data, ok := walletGetJSON("/block")
	if !ok {
		return 0, false
	}
	height, _ := data["current_height"].(float64)
	return int64(height), true
}

func textWalletShowBalance(ask func(string) string, ownAddress string) {
	prompt := "Address to check: "
	if ownAddress != "" {
		prompt = "Address to check (press Enter for this machine's address): "
	}
	address := ask(prompt)
	if address == "" {
		address = ownAddress
	}
	if address == "" {
		fmt.Println("❌ No address given.")
		return
	}
	data, ok := walletGetJSON("/audit?address=" + url.QueryEscape(address))
	if !ok {
		fmt.Println("❌ No node answered. Start your miner or check your connection.")
		return
	}
	balance, _ := data["balance"].(float64)
	fmt.Printf("\n💰 Balance of %s\n   %s CVN\n", address, formatAmountExact(balance))
	if coins, _, err := FetchSpendableCoinsOverHTTP(address); err == nil {
		var spendable float64
		for _, coin := range coins {
			spendable += coin.Amount
		}
		fmt.Printf("   Ready to spend now: %s CVN in %d coin(s)\n", formatAmountExact(spendable), len(coins))
	}
}

func textWalletSend(ask func(string) string, reader *bufio.Reader) {
	recipient := ask("\nRecipient address (CVN_...): ")
	if !validRecipient(recipient) || !strings.HasPrefix(recipient, "CVN_") {
		fmt.Println("❌ That is not a valid CVN address.")
		return
	}
	amount, err := strconv.ParseFloat(ask("Amount of CVN to send: "), 64)
	if err != nil || !validAmount(amount) {
		fmt.Println("❌ The amount must be a number greater than zero.")
		return
	}
	fee := 0.0
	if feeText := ask("Voluntary fee (press Enter for 0): "); feeText != "" {
		fee, err = strconv.ParseFloat(feeText, 64)
		if err != nil || fee < 0 || fee > TxMaxAmount {
			fmt.Println("❌ The fee must be zero or a positive number.")
			return
		}
	}

	privateKeyHex := readSecretLine(reader, "Private key of the sending wallet (not shown while typing): ")
	publicKeyHex, err := publicKeyFromPrivateKey(privateKeyHex)
	if err != nil {
		fmt.Println("❌ " + err.Error())
		return
	}
	if len(privateKeyHex)%2 == 1 {
		privateKeyHex = "0" + privateKeyHex
	}
	senderAddress, err := DeriveAddressFromPublicKey(publicKeyHex)
	if err != nil {
		fmt.Println("❌ " + err.Error())
		return
	}

	fmt.Println("\n--------------------------------------------------------------------")
	fmt.Printf(" From:   %s\n", senderAddress)
	fmt.Printf(" To:     %s\n", recipient)
	fmt.Printf(" Amount: %s CVN\n", formatAmountExact(amount))
	fmt.Printf(" Fee:    %s CVN\n", formatAmountExact(fee))
	fmt.Println("--------------------------------------------------------------------")
	if answer := strings.ToLower(ask("Send this transfer? It cannot be undone. (y/n): ")); answer != "y" && answer != "yes" {
		fmt.Println("Nothing was sent.")
		return
	}

	tx, err := SendTransferFromWallet(privateKeyHex, publicKeyHex, recipient, amount, fee)
	if err != nil {
		fmt.Printf("❌ Not sent: %v\n", err)
		return
	}
	fmt.Printf("✅ Sent. Transfer ID: %s\n   It will confirm in one of the next blocks.\n", tx.ID)
}

func textWalletCreate(ask func(string) string, ownAddress string) {
	privateKeyHex, publicKeyHex, address, err := GenerateKeyPair()
	if err != nil {
		fmt.Printf("❌ Could not create a wallet: %v\n", err)
		return
	}
	address = strings.TrimSpace(address)
	fmt.Println("\n====================================================================")
	fmt.Println(" NEW WALLET")
	fmt.Println("====================================================================")
	fmt.Printf(" Address (safe to share):  %s\n", address)
	fmt.Printf(" Public key:               %s\n", publicKeyHex)
	fmt.Printf(" PRIVATE KEY (keep secret): %s\n", privateKeyHex)
	fmt.Println("--------------------------------------------------------------------")
	fmt.Println(" ⚠️  Write the private key down now. It is not saved anywhere, and")
	fmt.Println("    without it the coins at this address can never be spent.")
	fmt.Println("====================================================================")

	question := "Use this address for mining on this machine? (y/n): "
	if ownAddress != "" {
		question = "Replace this machine's mining address (" + ownAddress + ") with the new one? (y/n): "
	}
	if answer := strings.ToLower(ask(question)); answer != "y" && answer != "yes" {
		fmt.Println("miner_config.json was left as it is.")
		return
	}
	configBytes, _ := json.MarshalIndent(map[string]string{"miner_address": address}, "", "  ")
	if err := os.WriteFile("miner_config.json", configBytes, 0644); err != nil {
		fmt.Printf("❌ Could not write miner_config.json: %v\n", err)
		return
	}
	fmt.Println("✅ Saved. Restart the miner for it to pay rewards to the new address.")
}

// readSecretLine reads one line without showing it on screen where the
// terminal allows that (Linux and macOS). On Windows the text stays visible.
func readSecretLine(reader *bufio.Reader, prompt string) string {
	fmt.Print(prompt)
	hidden := false
	if runtime.GOOS != "windows" {
		hide := exec.Command("stty", "-echo")
		hide.Stdin = os.Stdin
		hidden = hide.Run() == nil
	}
	line, _ := reader.ReadString('\n')
	if hidden {
		show := exec.Command("stty", "echo")
		show.Stdin = os.Stdin
		_ = show.Run()
		fmt.Println()
	}
	return strings.TrimSpace(line)
}
