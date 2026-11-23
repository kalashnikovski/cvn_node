package node

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math/big"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"go.etcd.io/bbolt"
)

// THE POOL'S RECORD
//
// A pool has to remember two things: who did how much work lately, and which
// blocks it found. Both are kept in a file of the pool's own, beside the
// chain and separate from it, so that nothing a pool writes can ever touch
// what the node believes about the chain.
//
// A share is one answer from a miner that met the easy difficulty the pool
// gave it. It is recorded with the address to credit and its weight, which is
// that difficulty: an answer at difficulty 40,000 stands for twice the work
// of one at 20,000, however fast or slow the machine that found it.
//
// When the pool finds a block, the reward is divided among the most recent
// shares, counted back from the winning one until their weights add up to the
// work of poolWindowBlocks blocks at the network's difficulty. The division
// is worked out once, at that moment, and written down with the block.

// poolShare is one accepted answer.
type poolShare struct {
	Seq     uint64 `json:"seq"`
	At      int64  `json:"at"`
	Address string `json:"address"`
	Weight  int64  `json:"weight"`
}

// poolPart is one address's part of a block's reward.
type poolPart struct {
	Address string `json:"address"`
	Units   int64  `json:"units"`
	Work    int64  `json:"work"`
}

// What has become of a block the pool found. It is recorded as waiting. When
// enough blocks stand on it that this node would never replace it, it is
// confirmed and its parts are owed; if another block stands at its height by
// then, it is lost and nothing is owed for it (see poolSettle in pool.go).
const (
	poolBlockWaiting   = "waiting"
	poolBlockConfirmed = "confirmed"
	poolBlockLost      = "lost"
)

// poolAccount is what the record says of one address: its parts of every
// confirmed block, how much of that has been paid, and how much is owed.
// Owed is always Earned less Paid.
type poolAccount struct {
	Earned int64 `json:"earned"`
	Owed   int64 `json:"owed"`
	Paid   int64 `json:"paid"`
}

// poolBlock is a block the pool found, with the division of its reward.
type poolBlock struct {
	Height      int64      `json:"height"`
	Hash        string     `json:"hash"`
	At          int64      `json:"at"`
	Finder      string     `json:"finder"`
	Difficulty  int        `json:"difficulty"`
	RewardUnits int64      `json:"reward_units"`
	WindowWork  int64      `json:"window_work"`
	State       string     `json:"state"`
	Parts       []poolPart `json:"parts"`
	// The height of the chain when the block was confirmed or lost.
	DecidedAtHeight int64 `json:"decided_at_height,omitempty"`
}

var (
	poolSharesBucket   = []byte("shares")
	poolBlocksBucket   = []byte("blocks")
	poolAccountsBucket = []byte("accounts")
)

// poolSharesKeptAtMost bounds the record whatever the difficulty does.
const poolSharesKeptAtMost = 2000000

// poolKeepWindows is how many windows' worth of shares are kept. One is
// needed; the rest is room for the network's difficulty to rise.
const poolKeepWindows = 4

type poolRecord struct {
	sync.Mutex
	db      *bbolt.DB
	shares  []poolShare // oldest first
	nextSeq uint64
	blocks  []poolBlock // oldest first
	// What each address has earned, been paid and is owed.
	accounts map[string]poolAccount
	names    map[string]string
	opened   time.Time
	added    int
}

// poolLedger is the record of the pool this node runs, if it runs one.
var poolLedger *poolRecord

// poolLedgerPath is where the pool's record is kept: beside the chain.
func poolLedgerPath() string {
	return filepath.Join(filepath.Dir(BlockchainFile), "pool_"+Params.Name+".db")
}

// openPoolLedger opens the pool's record, making it if there is none, and
// reads what it holds.
func openPoolLedger(path string) (*poolRecord, error) {
	db, err := bbolt.Open(path, 0600, &bbolt.Options{Timeout: 3 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("the pool's record %s could not be opened: %v", path, err)
	}
	record := &poolRecord{db: db, names: make(map[string]string), accounts: make(map[string]poolAccount), opened: time.Now()}
	err = db.Update(func(tx *bbolt.Tx) error {
		shares, err := tx.CreateBucketIfNotExists(poolSharesBucket)
		if err != nil {
			return err
		}
		blocks, err := tx.CreateBucketIfNotExists(poolBlocksBucket)
		if err != nil {
			return err
		}
		accounts, err := tx.CreateBucketIfNotExists(poolAccountsBucket)
		if err != nil {
			return err
		}
		if err := accounts.ForEach(func(address, value []byte) error {
			var account poolAccount
			if json.Unmarshal(value, &account) == nil {
				record.accounts[string(address)] = account
			}
			return nil
		}); err != nil {
			return err
		}
		if err := shares.ForEach(func(_, value []byte) error {
			var share poolShare
			if json.Unmarshal(value, &share) == nil && share.Weight > 0 {
				share.Address = record.sameName(share.Address)
				record.shares = append(record.shares, share)
				if share.Seq >= record.nextSeq {
					record.nextSeq = share.Seq + 1
				}
			}
			return nil
		}); err != nil {
			return err
		}
		return blocks.ForEach(func(_, value []byte) error {
			var block poolBlock
			if json.Unmarshal(value, &block) == nil {
				record.blocks = append(record.blocks, block)
			}
			return nil
		})
	})
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("the pool's record %s could not be read: %v", path, err)
	}
	return record, nil
}

