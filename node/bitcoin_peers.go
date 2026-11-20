package node

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	mathrand "math/rand"
	"net"
	"sort"
	"strconv"
	"sync"
	"time"
)

// WHERE BITCOIN'S HEADERS COME FROM
//
// The headers checked in bitcoin_headers.go have to be fetched from
// somewhere. Two kinds of source are used, and neither is believed: every
// header from either goes through the same checks.
//
//   - Computers on the Bitcoin network. The node finds some through the
//     Bitcoin project's own published look-up names, connects to a few, and
//     asks each for the headers after the ones it has. This is the same
//     exchange two Bitcoin nodes have with each other, and it is the only
//     part of Bitcoin's protocol used here: no blocks, no transactions, and
//     nothing is sent that other Bitcoin nodes would pass on.
//   - Public Bitcoin block explorers, the ones in anchor_wait.go. They are
//     asked only when no computer on the Bitcoin network could be reached,
//     which happens on networks that block Bitcoin's port.
//
// `--no-anchor-lookup` switches both off.

// bitcoinLookupNames are names the Bitcoin project publishes for finding
// computers on its network. A variable so tests can stand in.
var bitcoinLookupNames = []string{
	"seed.bitcoin.sipa.be",
	"dnsseed.bluematt.me",
	"seed.bitcoin.jonasschnelli.ch",
	"seed.btc.petertodd.net",
	"seed.bitcoin.sprovoost.nl",
	"dnsseed.emzy.de",
	"seed.bitcoin.wiz.biz",
	"seed.mainnet.achownodes.xyz",
}

const (
	bitcoinPort = "8333"
	// bitcoinProtocolVersion is an old, widely understood version of the
	// protocol: everything needed here has been in it since 2012.
	bitcoinProtocolVersion = 70015
	bitcoinHeadersPerReply = 2000
	// bitcoinLargestMessage bounds what is read from another computer. A
	// full reply of headers is about 162,000 bytes.
	bitcoinLargestMessage = 1 << 20
	bitcoinPeersWanted    = 3
)

// bitcoinMagic begins every message on Bitcoin's main network.
var bitcoinMagic = [4]byte{0xf9, 0xbe, 0xb4, 0xd9}

// findBitcoinPeers looks the names up and returns addresses to try, in no
// particular order. A variable so tests can stand in.
var findBitcoinPeers = func() []string {
	var (
		mu        sync.Mutex
		addresses []string
		wait      sync.WaitGroup
	)
	for _, name := range bitcoinLookupNames {
		wait.Add(1)
		go func(name string) {
			defer wait.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			hosts, err := net.DefaultResolver.LookupHost(ctx, name)
			if err != nil {
				return
			}
			mu.Lock()
			for _, host := range hosts {
				addresses = append(addresses, net.JoinHostPort(host, bitcoinPort))
			}
			mu.Unlock()
		}(name)
	}
	wait.Wait()
	mathrand.Shuffle(len(addresses), func(a, b int) { addresses[a], addresses[b] = addresses[b], addresses[a] })
	return addresses
}

// bitcoinPeer is an open connection to one computer on the Bitcoin network.
type bitcoinPeer struct {
	conn    net.Conn
	address string
}

func (p *bitcoinPeer) close() { _ = p.conn.Close() }

// send writes one message: Bitcoin's four-byte mark, the command, the
// length, the first four bytes of the payload's double hash, the payload.
func (p *bitcoinPeer) send(command string, payload []byte) error {
	message := make([]byte, 24, 24+len(payload))
	copy(message[0:4], bitcoinMagic[:])
	copy(message[4:16], command)
	binary.LittleEndian.PutUint32(message[16:20], uint32(len(payload)))
	sum := doubleSHA256(payload)
	copy(message[20:24], sum[:4])
	_, err := p.conn.Write(append(message, payload...))
	return err
}

