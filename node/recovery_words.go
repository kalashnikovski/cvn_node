package node

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// RECOVERY WORDS AND THE SAVED KEY
//
// A private key is 64 characters of 0-9 and a-f. Copied by hand, one wrong
// character gives a different key, and nothing says so until the coins
// cannot be spent. Two things make a key easier to keep:
//
//   - Recovery words: the same key written as 24 ordinary English words, the
//     last of which carries a check. A wrong or misplaced word is noticed at
//     once. The words ARE the key, written another way; nothing is derived,
//     so every key ever made has its words, and either form opens the wallet.
//     The word list and the encoding are those of Bitcoin's BIP39, so the
//     words can be written on any card or steel plate made for that.
//
//   - A saved key: the key kept in a file on this computer, locked with a
//     password, so that it need not be pasted for every transfer. The file
//     is useless without the password. It is a convenience and not a
//     backup: the recovery words on paper are the backup.
//
// Neither changes anything about the network. An address, a signature and a
// transfer are exactly what they were.

var recoveryWordList = strings.Fields(recoveryWordsEnglish)

var recoveryWordNumber = func() map[string]int {
	numbers := make(map[string]int, len(recoveryWordList))
	for number, word := range recoveryWordList {
		numbers[word] = number
	}
	return numbers
}()

const recoveryWordCount = 24

// privateKeyBytes reads a private key written in hex into exactly 32 bytes.
func privateKeyBytes(privateKeyHex string) ([]byte, error) {
	privateKeyHex = strings.TrimSpace(privateKeyHex)
	if len(privateKeyHex)%2 == 1 {
		privateKeyHex = "0" + privateKeyHex
	}
	raw, err := hex.DecodeString(privateKeyHex)
	if err != nil || len(raw) == 0 || len(raw) > 32 {
		return nil, fmt.Errorf("that is not a valid private key")
	}
	key := make([]byte, 32)
	copy(key[32-len(raw):], raw)
	return key, nil
}

// recoveryWordsFromKey writes a private key as 24 words: its 256 bits, then
// the first 8 bits of their SHA-256 hash as a check, cut into 24 groups of
// 11 bits, each naming one of the 2,048 words.
func recoveryWordsFromKey(privateKeyHex string) (string, error) {
	key, err := privateKeyBytes(privateKeyHex)
	if err != nil {
		return "", err
	}
	check := sha256.Sum256(key)
	bits := append(key, check[0])
	words := make([]string, recoveryWordCount)
	for i := range words {
		number := 0
		for b := 0; b < 11; b++ {
			bit := i*11 + b
			number = number<<1 | int(bits[bit/8]>>(7-bit%8)&1)
		}
		words[i] = recoveryWordList[number]
	}
	return strings.Join(words, " "), nil
}

// keyFromRecoveryWords reads 24 recovery words back into the private key,
// in hex. Capital letters and extra spaces do not matter. A word that is not
// in the list, or a set of words whose check does not hold, is refused with
// a message that says which.
func keyFromRecoveryWords(text string) (string, error) {
	words := strings.Fields(strings.ToLower(text))
	if len(words) != recoveryWordCount {
		return "", fmt.Errorf("recovery words come in a set of %d; %d were given", recoveryWordCount, len(words))
	}
	bits := make([]byte, 33)
	for i, word := range words {
		number, known := recoveryWordNumber[word]
		if !known {
			return "", fmt.Errorf("word %d (%q) is not one of the recovery words. Check its spelling", i+1, word)
		}
		for b := 0; b < 11; b++ {
			if number>>(10-b)&1 == 1 {
				bit := i*11 + b
				bits[bit/8] |= 1 << (7 - bit%8)
			}
		}
	}
	if check := sha256.Sum256(bits[:32]); check[0] != bits[32] {
		return "", fmt.Errorf("these %d words do not belong together: one of them is wrong, or they are out of order", recoveryWordCount)
	}
	return hex.EncodeToString(bits[:32]), nil
}

// savedKeyFile is where a key locked with a password is kept: beside the
// program, like miner_config.json. A variable so tests can use another place.
var savedKeyFile = "wallet_key.json"

// savedKeyRounds is how many rounds of PBKDF2 turn the password into the key
// that locks the file. It makes each guess at the password slow. A variable
// so tests need not wait.
var savedKeyRounds = 600000

const savedKeyMinPassword = 8

type savedKey struct {
	About     string `json:"about"`
	Address   string `json:"address"`
	Method    string `json:"method"`
	Rounds    int    `json:"rounds"`
	Salt      string `json:"salt"`
	Nonce     string `json:"nonce"`
	LockedKey string `json:"locked_key"`
}

const savedKeyMethod = "pbkdf2-sha256, aes-256-gcm"

