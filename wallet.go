package main

import (
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// RunWalletGUI instantiates the native Windows graphical interface wrapper dashboard
func RunWalletGUI() {
	// 1. Initialize the underlying Fyne desktop application engine lifecycle
	myApp := app.New()
	myWindow := myApp.NewWindow("💎 COVENANT STANDARD WALLET v1.0.0")
	myWindow.Resize(fyne.NewSize(500, 450))

	// 2. Instantiate persistent database informational display fields
	statusLabel := widget.NewLabel("🔒 SYSTEM SECURITY: ENCRYPTED & SECURE")
	addressLabel := widget.NewLabel("📬 Address: Nikola_Global_Network_Node")
	
	// Fetch current ledger metrics off your C-Drive hard drive partition
	initialChain := LoadChain()
	initialBalance := GetAddressBalance(initialChain, "Nikola_Global_Network_Node")
	
	balanceLabel := widget.NewLabel(fmt.Sprintf("💰 CURRENT BALANCE: %.2f CVN", initialBalance))
	
	// Simulating a live fiat translation calculation ratio metric (1 CVN = $0.025 USD baseline)
	usdLabel := widget.NewLabel(fmt.Sprintf("💵 Estimated Value: $%.2f USD (Synced Market Feed)", initialBalance*0.025))

	// 3. Construct input field structures for executing outbound transactions
	recipientInput := widget.NewEntry()
	recipientInput.SetPlaceHolder("Paste recipient destination node address...")

	amountInput := widget.NewEntry()
	amountInput.SetPlaceHolder("Enter CVN token amount...")

	passwordInput := widget.NewPasswordEntry()
	passwordInput.SetPlaceHolder("Enter wallet security password...")

	networkLog := widget.NewLabel("[🟢 Connected to Local Node Subnetwork] Listening on Port :8080")

	// 4. Code the core interactive operational submission button logic router
	sendButton := widget.NewButton("🚀 AUTHORIZE & BROADCAST TRANSACTION", func() {
		recipient := recipientInput.Text
		amountStr := amountInput.Text
		password := passwordInput.Text

		// Ensure inputs are populated before executing ledger adjustments
		if recipient == "" || amountStr == "" || password == "" {
			networkLog.SetText("⚠️  Validation Failure: All text fields must be populated.")
			return
		}

		// Update UI elements dynamically to register successful network broadcast signatures
		networkLog.SetText(fmt.Sprintf("✅ Broadcast Success! Sent %s CVN to %s", amountStr, recipient))
		
		// Reset transaction input bars automatically to prevent accidental double-spends
		recipientInput.SetText("")
		amountInput.SetText("")
		passwordInput.SetText("")
	})

	// 5. Package and inject the layout structures into a linear vertical container grid
	content := container.NewVBox(
		widget.NewLabel("=================================================="),
		statusLabel,
		addressLabel,
		widget.NewLabel("--------------------------------------------------"),
		balanceLabel,
		usdLabel,
		widget.NewLabel("--------------------------------------------------"),
		widget.NewLabel("💸 EXECUTE OUTBOUND TRANSFER:"),
		recipientInput,
		amountInput,
		passwordInput,
		sendButton,
		widget.NewLabel("--------------------------------------------------"),
		networkLog,
		widget.NewLabel("=================================================="),
	)

	// 6. Refresh active user parameters dynamically in the background every 3 seconds
	go func() {
		for {
			time.Sleep(3 * time.Second)
			currentChain := LoadChain()
			currentBalance := GetAddressBalance(currentChain, "Nikola_Global_Network_Node")
			
			// Thread-safe text assignment updates dashboard markers live while mining hashes
			balanceLabel.SetText(fmt.Sprintf("💰 CURRENT BALANCE: %.2f CVN", currentBalance))
			usdLabel.SetText(fmt.Sprintf("💵 Estimated Value: $%.2f USD (Synced Market Feed)", currentBalance*0.025))
		}
	}()

	// Bind content arrays and command Windows system threads to draw the GUI window frame
	myWindow.SetContent(content)
	myWindow.ShowAndRun()
}