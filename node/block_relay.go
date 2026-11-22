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
	// before asking someone else who announced it. A full block of 300,000
	// bytes over a connection that has been idle takes about six trips there
	// and back; between Australia and Europe that is a little over two
	// seconds. At two seconds a far-away node asked a second and a third
	// node for a block that was already on its way, and each sent a whole
	// copy. The wait only comes into play when the first node asked does not
	// deliver, so a longer one does not slow a block down otherwise.
	blockFetchPatience = 4 * time.Second
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
	identities  map[net.Conn]string // connections -> the name the node at the other end gave itself
	fetching    map[string]*blockFetch
}{
	recent:     make(map[string]string),
	uplinks:    make(map[net.Conn]string),
	announces:  make(map[net.Conn]bool),
	identities: make(map[net.Conn]string),
	fetching:   make(map[string]*blockFetch),
}

// blockFetch is what is known about one block that was announced and asked
// for: when it was last asked for, and which other nodes have announced it
// since and can be asked next.
type blockFetch struct {
	began     time.Time // when it was first asked for
	lastAsked time.Time
	arrived   time.Time       // when a copy of the block came in; zero until then
	first     string          // the address whose announcement began it
	heard     map[string]bool // addresses that have announced it
	waiting   []blockAnnouncer
	following bool // a goroutine is waiting to ask the next one
}

type blockAnnouncer struct {
	conn net.Conn
	from string
}

const (
	// blockFetchesKept is how many announced blocks are followed at once.
	blockFetchesKept = 256
	// blockFetchesPerAddress is how many of those one address may have
	// begun. Announcing a block that does not exist costs nothing, so one
	// address must not be able to fill the table.
	blockFetchesPerAddress = 8
	// blockAnnouncersKept is how many nodes are remembered, for one block,
	// as able to supply it.
	blockAnnouncersKept = 16
	// blockFetchKept is how long an announced block is followed.
	blockFetchKept = 30 * time.Second
)

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
	delete(blockRelay.identities, conn)
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

// noteRelayIdentity records the name the node at the other end of a
// connection gave itself (a random name each node makes up when it starts).
func noteRelayIdentity(conn net.Conn, identity string) {
	if identity == "" || len(identity) > 64 {
		return
	}
	blockRelay.Lock()
	blockRelay.identities[conn] = identity
	blockRelay.Unlock()
}

