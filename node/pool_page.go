package node

import (
	"encoding/json"
	"html/template"
	"net/http"
	"sync"
	"time"
)

// THE POOL PAGE
//
// A pool shows its whole record to anyone who asks: how fast it is mining,
// every block it found and how that block's reward was divided, and what
// each address has done lately. Addresses and amounts are public on this
// chain in any case. Showing the division means a miner need not take the
// pool's word for it: the shares of a block can be added up by anyone.
//
// `/pool` is the page for a person and `/pool.json` the same figures for a
// program. Both are answered from a reading of the record that is taken at
// most every few seconds, however often they are asked for.

const (
	poolPageBlocks    = 50
	poolPageAddresses = 500
	poolPagePayments  = 30
	poolPageKeptFor   = 5 * time.Second
)

type poolPageBlock struct {
	Height      int64  `json:"height"`
	Hash        string `json:"hash"`
	At          int64  `json:"time"`
	Finder      string `json:"finder"`
	Reward      string `json:"reward"`
	RewardUnits int64  `json:"reward_units"`
	State       string `json:"state"`
	// For a block still waiting: the height of the chain at which it counts.
	CountsAtHeight int64          `json:"counts_at_height,omitempty"`
	Parts          []poolPagePart `json:"parts"`
}

type poolPagePart struct {
	Address string `json:"address"`
	Amount  string `json:"amount"`
	Units   int64  `json:"units"`
	Work    int64  `json:"work"`
}

type poolPageAddress struct {
	Address   string  `json:"address"`
	Work      int64   `json:"window_work"`
	Percent   float64 `json:"window_percent"`
	LastShare int64   `json:"last_share"`
	// Its parts of the blocks that count, what of that has been paid, and
	// what is owed, in CVN and in whole units.
	Earned       string `json:"earned"`
	Owed         string `json:"owed"`
	Pending      string `json:"pending"`
	Paid         string `json:"paid"`
	EarnedUnits  int64  `json:"earned_units"`
	OwedUnits    int64  `json:"owed_units"`
	PendingUnits int64  `json:"pending_units"`
	PaidUnits    int64  `json:"paid_units"`
}

// poolPagePayment is one payment as the page shows it.
type poolPagePayment struct {
	Serial        uint64 `json:"serial"`
	State         string `json:"state"`
	At            int64  `json:"time"`
	TxID          string `json:"tx_id,omitempty"`
	Addresses     int    `json:"addresses"`
	Amount        string `json:"amount"`
	Units         int64  `json:"units"`
	FeeUnits      int64  `json:"fee_units"`
	MinedAtHeight int64  `json:"mined_at_height,omitempty"`
	// What each address was debited and received, in whole units.
	Lines []poolPaymentLine `json:"lines"`
}

func pageAddress(address string, work int64, percent float64, lastShare int64, account poolAccount) poolPageAddress {
	return poolPageAddress{
		Address: address, Work: work, Percent: percent, LastShare: lastShare,
		Earned: FormatUnits(account.Earned), Owed: FormatUnits(account.Owed), Pending: FormatUnits(account.Pending), Paid: FormatUnits(account.Paid),
		EarnedUnits: account.Earned, OwedUnits: account.Owed, PendingUnits: account.Pending, PaidUnits: account.Paid,
	}
}

// poolPage is everything the pool page shows.
type poolPage struct {
	Network           string            `json:"network"`
	PoolAddress       string            `json:"pool_address"`
	FeePercent        int               `json:"fee_percent"`
	WindowBlocks      int64             `json:"window_blocks"`
	ShareSeconds      float64           `json:"share_seconds"`
	Height            int64             `json:"height"`
	NetworkDifficulty int               `json:"network_difficulty"`
	Miners            int               `json:"miners_connected"`
	Speed             float64           `json:"speed_hashes_per_second"`
	SpeedText         string            `json:"speed"`
	BlocksFound       int               `json:"blocks_found"`
	BlocksWaiting     int               `json:"blocks_waiting"`
	BlocksConfirmed   int               `json:"blocks_confirmed"`
	BlocksLost        int               `json:"blocks_lost"`
	ConfirmBlocks     int64             `json:"confirm_blocks"`
	Owed              string            `json:"owed"`
	OwedUnits         int64             `json:"owed_units"`
	EarnedUnits       int64             `json:"earned_units"`
	PendingUnits      int64             `json:"pending_units"`
	Pending           string            `json:"pending"`
	PaidUnits         int64             `json:"paid_units"`
	Paid              string            `json:"paid"`
	MinimumPayment    string            `json:"minimum_payment"`
	PaymentFee        string            `json:"payment_fee"`
	PaymentsSettled   int               `json:"payments_settled"`
	Payments          []poolPagePayment `json:"payments"`
	Held              string            `json:"held"`
	HeldUnits         int64             `json:"held_units"`
	Blocks            []poolPageBlock   `json:"blocks"`
	WindowWork        int64             `json:"window_work"`
	Addresses         []poolPageAddress `json:"addresses"`
	AddressesNotShown int               `json:"addresses_not_shown"`
	At                int64             `json:"time"`
}

