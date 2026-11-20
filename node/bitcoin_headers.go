package node

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"sync"
	"time"
)

// FOLLOWING BITCOIN'S HEADERS
//
// A launch network starts from one Bitcoin block, named by its height (see
// genesis.go). A Bitcoin header proves that real work went into it, but it
// does not say which height it stands at: it names only the block before it.
// So a node that is handed "the header of block 980,000" cannot tell, from
// the header alone, that it is not the header of block 979,999.
//
// The node therefore works the height out for itself. This software carries
// one Bitcoin header that was already buried deep when it was written, the
// starting block below. From there the node collects every Bitcoin header up
// to the height it is waiting for, and checks each one the way a Bitcoin node
// does:
//
//   - it names the header before it, by that header's hash;
//   - its own hash meets the target it states;
//   - the target it states is the one Bitcoin's rules give at that height:
//     the same as the block before, except every 2,016 blocks, when it is
//     worked out afresh from how long the last 2,016 took;
//   - its date is later than the middle one of the eleven before it, and not
//     more than two hours ahead of this node's clock.
//
// A header that passes is the next block of Bitcoin's chain or of a branch of
// it that cost as much to make. Counting from the starting block gives its
// height. Where two branches are offered the node keeps the one with more
// work in it, as Bitcoin does.
//
// It does not matter where the headers come from. Other computers on the
// Bitcoin network supply them, and public block explorers are asked when
// none of those can be reached (bitcoin_peers.go), but none of them is
// believed: a wrong header fails the checks, and leaving headers out only
// makes the node wait. What a source cannot do any longer is pass off a real
// Bitcoin block of another height as the one the network starts from.

// bitcoinStartingBlock is a Bitcoin block fixed in this software, from which
// the headers are followed.
type bitcoinStartingBlock struct {
	Height int64
	Header string // 160 hex digits
}

// bitcoinStart is the block the following begins from. It must be the first
// block of one of Bitcoin's 2,016-block periods, so that the target at the
// next change can be worked out. Block 975,744 was mined on 12 November 2026
// and had about a thousand blocks on top of it when this was written. Its
// hash is 0000000000000000000123e2681e2f82acdcb9430a15eec24cc85b400691d479;
// anyone can compare that with a Bitcoin block explorer.
//
// A network can only be anchored to a Bitcoin block after this one. A
// variable so tests can stand in a chain of their own.
var bitcoinStart = bitcoinStartingBlock{
	Height: 975744,
	Header: "00800820d8d0cab226457cc80a9c4fdc0aa45c33090948b566d001000000000000000000bf9072bf5d103441e0e44e084403b61cebd2303fbfa75b681273e699076b00a47aebf76a7316021700d87f1c",
}

// Bitcoin's own figures. Variables so tests can use a small chain.
var (
	// bitcoinPeriodBlocks is how many blocks pass between changes of target.
	bitcoinPeriodBlocks int64 = 2016
	// bitcoinPeriodSeconds is how long that many blocks are meant to take.
	bitcoinPeriodSeconds int64 = 14 * 24 * 60 * 60
	// bitcoinEasiestBits is the easiest target Bitcoin ever allows.
	bitcoinEasiestBits uint32 = 0x1d00ffff
)

// bitcoinMedianSpan is how many earlier blocks a block's date is compared
// with: it must be later than the middle one.
const bitcoinMedianSpan = 11

// bitcoinHeader is one 80-byte Bitcoin block header and what was read from it.
type bitcoinHeader struct {
	raw  [80]byte
	hash [32]byte // as it is hashed; explorers show these bytes reversed
	prev [32]byte
	time int64
	bits uint32
}

func doubleSHA256(data []byte) [32]byte {
	first := sha256.Sum256(data)
	return sha256.Sum256(first[:])
}

// decodeBitcoinHeader reads an 80-byte header. It checks nothing but length.
func decodeBitcoinHeader(raw []byte) (bitcoinHeader, error) {
	var header bitcoinHeader
	if len(raw) != 80 {
		return header, fmt.Errorf("a Bitcoin header is 80 bytes; this has %d", len(raw))
	}
	copy(header.raw[:], raw)
	header.hash = doubleSHA256(raw)
	copy(header.prev[:], raw[4:36])
	header.time = int64(binary.LittleEndian.Uint32(raw[68:72]))
	header.bits = binary.LittleEndian.Uint32(raw[72:76])
	return header, nil
}

// reversedHex writes a hash the way Bitcoin explorers show it.
func reversedHex(hash [32]byte) string {
	reversed := make([]byte, 32)
	for i, b := range hash {
		reversed[31-i] = b
	}
	return hex.EncodeToString(reversed)
}

