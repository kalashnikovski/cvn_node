package node

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// A wallet reads what a node answers up to a limit and no further
// (decodeAnswerWithin, in decode_limits.go).

// endlessReader gives the letter a for ever and counts what was read.
type endlessReader struct{ read int64 }

func (r *endlessReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'a'
	}
	r.read += int64(len(p))
	return len(p), nil
}

func TestAnAnswerIsDecodedUpToItsLimitAndNoFurther(t *testing.T) {
	const limit = 100
	// A JSON string of exactly `limit` bytes, quotes included.
	exact := `"` + strings.Repeat("a", limit-2) + `"`
	var text string
	if err := decodeAnswerWithin(strings.NewReader(exact), limit, &text); err != nil || len(text) != limit-2 {
		t.Fatalf("an answer of exactly the limit gave %d characters and %v; want it decoded", len(text), err)
	}
	over := `"` + strings.Repeat("a", limit-1) + `"`
	if err := decodeAnswerWithin(strings.NewReader(over), limit, &text); !errors.Is(err, errAnswerTooLarge) {
		t.Errorf("an answer one byte over the limit gave %v; want errAnswerTooLarge", err)
	}
	endless := &endlessReader{}
	if err := decodeAnswerWithin(endless, limit, &text); !errors.Is(err, errAnswerTooLarge) {
		t.Errorf("an answer without an end gave %v; want errAnswerTooLarge", err)
	}
	if endless.read > limit+1 {
		t.Errorf("%d bytes were read of an answer without an end, with a limit of %d; want no more than %d", endless.read, limit, limit+1)
	}
	err := decodeAnswerWithin(strings.NewReader(`{"a": [1, 2`), limit, &text)
	if err == nil || errors.Is(err, errAnswerTooLarge) {
		t.Errorf("an answer of malformed JSON gave %v; want a decoding error that is not errAnswerTooLarge", err)
	}
}

// pointTheWalletAt makes the server the only node a wallet can ask: not the
// node that may run on this machine, and not a real seed.
func pointTheWalletAt(t *testing.T, server *httptest.Server) {
	t.Helper()
	onNetwork(t, "devnet")
	_, port, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if Params.SyncPort, err = strconv.Atoi(port); err != nil {
		t.Fatal(err)
	}
	Params.SyncFallbackPort = Params.SyncPort
	Params.SeedHosts = nil
}

// streamLongerThan writes the start of a valid JSON value, then the filler
// over and over until more than `limit` bytes have gone, then the end. It
// stops as soon as the reader has gone away.
func streamLongerThan(w http.ResponseWriter, limit int, start, filler, end string) {
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	written := 0
	chunk := strings.Repeat(filler, 4096/len(filler))
	if n, err := w.Write([]byte(start)); err != nil {
		return
	} else {
		written += n
	}
	for written <= limit+len(chunk) {
		n, err := w.Write([]byte(chunk))
		if err != nil {
			return
		}
		written += n
		if flusher != nil {
			flusher.Flush()
		}
	}
	_, _ = w.Write([]byte(end))
}

