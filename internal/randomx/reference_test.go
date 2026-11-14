//go:build randomx_reference

package randomx

import (
	"encoding/hex"
	"testing"
)

// RandomX's official test values (from the reference implementation's test
// suite). Passing these shows the engine in this directory is RandomX.
var officialVectors = []struct {
	key, input string
	inputIsHex bool
	want       string
}{
	{"test key 000", "This is a test", false, "639183aae1bf4c9a35884cb46b09cad9175f04efd7684e7262a0ac1c2f0b4e3f"},
	{"test key 000", "Lorem ipsum dolor sit amet", false, "300a0adb47603dedb42228ccb2b211104f4da45af709cd7547cd049e9489c969"},
	{"test key 000", "sed do eiusmod tempor incididunt ut labore et dolore magna aliqua", false, "c36d4ed4191e617309867ed66a443be4075014e2b061bcdaf9ce7b721d2b77a8"},
	{"test key 001", "sed do eiusmod tempor incididunt ut labore et dolore magna aliqua", false, "e9ff4503201c0c2cca26d285c93ae883f9b1d30c9eb240b820756f2d5a7905fc"},
	{"test key 001", "0b0b98bea7e805e0010a2126d287a2a0cc833d312cb786385a7c2f9de69d25537f584a9bc9977b00000000666fd8753bf61a8631f12984e3fd44f4014eca629276817b56f32e9b68bd82f416", true, "c56414121acda1713c2f2a819d8ae38aed7c80c35c2a769298d34f03833cd5f1"},
}

func TestOfficialRandomXValues(t *testing.T) {
	flags := GetFlags()
	cache, err := NewCache(flags)
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()

	currentKey := ""
	var vm *VM
	for _, vector := range officialVectors {
		if vector.key != currentKey {
			if vm != nil {
				vm.Close()
			}
			cache.Init([]byte(vector.key))
			if vm, err = NewVM(flags, cache, nil); err != nil {
				t.Fatal(err)
			}
			currentKey = vector.key
		}
		input := []byte(vector.input)
		if vector.inputIsHex {
			input, _ = hex.DecodeString(vector.input)
		}
		var output [RANDOMX_HASH_SIZE]byte
		vm.CalculateHash(input, &output)
		if got := hex.EncodeToString(output[:]); got != vector.want {
			t.Errorf("key %q, input %.20q: got %s, want %s", vector.key, vector.input, got, vector.want)
		}
	}
	if vm != nil {
		vm.Close()
	}
}
