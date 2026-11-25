package node

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// allocatedWhile is how many bytes were allocated while f ran.
func allocatedWhile(f func()) uint64 {
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// emptyItems is n list items of the given text, separated by commas.
func emptyItems(item string, n int) string {
	return strings.TrimSuffix(strings.Repeat(item+",", n), ",")
}

func TestJsonListItemsCountsEveryListItem(t *testing.T) {
	cases := map[string]int{
		`[]`:                                0,
		`[ ]`:                               0,
		`[1]`:                               1,
		`[1,2,[3,4]]`:                       5,
		"[\n1 , 2 ]":                        2,
		`{"a":[{},{}],"b":"[,,]"}`:          2,
		`["a,b","[x]",""]`:                  3,
		`{"s":"\"[1,2]\"","t":[null,null]}`: 2,
		`{"a":{"b":{"c":[[],[],[]]}}}`:      3,
	}
	for text, want := range cases {
		if got := jsonListItems([]byte(text)); got != want {
			t.Errorf("%s: counted %d items, want %d", text, got, want)
		}
	}
}

// A block message made of empty transfers decoded to 699,037 transactions
// and held 369 MB, on seeds with a memory cap of 800 MB. It, and the same
// trick with nulls, numbers or padded items, must be refused before
// decoding, and refusing it must cost next to nothing.
func TestABlockOfEmptyTransfersIsRefusedBeforeDecoding(t *testing.T) {
	onNetwork(t, "devnet")
	room := MaxPayloadFrameSize - len("BLOCK_BROADCAST:") - 40
	for _, item := range []string{`{}`, `null`, `0`, `{"zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz":0}`} {
		payload := []byte(`{"transactions":[` + emptyItems(item, room/(len(item)+1)) + `]}`)
		var err error
		var block Block
		used := allocatedWhile(func() { err = decodeBlockFromPeer(payload, &block) })
		if err == nil {
			t.Errorf("a %d-byte block of %q items was decoded", len(payload), item)
		}
		if used > 4<<20 {
			t.Errorf("refusing a block of %q items allocated %d MB", item, used>>20)
		}
	}

	// A transfer can have at most 200 inputs and 50 outputs.
	payload := []byte(`{"inputs":[` + emptyItems(`{}`, room/3) + `]}`)
	var tx Transaction
	if err := decodeTransferFromPeer(payload, &tx); err == nil {
		t.Error("a transfer of hundreds of thousands of empty inputs was decoded")
	}
}

const hex64 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

// honestBlockOfShape builds a block as full as the rules allow, of transfers
// with the given numbers of inputs and outputs. The signatures are not real,
// but every field has the length a real one has, so its size on the wire is
// a real block's. Inputs spend coins from rewards with the shortest
// identifiers the rules allow when shortSources is set, which makes the
// JSON largest against the rules' measure.
func honestBlockOfShape(inputs, outputs int, shortSources bool) Block {
	coinbase := Transaction{
		Version: 1, ID: rewardTransactionID(1000, "x"), Witness: testAddress("miner"),
		PublicKey: CoinbasePublicKeyMarker, Signature: CoinbaseSignatureMarker, Timestamp: 1764000000,
	}
	for i := 0; i < RewardMaxOutputs; i++ {
		coinbase.Outputs = append(coinbase.Outputs, UTXOOutput{Recipient: testAddress(fmt.Sprint("reward ", i)), Amount: UnitsAmount(1)})
	}
	block := Block{
		Version: 1, Index: 1000, Timestamp: 1764000000, PrevHash: hex64, Hash: hex64, Difficulty: 5000,
		UncleHash: emptyUncleHash, UncleMiner: emptyUncleMiner, Transactions: []Transaction{coinbase},
	}
	size := blockSizeBytes(block)
	for n := 0; ; n++ {
		tx := Transaction{
			Version: 1, PublicKey: strings.Repeat("b", 128), Signature: hex64 + "|" + hex64, Timestamp: 1764000000,
		}
		for i := 0; i < inputs; i++ {
			source := hex64
			if shortSources {
				source = fmt.Sprintf("TX_COINBASE_H%d_a", 1+(n*inputs+i)/RewardMaxOutputs)
			}
			tx.Inputs = append(tx.Inputs, UTXOInput{SourceTxID: source, Index: i % RewardMaxOutputs, AmountUnits: 1})
		}
		for i := 0; i < outputs; i++ {
			tx.Outputs = append(tx.Outputs, UTXOOutput{Recipient: testAddress(fmt.Sprint("payee ", n, " ", i)), Amount: UnitsAmount(1)})
		}
		tx.ID = ComputeTransactionID(tx)
		if size+transactionSizeBytes(tx) > Params.MaxBlockBytes {
			return block
		}
		size += transactionSizeBytes(tx)
		block.Transactions = append(block.Transactions, tx)
	}
}

// The limits must never refuse a block the rules accept: a node that could
// not take in a valid block would be left behind for good. Full blocks of
// every shape are decoded, and so is every transfer in them.
func TestHonestFullBlocksAreDecoded(t *testing.T) {
	onNetwork(t, "devnet")
	shapes := []struct {
		name            string
		inputs, outputs int
		short           bool
	}{
		{"most inputs", TxMaxInputs, 1, false},
		{"most inputs, shortest coin identifiers", TxMaxInputs, 1, true},
		{"most outputs", 1, TxMaxOutputs, false},
		{"smallest transfers", 1, 1, true},
	}
	for _, shape := range shapes {
		block := honestBlockOfShape(shape.inputs, shape.outputs, shape.short)
		raw, _ := json.Marshal(block)
		var decoded Block
		if err := decodeBlockFromPeer(raw, &decoded); err != nil {
			t.Errorf("%s: a full block (%d bytes by the rules, %d as JSON, %d list items) was refused: %v",
				shape.name, blockSizeBytes(block), len(raw), jsonListItems(raw), err)
			continue
		}
		if len(decoded.Transactions) != len(block.Transactions) || blockSizeBytes(decoded) != blockSizeBytes(block) {
			t.Errorf("%s: the block did not decode to what was sent", shape.name)
		}
		for _, tx := range block.Transactions[1:] {
			rawTx, _ := json.Marshal(tx)
			var decodedTx Transaction
			if err := decodeTransferFromPeer(rawTx, &decodedTx); err != nil {
				t.Fatalf("%s: a transfer of %d inputs and %d outputs was refused: %v", shape.name, len(tx.Inputs), len(tx.Outputs), err)
			}
		}
		t.Logf("%s: %d bytes by the rules, %d as JSON (%.2f times), %d list items (most allowed %d)",
			shape.name, blockSizeBytes(block), len(raw), float64(len(raw))/float64(blockSizeBytes(block)), jsonListItems(raw), maxBlockListItems())
	}
}

// A node answering a sync request stops once its blocks reach
// syncBatchMaxBytes by the rules' measure. The largest such answer, in the
// shape that is largest as JSON, must fit the limit on what is read.
func TestAFullHonestBatchFitsTheReplyLimit(t *testing.T) {
	onNetwork(t, "devnet")
	block := honestBlockOfShape(TxMaxInputs, 1, true)
	var batch []Block
	size := 0
	for len(batch) < syncBatchMaxBlocks {
		batch = append(batch, block)
		if size += blockSizeBytes(block); size >= syncBatchMaxBytes {
			break
		}
	}
	raw, _ := json.Marshal(batch)
	if len(raw) > syncReplyLimitBytes {
		t.Fatalf("the largest honest reply is %d bytes, over the %d read", len(raw), syncReplyLimitBytes)
	}
	var decoded []Block
	if err := decodeBlockBatchFromPeer(raw, &decoded); err != nil || len(decoded) != len(batch) {
		t.Fatalf("the largest honest reply was refused: %v", err)
	}
	t.Logf("largest honest reply: %d blocks, %.1f MB (limit %d MB)", len(batch), float64(len(raw))/(1<<20), syncReplyLimitBytes>>20)
}

// A stranger chosen as the source of a sync could answer with up to 64 MB of
// empty blocks, which decoded to gigabytes. Now it is refused, cheaply.
func TestASyncReplyOfEmptyBlocksIsRefused(t *testing.T) {
	onNetwork(t, "devnet")
	padded := `[{"transactions":[` + emptyItems(`{}`, 3_000_000) + `]}]` // about 9 MB
	over := `[` + strings.Repeat(" ", syncReplyLimitBytes) + `]`         // honest-looking, but over the limit
	for _, body := range []string{padded, over} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(body))
		}))
		var batch []Block
		var answered bool
		used := allocatedWhile(func() { batch, answered = fetchBlockBatchChecked(server.URL, 1) })
		server.Close()
		if answered || len(batch) != 0 {
			t.Errorf("a reply of %d bytes was taken (%d blocks)", len(body), len(batch))
		}
		if used > 128<<20 {
			t.Errorf("refusing a reply of %d bytes allocated %d MB", len(body), used>>20)
		}
	}
}