func (h bitcoinHeader) hashHex() string { return reversedHex(h.hash) }
func (h bitcoinHeader) hex() string     { return hex.EncodeToString(h.raw[:]) }

// meetsItsTarget reports whether the header's hash, read as a number, is no
// larger than the target the header states.
func (h bitcoinHeader) meetsItsTarget() bool {
	target, ok := compactTarget(h.bits)
	if !ok {
		return false
	}
	reversed := make([]byte, 32)
	for i, b := range h.hash {
		reversed[31-i] = b
	}
	return new(big.Int).SetBytes(reversed).Cmp(target) <= 0
}

// work is how many hashes a header with this target stands for on average:
// 2^256 / (target + 1), as Bitcoin counts it.
func (h bitcoinHeader) work() *big.Int {
	target, ok := compactTarget(h.bits)
	if !ok {
		return new(big.Int)
	}
	all := new(big.Int).Lsh(big.NewInt(1), 256)
	return all.Div(all, target.Add(target, big.NewInt(1)))
}

// compactFromTarget writes a target in Bitcoin's compact form, the opposite
// of compactTarget, rounding exactly as Bitcoin does.
func compactFromTarget(target *big.Int) uint32 {
	size := uint((target.BitLen() + 7) / 8)
	var mantissa uint32
	if size <= 3 {
		mantissa = uint32(target.Uint64() << (8 * (3 - size)))
	} else {
		mantissa = uint32(new(big.Int).Rsh(target, 8*(size-3)).Uint64())
	}
	// The top bit of the three bytes would mark a negative number, so a
	// value that has it set is written with one byte less precision.
	if mantissa&0x00800000 != 0 {
		mantissa >>= 8
		size++
	}
	return mantissa | uint32(size)<<24
}

// nextBitcoinBits is the target for the first block of a new period, from
// the target of the period that just ended and the dates of that period's
// first and last blocks. This is Bitcoin's rule: the target moves in
// proportion to how long the period took against two weeks, by no more than
// a factor of four either way, and never past the easiest target.
func nextBitcoinBits(lastBits uint32, firstTime, lastTime int64) uint32 {
	took := lastTime - firstTime
	if took < bitcoinPeriodSeconds/4 {
		took = bitcoinPeriodSeconds / 4
	}
	if took > bitcoinPeriodSeconds*4 {
		took = bitcoinPeriodSeconds * 4
	}
	target, ok := compactTarget(lastBits)
	if !ok {
		return 0
	}
	target.Mul(target, big.NewInt(took))
	target.Div(target, big.NewInt(bitcoinPeriodSeconds))
	if easiest, _ := compactTarget(bitcoinEasiestBits); target.Cmp(easiest) > 0 {
		target = easiest
	}
	return compactFromTarget(target)
}

// errHeadersDoNotConnect means none of the headers offered names a block
// this node has: they belong further along, or to another chain.
var errHeadersDoNotConnect = errors.New("these headers do not connect to any Bitcoin block this node has followed")

// bitcoinHeaderChain is the run of checked Bitcoin headers from the starting
// block to the newest one this node has been shown.
type bitcoinHeaderChain struct {
	mu      sync.Mutex
	start   int64
	headers []bitcoinHeader // headers[0] is the starting block
	index   map[[32]byte]int
}

// newBitcoinHeaderChain begins a chain at the given starting block.
func newBitcoinHeaderChain(from bitcoinStartingBlock) (*bitcoinHeaderChain, error) {
	raw, err := hex.DecodeString(from.Header)
	if err != nil {
		return nil, fmt.Errorf("the Bitcoin starting block's header is not hex")
	}
	first, err := decodeBitcoinHeader(raw)
	if err != nil {
		return nil, err
	}
	if !first.meetsItsTarget() {
		return nil, fmt.Errorf("the Bitcoin starting block's header does not carry the work it states")
	}
	if from.Height <= 0 || from.Height%bitcoinPeriodBlocks != 0 {
		return nil, fmt.Errorf("the Bitcoin starting block must be the first of a %d-block period; %d is not", bitcoinPeriodBlocks, from.Height)
	}
	return &bitcoinHeaderChain{
		start:   from.Height,
		headers: []bitcoinHeader{first},
		index:   map[[32]byte]int{first.hash: 0},
	}, nil
}

// tipHeight is the height of the newest header followed.
func (c *bitcoinHeaderChain) tipHeight() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.start + int64(len(c.headers)) - 1
}

// headerAt gives the header at a height, as hex, once it has been followed.
func (c *bitcoinHeaderChain) headerAt(height int64) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	position := height - c.start
	if position < 0 || position >= int64(len(c.headers)) {
		return "", false
	}
	return c.headers[position].hex(), true
}

