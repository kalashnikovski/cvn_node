package node

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"strconv"
)

// DECODING WHAT STRANGERS SEND
//
// A byte limit is not a memory limit. In JSON an empty list item costs two or
// three bytes ("{}," or "0,"), but decoded into a transfer, an input or an
// output it costs well over a hundred, and the list holding it grows by
// doubling. One 2 MB block message of empty transfers decoded to 699,037
// transactions and held 369 MB, on seeds that run with a memory cap of
// 800 MB (found 25 November). So before anything a stranger sends is
// decoded, the items in its lists are counted, and anything with more than
// a valid one can hold is refused unread.
//
// None of this decides what is valid. Every limit here is at or above what
// the rules already allow, so no valid block or transfer is refused; only
// what could never be valid is refused sooner.

// jsonListItems counts the items in every list (JSON array) in data, at any
// depth. It reads data once and allocates only for nesting. It assumes data
// is valid JSON: on anything else its count may be wrong, but json.Unmarshal
// checks the whole text before it allocates anything, so invalid data is
// refused either way.
func jsonListItems(data []byte) int {
	const (
		inObject = 0
		inList   = 1
	)
	var nesting []byte
	count := 0
	expectItem := false // just after "[" or after "," in a list
	inString, escaped := false, false
	for _, c := range data {
		if inString {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			continue
		}
		if expectItem && c != ']' {
			count++
		}
		expectItem = false
		switch c {
		case '"':
			inString = true
		case '[':
			nesting = append(nesting, inList)
			expectItem = true
		case '{':
			nesting = append(nesting, inObject)
		case ']', '}':
			if len(nesting) > 0 {
				nesting = nesting[:len(nesting)-1]
			}
		case ',':
			if len(nesting) > 0 && nesting[len(nesting)-1] == inList {
				expectItem = true
			}
		}
	}
	return count
}

// minWireBytesPerListItem: as nodes write them, the transfers, inputs and
// outputs of valid blocks take about 79 bytes of JSON each at the very least
// (an output, to an address of 52 characters; an input spending a coin from
// a reward with the shortest identifier the rules allow takes 84; a
// transfer, hundreds). 48 leaves a margin. Data with more items than its
// length allows at this rate is made of empty or near-empty items, which
// no valid block has. Within it, a stranger's item costs at least 48 bytes
// to send and at most about 176 to hold once decoded.
const minWireBytesPerListItem = 48

// maxBlockListItems is the most list items a valid block can hold, or -1 on
// a network with no block size limit. Every transfer, input and output adds
// at least 7 bytes to blockSizeBytes (an output's two length-prefixed fields
// can be no shorter; a transfer or an input adds more), and a valid block is
// at most MaxBlockBytes.
func maxBlockListItems() int {
	if Params.MaxBlockBytes <= 0 {
		return -1
	}
	return Params.MaxBlockBytes/7 + 64
}

// maxTransferListItems is the most list items a valid transfer can hold: its
// inputs and its outputs.
const maxTransferListItems = TxMaxInputs + TxMaxOutputs

// tooDenseForItsLength reports whether data has more list items than any
// valid block or transfer of its length could have.
func tooDenseForItsLength(items, length int) bool {
	return items > length/minWireBytesPerListItem+16
}

var errTooManyListItems = errors.New("more list items than a valid one can hold")

// largeBlockDecodes limits how many large blocks from other nodes are
// decoded at once. Within the limits above a block can still take some
// megabytes to decode; a few at a time keeps that bounded however many
// connections send one together.
var largeBlockDecodes = make(chan struct{}, 4)

const largeBlockMessageBytes = 256 << 10