func savedKeyCipher(password string, salt []byte, rounds int) (cipher.AEAD, error) {
	lock, err := pbkdf2.Key(sha256.New, password, salt, rounds, 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(lock)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// saveKeyWithPassword writes the private key to savedKeyFile, locked with
// the password. It returns the address the key belongs to.
func saveKeyWithPassword(privateKeyHex, password string) (string, error) {
	if len(password) < savedKeyMinPassword {
		return "", fmt.Errorf("the password must be at least %d characters", savedKeyMinPassword)
	}
	key, err := privateKeyBytes(privateKeyHex)
	if err != nil {
		return "", err
	}
	publicKeyHex, err := publicKeyFromPrivateKey(hex.EncodeToString(key))
	if err != nil {
		return "", err
	}
	address, err := DeriveAddressFromPublicKey(publicKeyHex)
	if err != nil {
		return "", err
	}
	address = strings.TrimSpace(address)
	salt, nonce := make([]byte, 16), make([]byte, 12)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealer, err := savedKeyCipher(password, salt, savedKeyRounds)
	if err != nil {
		return "", err
	}
	// The address is bound into the lock, so a file edited to show another
	// address does not open.
	record := savedKey{
		About:     "A CVN private key, locked with a password. It cannot be opened without the password. It is not a backup: keep your 24 recovery words on paper.",
		Address:   address,
		Method:    savedKeyMethod,
		Rounds:    savedKeyRounds,
		Salt:      hex.EncodeToString(salt),
		Nonce:     hex.EncodeToString(nonce),
		LockedKey: hex.EncodeToString(sealer.Seal(nil, nonce, key, []byte(address))),
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(savedKeyFile, append(data, '\n'), 0600); err != nil {
		return "", err
	}
	return address, nil
}

// readSavedKey reads savedKeyFile without opening the lock.
func readSavedKey() (savedKey, bool) {
	data, err := os.ReadFile(savedKeyFile)
	if err != nil {
		return savedKey{}, false
	}
	var record savedKey
	if json.Unmarshal(data, &record) != nil || record.LockedKey == "" {
		return savedKey{}, false
	}
	return record, true
}

// savedKeyAddress is the address of the key saved on this computer, if any.
func savedKeyAddress() (string, bool) {
	record, found := readSavedKey()
	return record.Address, found
}

// openSavedKey unlocks the saved key with the password and returns it in hex.
func openSavedKey(password string) (string, error) {
	record, found := readSavedKey()
	if !found {
		return "", fmt.Errorf("no key is saved on this computer")
	}
	// The rounds are read from the file, which anyone who can write to the
	// folder can edit. A file asking for an absurd number is refused, so it
	// cannot hang the wallet.
	if record.Method != savedKeyMethod || record.Rounds < 1 || record.Rounds > 50_000_000 {
		return "", fmt.Errorf("%s is not a saved key this program can open", savedKeyFile)
	}
	salt, errSalt := hex.DecodeString(record.Salt)
	nonce, errNonce := hex.DecodeString(record.Nonce)
	locked, errLocked := hex.DecodeString(record.LockedKey)
	if errSalt != nil || errNonce != nil || errLocked != nil || len(nonce) != 12 {
		return "", fmt.Errorf("%s is damaged", savedKeyFile)
	}
	sealer, err := savedKeyCipher(password, salt, record.Rounds)
	if err != nil {
		return "", err
	}
	key, err := sealer.Open(nil, nonce, locked, []byte(record.Address))
	if err != nil || len(key) != 32 {
		return "", fmt.Errorf("that is not the password of the key saved on this computer")
	}
	return hex.EncodeToString(key), nil
}

func looksLikeHex(text string) bool {
	if text == "" {
		return false
	}
	for _, c := range text {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

// signingKeyFrom works out the private key from what a person typed where a
// wallet asks for it. That may be:
//
//   - the 24 recovery words;
//   - the private key itself, in hex;
//   - the password of the key saved on this computer.
//
// It returns the private key as 64 hex characters, and its public key.
func signingKeyFrom(typed string) (privateKeyHex, publicKeyHex string, err error) {
	typed = strings.TrimSpace(typed)
	_, haveSavedKey := readSavedKey()
	switch {
	case typed == "":
		return "", "", fmt.Errorf("no private key was given")
	case len(strings.Fields(typed)) == recoveryWordCount:
		privateKeyHex, err = keyFromRecoveryWords(typed)
	case looksLikeHex(typed) && len(typed) >= 63 && len(typed) <= 64:
		privateKeyHex = typed
	case haveSavedKey:
		privateKeyHex, err = openSavedKey(typed)
		if err != nil && looksLikeHex(typed) && len(typed) <= 64 {
			// A short key written without its leading zeros.
			privateKeyHex, err = typed, nil
		}
	case looksLikeHex(typed) && len(typed) <= 64:
		privateKeyHex = typed
	case len(strings.Fields(typed)) > 1:
		_, err = keyFromRecoveryWords(typed)
	default:
		err = fmt.Errorf("that is not a private key (64 characters, 0-9 and a-f) and not %d recovery words", recoveryWordCount)
	}
	if err != nil {
		return "", "", err
	}
	key, err := privateKeyBytes(privateKeyHex)
	if err != nil {
		return "", "", err
	}
	privateKeyHex = hex.EncodeToString(key)
	publicKeyHex, err = publicKeyFromPrivateKey(privateKeyHex)
	if err != nil {
		return "", "", err
	}
	return privateKeyHex, publicKeyHex, nil
}
