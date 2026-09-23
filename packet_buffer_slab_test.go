package astits

import (
	"bytes"
	"context"
	"math/rand"
	"testing"

	"github.com/asticode/go-astikit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// slabTestStream builds n packets of the given size with varied PIDs,
// adaptation fields and payload bytes; the 188-byte packet is at the end of
// each, after a junk prefix that starts with the sync byte astits checks.
func slabTestStream(r *rand.Rand, n, packetSize int) []byte {
	var out []byte
	for k := 0; k < n; k++ {
		pkt := make([]byte, packetSize)
		r.Read(pkt[:packetSize-MpegTsPacketSize])
		pkt[0] = syncByte
		ts := pkt[packetSize-MpegTsPacketSize:]
		ts[0] = syncByte
		pid := uint16(0x100 + r.Intn(4))
		ts[1] = byte(pid>>8) & 0x1f
		if k%7 == 0 {
			ts[1] |= 0x40 // PUSI
		}
		ts[2] = byte(pid)
		cc := byte(k & 0xf)
		off := 4
		if r.Intn(3) == 0 {
			ts[3] = 0x30 | cc // adaptation field + payload
			afLen := r.Intn(40)
			ts[4] = byte(afLen)
			if afLen > 0 {
				ts[5] = 0 // no flags
				for j := 6; j < 5+afLen; j++ {
					ts[j] = 0xff
				}
			}
			off = 5 + afLen
		} else {
			ts[3] = 0x10 | cc // payload only
		}
		r.Read(ts[off:])
		out = append(out, pkt...)
	}
	return out
}

// TestPacketBufferSlabPayloadsSurvive keeps every packet across whole slabs
// and checks each payload still equals a copying parse of the same bytes: a
// slab region handed out must never be reused.
func TestPacketBufferSlabPayloadsSurvive(t *testing.T) {
	for _, size := range []int{MpegTsPacketSize, 192, 204} {
		r := rand.New(rand.NewSource(int64(size)))
		n := 5*packetSlabPackets + 3
		stream := slabTestStream(r, n, size)
		pb, err := newPacketBuffer(bytes.NewReader(stream), size, nil)
		require.NoError(t, err)

		var got []*Packet
		for {
			p, err := pb.next()
			if err == ErrNoMorePackets {
				break
			}
			require.NoError(t, err)
			got = append(got, p)
		}
		require.Len(t, got, n)
		for k, p := range got {
			want, err := parsePacket(astikit.NewBytesIterator(stream[k*size:(k+1)*size]), nil)
			require.NoError(t, err)
			assert.Equal(t, want, p, "packet %d (size %d)", k, size)
		}
	}
}

// TestPacketBufferSlabPayloadCapped: appending to one packet's payload must
// not overwrite the next packet sharing its slab.
func TestPacketBufferSlabPayloadCapped(t *testing.T) {
	stream := slabTestStream(rand.New(rand.NewSource(1)), 3, MpegTsPacketSize)
	pb, err := newPacketBuffer(bytes.NewReader(stream), MpegTsPacketSize, nil)
	require.NoError(t, err)
	p1, err := pb.next()
	require.NoError(t, err)
	p2, err := pb.next()
	require.NoError(t, err)
	before := append([]byte(nil), p2.Payload...)
	_ = append(p1.Payload, bytes.Repeat([]byte{0xaa}, MpegTsPacketSize)...)
	assert.Equal(t, before, p2.Payload)
}

// TestPacketBufferSlabSkipper: skipped packets are not returned and do not
// disturb the packets that are.
func TestPacketBufferSlabSkipper(t *testing.T) {
	stream := slabTestStream(rand.New(rand.NewSource(2)), 3*packetSlabPackets, MpegTsPacketSize)
	skip := func(p *Packet) bool { return p.Header.PID == 0x101 }
	pb, err := newPacketBuffer(bytes.NewReader(stream), MpegTsPacketSize, skip)
	require.NoError(t, err)
	var got []*Packet
	for {
		p, err := pb.next()
		if err == ErrNoMorePackets {
			break
		}
		require.NoError(t, err)
		got = append(got, p)
	}
	var want []*Packet
	for k := 0; k < 3*packetSlabPackets; k++ {
		p, err := parsePacket(astikit.NewBytesIterator(stream[k*MpegTsPacketSize:(k+1)*MpegTsPacketSize]), skip)
		if err == errSkippedPacket {
			continue
		}
		require.NoError(t, err)
		want = append(want, p)
	}
	assert.Equal(t, want, got)
}

func BenchmarkDemuxerNextPacket(b *testing.B) {
	stream := slabTestStream(rand.New(rand.NewSource(3)), 4096, MpegTsPacketSize)
	b.SetBytes(int64(len(stream)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		dmx := NewDemuxer(context.Background(), bytes.NewReader(stream), DemuxerOptPacketSize(MpegTsPacketSize))
		for {
			if _, err := dmx.NextPacket(); err != nil {
				break
			}
		}
	}
}
