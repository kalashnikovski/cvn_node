package node

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// A stand-in for external mining software. It speaks the same protocol as
// XMRig and applies XMRig's own rule for deciding whether a nonce is good:
// the top eight bytes of the RandomX result, read lowest byte first, must be
// below the target the server sent.
type fakeMiner struct {
	t      *testing.T
	conn   net.Conn
	reader *bufio.Reader
	nextID int
}

type minerJob struct {
	Blob     string `json:"blob"`
	JobID    string `json:"job_id"`
	Target   string `json:"target"`
	Algo     string `json:"algo"`
	Height   int64  `json:"height"`
	SeedHash string `json:"seed_hash"`
}

func connectFakeMiner(t *testing.T, address string) *fakeMiner {
	t.Helper()
	conn, err := net.Dial("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return &fakeMiner{t: t, conn: conn, reader: bufio.NewReader(conn)}
}

func (m *fakeMiner) readMessage() map[string]json.RawMessage {
	m.t.Helper()
	_ = m.conn.SetReadDeadline(time.Now().Add(20 * time.Second))
	line, err := m.reader.ReadBytes('\n')
	if err != nil {
		m.t.Fatalf("no message from the work server: %v", err)
	}
	var message map[string]json.RawMessage
	if err := json.Unmarshal(line, &message); err != nil {
		m.t.Fatalf("unreadable message %q", line)
	}
	return message
}

// call sends a request and returns the reply to it, setting aside any work
// the server pushes in between.
func (m *fakeMiner) call(method string, params interface{}) (result json.RawMessage, errorText string, pushed []minerJob) {
	m.t.Helper()
	m.nextID++
	request, _ := json.Marshal(map[string]interface{}{"id": m.nextID, "jsonrpc": "2.0", "method": method, "params": params})
	if _, err := m.conn.Write(append(request, '\n')); err != nil {
		m.t.Fatal(err)
	}
	for {
		message := m.readMessage()
		if string(message["method"]) == `"job"` {
			var job minerJob
			_ = json.Unmarshal(message["params"], &job)
			pushed = append(pushed, job)
			continue
		}
		if raw := message["error"]; len(raw) > 0 && string(raw) != "null" {
			var failure struct {
				Message string `json:"message"`
			}
			_ = json.Unmarshal(raw, &failure)
			return nil, failure.Message, pushed
		}
		return message["result"], "", pushed
	}
}

func (m *fakeMiner) waitForPushedJob() minerJob {
	m.t.Helper()
	for {
		message := m.readMessage()
		if string(message["method"]) == `"job"` {
			var job minerJob
			_ = json.Unmarshal(message["params"], &job)
			return job
		}
	}
}

// search tries nonces the way mining software does and returns the first
// that is good (wantGood) or the first that is not.
func (m *fakeMiner) search(job minerJob, wantGood bool) string {
	m.t.Helper()
	blob, err := hex.DecodeString(job.Blob)
	if err != nil || len(blob) != powBlobSize {
		m.t.Fatalf("the blob should be %d bytes, got %q", powBlobSize, job.Blob)
	}
	key, _ := hex.DecodeString(job.SeedHash)
	targetBytes, _ := hex.DecodeString(job.Target)
	if len(targetBytes) != 8 {
		m.t.Fatalf("the target should be 8 bytes, got %q", job.Target)
	}
	var target uint64
	for i, b := range targetBytes {
		target |= uint64(b) << (8 * uint(i))
	}
	for nonce := uint32(0); nonce < 100000; nonce++ {
		blob[39], blob[40], blob[41], blob[42] = byte(nonce), byte(nonce>>8), byte(nonce>>16), byte(nonce>>24)
		value, ok := randomXVerify(key, blob)
		if !ok {
			m.t.Fatal("RandomX failed")
		}
		top, _ := strconv.ParseUint(value[:16], 16, 64)
		if (top < target) == wantGood {
			return hex.EncodeToString(blob[39:43])
		}
	}
	m.t.Fatal("no suitable nonce found")
	return ""
}

func startWorkServerForTest(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go serveMiningWork(listener)
	return listener.Addr().String()
}

func TestMiningBlobLayout(t *testing.T) {
	onNetwork(t, "devnet")
	block := Block{Version: 1, Index: 7, Timestamp: 1700000000, PrevHash: strings.Repeat("ab", 32), Difficulty: 5000, Nonce: 0x04030201,
		Transactions: []Transaction{{ID: "TX_COINBASE_H7_x"}}}
	commitment := powCommitment(block)
	if withOtherNonce := block; true {
		withOtherNonce.Nonce = 99
		if powCommitment(withOtherNonce) != commitment {
			t.Error("the commitment must not depend on the nonce")
		}
	}
	changed := block
	changed.Timestamp++
	if powCommitment(changed) == commitment {
		t.Error("the commitment must change when anything else in the block changes")
	}
	blob, ok := powBlob(commitment, uint32(block.Nonce))
	if !ok || len(blob) != 43 || blob[0] != 1 {
		t.Fatalf("blob is %x", blob)
	}
	for _, b := range blob[1:7] {
		if b != 0 {
			t.Fatalf("bytes 1-6 must be zero: %x", blob)
		}
	}
	if hex.EncodeToString(blob[7:39]) != commitment {
		t.Error("bytes 7-38 must be the commitment")
	}
	if blob[39] != 1 || blob[40] != 2 || blob[41] != 3 || blob[42] != 4 {
		t.Errorf("the nonce must sit at byte 39, lowest byte first: %x", blob[39:])
	}
	// A nonce that does not fit in four bytes can never be valid.
	block.Nonce = powNonceLimit
	block.Hash = CalculateHash(block)
	if BlockProofValid(block, 1) {
		t.Error("a nonce wider than four bytes was accepted")
	}
}

func TestWorkValueIsReadLastByteFirst(t *testing.T) {
	var hash [32]byte
	hash[31] = 0x01 // most significant
	hash[0] = 0xff  // least significant
	if got := powWorkValue(hash); got != "01"+strings.Repeat("00", 30)+"ff" {
		t.Errorf("work value is %s", got)
	}
	if stratumTargetHex(1) != "ffffffffffffffff" || stratumTargetHex(256) != "ffffffffffffff00" {
		t.Errorf("targets: %s %s", stratumTargetHex(1), stratumTargetHex(256))
	}
}

func TestExternalMinerFindsABlock(t *testing.T) {
	startRandomXChain(t, []PowEra{{FromHeight: 1, Algorithm: PowRandomX, MinDifficulty: 32}})
	Params.HeartbeatInitialDifficulty = 32
	atomic.StoreInt32(&externalMinersConnected, 0)
	server := startWorkServerForTest(t)

	miner := connectFakeMiner(t, server)
	// Mining software commonly appends a rig name to the address.
	result, errorText, _ := miner.call("login", map[string]string{"login": "CVN_external_miner.rig1", "pass": "x", "agent": "test"})
	if errorText != "" {
		t.Fatalf("login refused: %s", errorText)
	}
	var login struct {
		ID     string   `json:"id"`
		Status string   `json:"status"`
		Job    minerJob `json:"job"`
	}
	if err := json.Unmarshal(result, &login); err != nil || login.Status != "OK" || login.ID == "" {
		t.Fatalf("unexpected login reply: %s", result)
	}
	job := login.Job
	if job.Height != 1 || job.Algo != "rx/0" || len(job.SeedHash) != 64 || job.SeedHash != GetBlockByHeightFromDB(0).Hash {
		t.Fatalf("first job should be for block 1, keyed by the genesis block: %+v", job)
	}
	if atomic.LoadInt32(&externalMinersConnected) != 1 {
		t.Error("the built-in miner should pause while an external miner is connected")
	}

	// A nonce that misses the target is refused and changes nothing.
	if _, errorText, _ = miner.call("submit", map[string]string{"id": login.ID, "job_id": job.JobID, "nonce": miner.search(job, false), "result": strings.Repeat("0", 64)}); !strings.Contains(errorText, "low difficulty") {
		t.Errorf("a nonce without enough work got %q, want a low-difficulty refusal", errorText)
	}
	if GetLatestBlockInternal().Index != 0 {
		t.Fatal("a refused nonce changed the chain")
	}
	if _, errorText, _ = miner.call("submit", map[string]string{"id": login.ID, "job_id": "no-such-job", "nonce": "00000000"}); errorText == "" {
		t.Error("a nonce for an unknown job was accepted")
	}

	// A good nonce becomes a block that pays the address given at login.
	goodNonce := miner.search(job, true)
	result, errorText, pushed := miner.call("submit", map[string]string{"id": login.ID, "job_id": job.JobID, "nonce": goodNonce, "result": strings.Repeat("0", 64)})
	if errorText != "" || !strings.Contains(string(result), "OK") {
		t.Fatalf("a good nonce was refused: %s %s", errorText, result)
	}
	tip := GetLatestBlockInternal()
	if tip.Index != 1 || tip.Transactions[0].Outputs[0].Recipient != "CVN_external_miner" {
		t.Fatalf("block 1 should exist and pay the external miner: height %d", tip.Index)
	}
	if balanceOf("CVN_external_miner") != cvn("50") {
		t.Errorf("the external miner has %s, want 50", FormatUnits(balanceOf("CVN_external_miner")))
	}

	// The same nonce again is stale: the chain has moved on.
	if _, errorText, more := miner.call("submit", map[string]string{"id": login.ID, "job_id": job.JobID, "nonce": goodNonce}); errorText == "" {
		t.Error("a nonce for a block already found was accepted again")
	} else {
		pushed = append(pushed, more...)
	}

	// New work for the next block arrives without being asked for.
	var next minerJob
	if len(pushed) > 0 {
		next = pushed[len(pushed)-1]
	} else {
		next = miner.waitForPushedJob()
	}
	for next.Height < 2 {
		next = miner.waitForPushedJob()
	}
	if next.Height != 2 || next.JobID == job.JobID || next.Blob == job.Blob {
		t.Fatalf("expected fresh work for block 2, got %+v", next)
	}
	if _, errorText, _ = miner.call("submit", map[string]string{"id": login.ID, "job_id": next.JobID, "nonce": miner.search(next, true)}); errorText != "" {
		t.Fatalf("the second block was refused: %s", errorText)
	}
	if GetLatestBlockInternal().Index != 2 || balanceOf("CVN_external_miner") != cvn("100") {
		t.Errorf("after two blocks: height %d, balance %s", GetLatestBlockInternal().Index, FormatUnits(balanceOf("CVN_external_miner")))
	}

	miner.conn.Close()
	for i := 0; i < 100 && atomic.LoadInt32(&externalMinersConnected) != 0; i++ {
		time.Sleep(20 * time.Millisecond)
	}
	if atomic.LoadInt32(&externalMinersConnected) != 0 {
		t.Error("the built-in miner should resume once the external miner disconnects")
	}
}

func TestWorkServerRefusesAMinerWithoutAnAddress(t *testing.T) {
	startRandomXChain(t, []PowEra{{FromHeight: 1, Algorithm: PowRandomX, MinDifficulty: 32}})
	savedAddress := CustomMinerAddress
	CustomMinerAddress = ""
	defer func() { CustomMinerAddress = savedAddress }()
	server := startWorkServerForTest(t)

	miner := connectFakeMiner(t, server)
	if _, errorText, _ := miner.call("login", map[string]string{"login": "not-an-address"}); errorText == "" {
		t.Error("a miner with no CVN address to pay was given work")
	}
}

func TestWorkServerOnlyRunsOnRandomXNetworks(t *testing.T) {
	onNetwork(t, "testnet-1")
	if networkUsesRandomX() || Params.StratumPort != 0 || StartMiningWorkServer() != "" {
		t.Error("testnet-1 mines with SHA-256 and must not start a work server")
	}
	onNetwork(t, "devnet")
	if !networkUsesRandomX() || Params.StratumPort != 28083 {
		t.Error("devnet must offer mining work on port 28083")
	}
}