// sendOverOpenConnections sends a block or transfer down every open
// connection, once to each node. Where announcement is not empty, the
// ends that understand announcements are sent that and the others the
// payload. It returns the addresses reached.
//
// Two connections are taken to be with one node only when they are with the
// same address and the far end gave the same name on both: that is this node
// and another having each opened a connection to the other. The address alone
// is not enough. Two nodes in one house, one office, or behind one address
// shared by a mobile carrier look alike from outside, and when the address
// alone was used each block went to only one of them.
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
	sent := make(map[string]bool)
	for i, connection := range connections {
		if identity := blockRelay.identities[connection.conn]; identity != "" {
			node := connection.host + "|" + identity
			if sent[node] {
				continue
			}
			sent[node] = true
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

// handleBlockAnnouncement answers "I have block H" from the other end of a
// connection. If the block is not held here it is asked for: from this
// connection now, when nobody has been asked yet, and otherwise from this one
// in its turn if the block still has not come.
//
// Each address is asked once for a block, and those that announced it are
// asked in the order they did. When the turns were drawn for instead, a node
// that announced a block over and over and never sent it held nearly every
// turn, and the node that would have sent it was never asked.
func handleBlockAnnouncement(conn net.Conn, from, body string) {
	var announced blockAnnouncement
	if json.Unmarshal([]byte(body), &announced) != nil || announced.Height <= 0 || !isLowerHex(announced.Hash, 64) {
		return
	}
	if haveBlock(announced) {
		return
	}
	blockRelay.Lock()
	fetch := blockRelay.fetching[announced.Hash]
	if fetch != nil {
		if fetch.arrived.IsZero() && !fetch.heard[from] && len(fetch.waiting) < blockAnnouncersKept {
			fetch.heard[from] = true
			fetch.waiting = append(fetch.waiting, blockAnnouncer{conn, from})
			if !fetch.following {
				fetch.following = true
				go askNextAnnouncer(announced)
			}
		}
		blockRelay.Unlock()
		return
	}
	// A block nobody has announced before. Old entries are cleared, and the
	// announcement is let pass if this address has begun its share already.
	begun := make(map[string]int)
	for hash, other := range blockRelay.fetching {
		if time.Since(other.lastAsked) > blockFetchKept {
			delete(blockRelay.fetching, hash)
		} else {
			begun[other.first]++
		}
	}
	if begun[from] >= blockFetchesPerAddress {
		blockRelay.Unlock()
		return
	}
	if len(blockRelay.fetching) >= blockFetchesKept {
		// The table is full. Thirty-two addresses, each announcing its eight
		// blocks that do not exist, used to fill it, and a real block
		// announced by an honest neighbour was then not asked for (review
		// round 7, new finding 3). So room is made at the cost of whoever
		// holds the most places: the oldest block begun by the address with
		// the most gives way. Addresses that fill the table lose places
		// to those that hardly use it, never the other way about.
		most := ""
		for address, places := range begun {
			if places > begun[most] || (places == begun[most] && address < most) {
				most = address
			}
		}
		// "More places than the announcer" was not enough: with the table
		// full of addresses holding one place each, a new address holding
		// none pushed one of them out, and that one might be the honest
		// neighbour whose block was on its way (the review of the round 7
		// fixes, finding 2). So the address that gives way must still hold
		// at least as many places afterwards as the one announcing now.
		// Between two that would end up level, the one already being
		// fetched from stays.
		if begun[most] < begun[from]+2 {
			blockRelay.Unlock()
			return
		}
		oldest := ""
		for hash, other := range blockRelay.fetching {
			if other.first == most && (oldest == "" || other.began.Before(blockRelay.fetching[oldest].began)) {
				oldest = hash
			}
		}
		delete(blockRelay.fetching, oldest)
	}
	now := time.Now()
	blockRelay.fetching[announced.Hash] = &blockFetch{began: now, lastAsked: now, first: from, heard: map[string]bool{from: true}}
	blockRelay.Unlock()
	askForAnnouncedBlock(blockAnnouncer{conn, from}, announced)
}

// noteAnnouncedBlockArrived records that a copy of a block has come in. If
// the block is one this node asked for after an announcement, it returns how
// long ago it was first asked for. From then on nobody else is asked for it.
func noteAnnouncedBlockArrived(hash string) (sinceAsked time.Duration, asked bool) {
	blockRelay.Lock()
	defer blockRelay.Unlock()
	fetch := blockRelay.fetching[hash]
	if fetch == nil || !fetch.arrived.IsZero() {
		return 0, false
	}
	fetch.arrived = time.Now()
	fetch.waiting = nil
	return fetch.arrived.Sub(fetch.began), true
}

func askForAnnouncedBlock(announcer blockAnnouncer, announced blockAnnouncement) {
	request, _ := json.Marshal(announced)
	log.Printf("📦 [RELAY] Block #%d was announced by %s; asking for it.\n", announced.Height, announcer.from)
	writeFrame(announcer.conn, "BLOCK_REQUEST:"+string(request))
}

// askNextAnnouncer waits out the patience allowed to whoever was asked last
// and, if the block has still not come, asks the next node that announced it.
// It ends when the block is here or nobody is left to ask.
func askNextAnnouncer(announced blockAnnouncement) {
	for {
		blockRelay.Lock()
		fetch := blockRelay.fetching[announced.Hash]
		if fetch == nil {
			blockRelay.Unlock()
			return
		}
		wait := blockFetchPatience - time.Since(fetch.lastAsked)
		blockRelay.Unlock()
		if wait > 0 {
			time.Sleep(wait)
		}
		held := haveBlock(announced)

		blockRelay.Lock()
		fetch = blockRelay.fetching[announced.Hash]
		if fetch == nil {
			blockRelay.Unlock()
			return
		}
		// A copy that has come in and is being checked is enough: asking
		// another node now would only bring a second copy of the same block.
		if held || !fetch.arrived.IsZero() || len(fetch.waiting) == 0 {
			fetch.following = false
			fetch.waiting = nil
			blockRelay.Unlock()
			return
		}
		if time.Since(fetch.lastAsked) < blockFetchPatience {
			blockRelay.Unlock()
			continue
		}
		next := fetch.waiting[0]
		fetch.waiting = fetch.waiting[1:]
		fetch.lastAsked = time.Now()
		blockRelay.Unlock()
		askForAnnouncedBlock(next, announced)
	}
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
	fetch, pending := blockRelay.fetching[block.PrevHash]
	var asked time.Time
	if pending {
		asked = fetch.lastAsked
	}
	blockRelay.Unlock()
	until := asked.Add(2 * blockFetchPatience)
	// The wait is for the parent to be held, not for it to be the newest
	// block. A rival of the newest block has a parent that is held and is not
	// the newest, and it used to be kept waiting here for nothing, for up to
	// twice the wait, when that parent had been fetched a moment before.
	parentIsHeld := func() bool {
		height, tip := latestTipID()
		return tip == block.PrevHash || height >= block.Index
	}
	if !pending || time.Now().After(until) || parentIsHeld() {
		return false
	}
	go func() {
		for time.Now().Before(until) && !parentIsHeld() {
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
