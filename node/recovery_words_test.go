package node

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The word list is Bitcoin's, unchanged and in order. A word stands for its
// place in the list, so one word out of place would turn every set of
// recovery words written down before it into a different key.
func TestTheRecoveryWordListIsTheStandardOne(t *testing.T) {
	if len(recoveryWordList) != 2048 {
		t.Fatalf("the list holds %d words, want 2048", len(recoveryWordList))
	}
	sum := sha256.Sum256([]byte(strings.Join(recoveryWordList, "\n") + "\n"))
	const want = "2f5eed53a4727b4bf8880d8f3f199efc90e58503646d9ff8eff3a2ed3b24dbda"
	if got := hex.EncodeToString(sum[:]); got != want {
		t.Fatalf("the word list's SHA-256 is %s, want %s", got, want)
	}
	if len(recoveryWordNumber) != 2048 {
		t.Fatal("a word appears twice in the list")
	}
}

// The published BIP39 test values: these keys must give exactly these words.
func TestRecoveryWordsMatchThePublishedValues(t *testing.T) {
	cases := map[string]string{
		"0000000000000000000000000000000000000000000000000000000000000000": "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon art",
		"7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f": "legal winner thank year wave sausage worth useful legal winner thank year wave sausage worth useful legal winner thank year wave sausage worth title",
		"8080808080808080808080808080808080808080808080808080808080808080": "letter advice cage absurd amount doctor acoustic avoid letter advice cage absurd amount doctor acoustic avoid letter advice cage absurd amount doctor acoustic bless",
		"ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff": "zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo vote",
		"68a79eaca2324873eacc50cb9c6eca8cc68ea5d936f98787c60c7ebc74e6ce7c": "hamster diagram private dutch cause delay private meat slide toddler razor book happy fancy gospel tennis maple dilemma loan word shrug inflict delay length",
	}
	for key, want := range cases {
		got, err := recoveryWordsFromKey(key)
		if err != nil || got != want {
			t.Errorf("a published key gave %q (%v), want %q", got, err, want)
		}
		back, err := keyFromRecoveryWords(want)
		if err != nil || back != key {
			t.Errorf("the published words gave back another key (%v)", err)
		}
	}
}

func TestRecoveryWordsAndTheKeyAreTheSameThing(t *testing.T) {
	for i := 0; i < 50; i++ {
		privateKeyHex, publicKeyHex, _, err := GenerateKeyPair()
		if err != nil {
			t.Fatal(err)
		}
		words, err := recoveryWordsFromKey(privateKeyHex)
		if err != nil || len(strings.Fields(words)) != 24 {
			t.Fatalf("a new key has no 24 words: %v", err)
		}
		// Typed back carelessly: capitals, extra spaces, a line break.
		typed := "  " + strings.ToUpper(strings.Replace(words, " ", "  \n ", 5)) + " "
		gotPrivate, gotPublic, err := signingKeyFrom(typed)
		if err != nil {
			t.Fatalf("the words were not accepted where a wallet asks for the key: %v", err)
		}
		wantPrivate, _, err := signingKeyFrom(privateKeyHex)
		if err != nil || gotPrivate != wantPrivate || gotPublic != publicKeyHex {
			t.Fatal("the words and the key they were made from do not open the same wallet")
		}
	}
}

func TestWrongRecoveryWordsAreNoticed(t *testing.T) {
	privateKeyHex, _, _, _ := GenerateKeyPair()
	words, _ := recoveryWordsFromKey(privateKeyHex)
	list := strings.Fields(words)

	// A word swapped for another word of the list: caught by the check,
	// 255 times in 256. Changing the last word alone always changes the check.
	changed := append([]string{}, list...)
	if changed[23] == "zoo" {
		changed[23] = "abandon"
	} else {
		changed[23] = "zoo"
	}
	if _, err := keyFromRecoveryWords(strings.Join(changed, " ")); err == nil {
		// zoo and the right word can share a check only if they are the same word.
		t.Error("a changed last word was accepted")
	}
	// A word that is no word of the list is named.
	misspelt := append([]string{}, list...)
	misspelt[4] = "covenent"
	if _, err := keyFromRecoveryWords(strings.Join(misspelt, " ")); err == nil || !strings.Contains(err.Error(), "word 5") {
		t.Errorf("a misspelt fifth word gave: %v", err)
	}
	// Too few, too many.
	for _, count := range []int{0, 1, 12, 23, 25} {
		text := strings.Join(append(append([]string{}, list...), list...)[:count], " ")
		if _, _, err := signingKeyFrom(text); err == nil {
			t.Errorf("%d words were accepted as a key", count)
		}
	}
	// Out of order: over many keys, swapping two different words is caught
	// almost always. Any one acceptance in 40 tries would be a broken check.
	accepted := 0
	for i := 0; i < 40; i++ {
		key, _, _, _ := GenerateKeyPair()
		w, _ := recoveryWordsFromKey(key)
		l := strings.Fields(w)
		if l[0] == l[1] {
			continue
		}
		l[0], l[1] = l[1], l[0]
		if _, err := keyFromRecoveryWords(strings.Join(l, " ")); err == nil {
			accepted++
		}
	}
	if accepted > 3 {
		t.Errorf("words out of order were accepted %d times in 40", accepted)
	}
}

