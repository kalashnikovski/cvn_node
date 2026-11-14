package aes

import (
	"runtime"
	"unsafe"

	"cvn_node/internal/randomx/internal/keys"
)

type SoftAES struct {
}

func NewSoftAES() SoftAES {
	return SoftAES{}
}

func (aes SoftAES) HashAes1Rx4(input []byte, output *[64]byte) {
	if len(input)%len(output) != 0 {
		panic("unsupported")
	}
	// states are copied
	states := (*[4][4]uint32)(unsafe.Pointer(output))
	*states = keys.AesHash1R_State

	for input_ptr := 0; input_ptr < len(input); input_ptr += 64 {
		in := (*[4][4]uint32)(unsafe.Pointer(unsafe.SliceData(input[input_ptr:])))

		soft_aesroundtrip_encdec(states, in)
	}

	soft_aesroundtrip_encdec1(states, &keys.AesHash1R_XKeys[0])

	soft_aesroundtrip_encdec1(states, &keys.AesHash1R_XKeys[1])

	runtime.KeepAlive(output)
}

func (aes SoftAES) FillAes1Rx4(state *[64]byte, output []byte) {
	if len(output)%len(state) != 0 {
		panic("unsupported")
	}
	// Reference to state without copying
	states := (*[4][4]uint32)(unsafe.Pointer(state))

	for outptr := 0; outptr < len(output); outptr += len(state) {
		soft_aesroundtrip_decenc(states, &keys.AesGenerator1R_Keys)

		copy(output[outptr:], state[:])
	}
}

func (aes SoftAES) FillAes4Rx4(state *[64]byte, output []byte) {
	if len(output)%len(state) != 0 {
		panic("unsupported")
	}

	states := (*[4][4]uint32)(unsafe.Pointer(state))

	for outptr := 0; outptr < len(output); outptr += len(state) {
		soft_aesroundtrip_decenc(states, &fillAes4Rx4Keys0)
		soft_aesroundtrip_decenc(states, &fillAes4Rx4Keys1)
		soft_aesroundtrip_decenc(states, &fillAes4Rx4Keys2)
		soft_aesroundtrip_decenc(states, &fillAes4Rx4Keys3)

		copy(output[outptr:], state[:])
	}
}

//go:nosplit
func (aes SoftAES) MergeAes4Rx4(state *[64]byte, key *[64]byte) {
	// Reference to state without copying
	states := (*[4][4]uint32)(unsafe.Pointer(state))
	keys := (*[4][4]uint32)(unsafe.Pointer(key))

	soft_aesroundtrip_encdec1(states, &keys[0])
	soft_aesroundtrip_encdec1(states, &keys[1])
	soft_aesroundtrip_encdec1(states, &keys[2])
	soft_aesroundtrip_encdec1(states, &keys[3])
}

func (aes SoftAES) HashAndFillAes1Rx4(scratchpad []byte, output *[64]byte, fillState *[64]byte) {
	//TODO
	aes.HashAes1Rx4(scratchpad, output)
	aes.FillAes1Rx4(fillState, scratchpad)
}
