package node

import (
	"encoding/hex"
	"fmt"
	"math"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// A POOL: MANY MINERS, ONE NODE
//
// Started with `--pool CVN_ADDRESS`, a node becomes a pool. Miners anywhere
// point the fast miner at it and log in with their own CVN address. The node
// keeps a record of how much work each miner did (pool_ledger.go), and every
// block it builds pays the miners themselves, by that record: the block's
// reward has an output for each of them (pool_inblock.go). The pool never
// holds those coins. Only parts too small for the block, or more than a
// block has room for, go to the pool's address; each is owed to its miner
// once the block is too deep to be replaced, and paid by ordinary transfers
// which this node prepares and a separate command signs (pool_pay.go). The
// node itself holds no key.
//
// Nothing here is a rule of the chain. A block found by a pool is an
// ordinary block, checked by every node like any other, and anyone can run a
// pool of their own with this same option.
//
// HOW WORK IS HANDED OUT
//
// The work server (stratum.go) already gives every miner a block of its own
// to work on and checks each answer with CVN's RandomX. A pool changes three
// things:
//
//   - The block's reward is divided among the miners who did the recent
//     work, whoever is working on it.
//   - The miner is asked for much less than the network asks: a difficulty
//     of its own, chosen so that it sends an answer about every
//     poolShareSeconds. Each answer that meets it is a share, recorded with
//     that difficulty as its weight. A fast machine is given a higher
//     difficulty and a slow one a lower, and because a share counts for its
//     difficulty both are credited in proportion to the work they did.
//   - An answer that also meets the network's difficulty is a block, and is
//     placed on the chain exactly as before.
//
// One answer costs the pool one RandomX hash to check. The lowest difficulty
// a pool hands out (poolMinShareDifficulty) keeps the cost of making a share
// thousands of times the cost of checking it.

// poolAddress is where the blocks of the pool this node runs are paid. It is
// empty on a node that is not a pool.
var poolAddress string

// The pool's settings. They are variables so that tests can run a pool on a
// chain whose blocks take a handful of hashes.
var (
	// poolShareSeconds is how often each miner should send a share.
	poolShareSeconds = 30.0
	// poolStartDifficulty is what a miner is asked for before the pool has
	// seen how fast it is: one share in 30 seconds at about 1,700 hashes a
	// second.
	poolStartDifficulty = 50000
	// poolMinShareDifficulty is the least a miner is ever asked for.
	poolMinShareDifficulty = 2500
	// poolWindowBlocks is how many blocks' worth of work a reward is shared
	// over (decided 23 November 2026: twice a block's work).
	poolWindowBlocks = int64(2)
	// A miner's difficulty is looked at again after this many shares, or
	// after this long, whichever comes first.
	poolRetargetShares = 6
	poolRetargetAfter  = 2 * time.Minute
)

const (
	// poolSpeedOver is the stretch of time the pool's speed is read from.
	poolSpeedOver = 10 * time.Minute
	// One address on the internet may hold this many connections, and the
	// pool this many in all.
	poolConnectionsPerHost = 32
	poolMinersAtMost       = 2000
	// poolDuplicateShare is the answer to a share handed in twice.
	poolDuplicateShare = "duplicate share"
)

// poolRunning reports whether this node is a pool.
func poolRunning() bool {
	return poolAddress != ""
}

// poolAddressRequested reads `--pool CVN_ADDRESS` from the command line.
func poolAddressRequested(args []string) (address string, on bool, err error) {
	for i, arg := range args {
		if arg != "--pool" {
			continue
		}
		if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
			return "", false, fmt.Errorf("--pool needs the address the pool's blocks are paid to: --pool CVN_...")
		}
		address = strings.TrimSpace(args[i+1])
		if !looksLikeWalletAddress(address) {
			return "", false, fmt.Errorf("--pool was given %q, which is not a CVN wallet address: %s", address, addressFormDescription())
		}
		return address, true, nil
	}
	return "", false, nil
}