var poolPageKept struct {
	sync.Mutex
	page poolPage
	at   time.Time
}

// currentPoolPage reads the pool's record for the page, or returns the
// reading taken a moment ago.
func currentPoolPage() poolPage {
	poolPageKept.Lock()
	defer poolPageKept.Unlock()
	if time.Since(poolPageKept.at) < poolPageKeptFor && poolPageKept.page.PoolAddress == poolAddress {
		return poolPageKept.page
	}
	poolPageKept.page, poolPageKept.at = readPoolPage(time.Now()), time.Now()
	return poolPageKept.page
}

func readPoolPage(now time.Time) poolPage {
	StateMutex.RLock()
	tip := GetLatestBlockInternal()
	StateMutex.RUnlock()
	difficulty := tip.Difficulty
	if tip.Hash != "" {
		difficulty = CalculateNextDifficulty(tip, minerBlockDate(tip, now.Unix()))
	}
	view := poolLedger.view(difficulty, now.Unix(), poolPageBlocks)
	_, miners := poolSpeedNow()
	page := poolPage{
		Network: Params.Name, PoolAddress: poolAddress, FeePercent: 0,
		WindowBlocks: poolWindowBlocks, ShareSeconds: poolShareSeconds,
		Height: tip.Index, NetworkDifficulty: difficulty, Miners: miners,
		Speed: view.Speed, SpeedText: formatHashrate(view.Speed / 1000000.0),
		BlocksFound: view.BlocksFound, WindowWork: view.WindowWork, At: now.Unix(),
		BlocksWaiting: view.Waiting, BlocksConfirmed: view.Confirmed, BlocksLost: view.Lost,
		ConfirmBlocks: poolConfirmDepth(),
		Owed:          FormatUnits(view.Owed), OwedUnits: view.Owed, EarnedUnits: view.Earned, PaidUnits: view.Paid,
		Paid: FormatUnits(view.Paid), PendingUnits: view.Pending, Pending: FormatUnits(view.Pending),
		MinimumPayment: FormatUnits(poolMinimumPaymentUnits), PaymentFee: FormatUnits(poolPaymentFeeUnits),
		PaymentsSettled: view.PaymentsSettled,
	}
	for _, payment := range poolLedger.recentPayments(poolPagePayments) {
		at := payment.PreparedAt
		if payment.SentAt > 0 {
			at = payment.SentAt
		}
		page.Payments = append(page.Payments, poolPagePayment{
			Serial: payment.Serial, State: payment.State, At: at, TxID: payment.TxID, Addresses: len(payment.Lines),
			Amount: FormatUnits(payment.debits()), Units: payment.debits(), FeeUnits: payment.FeeUnits,
			MinedAtHeight: payment.MinedAtHeight, Lines: payment.Lines,
		})
	}
	// What the pool's address holds on the chain: the rewards of its blocks,
	// less what has been paid out.
	BalanceCacheMutex.RLock()
	page.HeldUnits = StateBalanceCache[poolAddress]
	BalanceCacheMutex.RUnlock()
	page.Held = FormatUnits(page.HeldUnits)
	for _, block := range view.Blocks {
		shown := poolPageBlock{
			Height: block.Height, Hash: block.Hash, At: block.At, Finder: block.Finder,
			Reward: FormatUnits(block.RewardUnits), RewardUnits: block.RewardUnits, State: block.State,
		}
		if block.State == poolBlockWaiting {
			shown.CountsAtHeight = block.Height + page.ConfirmBlocks
		}
		for _, part := range block.Parts {
			shown.Parts = append(shown.Parts, poolPagePart{Address: part.Address, Amount: FormatUnits(part.Units), Units: part.Units, Work: part.Work})
		}
		page.Blocks = append(page.Blocks, shown)
	}
	for i, address := range view.Addresses {
		if i >= poolPageAddresses {
			page.AddressesNotShown = len(view.Addresses) - i
			break
		}
		percent := 0.0
		if view.WindowWork > 0 {
			percent = float64(address.Work) * 100 / float64(view.WindowWork)
		}
		page.Addresses = append(page.Addresses, pageAddress(address.Address, address.Work, percent, address.LastShare, address.Account))
	}
	return page
}

