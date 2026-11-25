package node

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
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
		var spendable int64
		for _, coin := range coins {
			spendable += coin.Amount.Units()
		}
		fmt.Printf("   Ready to spend now: %s CVN in %d coin(s)\n", FormatUnits(spendable), len(coins))
		if len(coins) > TxMaxInputs {
			fmt.Printf("   One transfer can spend at most %d coins. \"Combine my coins\" in the menu merges them into fewer, larger coins.\n", TxMaxInputs)
		}
		height, heightKnown := walletChainHeight()
		for _, line := range describeCoinDates(coins, height, heightKnown) {
			fmt.Println("   " + line)
		}
		// A renewal this wallet sent with no fee some days ago, whose
		// coins are still as they were (renew_record.go).
		if waiting := renewalsNotGoneThrough(address, coins, time.Now()); waiting.any() {
			fmt.Printf("   ⚠️  %s.\n", waiting.describe(time.Now()))
			fmt.Println("   \"Renew my coins\" in the menu offers to do that.")
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

// describeCoinDates says, for the owner of these coins, what the seven-year
// sweep has done to them and what it will do next: how much has been set
// aside and until when it can be taken back, and when the oldest coin that
// has nothing set aside reaches its date. It says nothing when no coin has
// a date.
func describeCoinDates(coins []SpendableCoin, height int64, heightKnown bool) []string {
	var setAside, takeBackBy, nextFirstDate int64
	for _, coin := range coins {
		if coin.HeldUnits > 0 {
			setAside += coin.HeldUnits
			if coin.SweepAt > 0 && (takeBackBy == 0 || coin.SweepAt < takeBackBy) {
				takeBackBy = coin.SweepAt
			}
		} else if coin.SweepAt > 0 && (nextFirstDate == 0 || coin.SweepAt < nextFirstDate) {
			nextFirstDate = coin.SweepAt
		}
	}
	when := func(block int64) string {
		if !heightKnown {
			return ""
		}
		return ", " + describeBlocksAway(block-height) + " from now"
	}
	var lines []string
	if setAside > 0 {
		lines = append(lines,
			fmt.Sprintf("%s CVN of this was set aside at a seven-year date. It is still yours.", FormatUnits(setAside)),
			fmt.Sprintf("To keep it, spend or renew each coin it is on before that coin's next date. The earliest: before the end of block #%d%s. After a coin's date, what was set aside on it goes to the vault.", takeBackBy, when(takeBackBy)))
	}
	if nextFirstDate > 0 {
		lines = append(lines,
			fmt.Sprintf("Oldest coin: at the end of block #%d%s a fifth of it is set aside, unless it is spent before then.", nextFirstDate, when(nextFirstDate)),
			"What is set aside can be taken back by renewing in the seven years after; renewing before the date avoids it.")
	}
	if len(lines) > 0 {
		soonest := takeBackBy
		if nextFirstDate > 0 && (soonest == 0 || nextFirstDate < soonest) {
			soonest = nextFirstDate
		}
		if heightKnown && soonest-height <= renewUrgentBlocks {
			lines = append(lines, "\"Renew my coins\" in the menu does this in one step. A date is less than two days away: give the renewal a small fee, because one with no fee waits its turn when blocks are full.")
		} else {
			lines = append(lines, "\"Renew my coins\" in the menu does this in one step, with no fee.")
		}
	}
	return lines
}

// textWalletRenew sends back to their own address the coins that are near
// their seven-year date, and those with a fifth set aside. Each becomes a
// new coin with a new seven-year count and nothing set aside.
func textWalletRenew(ask func(string) string, reader *bufio.Reader) {
	fmt.Println()
	fmt.Println(" A coin that has not been spent for seven years has a fifth of its value")
	fmt.Println(" set aside. It stays yours for seven more years and then goes to the vault.")
	fmt.Println(" Spending the coin, even to your own address, makes it a new coin with a")
	fmt.Println(" new seven-year count, and takes back anything that was set aside on it.")
	fmt.Println(" Renewing sends back to your own address the coins that are within a year")
	fmt.Println(" of their date and those with a fifth set aside. No fee is needed. Your")
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
	// First, a renewal sent some days ago that has not gone through: the
	// wallet offers to send it again with a small fee.
	if textWalletRenewAgain(ask, privateKeyHex, publicKeyHex, address, coins, time.Now()) {
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
	if earliest-height <= renewUrgentBlocks {
		fmt.Println("\n A date is less than two days away. A renewal with no fee waits its turn when")
		fmt.Println(" blocks are full; a small fee, such as 0.001, makes sure it is mined in time.")
	}
	feeUnits := int64(0)
	if feeText := ask("Voluntary fee per transfer (press Enter for 0): "); feeText != "" {
		if feeUnits, err = ParseCVN(feeText); err != nil {
			fmt.Println("❌ The fee must be zero or a positive number, with at most 8 decimal places.")
			return
		}
	}
	renewing := 0
	var takingBack int64
	for _, group := range groups {
		renewing += len(group)
		for _, coin := range group {
			takingBack += coin.HeldUnits
		}
	}
	fmt.Println("\n--------------------------------------------------------------------")
	fmt.Printf(" Wallet:          %s\n", address)
	fmt.Printf(" Coins to renew:  %d of %d\n", renewing, len(coins))
	if takingBack > 0 {
		fmt.Printf(" To take back:    %s CVN that had been set aside on them\n", FormatUnits(takingBack))
	}
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

// textWalletRenewAgain deals with a renewal this wallet sent some days ago
// whose coins are still as they were (renew_record.go). It says so, asks
// what fee a second try should pay, shows what would be sent and what it
// would cost in all, and sends it only on a plain yes. It reports whether
// the matter was dealt with here; when there was no such renewal, or the
// owner chose to leave it, it reports false and renewing goes on as usual.
func textWalletRenewAgain(ask func(string) string, privateKeyHex, publicKeyHex, address string, coins []SpendableCoin, now time.Time) bool {
	waiting := renewalsNotGoneThrough(address, coins, now)
	if !waiting.any() {
		return false
	}
	fmt.Printf("\n ⚠️  %s.\n", waiting.describe(now))
	answer := ask(fmt.Sprintf(" Send it again? Press Enter for a fee of %s CVN for each transfer, type another fee, or type n to leave it: ", FormatUnits(waiting.proposedFeeUnits())))
	if lowered := strings.ToLower(answer); lowered == "n" || lowered == "no" {
		fmt.Println(" Left as it is.")
		return false
	}
	feeUnits := waiting.proposedFeeUnits()
	if answer != "" {
		typed, err := ParseCVN(answer)
		if err != nil {
			fmt.Println("❌ The fee must be a positive number, with at most 8 decimal places. Nothing was sent.")
			return true
		}
		if typed == 0 {
			fmt.Println(" A second try with no fee is the same as renewing again. Nothing was sent: choose \"Renew my coins\" once more, type n here, and renew as usual.")
			return true
		}
		feeUnits = typed
	}
	fmt.Println("\n--------------------------------------------------------------------")
	fmt.Printf(" Wallet:              %s\n", address)
	fmt.Printf(" Coins to send again: %d\n", len(waiting.Coins))
	fmt.Printf(" Transfers:           %d, each back to this same address as one new coin\n", waiting.transfers())
	fmt.Printf(" Fee for each:        %s CVN\n", FormatUnits(feeUnits))
	fmt.Printf(" Total fee:           %s CVN\n", FormatUnits(feeUnits*int64(waiting.transfers())))
	fmt.Println("--------------------------------------------------------------------")
	if confirm := strings.ToLower(ask("Send this renewal again? (y/n): ")); confirm != "y" && confirm != "yes" {
		fmt.Println("Nothing was sent.")
		return true
	}
	result, err := RenewAgainFromWallet(privateKeyHex, publicKeyHex, feeUnits, now)
	if len(result.Transfers) > 0 {
		fmt.Printf("✅ %s\n", result.Summary())
	}
	if err != nil {
		fmt.Printf("❌ %v\n", err)
	}
	return true
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
		privateKeyHex, publicKeyHex, err := signingKeyFrom(readSecretLine(reader, prompt))
		if err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}
		// The address is shown with the words, so that words written down
		// for the wrong key are noticed before they are relied on.
		address, _ := DeriveAddressFromPublicKey(publicKeyHex)
		fmt.Println()
		fmt.Printf(" These are the words for the address %s\n", strings.TrimSpace(address))
		fmt.Println(" Check that it is your address before you write them down.")
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
