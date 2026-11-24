package node

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.etcd.io/bbolt"
)

// PAYING OUT
//
// A pool's node holds no key. It knows what each address is owed, and which
// of the pool's coins are settled and free, and from those it prepares the
// payments: who is paid what, from which coins. Signing them is the work of
// a separate command, run by the pool's operator or by a timer:
//
//	cvn_node --network NAME --pool-pay
//
// The command asks the node on the same machine for the payments it has
// prepared, signs each with the pool's key, and hands the signed transfers
// back. The node checks that each is exactly the payment it prepared, signed
// by the pool's address, records it as sent, and passes it to the network.
// So the key is in memory only for the seconds the command runs, in a
// program that listens to nobody.
//
// The command signs what the node says is owed. A node that had been taken
// over could ask it to sign a wrong payment; what it could not do is learn
// the key. The pool's address never holds more than the rewards of its
// recent blocks.
//
// The two talk over a port that listens on this machine only (the work
// port plus one). A request that came through another program on the way,
// such as a web server put in front of the node, is refused.

// The settings of a payment run. They are variables so that tests can use
// small figures.
var (
	// An address owed less than this waits until it is owed more (decided
	// 23 November 2026: 1 CVN).
	poolMinimumPaymentUnits = int64(100000000)
	// The fee each payment transfer carries, shared equally among the
	// addresses it pays (decided 23 November 2026: 0.001 CVN).
	poolPaymentFeeUnits = int64(100000)
	// A payment prepared and not signed within this time is undone.
	poolPreparedFor = 10 * time.Minute
	// A payment sent and not yet mined is handed to the network again this
	// often.
	poolResendEvery = 5 * time.Minute
)

// poolPayPort is where the paying command reaches the pool's node.
func poolPayPort() int {
	return Params.StratumPort + 1
}

// poolPayHost is the address of this machine the two talk over: its own
// loopback address. (The local test network gives every node a loopback
// address of its own.)
func poolPayHost() string {
	if ip := net.ParseIP(Params.ListenHost); ip != nil && ip.IsLoopback() {
		return Params.ListenHost
	}
	return "127.0.0.1"
}

// ---------------------------------------------------------------------------
// The node's side
// ---------------------------------------------------------------------------

// poolCoinUsable reports whether a coin in the pool's record is on the chain
// as the record says, belongs to the pool, and is not already being spent by
// a waiting transfer.
func poolCoinUsable(coin poolCoin) bool {
	if GlobalBoltEngine == nil {
		return false
	}
	if MempoolPendingSpends()[coin.key()] {
		return false
	}
	usable := false
	_ = GlobalBoltEngine.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(utxoBucket)
		if bucket == nil {
			return nil
		}
		record, found := bucketLookup(bucket)(coin.key())
		usable = found && strings.TrimSpace(record.Recipient) == poolAddress && record.Amount.Units() == coin.Units && record.Amount.Spendable()
		return nil
	})
	return usable
}

// poolPrepare prepares a run of payments (see preparePayments).
func poolPrepare(now time.Time) ([]poolPayment, string, error) {
	if poolLedger == nil {
		return nil, "", fmt.Errorf("this node is not a pool")
	}
	if atomic.LoadInt32(&IsSyncingActive) == 1 || atomic.LoadInt32(&MiningHoldActive) == 1 {
		return nil, "", fmt.Errorf("the pool is catching up with the network; try again when it has")
	}
	// Nothing is paid from a record that does not know of everything the
	// pool's address has sent: it would show as owed what has been paid
	// already (pool_chain.go). The chain is read to its tip first, blocks
	// that have come to count being counted before that.
	poolSettle()
	if _, readToTip := poolReadChain(); !readToTip {
		return nil, "", fmt.Errorf("the pool could not read the chain to its newest block just now; try again in a minute")
	}
	if unknown := poolLedger.unknownTransfers(); len(unknown) > 0 {
		return nil, "", fmt.Errorf("the pool's address has sent %d transfer(s) that the pool's record does not know of (the first is %s, in block #%d). "+
			"That is what a record put back from an older copy looks like, or a transfer sent by hand with the pool's key. "+
			"Nothing is paid until each is %d blocks deep and the pool has taken it into its record, so that nobody is paid twice",
			len(unknown), unknown[0].Tx.ID, unknown[0].Height, poolConfirmDepth())
	}
	StateMutex.RLock()
	height := GetLatestBlockInternal().Index
	StateMutex.RUnlock()
	// Payments prepared earlier and never signed give their amounts and
	// coins back first.
	poolLedger.returnStalePayments(now.Add(-poolPreparedFor).Unix(), height)
	return poolLedger.preparePayments(poolPaymentPlan{
		Now: now.Unix(), PoolAddress: poolAddress,
		MinimumUnits: poolMinimumPaymentUnits, FeeUnits: poolPaymentFeeUnits,
		Usable: poolCoinUsable,
	})
}