// The probe of another node's chain read its answer into a general map, so
// an answer of nested empty lists took hundreds of megabytes. Only the two
// fields it needs are kept now.
func TestTheChainProbeKeepsOnlyWhatItNeeds(t *testing.T) {
	junk := `{"current_height":7,"total_work":"1234","padding":[` + emptyItems(`[]`, 600_000) + `]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(junk))
	}))
	defer server.Close()
	var height int64
	var work interface{ String() string }
	used := allocatedWhile(func() {
		h, w := peerChainStatus(server.URL)
		height = h
		if w != nil {
			work = w
		}
	})
	if height != 7 || work == nil || work.String() != "1234" {
		t.Errorf("the probe read height %d and work %v, want 7 and 1234", height, work)
	}
	if used > 24<<20 {
		t.Errorf("reading an answer of %d bytes allocated %d MB", len(junk), used>>20)
	}
}

// A node asked for its peers could answer with thousands of addresses, and
// this one would greet them all, every fifteen seconds. Now a few at most.
func TestAPeerListIsGreetedAFewAtATime(t *testing.T) {
	onNetwork(t, "devnet")
	PeerMapMutex.Lock()
	savedPeers := ActivePeerMap
	ActivePeerMap = make(map[string]time.Time)
	PeerMapMutex.Unlock()
	t.Cleanup(func() {
		PeerMapMutex.Lock()
		ActivePeerMap = savedPeers
		PeerMapMutex.Unlock()
	})

	// Every greeting arrives here, whichever spelling of the name it used.
	greeter, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip(err)
	}
	defer greeter.Close()
	var greetings atomic.Int32
	go func() {
		for {
			conn, err := greeter.Accept()
			if err != nil {
				return
			}
			greetings.Add(1)
			conn.Close()
		}
	}()
	port := greeter.Addr().(*net.TCPAddr).Port

	// "localhost" spelled in 300 ways: 300 different addresses for one place.
	var listed []string
	for mask := 0; len(listed) < 300; mask++ {
		name := []byte("localhost")
		for i := range name {
			if mask&(1<<i) != 0 {
				name[i] -= 'a' - 'A'
			}
		}
		listed = append(listed, fmt.Sprintf("%s:%d", name, port))
	}
	list, _ := json.Marshal(listed)

	asked, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip(err)
	}
	defer asked.Close()
	go func() {
		conn, err := asked.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		if _, _, err := readFrame(conn); err == nil {
			writeFrame(conn, "PEER_LIST:"+string(list))
		}
	}()

	askForPeers(asked.Addr().String())
	time.Sleep(3 * time.Second) // the greetings are sent in the background
	if got := greetings.Load(); got == 0 || got > peerListDialsAtMost {
		t.Errorf("a list of %d addresses led to %d greetings, want 1 to %d", len(listed), got, peerListDialsAtMost)
	}
}

// A frame's stated size used to be set aside in full before any of it
// arrived: 36 bytes from a stranger held 2 MB for up to 30 seconds. Now the
// space grows with what arrives.
func TestAFrameThatNeverArrivesHoldsLittle(t *testing.T) {
	header := make([]byte, 36)
	binary.BigEndian.PutUint32(header, MaxPayloadFrameSize)
	near, far := net.Pipe()
	go func() {
		_, _ = near.Write(header)
		near.Close()
	}()
	defer far.Close()
	var err error
	used := allocatedWhile(func() { _, _, err = readFrame(far) })
	if err == nil {
		t.Fatal("a frame that never arrived was read")
	}
	if used > 256<<10 {
		t.Errorf("a frame stating %d bytes and sending none allocated %d KB", MaxPayloadFrameSize, used>>10)
	}

	// A frame that does arrive in full still reads back exactly.
	onNetwork(t, "devnet")
	var sent bytes.Buffer
	writeFrame(&sent, strings.Repeat("x", MaxPayloadFrameSize))
	payload, _, err := readFrameFrom(sent.Bytes())
	if err != nil || len(payload) != MaxPayloadFrameSize {
		t.Fatalf("a frame at the limit read back as %d bytes: %v", len(payload), err)
	}
}
