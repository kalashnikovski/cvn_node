package node

import (
	"fmt"
	"strings"
)

// GIVING (not a rule of the chain)
//
// Someone sending coins may give something to a second address in the same
// transfer: a church, a charity, a friend. It is one more output of an
// ordinary transfer, chosen by the sender each time they send. The rules of
// the chain know nothing of it: a transfer with a gift is checked like any
// other transfer with two recipients.
//
// The wallet suggests no address and no amount, remembers nothing between
// transfers, and with no gift named a transfer is exactly what it always
// was. A gift cannot be left on by accident.

// giftShareSteps is how finely a share of the amount can be named: hundredths
// of one per cent.
const giftShareSteps = 10000

// ParseGift reads what a sender typed as the gift to go with a transfer of
// amountUnits: either an amount in CVN ("0.5") or a share of the amount being
// sent ("10%", with at most two decimal places). It returns the gift in
// units. A share is rounded down to a whole unit. A gift smaller than the
// smallest coin cannot be sent, and is refused here with the figures.
func ParseGift(text string, amountUnits int64) (int64, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, fmt.Errorf("no amount to give was named")
	}
	smallest := minimumOutputUnits()
	share, isShare := strings.CutSuffix(text, "%")
	if !isShare {
		giftUnits, err := ParseCVN(text)
		if err != nil {
			return 0, fmt.Errorf("the amount to give must be a number of CVN with at most 8 decimal places, or a share of the amount sent such as 10%%")
		}
		if giftUnits < smallest {
			return 0, fmt.Errorf("the smallest amount that can be given is %s CVN", FormatUnits(smallest))
		}
		return giftUnits, nil
	}
	// A share is read with the same care as an amount: "12.5" becomes a
	// whole number of hundred-millionths, which must be a whole number of
	// hundredths of one per cent.
	const unitsPerStep = UnitsPerCVN * 100 / giftShareSteps
	shareUnits, err := ParseCVN(share)
	if err != nil || shareUnits <= 0 || shareUnits%unitsPerStep != 0 || shareUnits > 100*UnitsPerCVN {
		return 0, fmt.Errorf("a share to give must be more than 0%% and at most 100%%, with at most two decimal places, such as 10%% or 2.5%%")
	}
	if amountUnits <= 0 || amountUnits > MaxAmountUnits {
		return 0, fmt.Errorf("a share can only be given of an amount that is being sent")
	}
	steps := shareUnits / unitsPerStep
	// amountUnits * steps / giftShareSteps, rounded down, without the
	// product ever leaving the range of the numbers used here.
	giftUnits := (amountUnits/giftShareSteps)*steps + (amountUnits%giftShareSteps)*steps/giftShareSteps
	if giftUnits < smallest {
		return 0, fmt.Errorf("%s%% of %s CVN is %s CVN, and the smallest amount that can be given is %s CVN", strings.TrimSpace(share), FormatUnits(amountUnits), FormatUnits(giftUnits), FormatUnits(smallest))
	}
	return giftUnits, nil
}

// BuildSignedTransferWithGift assembles and signs a transfer that pays
// amountUnits to `recipient` and giftUnits to `giftTo`, plus feeUnits to the
// miner, returning change to the sender. The gift is in addition to the
// amount: the recipient receives all of amountUnits.
func BuildSignedTransferWithGift(privateKeyHex, publicKeyHex, recipient string, amountUnits int64, giftTo string, giftUnits, feeUnits int64, coins []SpendableCoin) (Transaction, error) {
	recipient, giftTo = strings.TrimSpace(recipient), strings.TrimSpace(giftTo)
	if !validRecipient(recipient) {
		return Transaction{}, fmt.Errorf("invalid recipient address")
	}
	// Coins sent to something that is not a wallet address are gone for
	// good, so the address a gift goes to is held to a wallet address.
	if !looksLikeWalletAddress(giftTo) {
		return Transaction{}, errGiftAddress()
	}
	senderAddress, err := DeriveAddressFromPublicKey(publicKeyHex)
	if err != nil {
		return Transaction{}, err
	}
	if giftTo == recipient {
		return Transaction{}, errGiftToRecipient()
	}
	if giftTo == strings.TrimSpace(senderAddress) {
		return Transaction{}, errGiftToSender()
	}
	smallest := minimumOutputUnits()
	if amountUnits < smallest {
		return Transaction{}, fmt.Errorf("amount must be at least %s CVN", FormatUnits(smallest))
	}
	if giftUnits < smallest {
		return Transaction{}, fmt.Errorf("the smallest amount that can be given is %s CVN", FormatUnits(smallest))
	}
	if feeUnits < 0 {
		return Transaction{}, fmt.Errorf("fee cannot be negative")
	}
	requiredUnits, err := addUnits(amountUnits, giftUnits)
	if err == nil {
		requiredUnits, err = addUnits(requiredUnits, feeUnits)
	}
	if err != nil {
		return Transaction{}, err
	}
	// Coins are taken in the order given until they reach, exactly as for a
	// transfer with no gift (BuildSignedTransfer).
	var inputs []UTXOInput
	var gatheredUnits int64
	for _, coin := range coins {
		if len(inputs) >= TxMaxInputs {
			break
		}
		if !coin.Amount.Spendable() {
			continue
		}
		inputs = append(inputs, walletInput(coin))
		gatheredUnits += coin.Amount.Units()
		if gatheredUnits >= requiredUnits {
			break
		}
	}
	if gatheredUnits < requiredUnits {
		if len(inputs) >= TxMaxInputs {
			return Transaction{}, fmt.Errorf("this amount and gift need more than %d coins in one transfer; the largest %d add up to %s CVN, so send it in smaller parts", TxMaxInputs, TxMaxInputs, FormatUnits(gatheredUnits))
		}
		return Transaction{}, fmt.Errorf("not enough coins are ready to spend: the amount, the gift and the fee together need %s CVN and %s CVN are ready. "+
			"If you have just combined your coins or sent a transfer, your coins are tied up in it until the next block confirms it, usually within a minute or two. "+
			"Your balance is not lost; try again after the next block", FormatUnits(requiredUnits), FormatUnits(gatheredUnits))
	}
	outputs := []UTXOOutput{
		{Recipient: recipient, Amount: UnitsAmount(amountUnits)},
		{Recipient: giftTo, Amount: UnitsAmount(giftUnits)},
	}
	// Change too small to be a coin is left for the miner as part of the
	// fee, as in a transfer with no gift.
	if changeUnits := gatheredUnits - requiredUnits; changeUnits >= smallest {
		outputs = append(outputs, UTXOOutput{Recipient: senderAddress, Amount: UnitsAmount(changeUnits)})
	} else {
		feeUnits += changeUnits
	}
	return signTransfer(privateKeyHex, publicKeyHex, senderAddress, inputs, outputs, feeUnits)
}