// poolPaymentMatches reports whether a signed transfer is exactly the
// payment that was prepared: the same coins, the same outputs in the same
// order, the same fee, from the pool's address, and nothing besides.
func poolPaymentMatches(payment poolPayment, signed Transaction) error {
	if strings.TrimSpace(signed.Witness) != poolAddress {
		return fmt.Errorf("the transfer is not from the pool's address")
	}
	if signed.FreeWillOffering.Units() != payment.FeeUnits {
		return fmt.Errorf("the transfer's fee is %s CVN; the payment was prepared with %s", FormatUnits(signed.FreeWillOffering.Units()), FormatUnits(payment.FeeUnits))
	}
	if signed.DataSizeKB != 0 || len(signed.Extra) != 0 {
		return fmt.Errorf("the transfer carries something the payment did not")
	}
	if len(signed.Inputs) != len(payment.Inputs) || len(signed.Outputs) != len(payment.Outputs) {
		return fmt.Errorf("the transfer spends %d coins into %d outputs; the payment was prepared with %d and %d", len(signed.Inputs), len(signed.Outputs), len(payment.Inputs), len(payment.Outputs))
	}
	for i, input := range payment.Inputs {
		got := signed.Inputs[i]
		if got.SourceTxID != input.SourceTxID || got.Index != input.Index || got.AmountUnits != input.AmountUnits {
			return fmt.Errorf("coin %d of the transfer is not the coin the payment was prepared with", i+1)
		}
	}
	for i, output := range payment.Outputs {
		got := signed.Outputs[i]
		if strings.TrimSpace(got.Recipient) != output.Recipient || got.Amount.Units() != output.Amount.Units() {
			return fmt.Errorf("output %d of the transfer is not what the payment was prepared with", i+1)
		}
	}
	return nil
}