// locator lists hashes of blocks this node has, newest first and thinning
// out towards the starting block. Whoever is asked for headers sends the ones
// after the first of these it recognises, so a node on a branch that Bitcoin
// has since dropped is led back to where the branch began.
func (c *bitcoinHeaderChain) locator() [][32]byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	var hashes [][32]byte
	step := 1
	for position := len(c.headers) - 1; position > 0; position -= step {
		hashes = append(hashes, c.headers[position].hash)
		if len(hashes) >= 10 {
			step *= 2
		}
	}
	return append(hashes, c.headers[0].hash)
}

// checkNext applies Bitcoin's rules to one header that is to follow `before`,
// the checked headers up to it, the first of which stands at c.start.
func (c *bitcoinHeaderChain) checkNext(before []bitcoinHeader, next bitcoinHeader, now time.Time) error {
	previous := before[len(before)-1]
	height := c.start + int64(len(before))
	if next.prev != previous.hash {
		return fmt.Errorf("the header offered for Bitcoin block %d does not name block %d as the one before it", height, height-1)
	}
	expected := previous.bits
	if height%bitcoinPeriodBlocks == 0 {
		first := before[int64(len(before))-bitcoinPeriodBlocks]
		expected = nextBitcoinBits(previous.bits, first.time, previous.time)
	}
	if next.bits != expected {
		return fmt.Errorf("the header offered for Bitcoin block %d states a target Bitcoin's rules do not give at that height", height)
	}
	if !next.meetsItsTarget() {
		return fmt.Errorf("the header offered for Bitcoin block %d does not carry the work it states", height)
	}
	span := bitcoinMedianSpan
	if span > len(before) {
		span = len(before)
	}
	times := make([]int64, 0, span)
	for _, earlier := range before[len(before)-span:] {
		times = append(times, earlier.time)
	}
	sort.Slice(times, func(a, b int) bool { return times[a] < times[b] })
	if next.time <= times[len(times)/2] {
		return fmt.Errorf("the header offered for Bitcoin block %d is dated no later than the blocks before it", height)
	}
	if next.time > now.Unix()+bitcoinClockAllowance {
		return fmt.Errorf("the header offered for Bitcoin block %d is dated more than two hours ahead of this computer's clock", height)
	}
	return nil
}

// offer shows the chain a run of headers, oldest first. Those that follow a
// block the chain has, and pass every check, are taken: added at the end, or,
// where they form a branch with more work in it than what the chain holds
// from that point, put in its place. added is how far the tip moved up.
//
// Checking stops at the first header that fails; the ones before it are still
// used. The error says what was wrong, and is nil when nothing was.
func (c *bitcoinHeaderChain) offer(raws [][]byte, now time.Time) (added int64, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	offered := make([]bitcoinHeader, 0, len(raws))
	for _, raw := range raws {
		header, decodeErr := decodeBitcoinHeader(raw)
		if decodeErr != nil {
			return 0, decodeErr
		}
		offered = append(offered, header)
	}
	// Find where they join what is already held.
	join := -1
	for len(offered) > 0 {
		if position, known := c.index[offered[0].prev]; known {
			join = position
			break
		}
		offered = offered[1:]
	}
	if join < 0 {
		return 0, errHeadersDoNotConnect
	}
	// Headers already held at the same places need no second look.
	for len(offered) > 0 && join+1 < len(c.headers) && bytes.Equal(c.headers[join+1].raw[:], offered[0].raw[:]) {
		join++
		offered = offered[1:]
	}
	if len(offered) == 0 {
		return 0, nil
	}

	branch := append([]bitcoinHeader(nil), c.headers[:join+1]...)
	for _, next := range offered {
		if err = c.checkNext(branch, next, now); err != nil {
			break
		}
		branch = append(branch, next)
	}
	if len(branch) == join+1 {
		return 0, err
	}
	// Keep whichever has more work from the point where they part.
	ours, theirs := new(big.Int), new(big.Int)
	for _, header := range c.headers[join+1:] {
		ours.Add(ours, header.work())
	}
	for _, header := range branch[join+1:] {
		theirs.Add(theirs, header.work())
	}
	if theirs.Cmp(ours) <= 0 {
		return 0, err
	}
	for _, dropped := range c.headers[join+1:] {
		delete(c.index, dropped.hash)
	}
	added = int64(len(branch) - len(c.headers))
	c.headers = branch
	for position := join + 1; position < len(branch); position++ {
		c.index[branch[position].hash] = position
	}
	return added, err
}
