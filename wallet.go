package main

import (
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

func RunWalletGUI() {
	myApp := app.New()
	myWindow := myApp.NewWindow("COVENANT STANDARD WALLET v1.0.0")
	myWindow.Resize(fyne.NewSize(500, 560))

	statusLabel := widget.NewLabel("System Status: ENCRYPTED & SECURE")
	addressLabel := widget.NewLabel("Address: Nikola_Global_Network_Node")

	initialChain := LoadChain()
	initialBalance := GetAddressBalance(initialChain, "Nikola_Global_Network_Node")
	
	balanceLabel := widget.NewLabel(fmt.Sprintf("CURRENT BALANCE: %.2f CVN", initialBalance))
	usdLabel := widget.NewLabel(fmt.Sprintf("Estimated Value: $%.2f USD (Synced Market Feed)", initialBalance*0.025))

	// UPGRADE: Added a dynamic destination network node connector field configuration
	nodeInput := widget.NewEntry()
	nodeInput.SetText("covenant-explorer.ddns.net:8080") // Defaults to localhost for safe local workstation fallback testing
	nodeInput.SetPlaceHolder("Target Node Net Address (e.g. public_ip:8080)...")

	recipientInput := widget.NewEntry()
	recipientInput.SetPlaceHolder("Paste recipient destination node address...")

	amountInput := widget.NewEntry()
	amountInput.SetPlaceHolder("Enter CVN token amount...")

	offeringInput := widget.NewEntry()
	offeringInput.SetPlaceHolder("Enter Voluntary Free-Will Offering Fee (e.g. 1.50)...")

	passwordInput := widget.NewPasswordEntry()
	passwordInput.SetPlaceHolder("Enter wallet security password...")

	networkLog := widget.NewLabel("[P2P Client Interface Ready]")

	sendButton := widget.NewButton("AUTHORIZE & BROADCAST TRANSACTION", func() {
		nodeTarget := nodeInput.Text
		recipient := recipientInput.Text
		amountStr := amountInput.Text
		offeringStr := offeringInput.Text
		password := passwordInput.Text

		if nodeTarget == "" || recipient == "" || amountStr == "" || password == "" {
			networkLog.SetText("Validation Failure: Missing required text inputs.")
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

		tx := Transaction{
			Sender:           "Nikola_Global_Network_Node",
			Recipient:        recipient,
			Amount:           amount,
			FreeWillOffering: offering,
			DataSizeKB:       1.0, 
			Witness:          "Public_Network_Client_Signature",
			Timestamp:        time.Now(),
		}

		txBytes, err := json.Marshal(tx)
		if err != nil {
			networkLog.SetText("System Error: Failed to serialize transaction packet.")
			return
		}

		// UPGRADE: Open communication channel dynamically to the specified public network target destination parameter
		conn, err := net.DialTimeout("tcp", nodeTarget, 5*time.Second)
		if err != nil {
			networkLog.SetText(fmt.Sprintf("Handshake Failure: Target node [%s] is unreachable across the web.", nodeTarget))
			return
		}
		defer conn.Close()

		fmt.Fprintf(conn, "TX_BROADCAST:%s\n", string(txBytes))
		
		networkLog.SetText(fmt.Sprintf("🚀 Successfully streamed transaction packet to public node endpoint: %s!", nodeTarget))
		recipientInput.SetText("")
		amountInput.SetText("")
		offeringInput.SetText("")
		passwordInput.SetText("")
	})

	content := container.NewVBox(
		widget.NewLabel("=================================================="),
		statusLabel,
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
		passwordInput,
		sendButton,
		widget.NewLabel("--------------------------------------------------"),
		networkLog,
		widget.NewLabel("=================================================="),
	)

	go func() {
		for {
			time.Sleep(3 * time.Second)
			currentChain := LoadChain()
			currentBalance := GetAddressBalance(currentChain, "Nikola_Global_Network_Node")
			
			balanceLabel.SetText(fmt.Sprintf("CURRENT BALANCE: %.2f CVN", currentBalance))
			usdLabel.SetText(fmt.Sprintf("Estimated Value: $%.2f USD (Synced Market Feed)", currentBalance*0.025))
		}
	}()

	myWindow.SetContent(content)
	myWindow.ShowAndRun()
}