// startPool opens the pool's record and the port miners connect to.
func startPool() error {
	if !networkUsesRandomX() {
		return fmt.Errorf("a pool needs a network mined with RandomX, and %s is not one", Params.Name)
	}
	record, err := openPoolLedger(poolLedgerPath())
	if err != nil {
		return err
	}
	poolLedger = record
	listening := StartMiningWorkServer()
	if listening == "" {
		return fmt.Errorf("the pool could not open port %d for miners", Params.StratumPort)
	}
	announceNetworkStatus(fmt.Sprintf("⛏️ [POOL] This node is a pool. Its blocks pay %s. Miners connect to port %d and log in with their own CVN address.", poolAddress, Params.StratumPort))
	if err := startPoolPayListener(); err != nil {
		return err
	}
	go runPoolSettler()
	// Copies of the record, kept beside the chain's backups (pool_backup.go).
	if _, _, on := backupSettings(os.Args); on {
		go runPoolBackupSchedule(poolBackupEvery, poolBackupsKept)
	}
	return nil
}

// ---------------------------------------------------------------------------
// When a block counts
// ---------------------------------------------------------------------------

// poolConfirmDepth is how many blocks must stand on a block the pool found
// before it counts. It is the number of its own blocks a node will never
// replace (the reorganisation limit): once that many stand on a block, this
// node holds it for good, and the pool was really paid for it.
func poolConfirmDepth() int64 {
	if Params.Economics == nil || Params.Economics.ReorgLimitBlocks <= 0 {
		return 100
	}
	return Params.Economics.ReorgLimitBlocks
}

// poolSettleEvery is how often the pool looks at the blocks still waiting.
const poolSettleEvery = 15 * time.Second

func runPoolSettler() {
	for {
		time.Sleep(poolSettleEvery)
		// In this order: blocks that have come to count are counted before
		// the chain is read for payments the record does not know of.
		poolSettle()
		poolReadChain()
		poolSettlePayments(time.Now())
	}
}

// poolSettle decides every block the pool found that has waited long enough.
// A block still on the chain is confirmed, and its parts are owed. A block
// whose place another block has taken is lost, and nothing is owed for it:
// its reward never reached the pool. It returns how many it confirmed and
// how many it found lost.
func poolSettle() (confirmed, lost int) {
	if poolLedger == nil {
		return 0, 0
	}
	depth := poolConfirmDepth()
	for _, waiting := range poolLedger.waitingBlocks() {
		// While this node is replacing or extending its chain from another
		// node, what stands at a height is not settled. Looked at again later.
		if atomic.LoadInt32(&IsSyncingActive) == 1 {
			return confirmed, lost
		}
		StateMutex.RLock()
		tipHeight := GetLatestBlockInternal().Index
		held := GetBlockByHeightFromDB(waiting.Height)
		StateMutex.RUnlock()
		onChain := held.Hash == waiting.Hash
		rewardTx := ""
		if onChain && len(held.Transactions) > 0 && len(held.Transactions[0].Outputs) > 0 {
			rewardTx = held.Transactions[0].ID
		}
		if tipHeight < waiting.Height+depth || atomic.LoadInt32(&IsSyncingActive) == 1 {
			continue
		}
		decided, changed, err := poolLedger.decide(waiting.Height, waiting.Hash, onChain, tipHeight, rewardTx)
		if err != nil {
			announceNetworkStatus(fmt.Sprintf("⚠️ [POOL] Block #%d could not be settled in the pool's record: %v", waiting.Height, err))
			continue
		}
		if !changed {
			continue
		}
		if decided.State == poolBlockConfirmed {
			confirmed++
			inBlock := 0
			for _, part := range decided.Parts {
				if part.InBlock {
					inBlock++
				}
			}
			announceNetworkStatus(fmt.Sprintf("✅ [POOL] Block #%d counts: %d blocks stand on it. It paid %s CVN to %d address(es) itself, and %s CVN is now owed to %d more.",
				decided.Height, depth, FormatUnits(decided.RewardUnits-decided.PoolUnits), inBlock, FormatUnits(decided.PoolUnits), len(decided.Parts)-inBlock))
		} else {
			lost++
			announceNetworkStatus(fmt.Sprintf("⚠️ [POOL] Block #%d was replaced by another block. Nothing is owed for it.", decided.Height))
		}
	}
	return confirmed, lost
}

