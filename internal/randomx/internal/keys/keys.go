package keys

import (
	"encoding/binary"
	"golang.org/x/crypto/blake2b"
)

var AesGenerator1R_Keys = keyDataInitialize_4_16("RandomX AesGenerator1R keys")
var AesGenerator4R_Keys = keyDataInitialize_8_16("RandomX AesGenerator4R keys 0-3", "RandomX AesGenerator4R keys 4-7")
var AesHash1R_State = keyDataInitialize_4_16("RandomX AesHash1R state")
var AesHash1R_XKeys = keyDataInitialize_2_16("RandomX AesHash1R xkeys")

var SuperScalar_Constants = keyDataInitialize_8_64("RandomX SuperScalarHash initialize")

func keyDataInitialize_8_16(input1, input2 string) (out [8][4]uint32) {
	data := keyDataInitialize512(input1)
	for i := range out[:4] {
		for j := range out[i] {
			out[i][j] = binary.LittleEndian.Uint32(data[i*16+j*4:])
		}
	}
	data2 := keyDataInitialize512(input2)
	for i := range out[4:] {
		for j := range out[i+4] {
			out[i+4][j] = binary.LittleEndian.Uint32(data2[i*16+j*4:])
		}
	}
	return out
}

func keyDataInitialize_4_16(input string) (out [4][4]uint32) {
	data := keyDataInitialize512(input)
	for i := range out {
		for j := range out[i] {
			out[i][j] = binary.LittleEndian.Uint32(data[i*16+j*4:])
		}
	}
	return out
}

func keyDataInitialize_2_16(input string) (out [2][4]uint32) {
	data := keyDataInitialize256(input)
	for i := range out {
		for j := range out[i] {
			out[i][j] = binary.LittleEndian.Uint32(data[i*16+j*4:])
		}
	}
	return out
}

func keyDataInitialize_8_64(input string) (out [8]uint64) {
	data := keyDataInitialize512(input)
	for i := range out[1:] {
		out[i+1] = binary.LittleEndian.Uint64(data[8+i*8:])
	}

	// Multiplier was selected because it gives an excellent distribution for linear generators (D. Knuth: The Art of Computer Programming – Vol 2.)
	out[0] = 6364136223846793005
	// Additionally, the constant for r1 was increased by 2^33+700
	out[1] += (1 << 33) + 700
	// and the constant for r3 was increased by 2^14
	out[3] += 1 << 14

	return out
}

func keyDataInitialize512(input string) [blake2b.Size]byte {
	return blake2b.Sum512([]byte(input))
}

func keyDataInitialize256(input string) [blake2b.Size256]byte {
	return blake2b.Sum256([]byte(input))
}
