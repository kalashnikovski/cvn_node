package node

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// WORK SERVER FOR EXTERNAL MINERS
//
// On a network that mines with RandomX, the node offers mining work over a
// small text protocol (the "stratum" protocol that RandomX mining software
// such as XMRig already speaks). An external miner connects, is given a
// mining blob, a target and a key, and sends back any nonce that meets the
// target. The node then assembles the block and checks it exactly as it
// checks every other block; the external miner is never trusted.
//
// This lets people mine CVN with the fastest RandomX software available
// instead of only with the miner built into this program.
//
// By default the server listens on this machine only (127.0.0.1), for a miner
// running beside the node. `--mining-listen ADDRESS` (for example 0.0.0.0)
// opens it to other machines on the network; anyone who can reach it can mine
// to an address of their choosing, but can do nothing else.
//
// While an external miner is connected the built-in miner pauses, so the two
// do not compete for the same processor.

// externalMinersConnected counts connected external miners.
var externalMinersConnected int32

const (
	stratumJobRefresh  = 15 * time.Second // a new job at least this often, so its date stays current
	stratumTipPoll     = 250 * time.Millisecond
	stratumJobsKept    = 8
	stratumLineLimit   = 16 * 1024
	stratumIdleTimeout = 10 * time.Minute
)

type stratumJob struct {
	id         string
	template   Block // complete except for the nonce and the identifier
	txDigest   string
	commitment string
	key        []byte
	tipHash    string
}

// stratumBadWork is the answer to a submission whose proof does not meet the
// target, and stratumBadWorkLimit how many of them one connection may send.
const (
	stratumBadWork      = "low difficulty share"
	stratumBadWorkLimit = 30
)

type stratumClient struct {
	badWork int
	conn    net.Conn
	writeMu sync.Mutex
	session string
	address string
	jobs    []*stratumJob
	jobsMu  sync.Mutex
}

type stratumRequest struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

var stratumSerial int64

// StartMiningWorkServer starts the work server if this network mines with
// RandomX and has a port for it. It returns the address it listens on.
func StartMiningWorkServer() string {
	if Params.StratumPort <= 0 || !networkUsesRandomX() {
		return ""
	}
	host := "127.0.0.1"
	for i, arg := range os.Args {
		if arg == "--mining-listen" && i+1 < len(os.Args) {
			host = os.Args[i+1]
		}
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(host, fmt.Sprintf("%d", Params.StratumPort)))
	if err != nil {
		announceNetworkStatus(fmt.Sprintf("⚠️ [WORK SERVER] Could not listen on port %d: %v", Params.StratumPort, err))
		return ""
	}
	go serveMiningWork(listener)
	announceNetworkStatus(fmt.Sprintf("⛏️ [WORK SERVER] External miners can connect to %s.", listener.Addr()))
	return listener.Addr().String()
}

func networkUsesRandomX() bool {
	for _, era := range Params.PowSchedule {
		if era.Algorithm == PowRandomX {
			return true
		}
	}
	return false
}

func serveMiningWork(listener net.Listener) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		go handleStratumClient(conn)
	}
}

// buildStratumJob prepares work on top of the current tip paying `address`.
func buildStratumJob(address string) (*stratumJob, bool) {
	if atomic.LoadInt32(&IsSyncingActive) == 1 || atomic.LoadInt32(&MiningHoldActive) == 1 {
		return nil, false
	}
	StateMutex.RLock()
	tip := GetLatestBlockInternal()
	StateMutex.RUnlock()
	nextIndex := tip.Index + 1
	if tip.Hash == "" || powAlgorithmAt(nextIndex) != PowRandomX {
		return nil, false
	}
	timestamp := time.Now().Unix()
	if timestamp <= tip.Timestamp {
		timestamp = tip.Timestamp + 1
	}
	if mustWaitForClock(timestamp, time.Now().Unix()) {
		return nil, false // the tip is dated ahead; wait for the clock to catch up
	}
	key, ok := powKeyFor(nextIndex)
	if !ok {
		return nil, false
	}
	serial := atomic.AddInt64(&stratumSerial, 1)
	transactions, uncleHash, uncleMiner := buildBlockContents(tip, address, timestamp, fmt.Sprintf("external-%d-%d", serial, time.Now().UnixNano()))
	template := Block{
		Version: 1, Index: nextIndex, Timestamp: timestamp, Transactions: transactions, PrevHash: tip.Hash,
		Difficulty: CalculateNextDifficulty(tip, timestamp), UncleHash: uncleHash, UncleMiner: uncleMiner,
	}
	txDigest := BlockTransactionsDigest(nextIndex, transactions)
	return &stratumJob{
		id:         fmt.Sprintf("%x", serial),
		template:   template,
		txDigest:   txDigest,
		commitment: CalculateHashWithTxDigest(template, txDigest),
		key:        key,
		tipHash:    tip.Hash,
	}, true
}

