package blake2

import (
	"encoding/binary"
	"golang.org/x/crypto/blake2b"
)

type Generator struct {
	state [blake2b.Size]byte
	i     int
}

func New(seed []byte, nonce uint32) *Generator {
	var state [blake2b.Size]byte
	copy(state[:60], seed)
	binary.LittleEndian.PutUint32(state[60:], nonce)
	g := &Generator{
		i:     len(state),
		state: state,
	}

	return g
}

func (g *Generator) GetUint32() (v uint32) {
	if (g.i + 4) > len(g.state) {
		g.reseed()
	}
	v = binary.LittleEndian.Uint32(g.state[g.i:])
	g.i += 4
	return v
}

func (g *Generator) GetByte() (v byte) {
	if (g.i + 1) > len(g.state) {
		g.reseed()
	}
	v = g.state[g.i]
	g.i++
	return v
}

func (g *Generator) reseed() {
	g.state = blake2b.Sum512(g.state[:])
	g.i = 0
}
