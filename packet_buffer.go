package astits

import (
	"bufio"
	"errors"
	"fmt"
	"io"

	"github.com/asticode/go-astikit"
)

// packetBuffer represents a packet buffer
type packetBuffer struct {
	packetSize int
	s          PacketSkipper
	r          io.Reader

	// Packets are read straight into slabs shared by packetSlabPackets
	// packets, and their payloads are windows of it, so steady-state demuxing
	// allocates twice per slab instead of a Packet plus a payload copy per
	// packet. A slab region is never reused once handed out.
	slab    []byte
	packets []Packet
}

// packetSlabPackets is the number of packets sharing one slab: large enough to
// amortise the two allocations to a fraction of a percent of the per-packet
// work, small enough (~12 KiB for 188-byte packets) that a packet held back
// in a slow PID's accumulator pins little memory.
const packetSlabPackets = 64

// newPacketBuffer creates a new packet buffer
func newPacketBuffer(r io.Reader, packetSize int, s PacketSkipper) (pb *packetBuffer, err error) {
	// Init
	pb = &packetBuffer{
		packetSize: packetSize,
		s:          s,
		r:          r,
	}

	// Packet size is not set
	if pb.packetSize == 0 {
		// Auto detect packet size
		if pb.packetSize, err = autoDetectPacketSize(r); err != nil {
			err = fmt.Errorf("astits: auto detecting packet size failed: %w", err)
			return
		}
	}
	return
}

// autoDetectPacketSize updates the packet size based on the first bytes
// Minimum packet size is 188 and is bounded by 2 sync bytes
// Assumption is made that the first byte of the reader is a sync byte
func autoDetectPacketSize(r io.Reader) (packetSize int, err error) {
	// Read first bytes
	const l = 193
	var b = make([]byte, l)
	shouldRewind, rerr := peek(r, b)
	if rerr != nil {
		err = fmt.Errorf("astits: reading first %d bytes failed: %w", l, rerr)
		return
	}

	// Packet must start with a sync byte
	if b[0] != syncByte {
		err = ErrPacketMustStartWithASyncByte
		return
	}

	// Look for sync bytes
	for idx, b := range b {
		if b == syncByte && idx >= MpegTsPacketSize {
			// Update packet size
			packetSize = idx

			if !shouldRewind {
				return
			}

			// Rewind or sync reader
			var n int64
			if n, err = rewind(r); err != nil {
				err = fmt.Errorf("astits: rewinding failed: %w", err)
				return
			} else if n == -1 {
				var ls = packetSize - (l - packetSize)
				if _, err = r.Read(make([]byte, ls)); err != nil {
					err = fmt.Errorf("astits: reading %d bytes to sync reader failed: %w", ls, err)
					return
				}
			}
			return
		}
	}
	err = fmt.Errorf("astits: only one sync byte detected in first %d bytes", l)
	return
}

// bufio.Reader can't be rewinded, which leads to packet loss on packet size autodetection
// but it has handy Peek() method
// so what we do here is peeking bytes for bufio.Reader and falling back to rewinding/syncing for all other readers
func peek(r io.Reader, b []byte) (shouldRewind bool, err error) {
	if br, ok := r.(*bufio.Reader); ok {
		var bs []byte
		bs, err = br.Peek(len(b))
		if err != nil {
			return
		}
		copy(b, bs)
		return false, nil
	}

	_, err = r.Read(b)
	shouldRewind = true
	return
}

// rewind rewinds the reader if possible, otherwise n = -1
func rewind(r io.Reader) (n int64, err error) {
	if s, ok := r.(io.Seeker); ok {
		if n, err = s.Seek(0, 0); err != nil {
			err = fmt.Errorf("astits: seeking to 0 failed: %w", err)
			return
		}
		return
	}
	n = -1
	return
}

// next fetches the next packet from the buffer
func (pb *packetBuffer) next() (p *Packet, err error) {
	// Loop to make sure we return a packet even if first packets are skipped
	for p == nil {
		if len(pb.slab) < pb.packetSize {
			pb.slab = make([]byte, packetSlabPackets*pb.packetSize)
		}
		if len(pb.packets) == 0 {
			pb.packets = make([]Packet, packetSlabPackets)
		}
		buf := pb.slab[:pb.packetSize:pb.packetSize]

		// Read
		if _, err = io.ReadFull(pb.r, buf); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				err = ErrNoMorePackets
			} else {
				err = fmt.Errorf("astits: reading %d bytes failed: %w", pb.packetSize, err)
			}
			return
		}

		// Parse packet
		cand := &pb.packets[0]
		*cand = Packet{}
		parsed, perr := parsePacketInto(astikit.NewBytesIterator(buf), pb.s, cand, true)
		if parsed {
			// The packet may be retained, so its slab region and slot are spent
			p = cand
			pb.slab = pb.slab[pb.packetSize:]
			pb.packets = pb.packets[1:]
		}
		if perr != nil && !errors.Is(perr, errSkippedPacket) {
			err = fmt.Errorf("astits: building packet failed: %w", perr)
			return
		}
	}

	return
}