// stratumTargetHex is the target as external miners expect it: the largest
// acceptable value of the top eight bytes of the work, lowest byte first.
func stratumTargetHex(difficulty int) string {
	if difficulty < 1 {
		difficulty = 1
	}
	target := ^uint64(0) / uint64(difficulty)
	raw := make([]byte, 8)
	for i := range raw {
		raw[i] = byte(target >> (8 * uint(i)))
	}
	return hex.EncodeToString(raw)
}

func (j *stratumJob) describe() map[string]interface{} {
	blob, _ := powBlob(j.commitment, 0)
	return map[string]interface{}{
		"blob":      hex.EncodeToString(blob),
		"job_id":    j.id,
		"target":    stratumTargetHex(j.template.Difficulty),
		"algo":      "rx/0",
		"height":    j.template.Index,
		"seed_hash": hex.EncodeToString(j.key),
	}
}

func (c *stratumClient) send(message interface{}) bool {
	raw, err := json.Marshal(message)
	if err != nil {
		return false
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	_, err = c.conn.Write(append(raw, '\n'))
	return err == nil
}

func (c *stratumClient) reply(id json.RawMessage, result interface{}, errorText string) bool {
	message := map[string]interface{}{"id": id, "jsonrpc": "2.0", "error": nil, "result": result}
	if errorText != "" {
		message["error"] = map[string]interface{}{"code": -1, "message": errorText}
		message["result"] = nil
	}
	return c.send(message)
}

// newJob builds fresh work for this client and remembers it.
func (c *stratumClient) newJob() (*stratumJob, bool) {
	job, ok := buildStratumJob(c.address)
	if !ok {
		return nil, false
	}
	c.jobsMu.Lock()
	c.jobs = append(c.jobs, job)
	if len(c.jobs) > stratumJobsKept {
		c.jobs = c.jobs[len(c.jobs)-stratumJobsKept:]
	}
	c.jobsMu.Unlock()
	return job, true
}

func (c *stratumClient) findJob(id string) *stratumJob {
	c.jobsMu.Lock()
	defer c.jobsMu.Unlock()
	for _, job := range c.jobs {
		if job.id == id {
			return job
		}
	}
	return nil
}

func handleStratumClient(conn net.Conn) {
	client := &stratumClient{conn: conn, address: strings.TrimSpace(CustomMinerAddress)}
	defer conn.Close()

	loggedIn := false
	done := make(chan struct{})
	defer func() {
		close(done)
		if loggedIn {
			noteNetworkEvent("⛏️ [WORK SERVER] External miner disconnected: " + conn.RemoteAddr().String())
			// The dashboard is told when the last one leaves, not about each.
			if atomic.AddInt32(&externalMinersConnected, -1) == 0 {
				announceNetworkStatus("⛏️ [WORK SERVER] No external miner is connected any more. The built-in miner takes over.")
			}
		}
	}()

	reader := bufio.NewReaderSize(conn, stratumLineLimit)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(stratumIdleTimeout))
		line, isPrefix, err := reader.ReadLine()
		if err != nil || isPrefix {
			return
		}
		var request stratumRequest
		if json.Unmarshal(line, &request) != nil {
			return
		}
		switch request.Method {
		case "login":
			var params struct {
				Login string `json:"login"`
			}
			_ = json.Unmarshal(request.Params, &params)
			// The login is the address to pay. Mining software often adds a
			// suffix after a dot or plus sign; only the address is kept.
			login := strings.TrimSpace(params.Login)
			if cut := strings.IndexAny(login, ".+"); cut > 0 {
				login = login[:cut]
			}
			// The login is the wallet the work is paid to. Anything that is
			// not a wallet address is refused outright: paying this node's
			// own address instead would take the miner's work without
			// telling them, and paying the text as given would burn it.
			if !looksLikeWalletAddress(login) {
				client.reply(request.ID, nil, "log in with a CVN wallet address: CVN_ followed by 40 hex digits")
				return
			}
			client.address = login
			job, ok := client.newJob()
			if !ok {
				client.reply(request.ID, nil, "the node is not ready to hand out work yet")
				return
			}
			client.session = fmt.Sprintf("%x", atomic.AddInt64(&stratumSerial, 1))
			if !client.reply(request.ID, map[string]interface{}{
				"id": client.session, "job": job.describe(), "extensions": []string{"algo"}, "status": "OK",
			}, "") {
				return
			}
			if !loggedIn {
				loggedIn = true
				// The dashboard is told when the first one arrives, not about each.
				if atomic.AddInt32(&externalMinersConnected, 1) == 1 {
					announceNetworkStatus(fmt.Sprintf("⛏️ [WORK SERVER] External miner connected from %s, paying %s. The built-in miner is paused.", conn.RemoteAddr(), client.address))
				} else {
					noteNetworkEvent(fmt.Sprintf("⛏️ [WORK SERVER] External miner connected from %s, paying %s.", conn.RemoteAddr(), client.address))
				}
				go client.pushJobs(done, job.tipHash)
			}
		case "submit":
			if !loggedIn {
				client.reply(request.ID, nil, "log in first")
				return
			}
			var params struct {
				JobID string `json:"job_id"`
				Nonce string `json:"nonce"`
			}
			_ = json.Unmarshal(request.Params, &params)
			if errorText := client.submit(params.JobID, params.Nonce); errorText != "" {
				if !client.reply(request.ID, nil, errorText) {
					return
				}
				// Every answer that does not carry the work costs this node
				// a proof check. A miner that keeps sending them is faulty
				// or hostile, and is disconnected. Answers that were good
				// but arrived after the chain moved on are not counted.
				if errorText == stratumBadWork {
					if client.badWork++; client.badWork >= stratumBadWorkLimit {
						noteNetworkEvent("⛏️ [WORK SERVER] Disconnecting " + conn.RemoteAddr().String() + ": too many answers without the work.")
						return
					}
				}
			} else if !client.reply(request.ID, map[string]string{"status": "OK"}, "") {
				return
			}
		case "keepalived":
			if !client.reply(request.ID, map[string]string{"status": "KEEPALIVED"}, "") {
				return
			}
		case "getjob":
			if job, ok := client.newJob(); ok {
				client.reply(request.ID, job.describe(), "")
			} else {
				client.reply(request.ID, nil, "no work available")
			}
		default:
			if !client.reply(request.ID, nil, "unknown method") {
				return
			}
		}
	}
}