// read takes one message off the connection.
func (p *bitcoinPeer) read() (command string, payload []byte, err error) {
	head := make([]byte, 24)
	if _, err = io.ReadFull(p.conn, head); err != nil {
		return "", nil, err
	}
	if !bytes.Equal(head[0:4], bitcoinMagic[:]) {
		return "", nil, errors.New("not a Bitcoin message")
	}
	length := binary.LittleEndian.Uint32(head[16:20])
	if length > bitcoinLargestMessage {
		return "", nil, errors.New("message too large")
	}
	payload = make([]byte, length)
	if _, err = io.ReadFull(p.conn, payload); err != nil {
		return "", nil, err
	}
	if sum := doubleSHA256(payload); !bytes.Equal(sum[:4], head[20:24]) {
		return "", nil, errors.New("message damaged on the way")
	}
	return string(bytes.TrimRight(head[4:16], "\x00")), payload, nil
}

// connectBitcoinPeer opens a connection and exchanges the greeting every
// Bitcoin connection begins with.
func connectBitcoinPeer(address string) (*bitcoinPeer, error) {
	conn, err := net.DialTimeout("tcp", address, 5*time.Second)
	if err != nil {
		return nil, err
	}
	peer := &bitcoinPeer{conn: conn, address: address}
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	const name = "/cvn-headers:1/"
	greeting := new(bytes.Buffer)
	_ = binary.Write(greeting, binary.LittleEndian, int32(bitcoinProtocolVersion))
	_ = binary.Write(greeting, binary.LittleEndian, uint64(0)) // offers no services
	_ = binary.Write(greeting, binary.LittleEndian, time.Now().Unix())
	greeting.Write(make([]byte, 26)) // the other side's address: not needed
	greeting.Write(make([]byte, 26)) // this side's address: not given
	nonce := make([]byte, 8)
	_, _ = rand.Read(nonce)
	greeting.Write(nonce)
	greeting.WriteByte(byte(len(name)))
	greeting.WriteString(name)
	_ = binary.Write(greeting, binary.LittleEndian, int32(0)) // height: not given
	greeting.WriteByte(0)                                     // send no transactions
	if err := peer.send("version", greeting.Bytes()); err != nil {
		peer.close()
		return nil, err
	}
	greeted, acknowledged := false, false
	for messages := 0; messages < 20 && !(greeted && acknowledged); messages++ {
		command, _, err := peer.read()
		if err != nil {
			peer.close()
			return nil, err
		}
		switch command {
		case "version":
			greeted = true
			if err := peer.send("verack", nil); err != nil {
				peer.close()
				return nil, err
			}
		case "verack":
			acknowledged = true
		}
	}
	if !greeted || !acknowledged {
		peer.close()
		return nil, errors.New("the greeting was not completed")
	}
	return peer, nil
}

// readCount reads one of Bitcoin's variable-length counts.
func readCount(data []byte) (count uint64, rest []byte, ok bool) {
	if len(data) < 1 {
		return 0, nil, false
	}
	switch first := data[0]; {
	case first < 0xfd:
		return uint64(first), data[1:], true
	case first == 0xfd && len(data) >= 3:
		return uint64(binary.LittleEndian.Uint16(data[1:3])), data[3:], true
	case first == 0xfe && len(data) >= 5:
		return uint64(binary.LittleEndian.Uint32(data[1:5])), data[5:], true
	case first == 0xff && len(data) >= 9:
		return binary.LittleEndian.Uint64(data[1:9]), data[9:], true
	}
	return 0, nil, false
}

func writeCount(buffer *bytes.Buffer, count int) {
	if count < 0xfd {
		buffer.WriteByte(byte(count))
		return
	}
	buffer.WriteByte(0xfd)
	_ = binary.Write(buffer, binary.LittleEndian, uint16(count))
}