// poolAcceptSigned takes the signed transfer of a prepared payment. It is
// recorded as sent before it is handed to the network, so that a program
// stopped in between still knows of it and hands it over when it starts
// again. If this node itself refuses the transfer, nothing has left the
// machine, and the payment is undone.
func poolAcceptSigned(serial uint64, signed Transaction, now time.Time) (string, error) {
	if poolLedger == nil {
		return "", fmt.Errorf("this node is not a pool")
	}
	payment, found := poolLedger.payment(serial)
	if !found {
		return "", fmt.Errorf("the pool has no payment number %d", serial)
	}
	if payment.State != poolPaymentPrepared {
		if payment.TxID != "" && payment.TxID == signed.ID {
			return payment.TxID, nil // handed in twice: once is enough
		}
		return "", fmt.Errorf("payment number %d is not waiting to be signed: it is %s", serial, payment.State)
	}
	signer, err := CheckTransferStandalone(signed)
	if err != nil {
		return "", fmt.Errorf("the transfer is not validly signed: %v", err)
	}
	if strings.TrimSpace(signer) != poolAddress {
		return "", fmt.Errorf("the transfer is signed by %s; the pool pays from %s", signer, poolAddress)
	}
	if err := poolPaymentMatches(payment, signed); err != nil {
		return "", err
	}
	StateMutex.RLock()
	height := GetLatestBlockInternal().Index
	StateMutex.RUnlock()
	// Tried against the coins before anything is written, so that a
	// transfer this node would refuse is never recorded as sent.
	if err := poolTransferWouldBeTaken(signed); err != nil {
		_, _ = poolLedger.returnPayment(serial, height)
		return "", fmt.Errorf("this node would not take the transfer (%v). Nothing was sent, and the amounts are owed again", err)
	}
	if _, recorded, err := poolLedger.markSent(serial, signed, now.Unix(), height); err != nil {
		return "", fmt.Errorf("the payment could not be recorded as sent: %v", err)
	} else if !recorded {
		return "", fmt.Errorf("payment number %d is no longer waiting to be signed", serial)
	}
	poolHandToNetwork(signed)
	poolResent.Lock()
	poolResent.at[serial] = now
	poolResent.Unlock()
	// A copy of the record that knows of this payment (pool_backup.go).
	poolBackupSoon()
	announceNetworkStatus(fmt.Sprintf("💸 [POOL] Payment %d sent: %s CVN to %d address(es). Transfer %s", serial, FormatUnits(payment.debits()), len(payment.Lines), signed.ID))
	return signed.ID, nil
}

// poolTransferWouldBeTaken checks a transfer against the coins as the
// mempool would, without putting it there.
func poolTransferWouldBeTaken(signed Transaction) error {
	if GlobalBoltEngine == nil {
		return fmt.Errorf("node is not ready")
	}
	pending := MempoolPendingSpends()
	var checkErr error
	_ = GlobalBoltEngine.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(utxoBucket)
		if bucket == nil {
			checkErr = fmt.Errorf("coin database missing")
			return nil
		}
		_, _, checkErr = checkTransferAgainstCoins(signed, bucketLookup(bucket), pending)
		return nil
	})
	return checkErr
}

// poolHandToNetwork puts a payment's transfer among the transfers waiting
// for a block and passes it to other nodes. Doing it again does no harm.
func poolHandToNetwork(signed Transaction) {
	if err := AdmitTransactionToMempool(signed); err != nil && err.Error() != "already known" {
		noteNetworkEvent(fmt.Sprintf("⚠️ [POOL] Transfer %s was not taken by this node just now: %v", signed.ID, err))
		return
	}
	inBackground(func() { BroadcastNewTransaction(signed) })
}

// poolResent is when each payment under way was last handed to the network,
// and how far the chain has been searched for it.
var poolResent = struct {
	sync.Mutex
	at       map[uint64]time.Time
	searched map[uint64]int64
}{at: make(map[uint64]time.Time), searched: make(map[uint64]int64)}

// poolFindTransfer looks for a transfer in the blocks from one height to
// another, and returns the block that holds it.
func poolFindTransfer(id string, from, to int64) (height int64, blockHash string, found bool) {
	if from < 1 {
		from = 1
	}
	for h := from; h <= to; h++ {
		block := GetBlockByHeightFromDB(h)
		for _, transaction := range block.Transactions {
			if transaction.ID == id {
				return h, block.Hash, true
			}
		}
	}
	return 0, "", false
}

