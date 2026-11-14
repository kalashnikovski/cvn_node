//go:build amd64 && !purego

package asm

//go:nosplit
//go:noescape
func FillAes1Rx4(states *[4][4]uint32, keys *[4][4]uint32, output *byte, outputLen uint64)

//go:nosplit
//go:noescape
func HashAes1Rx4(initialState *[4][4]uint32, xKeys *[2][4]uint32, output *[64]byte, input *byte, inputLen uint64)

//go:nosplit
//go:noescape
func MergeAes4Rx4(states *[4][4]uint32, keys *[4][4]uint32)

//go:nosplit
//go:noescape
func AESRoundTrip_DecEnc(states *[4][4]uint32, keys *[4][4]uint32)

//go:nosplit
//go:noescape
func AESRoundTrip_EncDec(states *[4][4]uint32, keys *[4][4]uint32)

//go:nosplit
//go:noescape
func AESRoundTrip_EncDec1(states *[4][4]uint32, key *[4]uint32)
