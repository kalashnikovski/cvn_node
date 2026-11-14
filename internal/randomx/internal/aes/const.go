// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package aes implements AES encryption (formerly Rijndael), as defined in
// U.S. Federal Information Processing Standards Publication 197.
//
// The AES operations in this package are not implemented using constant-time algorithms.
// An exception is when running on systems with enabled hardware support for AES
// that makes these operations constant-time. Examples include amd64 systems using AES-NI
// extensions and s390x systems using Message-Security-Assist extensions.
// On such systems, when the result of NewCipher is passed to cipher.NewGCM,
// the GHASH operation used by GCM is also constant-time.
package aes

import (
	"bytes"
	"math/bits"
)

// This file generates AES constants - 8720 bytes of initialized data.

// https://csrc.nist.gov/publications/fips/fips197/fips-197.pdf

// AES is based on the mathematical behavior of binary polynomials
// (polynomials over GF(2)) modulo the irreducible polynomial x⁸ + x⁴ + x³ + x + 1.
// Addition of these binary polynomials corresponds to binary xor.
// Reducing mod poly corresponds to binary xor with poly every
// time a 0x100 bit appears.
const poly = 1<<8 | 1<<4 | 1<<3 | 1<<1 | 1<<0 // x⁸ + x⁴ + x³ + x + 1

// Multiply b and c as GF(2) polynomials modulo poly
func mul(b, c uint32) uint32 {
	i := b
	j := c
	s := uint32(0)
	for k := uint32(1); k < 0x100 && j != 0; k <<= 1 {
		// Invariant: k == 1<<n, i == b * xⁿ

		if j&k != 0 {
			// s += i in GF(2); xor in binary
			s ^= i
			j ^= k // turn off bit to end loop early
		}

		// i *= x in GF(2) modulo the polynomial
		i <<= 1
		if i&0x100 != 0 {
			i ^= poly
		}
	}
	return s
}

// sbox0 FIPS-197 Figure 7. S-box substitution values generation
var sbox0 = func() (sbox [256]byte) {
	var p, q uint8 = 1, 1
	for {
		/* multiply p by 3 */
		if p&0x80 != 0 {
			p ^= (p << 1) ^ 0x1b
		} else {
			p ^= p << 1
		}

		/* divide q by 3 (equals multiplication by 0xf6) */
		q ^= q << 1
		q ^= q << 2
		q ^= q << 4
		if q&0x80 != 0 {
			q ^= 0x09
		}

		/* compute the affine transformation */
		xformed := q ^ bits.RotateLeft8(q, 1) ^ bits.RotateLeft8(q, 2) ^ bits.RotateLeft8(q, 3) ^ bits.RotateLeft8(q, 4)
		sbox[p] = xformed ^ 0x63

		if p == 1 {
			break
		}
	}

	/* 0 is a special case since it has no inverse */
	sbox[0] = 0x63
	return sbox
}()

// sbox1 FIPS-197 Figure 14. Inverse S-box substitution values generation
var sbox1 = func() (isbox [256]byte) {
	for i := range sbox0 {
		isbox[i] = uint8(bytes.IndexByte(sbox0[:], uint8(i)))
	}
	return isbox
}()

// encLut Lookup tables for encryption.
var encLut = func() (te [4][256]uint32) {
	for i := 0; i < 256; i++ {
		s := uint32(sbox0[i])
		s2 := mul(s, 2)
		s3 := mul(s, 3)
		w := s2<<24 | s<<16 | s<<8 | s3

		for j := 0; j < 4; j++ {
			te[j][i] = bits.ReverseBytes32(w)
			w = w<<24 | w>>8
		}
	}
	return te
}()

// decLut Lookup tables for decryption.
var decLut = func() (td [4][256]uint32) {
	for i := 0; i < 256; i++ {
		s := uint32(sbox1[i])
		s9 := mul(s, 0x9)
		sb := mul(s, 0xb)
		sd := mul(s, 0xd)
		se := mul(s, 0xe)
		w := se<<24 | s9<<16 | sd<<8 | sb
		for j := 0; j < 4; j++ {
			td[j][i] = bits.ReverseBytes32(w)
			w = w<<24 | w>>8
		}
	}
	return td
}()