// pushJobs sends the client new work whenever the chain tip changes, and at
// least every stratumJobRefresh so the block's date stays current.
func (c *stratumClient) pushJobs(done <-chan struct{}, lastTip string) {
	lastSent := time.Now()
	ticker := time.NewTicker(stratumTipPoll)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
		}
		StateMutex.RLock()
		tipHash := GetLatestBlockInternal().Hash
		StateMutex.RUnlock()
		if tipHash == lastTip && time.Since(lastSent) < stratumJobRefresh {
			continue
		}
		job, ok := c.newJob()
		if !ok {
			continue
		}
		lastTip, lastSent = job.tipHash, time.Now()
		if !c.send(map[string]interface{}{"jsonrpc": "2.0", "method": "job", "params": job.describe()}) {
			return
		}
	}
}

// submit checks a nonce an external miner found and, if it makes a valid
// block, adds that block to the chain. It returns "" on success.
func (c *stratumClient) submit(jobID, nonceHex string) string {
	// While the node is replacing or extending its chain from another node,
	// the tip is not settled, and the built-in miner is paused for the same
	// reason.
	if atomic.LoadInt32(&IsSyncingActive) == 1 {
		return "stale job: the node is catching up with the network"
	}
	job := c.findJob(jobID)
	if job == nil {
		return "unknown or expired job"
	}
	raw, err := hex.DecodeString(nonceHex)
	if err != nil || len(raw) != 4 {
		return "malformed nonce"
	}
	// The four bytes are given in the order they sit in the blob: lowest first.
	nonce := uint32(raw[0]) | uint32(raw[1])<<8 | uint32(raw[2])<<16 | uint32(raw[3])<<24

	block := job.template
	block.Nonce = int64(nonce)
	block.Hash = CalculateHashWithTxDigest(block, job.txDigest)
	if !BlockProofValid(block, block.Difficulty) {
		return stratumBadWork
	}
	StateMutex.RLock()
	tip := GetLatestBlockInternal()
	StateMutex.RUnlock()
	if tip.Hash != job.tipHash {
		return "stale job: the chain has moved on"
	}
	if !ValidateBlockSignatures(block) || !SaveBlockToStorage(block) {
		return "block rejected"
	}
	logMutex.Lock()
	liveTerminalEventLogs = append(liveTerminalEventLogs, fmt.Sprintf("[%s] 🎉 [SUCCESS] External miner solved valid block #%-3d | Hash: %s", time.Now().Format("15:04:05"), block.Index, block.Hash[:16]))
	logMutex.Unlock()
	return ""
}
