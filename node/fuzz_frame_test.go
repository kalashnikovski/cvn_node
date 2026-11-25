package node

import (
	"bytes"
	"encoding/binary"
	"errors"
	"net"
	"testing"
)

// readFrameFrom hands data to readFrame as if a peer had sent it and then
// closed the connection.
func readFrameFrom(data []byte) (payload, signature []byte, err error) {
	near, far := net.Pipe()
	go func() {
		_, _ = near.Write(data)
		near.Close()
	}()
	// Closing this end also frees the writer when readFrame stops early.
	defer far.Close()
	return readFrame(far)
}

// FuzzAFrameFromAnyPeerIsReadSafely gives readFrame, the first thing every
// message from another node meets, whatever bytes the fuzzer can think of.
// It must never panic, must refuse a stated length over the limit before
// reading further, must refuse a frame cut short, and must hand back exactly
// the bytes that were sent. Anything writeFrame writes must read back
// unchanged and with a good signature.
func FuzzAFrameFromAnyPeerIsReadSafely(f *testing.F) {
	saved := Params
	if err := SelectNetwork("devnet"); err != nil {
		f.Fatal(err)
	}
	f.Cleanup(func() { backgroundWork.Wait(); Params = saved; applyParams() })

	var valid bytes.Buffer
	writeFrame(&valid, `BLOCK_ANNOUNCE:{"height":7,"hash":"00"}`)
	f.Add(valid.Bytes())
	f.Add([]byte{})                                    // nothing at all
	f.Add([]byte{0, 0, 0, 5, 'h'})                     // shorter than its length says
	f.Add([]byte("GET / HTTP/1.1\r\nHost: x\r\n\r\n")) // a web request
	f.Add([]byte{0x16, 0x03, 0x01, 0x02, 0x00})        // a TLS hello
	over := make([]byte, 4)
	binary.BigEndian.PutUint32(over, MaxPayloadFrameSize+1)
	f.Add(over) // one byte over the limit
	atLimit := make([]byte, 36)
	binary.BigEndian.PutUint32(atLimit, MaxPayloadFrameSize)
	f.Add(atLimit) // exactly at the limit, then nothing

	f.Fuzz(func(t *testing.T, data []byte) {
		payload, signature, err := readFrameFrom(data)

		switch {
		case len(data) < 4:
			if err == nil {
				t.Fatalf("%d bytes were read as a whole frame", len(data))
			}
		default:
			stated := binary.BigEndian.Uint32(data[:4])
			var tooLarge frameTooLarge
			switch {
			case stated > MaxPayloadFrameSize:
				if !errors.As(err, &tooLarge) || tooLarge.length != stated {
					t.Fatalf("a stated length of %d, over the limit, gave %v", stated, err)
				}
				_ = tooLarge.looksLike() // logged for every such sender, so it must not panic either
			case len(data) < 36+int(stated):
				if err == nil {
					t.Fatalf("a frame stating %d bytes was accepted from %d", stated, len(data))
				}
			default:
				if err != nil {
					t.Fatalf("a complete frame of %d bytes was refused: %v", stated, err)
				}
				if !bytes.Equal(signature, data[4:36]) || !bytes.Equal(payload, data[36:36+int(stated)]) {
					t.Fatal("the frame read back is not the bytes that were sent")
				}
			}
		}

		// The same bytes, sent properly as a message, come back unchanged.
		if len(data) > MaxPayloadFrameSize {
			return
		}
		var sent bytes.Buffer
		writeFrame(&sent, string(data))
		payload, signature, err = readFrameFrom(sent.Bytes())
		if err != nil {
			t.Fatalf("a frame writeFrame made was refused: %v", err)
		}
		if !bytes.Equal(payload, data) {
			t.Fatal("a message did not read back as it was written")
		}
		if !VerifyPayloadHMAC(payload, signature) {
			t.Fatal("a message this node signed failed its own signature check")
		}
	})
}
