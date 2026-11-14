package node

import (
	"strings"
	"testing"
)

// On the launch rules an address carries a check on itself, so that a slip
// of the hand gives something that is not an address at all.
func TestAddressesCarryACheckOnTheLaunchRules(t *testing.T) {
	onNetwork(t, "devnet")
	_, publicKey, address, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	if len(address) != len("CVN_")+48 || !looksLikeWalletAddress(address) {
		t.Fatalf("a new address should be CVN_ and 48 characters and be accepted: %s", address)
	}
	if again, _ := DeriveAddressFromPublicKey(publicKey); again != address {
		t.Error("the same key gave two different addresses")
	}

	// Every single-character slip, in every position, is caught.
	missed := 0
	for i := len("CVN_"); i < len(address); i++ {
		for _, wrong := range "0123456789abcdef" {
			if byte(wrong) == address[i] {
				continue
			}
			if looksLikeWalletAddress(address[:i] + string(wrong) + address[i+1:]) {
				missed++
			}
		}
	}
	if missed != 0 {
		t.Errorf("%d single-character mistakes were accepted as addresses", missed)
	}
	// So are two neighbouring characters swapped, the commonest copying slip.
	for i := len("CVN_"); i < len(address)-1; i++ {
		if address[i] == address[i+1] {
			continue
		}
		swapped := address[:i] + string(address[i+1]) + string(address[i]) + address[i+2:]
		if looksLikeWalletAddress(swapped) {
			t.Errorf("swapping characters %d and %d was not caught", i, i+1)
		}
	}
	for name, text := range map[string]string{
		"an address cut short":              address[:len(address)-1],
		"an address with a character added": address + "0",
		"an address in capitals":            strings.ToUpper(address),
		"an address without its check":      address[:len("CVN_")+40],
		"another prefix":                    "cvn_" + address[4:],
		"words":                             "CVN_my_wallet",
		"nothing":                           "",
	} {
		if looksLikeWalletAddress(text) {
			t.Errorf("%s was accepted as an address: %q", name, text)
		}
	}
}

// The rules themselves refuse a coin made out to anything but an address, so
// that no wallet, however careless, can lose coins that way.
func TestCoinsCanOnlyBeMadeOutToAnAddress(t *testing.T) {
	startTestChain(t, "devnet")
	privateKey, publicKey, owner, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	extendTestChain(t, Params.GenesisTimestamp+60, owner, nil)
	coins := ListSpendableCoins(owner)
	good := testAddress("someone")
	mistyped := good[:20] + map[bool]string{true: "0", false: "1"}[good[20] != '0'] + good[21:]

	if _, err := BuildSignedTransfer(privateKey, publicKey, mistyped, cvn("10"), 0, coins); err == nil {
		t.Error("the wallet built a transfer to a mistyped address")
	}
	// A transfer made by hand, by software that does not check, is refused by
	// the rules: in the pool, and in a block.
	handMade, err := BuildSignedTransfer(privateKey, publicKey, good, cvn("10"), 0, coins)
	if err != nil {
		t.Fatal(err)
	}
	handMade.Outputs[0].Recipient = mistyped
	handMade.ID = ComputeTransactionID(handMade)
	r, s, _ := SignTransactionPayload(privateKey, handMade.ID)
	handMade.Signature = r + "|" + s
	if err := AdmitTransactionToMempool(handMade); err == nil {
		t.Error("a transfer to a mistyped address was taken into the pool")
	}
	block := mineTestBlock(t, Params.GenesisTimestamp+120, testAddress("miner"), nil, 0)
	block.Transactions = append(block.Transactions, handMade)
	digest := BlockTransactionsDigest(block.Index, block.Transactions)
	for nonce := int64(0); ; nonce++ {
		block.Nonce = nonce
		block.Hash = CalculateHashWithTxDigest(block, digest)
		if BlockProofValid(block, block.Difficulty) {
			break
		}
	}
	if ValidateBlockSignatures(block) {
		t.Error("a block holding a transfer to a mistyped address was accepted")
	}
	// Nor may a block pay its reward to something that is not an address.
	reward := mineTestBlock(t, Params.GenesisTimestamp+120, "CVN_not_an_address", nil, 0)
	if ValidateBlockSignatures(reward) {
		t.Error("a block paying its reward to text that is not an address was accepted")
	}
}

// An address a miner saved before the check existed is completed, since the
// check follows from the rest. An address someone types is not: there the
// check is the point.
func TestAnOlderMiningAddressIsCompleted(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("CVN_MINER_ADDRESS", "")
	onNetwork(t, "devnet")
	_, _, full, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	older := full[:len("CVN_")+40]
	if address, err := resolveMinerAddress([]string{"cvn_node", "--miner-address", older}); err != nil || address != full {
		t.Errorf("an address without its check was not completed: %q %v", address, err)
	}
	if address, err := resolveMinerAddress([]string{"cvn_node", "--miner-address", full}); err != nil || address != full {
		t.Errorf("a full address was changed or refused: %q %v", address, err)
	}
	wrongCheck := full[:len(full)-1] + map[bool]string{true: "0", false: "1"}[full[len(full)-1] != '0']
	if _, err := resolveMinerAddress([]string{"cvn_node", "--miner-address", wrongCheck}); err == nil {
		t.Error("an address with the wrong check was accepted for mining")
	}
	// The wallet does not complete what is typed into it.
	privateKey, publicKey, _, _ := GenerateKeyPair()
	if _, err := SendTransferFromWallet(privateKey, publicKey, older, cvn("1"), 0); err == nil || !strings.Contains(err.Error(), "not a CVN wallet address") {
		t.Errorf("the wallet accepted an address without its check: %v", err)
	}
}

// The first test network's addresses are as they always were.
func TestTestnet1AddressesAreUnchanged(t *testing.T) {
	onNetwork(t, "testnet-1")
	_, _, address, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	if len(address) != len("CVN_")+40 || !looksLikeWalletAddress(address) {
		t.Errorf("a testnet-1 address should be CVN_ and 40 characters: %s", address)
	}
	if !validRecipient("CVN_anything") {
		t.Error("testnet-1 must go on accepting the recipients it always has")
	}
}