func TestAWalletReadsNoMoreOfANodesCoinListThanItsLimit(t *testing.T) {
	long := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A valid list of coins that is longer than the wallet reads.
		streamLongerThan(w, coinListLimitBytes, "[", `{"tx_id":"TX_a","output_index":0},`, `{"tx_id":"TX_z","output_index":1}]`)
	}))
	defer long.Close()
	pointTheWalletAt(t, long)
	coins, node, err := FetchSpendableCoinsOverHTTP(testAddress("anyone"))
	if err == nil || !strings.Contains(err.Error(), "longer than the wallet reads") {
		t.Fatalf("a coin list longer than %d bytes gave %v; want an error saying it is longer than the wallet reads", coinListLimitBytes, err)
	}
	if len(coins) != 0 || node != "" {
		t.Errorf("%d coins came back from a list that was too long, from %q; want none", len(coins), node)
	}

	short := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"tx_id":"TX_a","output_index":0},{"tx_id":"TX_b","output_index":2}]`))
	}))
	defer short.Close()
	pointTheWalletAt(t, short)
	coins, _, err = FetchSpendableCoinsOverHTTP(testAddress("anyone"))
	if err != nil || len(coins) != 2 || coins[1].TxID != "TX_b" || coins[1].Index != 2 {
		t.Fatalf("a short list gave %v and %v; want the two coins", coins, err)
	}
}

func TestAWalletReadsNoMoreOfANodesOrdinaryAnswerThanItsLimit(t *testing.T) {
	long := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		streamLongerThan(w, nodeAnswerLimitBytes, `{"current_height":5,"x":"`, strings.Repeat("a", 64), `"}`)
	}))
	defer long.Close()
	pointTheWalletAt(t, long)
	if data, ok := walletGetJSON("/block"); ok {
		t.Fatalf("an answer longer than %d bytes was read: %d fields", nodeAnswerLimitBytes, len(data))
	}

	short := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"current_height":5}`))
	}))
	defer short.Close()
	pointTheWalletAt(t, short)
	if data, ok := walletGetJSON("/block"); !ok || data["current_height"] != float64(5) {
		t.Errorf("a short answer gave %v, %v; want it read", data, ok)
	}
}

// A length limit is not a memory limit: a list of 300,000 empty objects is
// under a megabyte and takes far more than that to decode.
func TestAnAnswerWithMoreListItemsThanAnyRealOneHasIsRefused(t *testing.T) {
	items := func(n int) string { return strings.Repeat("{},", n-1) + "{}" }
	const limit = nodeAnswerLimitBytes

	long := "[" + items(300000) + "]"
	if len(long) >= limit {
		t.Fatalf("set-up: the answer is %d bytes, want it under the limit of %d", len(long), limit)
	}
	var list []interface{}
	if err := decodeAnswerWithin(strings.NewReader(long), limit, &list); !errors.Is(err, errAnswerTooLarge) || len(list) != 0 {
		t.Errorf("a list of 300000 items gave %d items and %v; want errAnswerTooLarge and nothing decoded", len(list), err)
	}
	short := "[" + items(1000) + "]"
	list = nil
	if err := decodeAnswerWithin(strings.NewReader(short), limit, &list); err != nil || len(list) != 1000 {
		t.Errorf("a list of 1000 items gave %d items and %v; want it decoded", len(list), err)
	}

	// The same list inside an object.
	var object map[string]interface{}
	if err := decodeAnswerWithin(strings.NewReader(`{"a":`+long+`}`), limit, &object); !errors.Is(err, errAnswerTooLarge) || len(object) != 0 {
		t.Errorf("an object holding 300000 items gave %d fields and %v; want errAnswerTooLarge and nothing decoded", len(object), err)
	}
	object = nil
	if err := decodeAnswerWithin(strings.NewReader(`{"a":`+short+`}`), limit, &object); err != nil || len(object) != 1 {
		t.Errorf("an object holding 1000 items gave %d fields and %v; want it decoded", len(object), err)
	}
}

// A node that has not updated answers with an error and a page of text. That
// is a node that does not offer the lookup, whatever the length of the text.
func TestANodeThatRefusesWithALongAnswerIsNotTakenForALongCoinList(t *testing.T) {
	refusing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		chunk := []byte(strings.Repeat("a", 4096))
		for written := 0; written <= coinListLimitBytes+len(chunk); written += len(chunk) {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	}))
	defer refusing.Close()
	pointTheWalletAt(t, refusing)
	coins, node, err := FetchSpendableCoinsOverHTTP(testAddress("anyone"))
	if err == nil || !strings.Contains(err.Error(), "no updated node answered") {
		t.Fatalf("a node refusing with a long answer gave %v; want the ordinary error saying no updated node answered", err)
	}
	if strings.Contains(err.Error(), "longer than the wallet reads") {
		t.Errorf("a node refusing with a long answer was taken for a long coin list: %v", err)
	}
	if len(coins) != 0 || node != "" {
		t.Errorf("%d coins came back from a node that refused, from %q; want none", len(coins), node)
	}
}
