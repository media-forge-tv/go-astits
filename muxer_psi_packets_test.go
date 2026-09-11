package astits

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMuxerPacketizesLargePSI(t *testing.T) {
	for _, count := range []int{1, 14, 15, 16, 32, 64} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			m := NewMuxer(context.Background(), nil)
			for i := 0; i < count; i++ {
				require.NoError(t, m.AddElementaryStream(PMTElementaryStream{
					ElementaryPID: uint16(257 + i), StreamType: StreamTypePrivateData,
					ElementaryStreamDescriptors: []*Descriptor{
						{Tag: DescriptorTagEnhancedAC3, Length: 1, EnhancedAC3: &DescriptorEnhancedAC3{}},
						{Tag: DescriptorTagISO639LanguageAndAudioType, Length: 4, ISO639LanguageAndAudioType: &DescriptorISO639LanguageAndAudioType{Language: []byte("eng")}},
					},
				}))
			}
			m.SetPCRPID(257)
			cc := byte(0)
			for generation := 0; generation < 2; generation++ {
				if generation == 1 {
					m.pmt.ElementaryStreams[0].ElementaryStreamDescriptors[1].ISO639LanguageAndAudioType.Language = []byte("deu")
					m.pmtUpdated = true
				}
				for repeat := 0; repeat < 3; repeat++ {
					require.NoError(t, m.generatePMT())
					wire := m.pmtBytes.Bytes()
					require.Equal(t, 0, len(wire)%188)
					var section []byte
					for offset := 0; offset < len(wire); offset += 188 {
						packet := wire[offset : offset+188]
						require.Equal(t, byte(0x47), packet[0])
						require.Equal(t, uint16(pmtStartPID), uint16(packet[1]&31)<<8|uint16(packet[2]))
						require.Equal(t, cc, packet[3]&15)
						cc = (cc + 1) & 15
						require.Equal(t, byte(1), packet[3]>>4)
						if offset == 0 {
							require.NotZero(t, packet[1]&64)
							require.Zero(t, packet[4])
							section = append(section, packet[5:]...)
						} else {
							require.Zero(t, packet[1]&64)
							section = append(section, packet[4:]...)
						}
					}
					length := 3 + int(section[1]&15)*256 + int(section[2])
					require.Equal(t, 16+14*count, length)
					require.Equal(t, byte(generation), (section[5]>>1)&31)
					require.Equal(t, bytes.Repeat([]byte{0xff}, len(section)-length), section[length:])
					crc := uint32(0xffffffff)
					for _, value := range section[:length] {
						crc ^= uint32(value) << 24
						for bit := 0; bit < 8; bit++ {
							if crc&0x80000000 != 0 {
								crc = crc<<1 ^ 0x04c11db7
							} else {
								crc <<= 1
							}
						}
					}
					require.Zero(t, crc)
				}
			}
		})
	}
}

func TestMuxerRejectsOversizedPSISection(t *testing.T) {
	var wire bytes.Buffer
	m := NewMuxer(context.Background(), &wire)
	for i := 0; i < 210; i++ {
		require.NoError(t, m.AddElementaryStream(PMTElementaryStream{ElementaryPID: uint16(257 + i), StreamType: StreamTypeADTS}))
	}
	m.SetPCRPID(257)
	n, err := m.WriteTables()
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds 1021")
	require.Zero(t, n)
	require.Zero(t, wire.Len(), "invalid tables must not be partially published")
}