func (r *poolRecord) close() {
	r.Lock()
	defer r.Unlock()
	if r.db != nil {
		r.db.Close()
		r.db = nil
	}
}

// sameName returns one copy of an address for all the shares that carry it,
// so that a long record does not hold the same text a hundred thousand times.
func (r *poolRecord) sameName(address string) string {
	if kept, ok := r.names[address]; ok {
		return kept
	}
	r.names[address] = address
	return address
}

func poolSeqKey(seq uint64) []byte {
	key := make([]byte, 8)
	binary.BigEndian.PutUint64(key, seq)
	return key
}

func poolBlockKey(height int64, hash string) []byte {
	return []byte(fmt.Sprintf("%016x-%s", height, hash))
}

// addShare records a share and returns its place in the record.
func (r *poolRecord) addShare(address string, weight, at int64, networkDifficulty int) (uint64, error) {
	if weight <= 0 {
		return 0, fmt.Errorf("a share must carry work")
	}
	r.Lock()
	defer r.Unlock()
	if r.db == nil {
		return 0, fmt.Errorf("the pool's record is closed")
	}
	share := poolShare{Seq: r.nextSeq, At: at, Address: r.sameName(address), Weight: weight}
	raw, err := json.Marshal(share)
	if err != nil {
		return 0, err
	}
	if err := r.db.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket(poolSharesBucket).Put(poolSeqKey(share.Seq), raw)
	}); err != nil {
		return 0, err
	}
	r.nextSeq++
	r.shares = append(r.shares, share)
	if r.added++; r.added%500 == 0 {
		r.forgetOldSharesLocked(networkDifficulty, at)
	}
	return share.Seq, nil
}

// poolWindowWork is the work the reward of a block is shared over.
func poolWindowWork(networkDifficulty int) int64 {
	if networkDifficulty < 1 {
		networkDifficulty = 1
	}
	return int64(networkDifficulty) * poolWindowBlocks
}

