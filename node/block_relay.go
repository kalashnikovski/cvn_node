package node

import (
	"encoding/json"
	"log"
	"net"
	"sync"
	"time"
)

// HOW A BLOCK TRAVELS
//
// A node passes a new block to the nodes it is in touch with, and each of
// them passes it on. Three things keep that cheap as a network grows.
//
// Connections that are already open are used. A node keeps a few
// connections open to other nodes (relay_uplink.go) and other nodes keep
// some open to it. A block goes down every one of them, in whichever
// direction it was opened. Only a handful of nodes that are not connected
// either way are also called up afresh, so that an older node, or one this
// node has only just heard of, is not left out. The number called is fixed;
// it does not grow with the network.
//
// A large block is announced first. A block of ordinary size is simply sent:
// it is smaller than the conversation about it would be. A block near the
// size limit is 300,000 bytes, and a node connected to ten others would be
// sent it ten times. So for a large block a node sends two lines, its height
// and its identifier, and sends the block itself only to those that ask.
// A node asks the first that told it, and asks another if nothing has come
// after a moment. Only a node that has said it understands announcements is
// sent one; any other is sent the block as before, so nodes running older
// versions miss nothing.
//
// A block is passed on once. The same block used to leave a node twice.
//
// None of this is a rule of the chain: it decides how a block reaches a
// node, not whether the block is valid.

const (
	// announceBlocksOverBytes is the size above which a block is announced
	// and fetched. Below it the block itself is sent.
	announceBlocksOverBytes = 16 * 1024
	// blockFetchPatience is how long a node waits for a block it asked for
	// before asking someone else who announced it.
	blockFetchPatience = 2 * time.Second
	// recentBlocksKept is how many blocks last passed on are kept ready to
	// hand to a node that asks.
	recentBlocksKept = 32
	// freshCallsPerMessage is how many nodes with no open connection either
	// way are called up for each block or transfer.
	freshCallsPerMessage = 8
	// relayFeatureAnnounce is what a node says to tell the other end that it
	// understands announcements.
	relayFeatureAnnounce = "announce"
)

// blockAnnouncement is what is sent in place of a large block.
type blockAnnouncement struct {
	Height int64  `json:"height"`
	Hash   string `json:"hash"`
}

var blockRelay = struct {
	sync.Mutex
	recent      map[string]string // identifier -> the block as it is sent
	recentOrder []string
	uplinks     map[net.Conn]string // connections this node opened -> the other end's address
	announces   map[net.Conn]bool   // connections whose other end understands announcements
	fetching    map[string]time.Time
}{
	recent:    make(map[string]string),
	uplinks:   make(map[net.Conn]string),
	announces: make(map[net.Conn]bool),
	fetching:  make(map[string]time.Time),
}

// rememberBlockPassedOn keeps a block ready for nodes that ask for it. It
// reports whether this is the first time the block is being passed on.
func rememberBlockPassedOn(hash, payload string) (first bool) {
	blockRelay.Lock()
	defer blockRelay.Unlock()
	if _, already := blockRelay.recent[hash]; already {
		return false
	}
	blockRelay.recent[hash] = payload
	blockRelay.recentOrder = append(blockRelay.recentOrder, hash)
	if len(blockRelay.recentOrder) > recentBlocksKept {
		delete(blockRelay.recent, blockRelay.recentOrder[0])
		blockRelay.recentOrder = blockRelay.recentOrder[1:]
	}
	return true
}

func recentBlockPayload(hash string) (string, bool) {
	blockRelay.Lock()
	defer blockRelay.Unlock()
	payload, kept := blockRelay.recent[hash]
	return payload, kept
}

func registerUplinkConnection(conn net.Conn, host string) {
	blockRelay.Lock()
	blockRelay.uplinks[conn] = host
	blockRelay.Unlock()
}

// forgetRelayConnection is called when a connection of either kind closes.
func forgetRelayConnection(conn net.Conn) {
	blockRelay.Lock()
	delete(blockRelay.uplinks, conn)
	delete(blockRelay.announces, conn)
	blockRelay.Unlock()
}

// noteConnectionAnnounces records that the other end of a connection said
// it understands announcements. It reports whether that is news.
func noteConnectionAnnounces(conn net.Conn) (news bool) {
	blockRelay.Lock()
	defer blockRelay.Unlock()
	news = !blockRelay.announces[conn]
	blockRelay.announces[conn] = true
	return news
}

// sendOverOpenConnections sends a block or transfer down every open
// connection, once to each address. Where announcement is not empty, the
// ends that understand announcements are sent that and the others the
// payload. It returns the addresses reached.
func sendOverOpenConnections(payload, announcement string) map[string]bool {
	type open struct {
		conn net.Conn
		host string
	}
	var connections []open
	relaySubscribersMutex.RLock()
	for conn, host := range relaySubscribers {
		connections = append(connections, open{conn, host})
	}
	relaySubscribersMutex.RUnlock()

	reached := make(map[string]bool)
	blockRelay.Lock()
	for conn, host := range blockRelay.uplinks {
		connections = append(connections, open{conn, host})
	}
	messages := make([]string, len(connections))
	for i, connection := range connections {
		if reached[connection.host] {
			continue
		}
		reached[connection.host] = true
		messages[i] = payload
		if announcement != "" && blockRelay.announces[connection.conn] {
			messages[i] = announcement
		}
	}
	blockRelay.Unlock()

	for i, connection := range connections {
		if messages[i] != "" {
			go writeFrame(connection.conn, messages[i])
		}
	}
	return reached
}