// poolSettlePayments looks after the payments under way. One prepared and
// never signed is undone. One that was sent is looked for in the chain;
// until it is found it is handed to the network again from time to time,
// and once it is deep enough that it cannot be replaced it is settled. It
// returns how many it settled.
func poolSettlePayments(now time.Time) (settled int) {
	if poolLedger == nil || atomic.LoadInt32(&IsSyncingActive) == 1 {
		return 0
	}
	StateMutex.RLock()
	tipHeight := GetLatestBlockInternal().Index
	StateMutex.RUnlock()
	for _, serial := range poolLedger.returnStalePayments(now.Add(-poolPreparedFor).Unix(), tipHeight) {
		noteNetworkEvent(fmt.Sprintf("💸 [POOL] Payment %d was prepared and never signed. Its amounts are owed again.", serial))
	}
	depth := poolConfirmDepth()
	for _, payment := range poolLedger.unsettledPayments() {
		if payment.State != poolPaymentSent || payment.Tx == nil {
			continue
		}
		if atomic.LoadInt32(&IsSyncingActive) == 1 {
			return settled
		}
		// Is it still in the block it was found in? The chain may have changed.
		if payment.MinedAtHeight > 0 && GetBlockByHeightFromDB(payment.MinedAtHeight).Hash != payment.MinedInBlock {
			if poolLedger.noteMined(payment.Serial, 0, "") == nil {
				payment.MinedAtHeight, payment.MinedInBlock = 0, ""
				poolResent.Lock()
				delete(poolResent.searched, payment.Serial)
				poolResent.Unlock()
			}
		}
		if payment.MinedAtHeight == 0 {
			poolResent.Lock()
			from := poolResent.searched[payment.Serial] + 1
			lastSent := poolResent.at[payment.Serial]
			poolResent.Unlock()
			if from <= payment.SentAtHeight {
				from = payment.SentAtHeight + 1
			}
			if height, hash, found := poolFindTransfer(payment.TxID, from, tipHeight); found {
				if poolLedger.noteMined(payment.Serial, height, hash) == nil {
					payment.MinedAtHeight, payment.MinedInBlock = height, hash
				}
			} else {
				poolResent.Lock()
				poolResent.searched[payment.Serial] = tipHeight
				poolResent.Unlock()
				if now.Sub(lastSent) >= poolResendEvery {
					poolHandToNetwork(*payment.Tx)
					poolResent.Lock()
					poolResent.at[payment.Serial] = now
					poolResent.Unlock()
				}
			}
		}
		if payment.MinedAtHeight > 0 && tipHeight >= payment.MinedAtHeight+depth {
			done, changed, err := poolLedger.settlePayment(payment.Serial, tipHeight)
			if err != nil {
				announceNetworkStatus(fmt.Sprintf("⚠️ [POOL] Payment %d could not be settled in the pool's record: %v", payment.Serial, err))
				continue
			}
			if changed {
				settled++
				poolResent.Lock()
				delete(poolResent.at, payment.Serial)
				delete(poolResent.searched, payment.Serial)
				poolResent.Unlock()
				announceNetworkStatus(fmt.Sprintf("✅ [POOL] Payment %d is settled: %s CVN paid to %d address(es), in block #%d.", done.Serial, FormatUnits(done.debits()), len(done.Lines), done.MinedAtHeight))
			}
		}
	}
	return settled
}

// What the node and the paying command say to each other.
type poolPrepareReply struct {
	PoolAddress  string        `json:"pool_address"`
	Network      string        `json:"network"`
	Payments     []poolPayment `json:"payments"`
	Reason       string        `json:"reason,omitempty"`
	MinimumUnits int64         `json:"minimum_units"`
	FeeUnits     int64         `json:"fee_units"`
	Error        string        `json:"error,omitempty"`
}

type poolSignedRequest struct {
	Serial uint64      `json:"serial"`
	Tx     Transaction `json:"tx"`
}

type poolSignedReply struct {
	TxID  string `json:"tx_id,omitempty"`
	Error string `json:"error,omitempty"`
}

type poolReleaseRequest struct {
	Serials []uint64 `json:"serials"`
}

// fromThisMachine reports whether a request was made on this machine and
// came straight here.
func fromThisMachine(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return false
	}
	// A web server in front of the node calls from this machine too, on
	// behalf of anyone. It says so in these headers.
	for _, header := range []string{"X-Forwarded-For", "X-Forwarded-Host", "X-Real-Ip", "Forwarded", "Via"} {
		if r.Header.Get(header) != "" {
			return false
		}
	}
	return true
}

