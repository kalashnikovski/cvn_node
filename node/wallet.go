package node

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"runtime"
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
		fmt.Println(" 3. Combine my coins")
		fmt.Println(" 4. Create a new wallet")
		fmt.Println(" 5. Exit")
		fmt.Println("====================================================================")

		switch ask("Choose 1-5: ") {
		case "1":
			textWalletShowBalance(ask, ownAddress)
		case "2":
			textWalletSend(ask, reader)
		case "3":
			textWalletCombine(ask, reader)
		case "4":
			textWalletCreate(ask, ownAddress)
		case "5", "q", "exit":
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
	fmt.Printf("\n💰 Balance of %s\n   %s CVN\n", address, formatFloatExact(balance))
	if coins, _, err := FetchSpendableCoinsOverHTTP(address); err == nil {
		var spendable, earliestSweep int64
		for _, coin := range coins {
			spendable += coin.Amount.Units()
			if coin.SweepAt > 0 && (earliestSweep == 0 || coin.SweepAt < earliestSweep) {
				earliestSweep = coin.SweepAt
			}
		}
		fmt.Printf("   Ready to spend now: %s CVN in %d coin(s)\n", FormatUnits(spendable), len(coins))
		if len(coins) > TxMaxInputs {
			fmt.Printf("   One transfer can spend at most %d coins. \"Combine my coins\" in the menu merges them into fewer, larger coins.\n", TxMaxInputs)
		}
		if earliestSweep > 0 {
			fmt.Printf("   Oldest coin: a fifth of it moves to the vault at the end of block #%d unless it is spent before then.\n", earliestSweep)
		}
	}
}

func textWalletSend(ask func(string) string, reader *bufio.Reader) {
	recipient := ask("\nRecipient address (CVN_...): ")
	if !validRecipient(recipient) || !strings.HasPrefix(recipient, "CVN_") {
		fmt.Println("❌ That is not a valid CVN address.")
		return
	}
	amountUnits, err := ParseCVN(ask("Amount of CVN to send: "))
	if err != nil || amountUnits < 1 {
		fmt.Println("❌ The amount must be a number greater than zero, with at most 8 decimal places.")
		return
	}
	feeUnits := int64(0)
	if feeText := ask("Voluntary fee (press Enter for 0): "); feeText != "" {
		if feeUnits, err = ParseCVN(feeText); err != nil {
			fmt.Println("❌ The fee must be zero or a positive number, with at most 8 decimal places.")
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
	fmt.Printf(" Amount: %s CVN\n", FormatUnits(amountUnits))
	fmt.Printf(" Fee:    %s CVN\n", FormatUnits(feeUnits))
	fmt.Println("--------------------------------------------------------------------")
	if answer := strings.ToLower(ask("Send this transfer? It cannot be undone. (y/n): ")); answer != "y" && answer != "yes" {
		fmt.Println("Nothing was sent.")
		return
	}

	tx, err := SendTransferFromWallet(privateKeyHex, publicKeyHex, recipient, amountUnits, feeUnits)
	if err != nil {
		fmt.Printf("❌ Not sent: %v\n", err)
		return
	}
	fmt.Printf("✅ Sent. Transfer ID: %s\n   It will confirm in one of the next blocks.\n", tx.ID)
}

// textWalletCombine merges a wallet's many coins into a few large ones.
func textWalletCombine(ask func(string) string, reader *bufio.Reader) {
	fmt.Println()
	fmt.Println(" A wallet holds its balance as separate coins: a miner receives one for")
	fmt.Printf(" every block won. One transfer can spend at most %d of them. Combining\n", TxMaxInputs)
	fmt.Println(" sends your coins back to your own address as a few large coins. Your")
	fmt.Println(" balance does not change, apart from any fee you choose to pay.")
	fmt.Println()

	privateKeyHex := readSecretLine(reader, "Private key of the wallet (not shown while typing): ")
	publicKeyHex, err := publicKeyFromPrivateKey(privateKeyHex)
	if err != nil {
		fmt.Println("❌ " + err.Error())
		return
	}
	if len(privateKeyHex)%2 == 1 {
		privateKeyHex = "0" + privateKeyHex
	}
	address, err := DeriveAddressFromPublicKey(publicKeyHex)
	if err != nil {
		fmt.Println("❌ " + err.Error())
		return
	}
	coins, _, err := FetchSpendableCoinsOverHTTP(address)
	if err != nil {
		fmt.Printf("❌ %v\n", err)
		return
	}
	groups := planCombine(coins)
	if len(groups) == 0 {
		fmt.Printf("\n %s holds %d coin(s) ready to spend. There is nothing to combine.\n", address, len(coins))
		return
	}
	feeUnits := int64(0)
	if feeText := ask("Voluntary fee per transfer (press Enter for 0): "); feeText != "" {
		if feeUnits, err = ParseCVN(feeText); err != nil {
			fmt.Println("❌ The fee must be zero or a positive number, with at most 8 decimal places.")
			return
		}
	}
	combining := 0
	for _, group := range groups {
		combining += len(group)
	}
	fmt.Println("\n--------------------------------------------------------------------")
	fmt.Printf(" Wallet:        %s\n", address)
	fmt.Printf(" Coins now:     %d\n", len(coins))
	fmt.Printf(" Coins after:   %d\n", len(coins)-combining+len(groups))
	fmt.Printf(" Transfers:     %d, each back to this same address\n", len(groups))
	fmt.Printf(" Total fee:     %s CVN\n", FormatUnits(feeUnits*int64(len(groups))))
	fmt.Println("--------------------------------------------------------------------")
	if answer := strings.ToLower(ask("Combine these coins? (y/n): ")); answer != "y" && answer != "yes" {
		fmt.Println("Nothing was sent.")
		return
	}
	result, err := CombineCoinsFromWallet(privateKeyHex, publicKeyHex, feeUnits)
	if len(result.Transfers) > 0 {
		fmt.Printf("✅ %s\n", result.Summary())
	}
	if err != nil {
		fmt.Printf("❌ %v\n", err)
	}
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
	if err := saveMinerAddress(address); err != nil {
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