// forgetOldSharesLocked drops the shares that no block found from now on
// could reach: those older than several windows of work at today's
// difficulty, and older than the ten minutes the pool's speed is read from.
func (r *poolRecord) forgetOldSharesLocked(networkDifficulty int, now int64) {
	keepWork := poolWindowWork(networkDifficulty) * poolKeepWindows
	cut := 0
	var newer int64
	for i := len(r.shares) - 1; i >= 0; i-- {
		if newer >= keepWork && r.shares[i].At < now-int64(poolSpeedOver.Seconds()) {
			cut = i + 1
			break
		}
		newer += r.shares[i].Weight
	}
	if over := len(r.shares) - poolSharesKeptAtMost; over > cut {
		cut = over
	}
	if cut <= 0 {
		return
	}
	gone := r.shares[:cut]
	if err := r.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(poolSharesBucket)
		for _, share := range gone {
			if err := bucket.Delete(poolSeqKey(share.Seq)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return // kept for now; tried again later
	}
	r.shares = append([]poolShare(nil), r.shares[cut:]...)
	held := make(map[string]string, len(r.names))
	for _, share := range r.shares {
		held[share.Address] = share.Address
	}
	r.names = held
}

// poolSplit divides a reward among the most recent shares: those at or
// before `winning`, counted back until their weights add up to windowWork.
// The oldest share reached counts only for the part of it inside the window,
// so that the window is exact. Each address's part is rounded down to whole
// units, and the units left over go to the address with the most work, so
// the parts always add up to the reward.
func poolSplit(shares []poolShare, winning uint64, windowWork, rewardUnits int64) ([]poolPart, int64) {
	work := make(map[string]int64)
	remaining := windowWork
	for i := len(shares) - 1; i >= 0 && remaining > 0; i-- {
		share := shares[i]
		if share.Seq > winning {
			continue
		}
		counted := share.Weight
		if counted > remaining {
			counted = remaining
		}
		work[share.Address] += counted
		remaining -= counted
	}
	total := windowWork - remaining
	parts := make([]poolPart, 0, len(work))
	for address, done := range work {
		parts = append(parts, poolPart{Address: address, Work: done})
	}
	// Most work first; between equals, the address that sorts first. The
	// order decides who gets the units left over, so it must not depend on
	// the order a map happens to be read in.
	sort.Slice(parts, func(i, j int) bool {
		if parts[i].Work != parts[j].Work {
			return parts[i].Work > parts[j].Work
		}
		return parts[i].Address < parts[j].Address
	})
	if total <= 0 || rewardUnits <= 0 || len(parts) == 0 {
		return parts, total
	}
	var paid int64
	reward, whole := big.NewInt(rewardUnits), big.NewInt(total)
	for i := range parts {
		part := new(big.Int).Mul(reward, big.NewInt(parts[i].Work))
		parts[i].Units = part.Div(part, whole).Int64()
		paid += parts[i].Units
	}
	parts[0].Units += rewardUnits - paid
	return parts, total
}

// addBlock records a block the pool found, dividing its reward among the
// shares up to and including the one that won it.
func (r *poolRecord) addBlock(block Block, finder string, winning uint64, at int64) (poolBlock, error) {
	var rewardUnits int64
	if len(block.Transactions) > 0 {
		for _, output := range block.Transactions[0].Outputs {
			rewardUnits += output.Amount.Units()
		}
	}
	r.Lock()
	defer r.Unlock()
	if r.db == nil {
		return poolBlock{}, fmt.Errorf("the pool's record is closed")
	}
	parts, windowWork := poolSplit(r.shares, winning, poolWindowWork(block.Difficulty), rewardUnits)
	found := poolBlock{
		Height: block.Index, Hash: block.Hash, At: at, Finder: finder, Difficulty: block.Difficulty,
		RewardUnits: rewardUnits, WindowWork: windowWork, State: poolBlockWaiting, Parts: parts,
	}
	raw, err := json.Marshal(found)
	if err != nil {
		return poolBlock{}, err
	}
	if err := r.db.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket(poolBlocksBucket).Put(poolBlockKey(found.Height, found.Hash), raw)
	}); err != nil {
		return poolBlock{}, err
	}
	r.blocks = append(r.blocks, found)
	return found, nil
}

// waitingBlocks are the blocks whose fate is not decided yet, oldest first.
func (r *poolRecord) waitingBlocks() []poolBlock {
	r.Lock()
	defer r.Unlock()
	var waiting []poolBlock
	for _, block := range r.blocks {
		if block.State == poolBlockWaiting {
			waiting = append(waiting, block)
		}
	}
	return waiting
}