// SendTransferWithGiftFromWallet fetches the sender's coins from a node,
// builds a transfer with a gift, signs it and hands it to that node.
func SendTransferWithGiftFromWallet(privateKeyHex, publicKeyHex, recipient string, amountUnits int64, giftTo string, giftUnits, feeUnits int64) (Transaction, error) {
	if recipient = strings.TrimSpace(recipient); !looksLikeWalletAddress(recipient) {
		return Transaction{}, fmt.Errorf("%q is not a CVN wallet address on this network. An address is %s. Check it against the one you were given, character by character. Nothing was sent", recipient, addressFormDescription())
	}
	senderAddress, err := DeriveAddressFromPublicKey(strings.TrimSpace(publicKeyHex))
	if err != nil {
		return Transaction{}, err
	}
	coins, gateway, err := FetchSpendableCoinsOverHTTP(senderAddress)
	if err != nil {
		return Transaction{}, err
	}
	tx, err := BuildSignedTransferWithGift(strings.TrimSpace(privateKeyHex), strings.TrimSpace(publicKeyHex), recipient, amountUnits, giftTo, giftUnits, feeUnits, coins)
	if err != nil {
		return Transaction{}, err
	}
	if err := SubmitTransferOverHTTP(gateway, tx); err != nil {
		return Transaction{}, err
	}
	return tx, nil
}

// The reasons a gift's address is refused. None of them repeats what was
// typed: a person who pastes something else into that field by mistake, a
// private key for instance, must not have it shown back to them on a screen.
func errGiftAddress() error {
	return fmt.Errorf("what was typed as the address to give to is not a CVN wallet address on this network, so nothing can be given to it. An address is %s", addressFormDescription())
}

func errGiftToRecipient() error {
	return fmt.Errorf("the gift is for the same address the transfer goes to; send that address the whole amount instead")
}

func errGiftToSender() error {
	return fmt.Errorf("the gift is for the address it is sent from, which gives nothing to anyone")
}

// giftFromWhatWasTyped settles what a sender typed in the two gift fields of
// a wallet, for a transfer of amountUnits to `recipient`. With both empty
// there is no gift (giftUnits is 0 and the error is nil). With one filled and
// the other empty nothing is guessed: it is an error, so that no transfer
// goes out without the gift its sender meant, or with one they did not.
func giftFromWhatWasTyped(giftTo, giftText, recipient string, amountUnits int64) (string, int64, error) {
	giftTo, giftText = strings.TrimSpace(giftTo), strings.TrimSpace(giftText)
	switch {
	case giftTo == "" && giftText == "":
		return "", 0, nil
	case giftTo == "":
		return "", 0, fmt.Errorf("an amount to give was named, but no address to give it to. Fill in both, or neither")
	case giftText == "":
		return "", 0, fmt.Errorf("an address to give to was named, but no amount. Fill in both, or neither")
	}
	if !looksLikeWalletAddress(giftTo) {
		return "", 0, errGiftAddress()
	}
	if giftTo == strings.TrimSpace(recipient) {
		return "", 0, errGiftToRecipient()
	}
	giftUnits, err := ParseGift(giftText, amountUnits)
	if err != nil {
		return "", 0, err
	}
	return giftTo, giftUnits, nil
}

// giftInWords is what a wallet shows before a transfer with a gift is sent:
// the gift as a figure in CVN (a share has been worked out by then), whom it
// is for, and everything that leaves the wallet. Change too small to be a
// coin, less than the smallest coin, goes to the miner with the fee, as in
// any transfer; that is known only once the coins are chosen, and is said
// here in words.
func giftInWords(amountUnits, giftUnits, feeUnits int64, giftTo string) string {
	return fmt.Sprintf("%s CVN will be given to %s, in addition to the amount. In all %s CVN leaves this wallet: %s CVN to the recipient, %s CVN given and a fee of %s CVN (and, as with any transfer, change of less than %s CVN if there is any).",
		FormatUnits(giftUnits), giftTo, FormatUnits(amountUnits+giftUnits+feeUnits), FormatUnits(amountUnits), FormatUnits(giftUnits), FormatUnits(feeUnits), FormatUnits(minimumOutputUnits()))
}