// ---------------------------------------------------------------------------
// Who may connect
// ---------------------------------------------------------------------------

var poolConnections = struct {
	sync.Mutex
	byHost map[string]int
	all    int
}{byHost: make(map[string]int)}

// Answers without the work cost the pool a proof check each. An address
// that sends poolStrikesAllowed of them within poolStrikesOver, across all
// its connections, is refused for poolStrikeoutFor. Variables so tests need
// not wait.
var (
	poolStrikesAllowed = 100
	poolStrikesOver    = 10 * time.Minute
	poolStrikeoutFor   = 10 * time.Minute
)

var poolStrikes = struct {
	sync.Mutex
	byHost map[string]poolStrikeCount
}{byHost: make(map[string]poolStrikeCount)}

type poolStrikeCount struct {
	count       int
	since, till time.Time
}

// poolNoteBadWork counts an answer without the work from a host, and reports
// whether that host is now refused.
func poolNoteBadWork(host string, now time.Time) bool {
	poolStrikes.Lock()
	defer poolStrikes.Unlock()
	strikes := poolStrikes.byHost[host]
	if now.Sub(strikes.since) > poolStrikesOver {
		strikes = poolStrikeCount{since: now}
	}
	strikes.count++
	if strikes.count >= poolStrikesAllowed {
		strikes.till = now.Add(poolStrikeoutFor)
	}
	poolStrikes.byHost[host] = strikes
	// The table is kept small: what is long past is forgotten.
	if len(poolStrikes.byHost) > 4*poolMinersAtMost {
		for other, counted := range poolStrikes.byHost {
			if now.Sub(counted.since) > poolStrikesOver && now.After(counted.till) {
				delete(poolStrikes.byHost, other)
			}
		}
	}
	return now.Before(strikes.till)
}

// poolStruckOut reports whether a host is refused for now.
func poolStruckOut(host string, now time.Time) bool {
	poolStrikes.Lock()
	defer poolStrikes.Unlock()
	return now.Before(poolStrikes.byHost[host].till)
}

// poolAdmit takes a place for a connection from a host, or refuses it.
func poolAdmit(host string) bool {
	if poolStruckOut(host, time.Now()) {
		return false
	}
	poolConnections.Lock()
	defer poolConnections.Unlock()
	if poolConnections.all >= poolMinersAtMost || poolConnections.byHost[host] >= poolConnectionsPerHost {
		return false
	}
	poolConnections.byHost[host]++
	poolConnections.all++
	return true
}

func poolRelease(host string) {
	poolConnections.Lock()
	defer poolConnections.Unlock()
	if poolConnections.byHost[host] <= 1 {
		delete(poolConnections.byHost, host)
	} else {
		poolConnections.byHost[host]--
	}
	if poolConnections.all > 0 {
		poolConnections.all--
	}
}

// hostOf is the address a connection came from, without its port.
func hostOf(conn net.Conn) string {
	host, _, err := net.SplitHostPort(conn.RemoteAddr().String())
	if err != nil {
		return conn.RemoteAddr().String()
	}
	return host
}

// ---------------------------------------------------------------------------
// A miner's own difficulty
// ---------------------------------------------------------------------------

// poolMiner is what the pool keeps for one connected miner.
type poolMiner struct {
	sync.Mutex
	difficulty   int
	periodStart  time.Time
	periodWork   int64
	periodShares int
	changed      bool // new work at the new difficulty has not been sent yet
}