// addPoolPages adds the pool's pages to what a node answers. A node that is
// not a pool says so.
func addPoolPages(pages *http.ServeMux) {
	pages.HandleFunc("/pool.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")
		if !poolRunning() || poolLedger == nil {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "this node is not a pool"})
			return
		}
		// `?address=CVN_...` answers for one address, whether or not it is
		// among those the page has room to list.
		if address := r.URL.Query().Get("address"); address != "" {
			if !looksLikeWalletAddress(address) {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{"error": "that is not a CVN wallet address"})
				return
			}
			page := currentPoolPage()
			account, work := poolLedger.account(address, page.NetworkDifficulty)
			percent := 0.0
			if page.WindowWork > 0 {
				percent = float64(work) * 100 / float64(page.WindowWork)
			}
			json.NewEncoder(w).Encode(pageAddress(address, work, percent, 0, account))
			return
		}
		json.NewEncoder(w).Encode(currentPoolPage())
	})
	pages.HandleFunc("/pool", func(w http.ResponseWriter, r *http.Request) {
		if !poolRunning() || poolLedger == nil {
			http.Error(w, "This node is not a pool.", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// The page carries no script and loads nothing from anywhere else.
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_ = poolPageTemplate.Execute(w, currentPoolPage())
	})
}

var poolPageTemplate = template.Must(template.New("pool").Funcs(template.FuncMap{
	"when": func(at int64) string {
		if at <= 0 {
			return ""
		}
		return time.Unix(at, 0).UTC().Format("2 Jan 15:04") + " UTC"
	},
	"short": func(text string) string {
		if len(text) > 16 {
			return text[:16] + "…"
		}
		return text
	},
}).Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta http-equiv="refresh" content="30">
<meta name="referrer" content="no-referrer">
<title>CVN pool ({{.Network}})</title>
<style>
body { font-family: system-ui, sans-serif; margin: 0; background: #f6f5f1; color: #1c1b19; line-height: 1.5; }
main { max-width: 960px; margin: 0 auto; padding: 16px; }
h1 { font-size: 1.5rem; margin: 8px 0; }
h2 { font-size: 1.15rem; margin: 28px 0 8px; }
p { margin: 8px 0; }
.quiet { color: #5d5a52; font-size: 0.92rem; }
.scroll { overflow-x: auto; }
table { border-collapse: collapse; width: 100%; background: #fff; }
th, td { text-align: left; padding: 6px 10px; border-bottom: 1px solid #e3e0d8; white-space: nowrap; }
th { font-weight: 600; }
td.n, th.n { text-align: right; font-variant-numeric: tabular-nums; }
code { font-family: ui-monospace, Consolas, monospace; font-size: 0.9rem; word-break: break-all; }
details { margin: 0; }
summary { cursor: pointer; }
@media (prefers-color-scheme: dark) {
  body { background: #171614; color: #ebe8e0; }
  table { background: #201f1c; }
  th, td { border-color: #36342f; }
  .quiet { color: #a6a299; }
}
</style>
</head>
<body>
<main>
<h1>CVN pool</h1>
<p>Network: <b>{{.Network}}</b>. This pool takes no fee. A block's reward is divided among the most recent shares, counted back over the work of {{.WindowBlocks}} blocks.</p>
<p class="quiet">Blocks found by this pool pay <code>{{.PoolAddress}}</code>. A block counts once {{.ConfirmBlocks}} more blocks stand on it; from then on each address is owed its part. A block that another block has replaced by then counts for nothing. What is owed is paid by ordinary transfers from that address, to every address owed {{.MinimumPayment}} CVN or more; each transfer carries a fee of {{.PaymentFee}} CVN, shared among the addresses it pays.</p>

<div class="scroll">
<table>
<tr><th>Pool speed</th><td>{{.SpeedText}}</td></tr>
<tr><th>Miners connected</th><td>{{.Miners}}</td></tr>
<tr><th>Blocks found</th><td>{{.BlocksFound}} ({{.BlocksConfirmed}} count, {{.BlocksWaiting}} waiting, {{.BlocksLost}} lost)</td></tr>
<tr><th>Owed to miners</th><td>{{.Owed}} CVN</td></tr>
<tr><th>In payments on their way</th><td>{{.Pending}} CVN</td></tr>
<tr><th>Paid so far</th><td>{{.Paid}} CVN in {{.PaymentsSettled}} payment(s)</td></tr>
<tr><th>Held at the pool's address</th><td>{{.Held}} CVN</td></tr>
<tr><th>Chain height</th><td>{{.Height}}</td></tr>
<tr><th>Network difficulty</th><td>{{.NetworkDifficulty}}</td></tr>
</table>
</div>

<h2>To mine here</h2>
<p>Run the CVN edition of XMRig with this pool's address and port, and your own CVN address as the user name:</p>
<p><code>xmrig -o THIS-POOL:PORT -u CVN_YOUR_ADDRESS --keepalive</code></p>

<h2>Miners</h2>
<p class="quiet">Work is what each address has in the window a block found now would be divided over. Owed is its part of the blocks that count, not yet paid. On its way is in a payment that has been sent and is not yet too deep to be replaced.</p>
{{if .Addresses}}
<div class="scroll">
<table>
<tr><th>Address</th><th class="n">Work</th><th class="n">Part</th><th class="n">Owed</th><th class="n">On its way</th><th class="n">Paid</th><th>Last share</th></tr>
{{range .Addresses}}<tr><td><code>{{.Address}}</code></td><td class="n">{{.Work}}</td><td class="n">{{printf "%.2f" .Percent}}%</td><td class="n">{{.Owed}}</td><td class="n">{{.Pending}}</td><td class="n">{{.Paid}}</td><td>{{when .LastShare}}</td></tr>
{{end}}
</table>
</div>
{{if .AddressesNotShown}}<p class="quiet">{{.AddressesNotShown}} more addresses are in <a href="pool.json">pool.json</a>.</p>{{end}}
{{else}}
<p class="quiet">No shares yet.</p>
{{end}}

<h2>Blocks found</h2>
{{if .Blocks}}
<div class="scroll">
<table>
<tr><th class="n">Height</th><th>Block</th><th>Found</th><th class="n">Reward</th><th>State</th><th>Divided among</th></tr>
{{range .Blocks}}<tr><td class="n">{{.Height}}</td><td><code>{{short .Hash}}</code></td><td>{{when .At}}</td><td class="n">{{.Reward}}</td><td>{{.State}}{{if .CountsAtHeight}} until #{{.CountsAtHeight}}{{end}}</td>
<td><details><summary>{{len .Parts}} address(es)</summary>{{range .Parts}}<div><code>{{.Address}}</code> {{.Amount}} CVN</div>{{end}}</details></td></tr>
{{end}}
</table>
</div>
{{else}}
<p class="quiet">None yet.</p>
{{end}}

<h2>Payments</h2>
{{if .Payments}}
<div class="scroll">
<table>
<tr><th class="n">Number</th><th>When</th><th>State</th><th class="n">Amount</th><th class="n">Addresses</th><th>Transfer</th></tr>
{{range .Payments}}<tr><td class="n">{{.Serial}}</td><td>{{when .At}}</td><td>{{.State}}{{if .MinedAtHeight}} (block #{{.MinedAtHeight}}){{end}}</td><td class="n">{{.Amount}}</td><td class="n">{{.Addresses}}</td><td><code>{{short .TxID}}</code></td></tr>
{{end}}
</table>
</div>
{{else}}
<p class="quiet">None yet.</p>
{{end}}

<p class="quiet">The same figures for a program: <a href="pool.json">pool.json</a>. This page reloads every 30 seconds.</p>
</main>
</body>
</html>
`))