// headersAfter asks for the headers that follow the first block in `locator`
// the other computer recognises. It returns at most 2,000; fewer means the
// other computer has no more.
func (p *bitcoinPeer) headersAfter(locator [][32]byte) ([][]byte, error) {
	_ = p.conn.SetDeadline(time.Now().Add(20 * time.Second))
	request := new(bytes.Buffer)
	_ = binary.Write(request, binary.LittleEndian, uint32(bitcoinProtocolVersion))
	writeCount(request, len(locator))
	for _, hash := range locator {
		request.Write(hash[:])
	}
	request.Write(make([]byte, 32)) // no stopping point: as many as it will send
	if err := p.send("getheaders", request.Bytes()); err != nil {
		return nil, err
	}
	// Other messages may arrive first. A ping is answered; the rest are let go.
	for messages := 0; messages < 50; messages++ {
		command, payload, err := p.read()
		if err != nil {
			return nil, err
		}
		switch command {
		case "ping":
			if err := p.send("pong", payload); err != nil {
				return nil, err
			}
		case "headers":
			count, rest, ok := readCount(payload)
			if !ok || count > bitcoinHeadersPerReply || uint64(len(rest)) < count*81 {
				return nil, errors.New("a reply of headers that cannot be read")
			}
			headers := make([][]byte, 0, count)
			for i := uint64(0); i < count; i++ {
				headers = append(headers, rest[:80])
				rest = rest[81:] // each header is followed by a count that is always zero
			}
			return headers, nil
		}
	}
	return nil, errors.New("no headers were sent")
}

// bitcoinFollower keeps one chain of checked headers moving forward.
type bitcoinFollower struct {
	chain *bitcoinHeaderChain

	mu        sync.Mutex
	peers     []*bitcoinPeer
	addresses []string
	lastFound time.Time
	// reached says where the latest headers came from, for the messages:
	// "the Bitcoin network", "block explorers", or "" when nobody answered.
	reached string
}

// newBitcoinFollower begins following from the starting block fixed in this
// software. It is nil when a height cannot be reached that way: the height
// is not after the starting block.
func newBitcoinFollower(height int64) *bitcoinFollower {
	if height <= bitcoinStart.Height {
		return nil
	}
	chain, err := newBitcoinHeaderChain(bitcoinStart)
	if err != nil {
		return nil
	}
	return &bitcoinFollower{chain: chain}
}

// connectMore brings the open connections up to the number wanted.
func (f *bitcoinFollower) connectMore() {
	missing := bitcoinPeersWanted - len(f.peers)
	if missing <= 0 {
		return
	}
	if len(f.addresses) < missing*2 && time.Since(f.lastFound) > time.Minute {
		f.lastFound = time.Now()
		f.addresses = append(f.addresses, findBitcoinPeers()...)
	}
	// Twice as many are tried as are wanted: many listed computers are not
	// reachable at any given moment.
	attempts := missing * 2
	if attempts > len(f.addresses) {
		attempts = len(f.addresses)
	}
	trying := f.addresses[:attempts]
	f.addresses = f.addresses[attempts:]
	connected := make(chan *bitcoinPeer, len(trying))
	for _, address := range trying {
		go func(address string) {
			peer, err := connectBitcoinPeer(address)
			if err != nil {
				connected <- nil
				return
			}
			connected <- peer
		}(address)
	}
	for range trying {
		if peer := <-connected; peer != nil {
			if len(f.peers) < bitcoinPeersWanted {
				f.peers = append(f.peers, peer)
			} else {
				peer.close()
			}
		}
	}
}

// fromBitcoinNetwork asks each connected computer for what follows, until it
// has no more. It reports whether any of them answered.
func (f *bitcoinFollower) fromBitcoinNetwork() (answered bool) {
	f.connectMore()
	kept := f.peers[:0]
	for _, peer := range f.peers {
		good := true
		for replies := 0; replies < 50; replies++ {
			headers, err := peer.headersAfter(f.chain.locator())
			if err != nil {
				good = false
				break
			}
			answered = true
			// A computer whose headers fail the checks is on some other
			// chain, or is not honest. Either way it is not asked again.
			if _, err := f.chain.offer(headers, time.Now()); err != nil && len(headers) > 0 {
				good = false
				break
			}
			if len(headers) < bitcoinHeadersPerReply {
				break
			}
		}
		if good {
			kept = append(kept, peer)
		} else {
			peer.close()
		}
	}
	f.peers = kept
	return answered
}

// explorerBlock is what a block explorer says about one block, from which
// its 80-byte header can be put back together.
type explorerBlock struct {
	Height   int64  `json:"height"`
	Version  int64  `json:"version"`
	Previous string `json:"previousblockhash"`
	Merkle   string `json:"merkle_root"`
	Time     int64  `json:"timestamp"`
	Bits     int64  `json:"bits"`
	Nonce    int64  `json:"nonce"`
}