// poolPayAPI is what the pool's node answers to the paying command.
func poolPayAPI() *http.ServeMux {
	pages := http.NewServeMux()
	local := func(handle func(w http.ResponseWriter, r *http.Request)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if !fromThisMachine(r) {
				w.WriteHeader(http.StatusForbidden)
				json.NewEncoder(w).Encode(map[string]string{"error": "payments are prepared for a command on the pool's own machine only"})
				return
			}
			if r.Method != http.MethodPost {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			// A page open in a browser on this machine can make the browser
			// post here, but not with this content type.
			if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
				w.WriteHeader(http.StatusUnsupportedMediaType)
				return
			}
			handle(w, r)
		}
	}
	pages.HandleFunc("/prepare", local(func(w http.ResponseWriter, r *http.Request) {
		reply := poolPrepareReply{PoolAddress: poolAddress, Network: Params.Name, MinimumUnits: poolMinimumPaymentUnits, FeeUnits: poolPaymentFeeUnits}
		payments, reason, err := poolPrepare(time.Now())
		if err != nil {
			reply.Error = err.Error()
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		reply.Payments, reply.Reason = payments, reason
		json.NewEncoder(w).Encode(reply)
	}))
	pages.HandleFunc("/signed", local(func(w http.ResponseWriter, r *http.Request) {
		var request poolSignedRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 512*1024)).Decode(&request); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(poolSignedReply{Error: "the request could not be read"})
			return
		}
		id, err := poolAcceptSigned(request.Serial, request.Tx, time.Now())
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(poolSignedReply{Error: err.Error()})
			return
		}
		json.NewEncoder(w).Encode(poolSignedReply{TxID: id})
	}))
	// The command gives back payments it will not sign after all, so that
	// their amounts and coins need not wait out the ten minutes.
	pages.HandleFunc("/release", local(func(w http.ResponseWriter, r *http.Request) {
		var request poolReleaseRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&request); err != nil || poolLedger == nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		StateMutex.RLock()
		height := GetLatestBlockInternal().Index
		StateMutex.RUnlock()
		released := 0
		for _, serial := range request.Serials {
			if done, _ := poolLedger.returnPayment(serial, height); done {
				released++
			}
		}
		json.NewEncoder(w).Encode(map[string]int{"released": released})
	}))
	return pages
}

// startPoolPayListener opens the port the paying command talks to, on this
// machine's own address only.
func startPoolPayListener() error {
	address := net.JoinHostPort(poolPayHost(), strconv.Itoa(poolPayPort()))
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("the pool could not open %s for the paying command: %v", address, err)
	}
	server := &http.Server{
		Handler:           poolPayAPI(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    64 << 10,
	}
	go func() { _ = server.Serve(listener) }()
	return nil
}

// ---------------------------------------------------------------------------
// The paying command
// ---------------------------------------------------------------------------

// poolPayRequested reports whether the program was started to pay out, and
// reads `--password-file PATH`.
func poolPayRequested(args []string) (on bool, passwordFile string) {
	for i, arg := range args {
		switch arg {
		case "--pool-pay":
			on = true
		case "--password-file":
			if i+1 < len(args) {
				passwordFile = args[i+1]
			}
		}
	}
	return on, passwordFile
}

func poolPayCall(path string, request, reply interface{}) (int, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return 0, err
	}
	client := &http.Client{Timeout: 3 * time.Minute}
	resp, err := client.Post("http://"+net.JoinHostPort(poolPayHost(), strconv.Itoa(poolPayPort()))+path, "application/json", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, json.NewDecoder(resp.Body).Decode(reply)
}

