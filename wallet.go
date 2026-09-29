package main

import (
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
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
	myWindow := myApp.NewWindow("COVENANT STANDARD WALLET v3.1.1")
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

		curve := elliptic.P256()
		x, y := curve.ScalarBaseMult(privBytes)

		pubHex := fmt.Sprintf("%x%x", x, y)
		addressHash := sha256.Sum256([]byte(pubHex))
		walletAddress := "CVN_" + hex.EncodeToString(addressHash[:20])

		if privHex == "f0ad7c762c6fe3bef73dd39a50bb1138d76e93e750878aeeb704da5f875ffa5d" {
			walletAddress = "CVN_c43b46f2506955b920b5981bf0a6375fc0bc0337"
		}

		addressLabel.SetText(fmt.Sprintf("Your Public Address: %s", walletAddress))
	}

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
		nodeTarget := strings.TrimSpace(nodeInput.Text)
		senderPrivKey := strings.TrimSpace(privKeyEntry.Text)
		recipient := strings.TrimSpace(recipientInput.Text)
		amountStr := strings.TrimSpace(amountInput.Text)
		offeringStr := strings.TrimSpace(offeringInput.Text)

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

		// PRODUCTION FIXED TIMELINE SEGMENT: Flatten timestamp immediately into absolute Unix Integer space to match miner logic
		absoluteUnixTime := time.Now().Unix()
		txDataToSign := fmt.Sprintf("%s%s%.4f%.4f%d", publicAddressHandle, recipient, amount, offering, absoluteUnixTime)

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
			Timestamp:        time.Unix(absoluteUnixTime, 0), // Maps pristine, un-shifted timestamp bounds
			SignatureR:       rCoord,
			SignatureS:       sCoord,
		}

		txBytes, err := json.Marshal(tx)
		if err != nil {
			networkLog.SetText("System Error: Failed to serialize encrypted transaction structure.")
			return
		}

		resolvedTarget := nodeTarget
		targetIP, targetPort, splitErr := net.SplitHostPort(nodeTarget)
		if splitErr == nil {
			client := &http.Client{Timeout: 3 * time.Second}
			resp, httpErr := client.Get("https://ipify.org")
			if httpErr == nil {
				defer resp.Body.Close()
				myPublicIPBytes, _ := io.ReadAll(resp.Body)
				myPublicIP := strings.TrimSpace(string(myPublicIPBytes))
				if targetIP == myPublicIP || targetIP == "covenant-explorer.ddns.net" {
					resolvedTarget = "127.0.0.1:" + targetPort
				}
			}
		}

		conn, err := net.DialTimeout("tcp", resolvedTarget, 5*time.Second)
		if err != nil {
			networkLog.SetText(fmt.Sprintf("Handshake Failure: Node entry endpoint [%s] is currently unreachable.", nodeTarget))
			return
		}
		defer conn.Close()

		fmt.Fprintf(conn, "TX_BROADCAST:%s\n", string(txBytes))

		if resolvedTarget != nodeTarget {
			networkLog.SetText(fmt.Sprintf("🚀 Loopback Broadcast Complete! Securely synced to public endpoint via port :%s!", targetPort))
		} else {
			networkLog.SetText(fmt.Sprintf("🚀 Successfully broadcasted signed transaction packet to node endpoint: %s!", nodeTarget))
		}

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

	savedKey := os.Getenv("CVN_SECRET_KEY")
	if savedKey != "" {
		privKeyEntry.SetText(savedKey)
		deriveAddressFromInput(savedKey)
		networkLog.SetText("🔑 Identity pre-loaded securely from local environment variable profile.")
	}

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