// header rebuilds the block's 80 bytes. Explorers show hashes reversed.
func (b explorerBlock) header() ([]byte, bool) {
	previous, err1 := hex.DecodeString(b.Previous)
	merkle, err2 := hex.DecodeString(b.Merkle)
	if err1 != nil || err2 != nil || len(previous) != 32 || len(merkle) != 32 {
		return nil, false
	}
	raw := make([]byte, 80)
	binary.LittleEndian.PutUint32(raw[0:4], uint32(b.Version))
	for i := 0; i < 32; i++ {
		raw[4+i] = previous[31-i]
		raw[36+i] = merkle[31-i]
	}
	binary.LittleEndian.PutUint32(raw[68:72], uint32(b.Time))
	binary.LittleEndian.PutUint32(raw[72:76], uint32(b.Bits))
	binary.LittleEndian.PutUint32(raw[76:80], uint32(b.Nonce))
	return raw, true
}

// explorerPagesPerRound bounds how much is asked of the explorers at a time.
const explorerPagesPerRound = 60

// fromExplorers fills in from public block explorers, a page of blocks at a
// time. It reports whether any of them answered.
func (f *bitcoinFollower) fromExplorers() (answered bool) {
	sources := append([]string(nil), anchorSources...)
	mathrand.Shuffle(len(sources), func(a, b int) { sources[a], sources[b] = sources[b], sources[a] })
	pages := 0
	for _, source := range sources {
		text, ok := anchorGet(source + "/blocks/tip/height")
		theirTip, err := strconv.ParseInt(text, 10, 64)
		if !ok || err != nil {
			continue
		}
		answered = true
		// A page holds the blocks at and below the height asked for. The
		// first page asked reaches back over the newest blocks held, so
		// that a replaced block is noticed.
		back := int64(0)
		for pages < explorerPagesPerRound {
			ours := f.chain.tipHeight()
			if ours >= theirTip && back == 0 {
				break
			}
			top := ours + 10 - back
			if top > theirTip {
				top = theirTip
			}
			pages++
			body, ok := anchorGetUpTo(fmt.Sprintf("%s/blocks/%d", source, top), 256*1024)
			if !ok {
				break
			}
			var blocks []explorerBlock
			if json.Unmarshal([]byte(body), &blocks) != nil || len(blocks) == 0 {
				break
			}
			sort.Slice(blocks, func(a, b int) bool { return blocks[a].Height < blocks[b].Height })
			var headers [][]byte
			for _, block := range blocks {
				if raw, ok := block.header(); ok {
					headers = append(headers, raw)
				}
			}
			added, err := f.chain.offer(headers, time.Now())
			if errors.Is(err, errHeadersDoNotConnect) && back == 0 {
				// The page began above anything held here: look lower once.
				back = 10
				continue
			}
			back = 0
			if added == 0 {
				break
			}
		}
		if f.chain.tipHeight() >= theirTip {
			break
		}
	}
	return answered
}

// advance fetches whatever headers are new, and returns the height followed
// to. It is safe to call from more than one place.
func (f *bitcoinFollower) advance() int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	before := f.chain.tipHeight()
	f.reached = ""
	if f.fromBitcoinNetwork() {
		f.reached = "the Bitcoin network"
	}
	// The explorers are asked whenever the Bitcoin network brought nothing
	// new: usually there is nothing new, and it costs one small question,
	// but it also means that computers which answer and hold headers back
	// cannot keep this node waiting.
	if f.chain.tipHeight() == before && f.fromExplorers() {
		if f.reached == "" || f.chain.tipHeight() > before {
			f.reached = "block explorers"
		}
	}
	return f.chain.tipHeight()
}

// source says where the latest round's headers came from.
func (f *bitcoinFollower) source() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reached
}

// done closes the connections. The headers followed are kept.
func (f *bitcoinFollower) done() {
	f.mu.Lock()
	defer f.mu.Unlock()
	// Their addresses are tried first next time: they are known to answer.
	for _, peer := range f.peers {
		peer.close()
		f.addresses = append([]string{peer.address}, f.addresses...)
	}
	f.peers = nil
}