// poolNextShareDifficulty is the difficulty to ask of a miner next, given
// what it did at the current one: `work` is the weight of the `shares` it
// sent in `seconds`. The aim is one share every poolShareSeconds. A step is
// never more than four times up or down, never below the pool's least or
// above `ceiling` (the network's own difficulty), and a change of less than a
// fifth is not made at all, so that a steady miner is left alone.
func poolNextShareDifficulty(current int, work int64, shares int, seconds float64, ceiling int) int {
	if current < 1 {
		current = 1
	}
	if seconds < 1 {
		seconds = 1
	}
	speed := float64(work) / seconds
	if shares == 0 {
		// Nothing in all that time: it is slower than one share's worth.
		speed = float64(current) / 2 / seconds
	}
	next := speed * poolShareSeconds
	next = math.Min(next, float64(current)*4)
	next = math.Max(next, float64(current)/4)
	if ceiling > 0 {
		next = math.Min(next, float64(ceiling))
	}
	next = math.Max(next, float64(poolMinShareDifficulty))
	if math.Abs(next-float64(current)) < float64(current)/5 {
		return current
	}
	return int(next)
}

func (m *poolMiner) begin(now time.Time) {
	m.Lock()
	defer m.Unlock()
	m.difficulty = poolStartDifficulty
	if m.difficulty < poolMinShareDifficulty {
		m.difficulty = poolMinShareDifficulty
	}
	m.periodStart = now
}

func (m *poolMiner) asked() int {
	m.Lock()
	defer m.Unlock()
	return m.difficulty
}

// retargetLocked looks at the period just ended and starts the next.
func (m *poolMiner) retargetLocked(now time.Time, ceiling int) {
	next := poolNextShareDifficulty(m.difficulty, m.periodWork, m.periodShares, now.Sub(m.periodStart).Seconds(), ceiling)
	if next != m.difficulty {
		m.difficulty = next
		m.changed = true
	}
	m.periodStart, m.periodWork, m.periodShares = now, 0, 0
}

// noteShare counts a share towards the miner's speed.
func (m *poolMiner) noteShare(weight int64, now time.Time, ceiling int) {
	m.Lock()
	defer m.Unlock()
	m.periodWork += weight
	m.periodShares++
	if m.periodShares >= poolRetargetShares || now.Sub(m.periodStart) >= poolRetargetAfter {
		m.retargetLocked(now, ceiling)
	}
}

// easeIfQuiet lowers the difficulty of a miner that has sent too little for
// too long, so that a slow machine is not left without shares.
func (m *poolMiner) easeIfQuiet(now time.Time, ceiling int) {
	m.Lock()
	defer m.Unlock()
	if !m.periodStart.IsZero() && now.Sub(m.periodStart) >= poolRetargetAfter {
		m.retargetLocked(now, ceiling)
	}
}

// takeChanged reports, once, that the miner's difficulty has changed.
func (m *poolMiner) takeChanged() bool {
	m.Lock()
	defer m.Unlock()
	changed := m.changed
	m.changed = false
	return changed
}

// ---------------------------------------------------------------------------
// Shares
// ---------------------------------------------------------------------------

// shareDifficultyFor is what a job asks of this miner: its own difficulty,
// or the network's if that is lower.
func (c *stratumClient) shareDifficultyFor(networkDifficulty int) int {
	asked := c.pool.asked()
	if asked < poolMinShareDifficulty {
		asked = poolMinShareDifficulty
	}
	if asked > networkDifficulty {
		asked = networkDifficulty
	}
	return asked
}

