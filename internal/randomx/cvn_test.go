//go:build !randomx_reference

package randomx

import (
	"encoding/hex"
	"os"
	"runtime"
	"testing"
)

// Hashes under CVN's settings. They pin CVN's proof of work: if a change to
// this directory altered what the engine computes, these would no longer
// match and every node would disagree with the existing chain.
//
// The "standard" column is what published RandomX gives for the same key and
// input (its official test values); CVN's must differ.
var cvnVectors = []struct {
	key, input string
	want       string
	standard   string
}{
	{"test key 000", "This is a test", "d2db24590cf1c6bcf9ae6f70a4ae604e4d05c5e875ef03548762156536396b6b", "639183aae1bf4c9a35884cb46b09cad9175f04efd7684e7262a0ac1c2f0b4e3f"},
	{"test key 000", "Lorem ipsum dolor sit amet", "5991812fdef620e334156c618a4acc305fab85b5aa0051649cdef9d108d9c1b9", "300a0adb47603dedb42228ccb2b211104f4da45af709cd7547cd049e9489c969"},
	{"test key 001", "sed do eiusmod tempor incididunt ut labore et dolore magna aliqua", "73ac1e70b6cf0f67b28daf1453c6dd769fe6288661f828179f13724a788c5c0b", "e9ff4503201c0c2cca26d285c93ae883f9b1d30c9eb240b820756f2d5a7905fc"},
}

func TestCVNSettings(t *testing.T) {
	if RANDOMX_ARGON_SALT == "RandomX\x03" || RANDOMX_DATASET_EXTRA_SIZE == 33554368 {
		t.Fatal("CVN must not run with RandomX's published network settings")
	}
	if RANDOMX_DATASET_EXTRA_SIZE%64 != 0 {
		t.Fatal("the extra dataset size must be divisible by 64")
	}
	if RANDOMX_DATASET_EXTRA_SIZE > 33554368 {
		t.Fatal("the extra dataset size must not exceed the standard value: mining software reserves memory sized for standard RandomX")
	}
	// The settings that keep the algorithm hard for anything but ordinary
	// processors must stay exactly as published.
	if RANDOMX_ARGON_MEMORY != 262144 || RANDOMX_DATASET_BASE_SIZE != 2147483648 || RANDOMX_SCRATCHPAD_L3 != 2097152 ||
		RANDOMX_PROGRAM_SIZE != 256 || RANDOMX_PROGRAM_ITERATIONS != 2048 || RANDOMX_PROGRAM_COUNT != 8 {
		t.Fatal("a memory or program setting differs from published RandomX")
	}
}

func TestCVNValues(t *testing.T) {
	flags := GetFlags()
	cache, err := NewCache(flags)
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()

	currentKey := ""
	var vm *VM
	for _, vector := range cvnVectors {
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
		var output [RANDOMX_HASH_SIZE]byte
		vm.CalculateHash([]byte(vector.input), &output)
		got := hex.EncodeToString(output[:])
		if got == vector.standard {
			t.Errorf("key %q, input %.20q: CVN gives the same hash as standard RandomX", vector.key, vector.input)
		}
		if got != vector.want {
			t.Errorf("key %q, input %.20q: got %s, want %s", vector.key, vector.input, got, vector.want)
		}
	}
	if vm != nil {
		vm.Close()
	}
}

// Verifying uses the low-memory mode and mining the full-memory mode. They
// must always agree, or miners would produce blocks other nodes reject.
func TestVerifyingAndMiningModesAgree(t *testing.T) {
	if testing.Short() || os.Getenv("CI") != "" {
		t.Skip("needs about 2.3 GB of memory; skipped in short mode and on build servers")
	}
	flags := GetFlags()
	cache, err := NewCache(flags)
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	cache.Init([]byte(cvnVectors[0].key))

	light, err := NewVM(flags, cache, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer light.Close()

	fullFlags := flags | RANDOMX_FLAG_FULL_MEM
	dataset, err := NewDataset(fullFlags)
	if err != nil {
		t.Skipf("could not allocate the mining dataset: %v", err)
	}
	defer dataset.Close()
	dataset.InitDatasetParallel(cache, runtime.NumCPU())
	full, err := NewVM(fullFlags, nil, dataset)
	if err != nil {
		t.Fatal(err)
	}
	defer full.Close()

	for i := 0; i < 20; i++ {
		input := []byte{byte(i), byte(i * 7), 'C', 'V', 'N'}
		var a, b [RANDOMX_HASH_SIZE]byte
		light.CalculateHash(input, &a)
		full.CalculateHash(input, &b)
		if a != b {
			t.Fatalf("input %d: verifying mode gives %x, mining mode gives %x", i, a, b)
		}
	}
	var output [RANDOMX_HASH_SIZE]byte
	full.CalculateHash([]byte(cvnVectors[0].input), &output)
	if hex.EncodeToString(output[:]) != cvnVectors[0].want {
		t.Errorf("mining mode gives %x for the first recorded value, want %s", output, cvnVectors[0].want)
	}
}
