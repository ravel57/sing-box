package trusttunnel

import (
	"encoding/binary"
	"io"
	"net/netip"

	E "github.com/sagernet/sing/common/exceptions"
)

const (
	maxUDPPayload   = 65507
	clientFrameHead = 37 // src(16) + sport(2) + dst(16) + dport(2) + app name length(1)
	serverFrameHead = 36
)

type udpFrame struct {
	source      netip.AddrPort
	destination netip.AddrPort
	payload     []byte
}

// IPv4 travels as 16 bytes with 12 leading zeros, not as an IPv4-mapped address.
func putFrameAddr(dst []byte, addr netip.Addr) {
	clear(dst[:16])
	if addr.Is4() || addr.Is4In6() {
		v4 := addr.Unmap().As4()
		copy(dst[12:16], v4[:])
		return
	}
	v6 := addr.As16()
	copy(dst[:16], v6[:])
}

func frameAddr(raw []byte) netip.Addr {
	var b [16]byte
	copy(b[:], raw)
	addr := netip.AddrFrom16(b)
	if addr.IsLoopback() {
		return addr
	}
	for _, v := range b[:12] {
		if v != 0 {
			return addr
		}
	}
	return netip.AddrFrom4([4]byte(b[12:]))
}

func appendClientFrame(dst []byte, frame udpFrame, appName string) []byte {
	size := clientFrameHead + len(appName) + len(frame.payload)
	dst = binary.BigEndian.AppendUint32(dst, uint32(size))
	var head [clientFrameHead]byte
	putFrameAddr(head[0:16], frame.source.Addr())
	binary.BigEndian.PutUint16(head[16:18], frame.source.Port())
	putFrameAddr(head[18:34], frame.destination.Addr())
	binary.BigEndian.PutUint16(head[34:36], frame.destination.Port())
	head[36] = byte(len(appName))
	dst = append(dst, head[:]...)
	dst = append(dst, appName...)
	return append(dst, frame.payload...)
}

func readServerFrame(r io.Reader) (udpFrame, error) {
	var prefix [4]byte
	_, err := io.ReadFull(r, prefix[:])
	if err != nil {
		return udpFrame{}, err
	}
	size := binary.BigEndian.Uint32(prefix[:])
	if size < serverFrameHead || size > serverFrameHead+maxUDPPayload {
		return udpFrame{}, E.New("invalid UDP frame size: ", size)
	}
	body := make([]byte, size)
	_, err = io.ReadFull(r, body)
	if err != nil {
		return udpFrame{}, err
	}
	return udpFrame{
		source:      netip.AddrPortFrom(frameAddr(body[0:16]), binary.BigEndian.Uint16(body[16:18])),
		destination: netip.AddrPortFrom(frameAddr(body[18:34]), binary.BigEndian.Uint16(body[34:36])),
		payload:     body[serverFrameHead:],
	}, nil
}