// submitShare checks an answer from one of the pool's miners. An answer that
// meets the miner's own difficulty is recorded as a share; one that also
// meets the network's becomes a block. It returns "" for a share that was
// counted.
func (c *stratumClient) submitShare(jobID, nonceHex string) string {
	if atomic.LoadInt32(&IsSyncingActive) == 1 {
		return "stale job: the node is catching up with the network"
	}
	// A pool that has fallen behind the network hands out no new work (see
	// buildStratumJob), and takes none in: a block found on the old work
	// would be lost, and shares for it would be paid for out of blocks that
	// other work earned.
	if atomic.LoadInt32(&MiningHoldActive) == 1 {
		return "stale job: the pool is waiting for the network"
	}
	job := c.findJob(jobID)
	if job == nil {
		return "unknown or expired job"
	}
	// Work made when the pool held no shares pays the whole block to the
	// miner it was made for. Once another address has a share it is out of
	// date, so that it cannot be kept back and used to take a block that
	// others now have a part in (review of the pool, round 3, finding 6).
	if job.noShares && poolLedger.hasSharesOtherThan(c.address) {
		return "stale job: the pool's division has changed"
	}
	raw, err := hex.DecodeString(nonceHex)
	if err != nil || len(raw) != 4 {
		return "malformed nonce"
	}
	nonce := uint32(raw[0]) | uint32(raw[1])<<8 | uint32(raw[2])<<16 | uint32(raw[3])<<24

	// Work on a block that is no longer the newest is worth nothing to the
	// pool, and saying so costs no proof check.
	StateMutex.RLock()
	tip := GetLatestBlockInternal()
	StateMutex.RUnlock()
	if tip.Hash != job.tipHash {
		return "stale job: the chain has moved on"
	}
	if job.seen[nonce] {
		return poolDuplicateShare
	}
	// Written down before it is checked: the same nonce sent again, good
	// or not, costs no second proof check.
	if job.seen == nil {
		job.seen = make(map[uint32]bool)
	}
	job.seen[nonce] = true
	blob, ok := powBlob(job.commitment, nonce)
	if !ok {
		return "malformed job"
	}
	work, ok := randomXVerify(job.key, blob)
	if !ok {
		return "the pool could not check this share"
	}
	asked := job.askedDifficulty()
	if work > HeartbeatTargetHex(asked) {
		return stratumBadWork
	}

	now := time.Now()
	place, err := poolLedger.addShare(c.address, int64(asked), now.Unix(), job.template.Difficulty)
	if err != nil {
		noteNetworkEvent("⚠️ [POOL] A share could not be recorded: " + err.Error())
		return "the pool could not record this share"
	}
	c.pool.noteShare(int64(asked), now, job.template.Difficulty)

	if work <= HeartbeatTargetHex(job.template.Difficulty) {
		c.poolBlockFound(job, nonce, place, now)
	}
	return ""
}

// poolBlockFound places a block one of the pool's miners found and records
// it with the division of its reward. The share that found it is already
// recorded, and stays recorded whatever becomes of the block.
func (c *stratumClient) poolBlockFound(job *stratumJob, nonce uint32, winningShare uint64, now time.Time) {
	block := job.template
	block.Nonce = int64(nonce)
	block.Hash = CalculateHashWithTxDigest(block, job.txDigest)
	if !BlockProofValid(block, block.Difficulty) {
		return
	}
	// Written into the record first, and placed second. A program stopped
	// between the two then leaves a block in the record that is not in the
	// chain, which is found out and counted as lost like any other. The
	// other way round it would leave a block in the chain that the record
	// never heard of, and the parts it paid the pool's address owed to
	// nobody (review of the pool, finding 2).
	found, err := poolLedger.addBlock(block, c.address, job.parts, job.poolIndex, job.poolUnits, job.windowWork, now.Unix())
	if err != nil {
		announceNetworkStatus(fmt.Sprintf("⚠️ [POOL] Block #%d was found, but could not be written into the pool's record, and was not placed: %v", block.Index, err))
		return
	}
	// Checked and stored with the chain held still, as every other block is.
	if !placeOnTip(block) {
		poolLedger.dropBlock(block.Index, block.Hash)
		noteNetworkEvent(fmt.Sprintf("⛏️ [POOL] A miner found block #%d, but the chain had moved on before it could be placed.", block.Index))
		return
	}
	logMutex.Lock()
	liveTerminalEventLogs = append(liveTerminalEventLogs, fmt.Sprintf("[%s] 🎉 [SUCCESS] The pool found block #%-3d | %s CVN shared among %d address(es) | Hash: %s",
		now.Format("15:04:05"), block.Index, FormatUnits(found.RewardUnits), len(found.Parts), block.Hash[:16]))
	logMutex.Unlock()
}

// poolSpeedNow is the pool's speed in hashes a second, and the number of
// miners connected.
func poolSpeedNow() (float64, int) {
	if poolLedger == nil {
		return 0, 0
	}
	return poolLedger.speed(time.Now()), int(atomic.LoadInt32(&externalMinersConnected))
}