// haveBlock reports whether this node already holds the block announced.
func haveBlock(announced blockAnnouncement) bool {
	if _, kept := recentBlockPayload(announced.Hash); kept {
		return true
	}
	return GetBlockByHeightFromDB(announced.Height).Hash == announced.Hash
}

// claimBlockFetch decides who asks for a block. It returns zero when the
// caller should ask now, and otherwise how long to wait before trying again,
// because the block was asked for a moment ago.
func claimBlockFetch(hash string) time.Duration {
	blockRelay.Lock()
	defer blockRelay.Unlock()
	if asked, pending := blockRelay.fetching[hash]; pending {
		if waited := time.Since(asked); waited < blockFetchPatience {
			return blockFetchPatience - waited
		}
	}
	if len(blockRelay.fetching) > 256 {
		for other, asked := range blockRelay.fetching {
			if time.Since(asked) > time.Minute {
				delete(blockRelay.fetching, other)
			}
		}
	}
	blockRelay.fetching[hash] = time.Now()
	return 0
}

// handleBlockAnnouncement answers "I have block H" from the other end of a
// connection: if the block is not held here, it is asked for, from this
// connection now or, when another was asked a moment ago, from this one
// later if the block still has not come.
func handleBlockAnnouncement(conn net.Conn, from, body string) {
	var announced blockAnnouncement
	if json.Unmarshal([]byte(body), &announced) != nil || announced.Height <= 0 || !isLowerHex(announced.Hash, 64) {
		return
	}
	if haveBlock(announced) {
		return
	}
	ask := func() {
		request, _ := json.Marshal(announced)
		log.Printf("📦 [RELAY] Block #%d was announced by %s; asking for it.\n", announced.Height, from)
		writeFrame(conn, "BLOCK_REQUEST:"+string(request))
	}
	wait := claimBlockFetch(announced.Hash)
	if wait == 0 {
		ask()
		return
	}
	go func() {
		time.Sleep(wait)
		if !haveBlock(announced) && claimBlockFetch(announced.Hash) == 0 {
			ask()
		}
	}()
}

// Transfers that arrive from other nodes are checked away from the
// connection they came in on. Checking one takes a little time, a transfer
// that spends 200 coins a good deal more, and blocks now arrive on the same
// connections. Were each transfer checked before the next message is read, a
// burst of transfers would hold up the block behind them, for seconds if the
// burst is large. So a transfer is put in a queue and the connection goes
// straight on to its next message. A transfer that finds the queue full is
// let go: whoever sent it still has it, and it reaches this node again in
// the block that carries it.
var (
	incomingTransfers     = make(chan Transaction, 1024)
	incomingTransferStart sync.Once
)

const incomingTransferCheckers = 2

func queueIncomingTransfer(tx Transaction) {
	incomingTransferStart.Do(func() {
		for checker := 0; checker < incomingTransferCheckers; checker++ {
			go func() {
				for waiting := range incomingTransfers {
					// Only transfers that pass the full checks are stored
					// and passed on; one already held is not passed on again.
					if AdmitTransactionToMempool(waiting) == nil {
						BroadcastNewTransaction(waiting)
					}
				}
			}()
		}
	})
	select {
	case incomingTransfers <- tx:
	default:
	}
}

// placeAfterItsParent deals with a block that has overtaken its parent. A
// large block is announced and then fetched, which takes one exchange longer
// than a small block that is simply sent. So when a small block follows a
// large one closely, the small one can arrive first. It cannot be placed
// without its parent, and treating that as having fallen behind would start
// a comparison of whole chains for nothing.
//
// If the block's parent is one this node asked for a moment ago, the block
// is kept until the parent is in place, or until the wait for the parent
// has run out, and is then placed in the ordinary way. It reports whether it
// took the block.
func placeAfterItsParent(block Block) bool {
	blockRelay.Lock()
	asked, pending := blockRelay.fetching[block.PrevHash]
	blockRelay.Unlock()
	until := asked.Add(2 * blockFetchPatience)
	parentIsTip := func() bool {
		_, tip := latestTipID()
		return tip == block.PrevHash
	}
	if !pending || time.Now().After(until) || parentIsTip() {
		return false
	}
	go func() {
		for time.Now().Before(until) && !parentIsTip() {
			time.Sleep(25 * time.Millisecond)
		}
		placeWireBlock(block)
	}()
	return true
}

// handleBlockRequest answers "send me block H" with the block, if it is one
// of those this node passed on lately. Older blocks are fetched the ordinary
// way, when a node catches up.
func handleBlockRequest(conn net.Conn, body string) {
	var wanted blockAnnouncement
	if json.Unmarshal([]byte(body), &wanted) != nil || !isLowerHex(wanted.Hash, 64) {
		return
	}
	if payload, kept := recentBlockPayload(wanted.Hash); kept {
		writeFrame(conn, payload)
	}
}