// poolPaymentLooksRight is the paying command's own look at a payment before
// it signs: the figures add up, the fee is the small one a pool charges, and
// any change comes back to the pool. It cannot know who is owed what; that
// is the node's record. It can refuse to sign something that does not hang
// together.
func poolPaymentLooksRight(payment poolPayment, ownAddress string, feeUnits int64) error {
	if len(payment.Lines) == 0 || len(payment.Inputs) == 0 || len(payment.Inputs) > TxMaxInputs || len(payment.Outputs) > TxMaxOutputs {
		return fmt.Errorf("it pays %d addresses from %d coins", len(payment.Lines), len(payment.Inputs))
	}
	if payment.FeeUnits != feeUnits || payment.FeeUnits < 0 || payment.FeeUnits > 10*poolPaymentFeeUnits {
		return fmt.Errorf("its fee is %s CVN", FormatUnits(payment.FeeUnits))
	}
	var spent, paid, debited int64
	for _, input := range payment.Inputs {
		if input.AmountUnits <= 0 {
			return fmt.Errorf("it spends a coin of no amount")
		}
		spent += input.AmountUnits
	}
	wantOutputs := len(payment.Lines)
	if payment.ChangeUnits > 0 {
		wantOutputs++
	}
	if len(payment.Outputs) != wantOutputs {
		return fmt.Errorf("it has %d outputs for %d addresses", len(payment.Outputs), len(payment.Lines))
	}
	for i, line := range payment.Lines {
		output := payment.Outputs[i]
		if output.Recipient != line.Address || output.Amount.Units() != line.Receives || !looksLikeWalletAddress(line.Address) {
			return fmt.Errorf("its output %d does not match the address it is for", i+1)
		}
		if line.Receives < minimumOutputUnits() || line.Receives > line.Debit {
			return fmt.Errorf("it pays %s CVN against %s owed", FormatUnits(line.Receives), FormatUnits(line.Debit))
		}
		paid += line.Receives
		debited += line.Debit
	}
	if payment.ChangeUnits > 0 {
		change := payment.Outputs[len(payment.Outputs)-1]
		if change.Recipient != ownAddress || change.Amount.Units() != payment.ChangeUnits {
			return fmt.Errorf("its change does not come back to the pool's address")
		}
	}
	if debited != paid+payment.FeeUnits || spent != paid+payment.FeeUnits+payment.ChangeUnits {
		return fmt.Errorf("its figures do not add up: %s spent, %s paid, %s fee, %s change", FormatUnits(spent), FormatUnits(paid), FormatUnits(payment.FeeUnits), FormatUnits(payment.ChangeUnits))
	}
	return nil
}

// passwordFileTooOpen reports whether a file's permissions let anyone but
// its owner read it or write to it. The password file opens the pool's key,
// so the paying command will not use one that does. (Windows does not write
// a file's permissions this way, and nothing is checked there.)
func passwordFileTooOpen(mode os.FileMode) bool {
	return mode.Perm()&0o077 != 0
}

