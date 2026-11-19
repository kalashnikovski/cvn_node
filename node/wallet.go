package node

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
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
		fmt.Println(" 4. Renew my coins (before their seven-year date)")
		fmt.Println(" 5. Create a new wallet")
		fmt.Println(" 6. Recovery words and saved key")
		fmt.Println(" 7. Exit")
		fmt.Println("====================================================================")

		switch ask("Choose 1-7: ") {
		case "1":
			textWalletShowBalance(ask, ownAddress)
		case "2":
			textWalletSend(ask, reader)
		case "3":
			textWalletCombine(ask, reader)
		case "4":
			textWalletRenew(ask, reader)
		case "5":
			textWalletCreate(ask, ownAddress)
		case "6":
			textWalletKeys(ask, reader)
		case "7", "q", "exit":
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
			when := ""
			if height, ok := walletChainHeight(); ok {
				when = ", " + describeBlocksAway(earliestSweep-height) + " from now"
			}
			fmt.Printf("   Oldest coin: a fifth of it moves to the vault at the end of block #%d%s unless it is spent before then.\n", earliestSweep, when)
			fmt.Println("   \"Renew my coins\" in the menu gives such coins a new seven-year count in one step.")
		}
	}
}

func textWalletSend(ask func(string) string, reader *bufio.Reader) {
	recipient := ask("\nRecipient address (CVN_...): ")
	if !looksLikeWalletAddress(recipient) {
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

	privateKeyHex := readSecretLine(reader, "Private key, 24 recovery words, or the saved key's password (not shown while typing): ")
	privateKeyHex, publicKeyHex, err := signingKeyFrom(privateKeyHex)
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

// textWalletRenew sends the coins that are near their seven-year date back
// to their own address, which gives each a new seven-year count.
func textWalletRenew(ask func(string) string, reader *bufio.Reader) {
	fmt.Println()
	fmt.Println(" A coin that has not been spent for seven years has a fifth of its value")
	fmt.Println(" moved to the vault. Spending it, even to your own address, makes it a new")
	fmt.Println(" coin with a new seven-year count. Renewing sends the coins that are within")
	fmt.Println(" a year of their date back to your own address. Your balance does not")
	fmt.Println(" change, apart from any fee you choose to pay.")
	fmt.Println()

	privateKeyHex := readSecretLine(reader, "Private key, 24 recovery words, or the saved key's password (not shown while typing): ")
	privateKeyHex, publicKeyHex, err := signingKeyFrom(privateKeyHex)
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
	earliest := earliestSweepBlock(coins)
	if earliest == 0 {
		fmt.Printf("\n %s holds no coin with a seven-year date. There is nothing to renew.\n", address)
		return
	}
	height, ok := walletChainHeight()
	if !ok {
		fmt.Println("❌ No node could say what the newest block is. Start your miner or check your connection.")
		return
	}
	everything := false
	groups := planRenewal(coins, height, renewHorizonBlocks)
	if len(groups) == 0 {
		fmt.Printf("\n No coin of %s is within a year of its date.\n", address)
		fmt.Printf(" The earliest is the end of block #%d, %s from now. Nothing needs renewing yet.\n", earliest, describeBlocksAway(earliest-height))
		if answer := strings.ToLower(ask("Renew every coin anyway? (y/n): ")); answer != "y" && answer != "yes" {
			fmt.Println("Nothing was sent.")
			return
		}
		everything = true
		groups = planRenewal(coins, height, 0)
	}
	feeUnits := int64(0)
	if feeText := ask("Voluntary fee per transfer (press Enter for 0): "); feeText != "" {
		if feeUnits, err = ParseCVN(feeText); err != nil {
			fmt.Println("❌ The fee must be zero or a positive number, with at most 8 decimal places.")
			return
		}
	}
	renewing := 0
	for _, group := range groups {
		renewing += len(group)
	}
	fmt.Println("\n--------------------------------------------------------------------")
	fmt.Printf(" Wallet:          %s\n", address)
	fmt.Printf(" Coins to renew:  %d of %d\n", renewing, len(coins))
	fmt.Printf(" Earliest date:   end of block #%d, %s from now\n", earliest, describeBlocksAway(earliest-height))
	fmt.Printf(" Transfers:       %d, each back to this same address as one new coin\n", len(groups))
	fmt.Printf(" Total fee:       %s CVN\n", FormatUnits(feeUnits*int64(len(groups))))
	fmt.Println("--------------------------------------------------------------------")
	if answer := strings.ToLower(ask("Renew these coins? (y/n): ")); answer != "y" && answer != "yes" {
		fmt.Println("Nothing was sent.")
		return
	}
	result, err := RenewCoinsFromWallet(privateKeyHex, publicKeyHex, feeUnits, everything)
	if len(result.Transfers) > 0 {
		fmt.Printf("✅ %s\n", result.Summary())
	}
	if err != nil {
		fmt.Printf("❌ %v\n", err)
	}
}

// textWalletCombine merges a wallet's many coins into a few large ones.
func textWalletCombine(ask func(string) string, reader *bufio.Reader) {
	fmt.Println()
	fmt.Println(" A wallet holds its balance as separate coins: a miner receives one for")
	fmt.Printf(" every block won. One transfer can spend at most %d of them. Combining\n", TxMaxInputs)
	fmt.Println(" sends your coins back to your own address as a few large coins. Your")
	fmt.Println(" balance does not change, apart from any fee you choose to pay.")
	fmt.Println()

	privateKeyHex := readSecretLine(reader, "Private key, 24 recovery words, or the saved key's password (not shown while typing): ")
	privateKeyHex, publicKeyHex, err := signingKeyFrom(privateKeyHex)
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
	printRecoveryWords(privateKeyHex)
	fmt.Println("--------------------------------------------------------------------")
	fmt.Println(" ⚠️  Write the 24 words down now, on paper, in order. They are the")
	fmt.Println("    private key written another way, with a check that catches a")
	fmt.Println("    wrong word. Nothing is saved anywhere, and without the words or")
	fmt.Println("    the key the coins at this address can never be spent.")
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
// printRecoveryWords shows a key's 24 recovery words, numbered, six to a line.
func printRecoveryWords(privateKeyHex string) {
	words, err := recoveryWordsFromKey(privateKeyHex)
	if err != nil {
		return
	}
	fmt.Println(" RECOVERY WORDS (keep secret; the same key, written as 24 words):")
	list := strings.Fields(words)
	for i := 0; i < len(list); i += 6 {
		line := "   "
		for j := i; j < i+6 && j < len(list); j++ {
			line += fmt.Sprintf("%2d. %-10s", j+1, list[j])
		}
		fmt.Println(strings.TrimRight(line, " "))
	}
}

// textWalletKeys is the menu for keeping a key: its recovery words, and a
// copy on this computer locked with a password.
func textWalletKeys(ask func(string) string, reader *bufio.Reader) {
	fmt.Println()
	if address, found := savedKeyAddress(); found {
		fmt.Printf(" A key is saved on this computer for %s (in %s).\n", address, savedKeyFile)
	} else {
		fmt.Println(" No key is saved on this computer.")
	}
	fmt.Println(" a. Show the 24 recovery words of a private key")
	fmt.Println(" b. Check recovery words I have written down")
	fmt.Println(" c. Save a key on this computer, locked with a password")
	fmt.Println(" d. Remove the saved key from this computer")
	fmt.Println(" (anything else goes back)")
	const prompt = "Private key, 24 recovery words, or the saved key's password (not shown while typing): "
	switch strings.ToLower(ask("Choose a-d: ")) {
	case "a":
		privateKeyHex, _, err := signingKeyFrom(readSecretLine(reader, prompt))
		if err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}
		fmt.Println()
		printRecoveryWords(privateKeyHex)
		fmt.Println(" Write them on paper, in order. Anyone who has them has the coins.")
	case "b":
		privateKeyHex, err := keyFromRecoveryWords(readSecretLine(reader, "The 24 words, with spaces between them (not shown while typing): "))
		if err != nil {
			fmt.Printf("❌ %v.\n", err)
			return
		}
		publicKeyHex, err := publicKeyFromPrivateKey(privateKeyHex)
		if err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}
		address, _ := DeriveAddressFromPublicKey(publicKeyHex)
		fmt.Printf("✅ The words are right. They are the key of %s\n", strings.TrimSpace(address))
	case "c":
		privateKeyHex, _, err := signingKeyFrom(readSecretLine(reader, "Private key or 24 recovery words to save (not shown while typing): "))
		if err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}
		password := readSecretLine(reader, fmt.Sprintf("Choose a password, at least %d characters (not shown while typing): ", savedKeyMinPassword))
		if password != readSecretLine(reader, "The same password again: ") {
			fmt.Println("❌ The two passwords were not the same. Nothing was saved.")
			return
		}
		address, err := saveKeyWithPassword(privateKeyHex, password)
		if err != nil {
			fmt.Printf("❌ %v. Nothing was saved.\n", err)
			return
		}
		fmt.Printf("✅ Saved in %s, locked with your password, for %s\n", savedKeyFile, address)
		fmt.Println("   From now on, where a wallet asks for the private key you can type the password.")
		fmt.Println("   This file is not a backup. If this computer is lost, or the password forgotten,")
		fmt.Println("   only the 24 recovery words (or the private key) on paper bring the coins back.")
	case "d":
		if _, found := savedKeyAddress(); !found {
			fmt.Println("There is no saved key to remove.")
			return
		}
		if answer := strings.ToLower(ask("Remove the saved key? Make sure you have the recovery words on paper first. (y/n): ")); answer != "y" && answer != "yes" {
			fmt.Println("The saved key was left as it is.")
			return
		}
		if err := os.Remove(savedKeyFile); err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}
		fmt.Println("✅ The saved key was removed from this computer.")
	}
}

// readSecretLine asks for a private key or a password and does not show it
// as it is typed or pasted, on Windows as on Linux (see hideTyping). A key
// left on the screen is a key in every screenshot of that screen.
func readSecretLine(reader *bufio.Reader, prompt string) string {
	fmt.Print(prompt)
	restore, hidden := hideTyping()
	line, _ := reader.ReadString('\n')
	restore()
	if hidden {
		fmt.Println()
	}
	return strings.TrimSpace(line)
}
