package astits

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/asticode/go-astikit"
	"github.com/stretchr/testify/assert"
)

// referenceIsPESComplete is the original implementation: merge every payload,
// parse the header, compare the length. isPESComplete must agree with it.
func referenceIsPESComplete(ps []*Packet) bool {
	var l int
	for _, p := range ps {
		l += len(p.Payload)
	}
	payload := make([]byte, 0, l)
	for _, p := range ps {
		payload = append(payload, p.Payload...)
	}
	i := astikit.NewBytesIterator(payload)
	i.Seek(3)
	h, _, dataEnd, err := parsePESHeader(i)
	if err != nil || h.PacketLength == 0 {
		return false
	}
	return i.Len() >= dataEnd
}

// pesBytes builds a PES with an optional header (PTS only) and n data bytes.
// packetLength 0 marks it unbounded, as video PES usually are.
func pesBytes(n int, bounded bool) []byte {
	hdr := []byte{0x00, 0x00, 0x01, 0xe0, 0x00, 0x00, 0x80, 0x80, 0x05, 0x21, 0x00, 0x01, 0x00, 0x01}
	if bounded {
		pl := len(hdr) - 6 + n
		hdr[4], hdr[5] = byte(pl>>8), byte(pl)
	}
	b := append([]byte(nil), hdr...)
	for k := 0; k < n; k++ {
		b = append(b, byte(k))
	}
	return b
}

// splitPayloads cuts b into packets with random payload sizes (1..184 bytes).
func splitPayloads(r *rand.Rand, b []byte) []*Packet {
	var ps []*Packet
	for len(b) > 0 {
		n := 1 + r.Intn(184)
		if n > len(b) {
			n = len(b)
		}
		ps = append(ps, &Packet{Payload: b[:n]})
		b = b[n:]
	}
	return ps
}

func TestIsPESCompleteMatchesReference(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	for iter := 0; iter < 2000; iter++ {
		n := r.Intn(3000)
		b := pesBytes(n, r.Intn(2) == 0)
		switch r.Intn(4) {
		case 0:
			b = b[:r.Intn(len(b)+1)] // truncated: header or data missing
		case 1:
			b[7] = 0xff // malformed optional header flags
		}
		ps := splitPayloads(r, b)
		// Check every prefix, as the accumulator does packet by packet.
		for k := 1; k <= len(ps); k++ {
			want := referenceIsPESComplete(ps[:k])
			if got := isPESComplete(ps[:k]); got != want {
				t.Fatalf("iter %d prefix %d/%d (%d bytes): got %v want %v", iter, k, len(ps), len(b), got, want)
			}
		}
	}
}

func TestIsPESCompleteCases(t *testing.T) {
	one := func(b []byte) []*Packet { return []*Packet{{Payload: b}} }
	full := pesBytes(100, true)
	assert.False(t, isPESComplete(one(full[:5])), "shorter than the length field")
	assert.False(t, isPESComplete(one(pesBytes(100, false))), "unbounded PES")
	assert.False(t, isPESComplete(one(full[:len(full)-1])), "one byte short")
	assert.True(t, isPESComplete(one(full)), "exactly complete")
	assert.True(t, isPESComplete([]*Packet{{Payload: full[:3]}, {Payload: full[3:5]}, {Payload: full[5:]}}), "length field split across packets")
}

// BenchmarkIsPESCompleteAccumulate replays the accumulator's per-packet calls
// for one unbounded PES; the cost must grow linearly with the packet count.
func BenchmarkIsPESCompleteAccumulate(b *testing.B) {
	for _, n := range []int{100, 1000} {
		ps := splitPayloads(rand.New(rand.NewSource(1)), pesBytes(n*184, false))
		for k := range ps {
			ps[k].Payload = append([]byte(nil), ps[k].Payload...)
		}
		b.Run(fmt.Sprintf("packets=%d", len(ps)), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				for k := 1; k <= len(ps); k++ {
					isPESComplete(ps[:k])
				}
			}
		})
	}
}