// RunPoolPay is `--pool-pay`: it signs the payments the pool's node on this
// machine has prepared and hands them back. It returns the code the program
// ends with.
func RunPoolPay(args []string) int {
	_, passwordFile := poolPayRequested(args)
	fmt.Println("====================================================================")
	fmt.Printf(" PAYING OUT FROM THE POOL ON %s\n", strings.ToUpper(Params.Name))
	fmt.Println("====================================================================")
	if Params.StratumPort <= 0 {
		fmt.Printf("❌ %s is not mined with RandomX, so there is no pool on it.\n", Params.Name)
		return 1
	}

	// The key first: nothing is prepared for a command that cannot sign.
	var typed string
	byHand := passwordFile == ""
	if byHand {
		typed = readSecretLine(bufio.NewReader(os.Stdin), " The pool's private key, its 24 recovery words, or the saved key's password (not shown while typing): ")
	} else {
		if info, err := os.Stat(passwordFile); err == nil && runtime.GOOS != "windows" && passwordFileTooOpen(info.Mode()) {
			fmt.Printf("❌ %s can be read by other users of this machine (its permissions are %04o).\n", passwordFile, info.Mode().Perm())
			fmt.Printf("   It opens the pool's key. Make it readable by its owner alone and try again:  chmod 600 %s\n", passwordFile)
			return 1
		}
		raw, err := os.ReadFile(passwordFile)
		if err != nil {
			fmt.Printf("❌ %s could not be read: %v\n", passwordFile, err)
			return 1
		}
		typed = strings.TrimSpace(string(raw))
	}
	privateKeyHex, publicKeyHex, err := signingKeyFrom(typed)
	if err != nil {
		fmt.Println("❌ " + err.Error())
		return 1
	}
	ownAddress, err := DeriveAddressFromPublicKey(publicKeyHex)
	if err != nil {
		fmt.Println("❌ " + err.Error())
		return 1
	}
	ownAddress = strings.TrimSpace(ownAddress)

	var prepared poolPrepareReply
	status, err := poolPayCall("/prepare", struct{}{}, &prepared)
	if err != nil {
		fmt.Printf("❌ The pool's node on this machine did not answer (%v).\n   It must be running, started with --pool, on the same network.\n", err)
		return 1
	}
	release := func() {
		var serials []uint64
		for _, payment := range prepared.Payments {
			serials = append(serials, payment.Serial)
		}
		if len(serials) > 0 {
			var ignored map[string]int
			_, _ = poolPayCall("/release", poolReleaseRequest{Serials: serials}, &ignored)
		}
	}
	if status != http.StatusOK || prepared.Error != "" {
		fmt.Printf("❌ The pool's node could not prepare payments: %s\n", prepared.Error)
		return 1
	}
	if prepared.PoolAddress != ownAddress {
		release()
		fmt.Printf("❌ This key belongs to %s.\n   The pool on this machine pays from %s. Nothing was signed.\n", ownAddress, prepared.PoolAddress)
		return 1
	}
	if len(prepared.Payments) == 0 {
		fmt.Println(" Nothing to pay: " + prepared.Reason + ".")
		return 0
	}
	var total int64
	addresses := 0
	for _, payment := range prepared.Payments {
		if err := poolPaymentLooksRight(payment, ownAddress, prepared.FeeUnits); err != nil {
			release()
			fmt.Printf("❌ Payment %d from the pool's node does not hang together (%v). Nothing was signed.\n", payment.Serial, err)
			return 1
		}
		total += payment.debits()
		addresses += len(payment.Lines)
	}
	fmt.Printf(" %s CVN is to be paid to %d address(es) in %d transfer(s).\n", FormatUnits(total), addresses, len(prepared.Payments))
	fmt.Printf(" Each transfer carries a fee of %s CVN, shared among the addresses it pays.\n", FormatUnits(prepared.FeeUnits))
	if prepared.Reason != "" {
		fmt.Println(" Not everything that is owed: " + prepared.Reason + ".")
	}
	if byHand {
		fmt.Print(" Type YES to sign and send them: ")
		answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if strings.TrimSpace(answer) != "YES" {
			release()
			fmt.Println(" Nothing was signed. The amounts are still owed.")
			return 0
		}
	}

	failed := 0
	for _, payment := range prepared.Payments {
		signed, err := signTransfer(privateKeyHex, publicKeyHex, ownAddress, payment.Inputs, payment.Outputs, payment.FeeUnits)
		if err == nil {
			var reply poolSignedReply
			var status int
			if status, err = poolPayCall("/signed", poolSignedRequest{Serial: payment.Serial, Tx: signed}, &reply); err == nil && (status != http.StatusOK || reply.Error != "") {
				err = fmt.Errorf("%s", reply.Error)
			}
		}
		if err != nil {
			failed++
			fmt.Printf(" ❌ Payment %d was not sent: %v\n", payment.Serial, err)
			continue
		}
		fmt.Printf(" ✅ Payment %d sent: %s CVN to %d address(es). Transfer %s\n", payment.Serial, FormatUnits(payment.debits()), len(payment.Lines), signed.ID)
	}
	if failed > 0 {
		fmt.Printf(" %d payment(s) were not sent. Their amounts are owed again, or will be within ten minutes.\n", failed)
		return 1
	}
	fmt.Println(" All sent. Each is settled once its transfer is too deep in the chain to be replaced; the pool page shows it.")
	return 0
}