func useTemporarySavedKey(t *testing.T) {
	t.Helper()
	oldFile, oldRounds := savedKeyFile, savedKeyRounds
	savedKeyFile, savedKeyRounds = filepath.Join(t.TempDir(), "wallet_key.json"), 1000
	t.Cleanup(func() { savedKeyFile, savedKeyRounds = oldFile, oldRounds })
}

func TestAKeySavedWithAPassword(t *testing.T) {
	useTemporarySavedKey(t)
	privateKeyHex, publicKeyHex, address, _ := GenerateKeyPair()
	address = strings.TrimSpace(address)
	wantPrivate, _, _ := signingKeyFrom(privateKeyHex)
	const password = "correct horse battery"

	if _, err := saveKeyWithPassword(privateKeyHex, "short"); err == nil {
		t.Error("a five-character password was accepted")
	}
	if _, found := savedKeyAddress(); found {
		t.Fatal("a refused password still wrote a file")
	}
	savedFor, err := saveKeyWithPassword(privateKeyHex, password)
	if err != nil || savedFor != address {
		t.Fatalf("saving the key: address %q, %v", savedFor, err)
	}
	if got, found := savedKeyAddress(); !found || got != address {
		t.Errorf("the saved key says it belongs to %q", got)
	}

	// The key itself is nowhere in the file.
	data, _ := os.ReadFile(savedKeyFile)
	if strings.Contains(strings.ToLower(string(data)), strings.ToLower(wantPrivate)) || strings.Contains(string(data), password) {
		t.Fatal("the file holds the key or the password in the clear")
	}

	// The password opens it, anywhere a wallet asks for the key.
	gotPrivate, gotPublic, err := signingKeyFrom(password)
	if err != nil || gotPrivate != wantPrivate || gotPublic != publicKeyHex {
		t.Fatalf("the password did not give back the key: %v", err)
	}
	// A wrong password does not, and says so.
	if _, _, err := signingKeyFrom("correct horse battery!"); err == nil || !strings.Contains(err.Error(), "not the password") {
		t.Errorf("a wrong password gave: %v", err)
	}
	// The key and the words still work with a saved key present.
	if got, _, err := signingKeyFrom(privateKeyHex); err != nil || got != wantPrivate {
		t.Errorf("the key itself stopped working once a key was saved: %v", err)
	}
	words, _ := recoveryWordsFromKey(privateKeyHex)
	if got, _, err := signingKeyFrom(words); err != nil || got != wantPrivate {
		t.Errorf("the words stopped working once a key was saved: %v", err)
	}

	// A file edited to show another address does not open.
	edited := strings.Replace(string(data), address, "CVN_"+strings.Repeat("0", 48), 1)
	if err := os.WriteFile(savedKeyFile, []byte(edited), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := openSavedKey(password); err == nil {
		t.Error("a saved key whose address was edited still opened")
	}
	// A file asking for an absurd amount of work is refused at once.
	absurd := strings.Replace(string(data), `"rounds": 1000`, `"rounds": 2000000000`, 1)
	if absurd == string(data) {
		t.Fatal("the test did not find the rounds in the file")
	}
	_ = os.WriteFile(savedKeyFile, []byte(absurd), 0600)
	if _, err := openSavedKey(password); err == nil {
		t.Error("a saved key asking for two thousand million rounds was not refused")
	}
}

func TestWithNoSavedKeyAPasswordIsNotAKey(t *testing.T) {
	useTemporarySavedKey(t)
	for _, typed := range []string{"correct horse battery", "hunter2!", ""} {
		if _, _, err := signingKeyFrom(typed); err == nil {
			t.Errorf("%q was accepted as a key with no key saved", typed)
		}
	}
	// A key written without its leading zeros is still a key, as before.
	if _, _, err := signingKeyFrom("1"); err != nil {
		t.Errorf("the key 1 was refused: %v", err)
	}
}
