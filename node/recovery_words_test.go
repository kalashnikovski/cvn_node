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

	// A word swapped for another word of the list is caught by the check
	// 255 times in 256. The last word holds three bits of the key and the
	// eight bits of the check. So of the 2,048 words that could stand last,
	// exactly eight are accepted: one for each value of those three bits,
	// with the check that belongs to it. The right word is one of the
	// eight, and each of the other seven gives another key.
	//
	// (This used to put "zoo" in the last place and expect a refusal. That
	// was wrong for one key in 256, for which "zoo" is one of the eight, so
	// the test failed by chance about once in 256 runs.)
	acceptedLast, rightWordGivesTheKey := 0, false
	for _, word := range recoveryWordList {
		tried := append(append([]string{}, list[:23]...), word)
		key, err := keyFromRecoveryWords(strings.Join(tried, " "))
		if err != nil {
			continue
		}
		acceptedLast++
		if word == list[23] {
			rightWordGivesTheKey = key == privateKeyHex
		} else if key == privateKeyHex {
			t.Error("another word in the last place gave the same key")
		}
	}
	if acceptedLast != 8 || !rightWordGivesTheKey {
		t.Errorf("%d of the %d words were accepted in the last place, want 8; the right word gives the key: %v", acceptedLast, len(recoveryWordList), rightWordGivesTheKey)
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
		if count == 1 && looksLikeHex(text) {
			// With no key saved, a text made only of the characters 0-9
			// and a-f is taken for a private key written without its
			// leading zeros (TestWithNoSavedKeyAPasswordIsNotAKey). A few
			// words of the list are made of those letters alone, such as
			// face and dead. When a key's first word is one of them, that
			// one word is a key by that rule, and this test failed, about
			// once in two hundred runs. It says nothing about the words.
			continue
		}
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

// A key is 32 bytes, written as 64 characters. About one key in 256 begins
// with a zero byte, and programs up to rc12 wrote such a key without its
// zeros: 62 characters. A wallet with a key saved on the computer then took
// it for that key's password and refused it. These are such keys, typed each
// way they have ever been written, with no key saved, with this key saved
// and with another wallet's key saved.
func TestAKeyThatBeginsWithZerosIsStillAKey(t *testing.T) {
	useTemporarySavedKey(t)
	const password = "correct horse battery"
	anotherWallet := strings.Repeat("5c", 32)
	for _, whole := range []string{
		"00" + strings.Repeat("7a", 31),       // written as 62 characters
		"0" + strings.Repeat("7a3", 21),       // written as 63
		"0000" + strings.Repeat("7a", 30),     // written as 60
		"00000000" + strings.Repeat("7a", 28), // written as 56
	} {
		short := strings.TrimLeft(whole, "0")
		wantPublic, err := publicKeyFromPrivateKey(whole)
		if err != nil {
			t.Fatalf("the test's own key of %d characters is not a key: %v", len(short), err)
		}
		for _, saved := range []string{"", whole, anotherWallet} {
			_ = os.Remove(savedKeyFile)
			if saved != "" {
				if _, err := saveKeyWithPassword(saved, password); err != nil {
					t.Fatal(err)
				}
			}
			for _, typed := range []string{whole, short, strings.ToUpper(short)} {
				got, gotPublic, err := signingKeyFrom(typed)
				if err != nil || got != whole || gotPublic != wantPublic {
					t.Errorf("a key of %d characters typed with %d (a key saved: %v, this one: %v) was not read as itself: %v",
						len(short), len(typed), saved != "", saved == whole, err)
				}
			}
			// The saved key's password still opens the saved key.
			if saved != "" {
				if got, _, err := signingKeyFrom(password); err != nil || got != saved {
					t.Errorf("the password stopped opening the saved key: %v", err)
				}
			}
		}
	}
}

// A new key is written with all 64 characters, zeros included. One key in
// 256 begins with a zero byte, so among 4,000 there are about fifteen: a
// program that dropped them would not get through this.
func TestANewKeyIsAlwaysWrittenAtFullLength(t *testing.T) {
	for i := 0; i < 4000; i++ {
		privateKeyHex, publicKeyHex, address, err := GenerateKeyPair()
		if err != nil {
			t.Fatal(err)
		}
		if len(privateKeyHex) != 64 || len(publicKeyHex) != 128 {
			t.Fatalf("key %d was written with %d characters and its public key with %d", i, len(privateKeyHex), len(publicKeyHex))
		}
		if i%400 != 0 {
			continue
		}
		// And it is the key of the address it came with.
		rebuilt, err := publicKeyFromPrivateKey(privateKeyHex)
		if err != nil || rebuilt != publicKeyHex {
			t.Fatalf("key %d does not give back its own public key: %v", i, err)
		}
		if again, _ := DeriveAddressFromPublicKey(rebuilt); again != address {
			t.Fatalf("key %d does not give back its own address", i)
		}
	}
}

// A password may be made only of the characters 0-9 and a-f, as one made by
// a program often is ("openssl rand -hex 32" gives 64 of them). It is the
// password all the same, and must not be taken for some other wallet's key.
func TestAPasswordThatLooksLikeAKeyIsStillThePassword(t *testing.T) {
	useTemporarySavedKey(t)
	key := strings.Repeat("3d", 32)
	wantPublic, err := publicKeyFromPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	for _, password := range []string{
		strings.Repeat("ab12", 16), // 64 characters
		strings.Repeat("ab12", 14), // 56
		strings.Repeat("ab12", 8),  // 32
		"19841207",
	} {
		if _, err := saveKeyWithPassword(key, password); err != nil {
			t.Fatal(err)
		}
		got, gotPublic, err := signingKeyFrom(password)
		if err != nil || got != key || gotPublic != wantPublic {
			t.Errorf("a password of %d characters, all of them 0-9 and a-f, did not open the saved key: %v", len(password), err)
		}
	}

	// A password typed wrongly is refused and is never taken for a key,
	// unless it is as long as a key.
	for _, typed := range []string{"19841208", "deadbeef", strings.Repeat("ab12", 8)[1:], strings.Repeat("a", wholeKeyAtLeast-1)} {
		if _, _, err := signingKeyFrom(typed); err == nil || !strings.Contains(err.Error(), "not the password") {
			t.Errorf("a wrong password of %d characters gave: %v", len(typed), err)
		}
	}

	// Another wallet's whole key is still that wallet's key.
	anotherWallet := strings.Repeat("5c", 32)
	if got, _, err := signingKeyFrom(anotherWallet); err != nil || got != anotherWallet {
		t.Errorf("another wallet's key was not read as itself with a key saved: %v", err)
	}
}
