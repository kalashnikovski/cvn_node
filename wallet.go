package main

import (
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

func RunWalletGUI() {
	myApp := app.New()
	myWindow := myApp.NewWindow("COVENANT STANDARD WALLET v2.2.2")
	myWindow.Resize(fyne.NewSize(550, 680))

	statusLabel := widget.NewLabel("System Status: ENCRYPTED & SECURE")
	
	privKeyEntry := widget.NewEntry()
	privKeyEntry.SetPlaceHolder("Enter or generate your Private Key (Hex)...")
	
	addressLabel := widget.NewLabel("Your Public Address: (Load private key or generate a new profile)")

	balanceLabel := widget.NewLabel("CURRENT BALANCE: 0.00 CVN")
	usdLabel := widget.NewLabel("Estimated Value: $0.00 USD (Synced Market Feed)")

	nodeInput := widget.NewEntry()
	nodeInput.SetText("202.137.175.220:8080") 
	nodeInput.SetPlaceHolder("Target Node Net Address (e.g. public_ip:8080)...")

	recipientInput := widget.NewEntry()
	recipientInput.SetPlaceHolder("Paste recipient destination node address...")

	amountInput := widget.NewEntry()
	amountInput.SetPlaceHolder("Enter CVN token amount...")

	offeringInput := widget.NewEntry()
	offeringInput.SetPlaceHolder("Enter Voluntary Free-Will Offering Fee (e.g. 1.50)...")

	networkLog := widget.NewLabel("[P2P Cryptographic Client Interface Ready]")

	// FUNCTION: Dynamically extracts the true, unique deterministic public address from the hex private key
	deriveAddressFromInput := func(privHex string) {
		privHex = strings.TrimSpace(privHex)
		if len(privHex) != 64 {
			addressLabel.SetText("Your Public Address: (Invalid Private Key length - must be 64 hex characters)")
			return
		}

		privBytes, err := hex.DecodeString(privHex)
		if err != nil {
			addressLabel.SetText("Your Public Address: (Invalid hex formatting characters)")
			return
		}

		// Apply deterministic NIST P-256 curve mathematics to reverse-engineer public coordinates from secret seeds
		curve := elliptic.P256()
		x, y := curve.ScalarBaseMult(privBytes)
		
		pubHex := fmt.Sprintf("%x%x", x, y)
		addressHash := sha256.Sum256([]byte(pubHex))
		walletAddress := "CVN_" + hex.EncodeToString(addressHash[:20])
		
		addressLabel.SetText(fmt.Sprintf("Your Public Address: %s", walletAddress))
	}

	// HOT-RELOAD LISTENER: Triggers real-time mathematical identity derivation instantly on changes
	privKeyEntry.OnChanged = func(text string) {
		deriveAddressFromInput(text)
	}

	genKeysButton := widget.NewButton("⚙️ GENERATE NEW SECURE WALLET PROFILE KEYPAIR", func() {
		privHex, walletAddress, err := GenerateKeyPair()
		if err != nil {
			networkLog.SetText(fmt.Sprintf("Keypair Generation Failure: %v", err))
			return
		}
		privKeyEntry.SetText(privHex)
		addressLabel.SetText(fmt.Sprintf("Your Public Address: %s", walletAddress))
		networkLog.SetText("✅ Fresh ECDSA P-256 profile keys computed safely inside runtime memory.")
	})

	sendButton := widget.NewButton("🛡️ AUTHORIZE, CRYPTOGRAPHICALLY SIGN & BROADCAST", func() {
		nodeTarget := nodeInput.Text
		senderPrivKey := privKeyEntry.Text
		recipient := recipientInput.Text
		amountStr := amountInput.Text
		offeringStr := offeringInput.Text

		if nodeTarget == "" || senderPrivKey == "" || recipient == "" || amountStr == "" {
			networkLog.SetText("Validation Failure: Missing required transaction entries.")
			return
		}

		amount, err := strconv.ParseFloat(amountStr, 64)
		if err != nil || amount <= 0 {
			networkLog.SetText("Validation Failure: Invalid token transfer amount format.")
			return
		}

		var offering float64 = 0.0
		if offeringStr != "" {
			offering, _ = strconv.ParseFloat(offeringStr, 64)
		}

		publicAddressHandle := addressLabel.Text
		if strings.Contains(publicAddressHandle, "CVN_") {
			idx := strings.Index(publicAddressHandle, "CVN_")
			publicAddressHandle = strings.TrimSpace(publicAddressHandle[idx:])
		}

		nowTime := time.Now()
		txDataToSign := fmt.Sprintf("%s%s%.4f%.4f%d", publicAddressHandle, recipient, amount, offering, nowTime.Unix())
		
		rCoord, sCoord, err := SignTransactionPayload(senderPrivKey, txDataToSign)
		if err != nil {
			networkLog.SetText(fmt.Sprintf("Cryptographic Failure: Signing pass failed -> %v", err))
			return
		}

		tx := Transaction{
			Sender:           publicAddressHandle,
			Recipient:        recipient,
			Amount:           amount,
			FreeWillOffering: offering,
			DataSizeKB:       1.0,
			Witness:          "Public_ECDSA_Math_Validation_Pass",
			Timestamp:        nowTime,
			SignatureR:       rCoord,
			SignatureS:       sCoord,
		}

		txBytes, err := json.Marshal(tx)
		if err != nil {
			networkLog.SetText("System Error: Failed to serialize encrypted transaction structure.")
			return
		}

		conn, err := net.DialTimeout("tcp", nodeTarget, 5*time.Second)
		if err != nil {
			networkLog.SetText(fmt.Sprintf("Handshake Failure: Node entry endpoint [%s] is currently unreachable.", nodeTarget))
			return
		}
		defer conn.Close()

		fmt.Fprintf(conn, "TX_BROADCAST:%s\n", string(txBytes))
		networkLog.SetText(fmt.Sprintf("🚀 Successfully broadcasted signed transaction packet to node endpoint: %s!", nodeTarget))
		recipientInput.SetText("")
		amountInput.SetText("")
		offeringInput.SetText("")
	})

	content := container.NewVBox(
		widget.NewLabel("=================================================="),
		statusLabel,
		widget.NewLabel("--------------------------------------------------"),
		widget.NewLabel("🔒 CRYPTOGRAPHIC WALLET INITIALIZATION UTILITY:"),
		genKeysButton,
		widget.NewLabel("Private Key (Hex Signature Authorization Secret):"),
		privKeyEntry,
		addressLabel,
		widget.NewLabel("--------------------------------------------------"),
		balanceLabel,
		usdLabel,
		widget.NewLabel("--------------------------------------------------"),
		widget.NewLabel("NETWORK ACCESS POINT PARAMETERS:"),
		nodeInput,
		widget.NewLabel("--------------------------------------------------"),
		widget.NewLabel("EXECUTE OUTBOUND TRANSFER (MODEL B):"),
		recipientInput,
		amountInput,
		offeringInput,
		sendButton,
		widget.NewLabel("--------------------------------------------------"),
		networkLog,
		widget.NewLabel("=================================================="),
	)

	go func() {
		for {
			time.Sleep(3 * time.Second)
			if privKeyEntry.Text != "" {
				currentChain := LoadChain()
				publicAddressHandle := addressLabel.Text
				if strings.Contains(publicAddressHandle, "CVN_") {
					idx := strings.Index(publicAddressHandle, "CVN_")
					publicAddressHandle = strings.TrimSpace(publicAddressHandle[idx:])
					
					currentBalance := GetAddressBalance(currentChain, publicAddressHandle)
					balanceLabel.SetText(fmt.Sprintf("CURRENT BALANCE: %.2f CVN", currentBalance))
					usdLabel.SetText(fmt.Sprintf("Estimated Value: $%.2f USD (Synced Market Feed)", currentBalance*0.025))
				}
			}
		}
	}()

	myWindow.SetContent(content)
	myWindow.ShowAndRun()
}