// decodeBlockFromPeer decodes a block another node sent, refusing it unread
// if its lists hold more than a valid block can.
func decodeBlockFromPeer(data []byte, into *Block) error {
	items := jsonListItems(data)
	if most := maxBlockListItems(); (most >= 0 && items > most) || tooDenseForItsLength(items, len(data)) {
		return errTooManyListItems
	}
	if len(data) > largeBlockMessageBytes {
		largeBlockDecodes <- struct{}{}
		defer func() { <-largeBlockDecodes }()
	}
	return json.Unmarshal(data, into)
}

// decodeTransferFromPeer decodes a transfer another node or a wallet sent,
// refusing it unread if it has more inputs and outputs than a valid one can.
func decodeTransferFromPeer(data []byte, into *Transaction) error {
	items := jsonListItems(data)
	if items > maxTransferListItems || tooDenseForItsLength(items, len(data)) {
		return errTooManyListItems
	}
	return json.Unmarshal(data, into)
}

// decodeBlockBatchFromPeer decodes a reply of blocks to a sync request.
func decodeBlockBatchFromPeer(data []byte, into *[]Block) error {
	if tooDenseForItsLength(jsonListItems(data), len(data)) {
		return errTooManyListItems
	}
	return json.Unmarshal(data, into)
}

// peerListItemsAtMost is the longest list of peers read from another node. A
// node lists the peers it has heard from, at most MaxPeerMapCapacity.
const peerListItemsAtMost = 2 * MaxPeerMapCapacity

// peerListDialsAtMost is how many nodes newly named in one peer list are
// greeted. Lists come from any node this one asks, and a node that answered
// with thousands of addresses would otherwise have this one dial them all,
// every fifteen seconds, at whoever those addresses belong to.
const peerListDialsAtMost = 16

// ANSWERS FROM A NODE
//
// A wallet asks nodes over plain HTTP: the node on this machine first, then
// the public seeds. An answer was decoded however long it was, so a seed
// that had been taken over, or anyone able to stand between the wallet and
// a seed, could answer without end and have the wallet read until the
// computer had no memory left (security audit, November 2026, finding 2).
// No key is at risk in that, since none is ever sent; the wallet stops.
//
// Every answer is now read up to a limit and no further, and an answer
// with more list items than any real one has is refused before it is
// decoded: a length limit alone is not a memory limit, for the reason given
// at the top of this file.
const (
	// nodeAnswerLimitBytes is the most read of an ordinary answer: the
	// newest block, a balance, a status. The largest of these is a full
	// block, well under a megabyte.
	nodeAnswerLimitBytes = 4 << 20
	// coinListLimitBytes is the most read of a list of one address's coins,
	// which has no fixed length. It is room for some 200,000 coins.
	coinListLimitBytes = 32 << 20
	// answerListItemsAtMost is the most list items an answer may hold.
	answerListItemsAtMost = 250000
)

// errAnswerTooLarge is returned for an answer longer than its limit.
var errAnswerTooLarge = errors.New("the answer is longer than a node's answer can be")

// decodeAnswerWithin decodes the JSON a node answered with, reading no more
// than `limit` bytes of it.
func decodeAnswerWithin(body io.Reader, limit int64, into interface{}) error {
	raw, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return err
	}
	if int64(len(raw)) > limit || jsonListItems(raw) > answerListItemsAtMost {
		return errAnswerTooLarge
	}
	return json.Unmarshal(raw, into)
}

// decodePeerList decodes a peer list another node sent, refusing a list
// longer than any node sends.
func decodePeerList(data []byte) ([]string, error) {
	if jsonListItems(data) > peerListItemsAtMost {
		return nil, errTooManyListItems
	}
	var listed []string
	if err := json.Unmarshal(data, &listed); err != nil {
		return nil, err
	}
	return listed, nil
}

// looksLikePeerAddress reports whether text is a host and a port number, the
// only form a peer address takes.
func looksLikePeerAddress(text string) bool {
	host, port, err := net.SplitHostPort(text)
	if err != nil || host == "" || len(text) > 255 {
		return false
	}
	number, err := strconv.Atoi(port)
	return err == nil && number > 0 && number < 65536
}
