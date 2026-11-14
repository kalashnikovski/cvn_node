package randomx

import "golang.org/x/crypto/blake2b"

// CalculateCommitment Calculate a RandomX commitment from a RandomX hash and its input.
func CalculateCommitment(input []byte, hashIn, hashOut *[RANDOMX_HASH_SIZE]byte) {
	hasher, err := blake2b.New(RANDOMX_HASH_SIZE, nil)
	if err != nil {
		panic(err)
	}

	hasher.Write(input)
	hasher.Write(hashIn[:])
	hasher.Sum(hashOut[:0])
}