// decide settles a waiting block. If it is on the chain it is confirmed and
// each address in its division is owed its part; if not it is lost and
// nothing is owed. The block's new state and what it adds to every account
// are written together or not at all, so that a program stopped half-way can
// neither credit a block twice nor confirm one without crediting it. A block
// that is not waiting is left as it is.
func (r *poolRecord) decide(height int64, hash string, onChain bool, chainHeight int64) (poolBlock, bool, error) {
	r.Lock()
	defer r.Unlock()
	if r.db == nil {
		return poolBlock{}, false, fmt.Errorf("the pool's record is closed")
	}
	index := -1
	for i := range r.blocks {
		if r.blocks[i].Height == height && r.blocks[i].Hash == hash {
			index = i
			break
		}
	}
	if index < 0 || r.blocks[index].State != poolBlockWaiting {
		return poolBlock{}, false, nil
	}
	decided := r.blocks[index]
	decided.DecidedAtHeight = chainHeight
	decided.State = poolBlockLost
	credited := make(map[string]poolAccount)
	if onChain {
		decided.State = poolBlockConfirmed
		for _, part := range decided.Parts {
			if part.Units <= 0 {
				continue
			}
			account, touched := credited[part.Address]
			if !touched {
				account = r.accounts[part.Address]
			}
			account.Earned += part.Units
			account.Owed += part.Units
			credited[part.Address] = account
		}
	}
	rawBlock, err := json.Marshal(decided)
	if err != nil {
		return poolBlock{}, false, err
	}
	if err := r.db.Update(func(tx *bbolt.Tx) error {
		if err := tx.Bucket(poolBlocksBucket).Put(poolBlockKey(decided.Height, decided.Hash), rawBlock); err != nil {
			return err
		}
		accounts := tx.Bucket(poolAccountsBucket)
		for address, account := range credited {
			raw, err := json.Marshal(account)
			if err != nil {
				return err
			}
			if err := accounts.Put([]byte(address), raw); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return poolBlock{}, false, err
	}
	r.blocks[index] = decided
	for address, account := range credited {
		r.accounts[address] = account
	}
	return decided, true, nil
}

// account is what the record says of one address, and its work in the
// window a block found now would be shared over.
func (r *poolRecord) account(address string, networkDifficulty int) (poolAccount, int64) {
	r.Lock()
	defer r.Unlock()
	var winning uint64
	if r.nextSeq > 0 {
		winning = r.nextSeq - 1
	}
	parts, _ := poolSplit(r.shares, winning, poolWindowWork(networkDifficulty), 0)
	var work int64
	for _, part := range parts {
		if part.Address == address {
			work = part.Work
		}
	}
	return r.accounts[address], work
}

// poolAddressWork is what one address has done lately, and its account.
type poolAddressWork struct {
	Address   string
	Work      int64
	LastShare int64
	Account   poolAccount
}

// poolView is the record as the pool page shows it.
type poolView struct {
	Blocks      []poolBlock       // newest first, at most `blocks`
	BlocksFound int               // all of them
	Addresses   []poolAddressWork // most work first
	WindowWork  int64             // the work those addresses share
	Speed       float64           // hashes a second, over the last ten minutes
	Shares      int               // shares held
	// Blocks by what became of them, and the accounts added up.
	Waiting, Confirmed, Lost int
	Earned, Owed, Paid       int64
}

// view reads the record for the page. The window is the one a block found
// now would be shared over.
func (r *poolRecord) view(networkDifficulty int, now int64, blocks int) poolView {
	r.Lock()
	defer r.Unlock()
	view := poolView{BlocksFound: len(r.blocks), Shares: len(r.shares)}
	for i := len(r.blocks) - 1; i >= 0 && len(view.Blocks) < blocks; i-- {
		view.Blocks = append(view.Blocks, r.blocks[i])
	}
	var winning uint64
	if r.nextSeq > 0 {
		winning = r.nextSeq - 1
	}
	parts, total := poolSplit(r.shares, winning, poolWindowWork(networkDifficulty), 0)
	view.WindowWork = total
	last := make(map[string]int64, len(parts))
	for i := len(r.shares) - 1; i >= 0 && len(last) < len(parts); i-- {
		if _, seen := last[r.shares[i].Address]; !seen {
			last[r.shares[i].Address] = r.shares[i].At
		}
	}
	listed := make(map[string]bool, len(parts))
	for _, part := range parts {
		listed[part.Address] = true
		view.Addresses = append(view.Addresses, poolAddressWork{Address: part.Address, Work: part.Work, LastShare: last[part.Address], Account: r.accounts[part.Address]})
	}
	// Then the addresses that are owed something and are not mining now,
	// the most owed first.
	var others []poolAddressWork
	for address, account := range r.accounts {
		view.Earned += account.Earned
		view.Owed += account.Owed
		view.Paid += account.Paid
		if !listed[address] && account.Owed > 0 {
			others = append(others, poolAddressWork{Address: address, Account: account})
		}
	}
	sort.Slice(others, func(i, j int) bool {
		if others[i].Account.Owed != others[j].Account.Owed {
			return others[i].Account.Owed > others[j].Account.Owed
		}
		return others[i].Address < others[j].Address
	})
	view.Addresses = append(view.Addresses, others...)
	for _, block := range r.blocks {
		switch block.State {
		case poolBlockWaiting:
			view.Waiting++
		case poolBlockConfirmed:
			view.Confirmed++
		case poolBlockLost:
			view.Lost++
		}
	}
	view.Speed = poolSpeedFrom(r.shares, r.opened, time.Unix(now, 0))
	return view
}

// speed is the pool's mining speed in hashes a second.
func (r *poolRecord) speed(now time.Time) float64 {
	r.Lock()
	defer r.Unlock()
	return poolSpeedFrom(r.shares, r.opened, now)
}

// poolSpeedFrom works the pool's speed out from its shares. A share of weight
// w stands for w hashes on average, so the speed is the weight of the shares
// of the last ten minutes over those ten minutes, or over the time the pool
// has been running if that is less.
func poolSpeedFrom(shares []poolShare, opened, now time.Time) float64 {
	over := poolSpeedOver
	if running := now.Sub(opened); running < over {
		over = running
	}
	if over < 30*time.Second {
		over = 30 * time.Second
	}
	since := now.Add(-over).Unix()
	var work int64
	for i := len(shares) - 1; i >= 0 && shares[i].At >= since; i-- {
		work += shares[i].Weight
	}
	return float64(work) / over.Seconds()
}
