package trusttunnel

import (
	"context"
	"net"
	"net/netip"
	"os"
	"sync"
	"time"

	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/sagernet/sing/common/pipe"
)

// The tunnel source address is local to the stream; the server only echoes it back as the
// reply destination, so the port alone identifies a packet conn.
var tunnelSourceAddr = netip.MustParseAddr("198.18.0.1")

const udpQueueSize = 128

type udpMux struct {
	open    func(ctx context.Context) (net.Conn, error)
	resolve func(ctx context.Context, domain string) ([]netip.Addr, error)

	access  sync.Mutex
	session *udpSession
	nextID  uint16
}

type udpSession struct {
	conn       net.Conn
	writeLock  sync.Mutex
	conns      map[uint16]*udpConn
	closed     bool
	closeCause error
}

func (m *udpMux) listen(ctx context.Context) (*udpConn, error) {
	m.access.Lock()
	defer m.access.Unlock()
	if m.session == nil || m.session.closed {
		conn, err := m.open(ctx)
		if err != nil {
			return nil, err
		}
		session := &udpSession{conn: conn, conns: make(map[uint16]*udpConn)}
		m.session = session
		go m.readLoop(session)
	}
	session := m.session
	var port uint16
	for range 1 << 16 {
		m.nextID++
		if m.nextID < 1024 {
			m.nextID = 1024
		}
		if session.conns[m.nextID] == nil {
			port = m.nextID
			break
		}
	}
	if port == 0 {
		return nil, E.New("too many UDP associations")
	}
	conn := &udpConn{
		mux:          m,
		session:      session,
		source:       netip.AddrPortFrom(tunnelSourceAddr, port),
		queue:        make(chan udpFrame, udpQueueSize),
		done:         make(chan struct{}),
		readDeadline: pipe.MakeDeadline(),
	}
	session.conns[port] = conn
	return conn, nil
}

func (m *udpMux) readLoop(session *udpSession) {
	for {
		frame, err := readServerFrame(session.conn)
		if err != nil {
			m.closeSession(session, err)
			return
		}
		m.access.Lock()
		conn := session.conns[frame.destination.Port()]
		m.access.Unlock()
		if conn == nil {
			continue
		}
		select {
		case conn.queue <- frame:
		default:
		}
	}
}

func (m *udpMux) closeSession(session *udpSession, cause error) {
	m.access.Lock()
	if session.closed {
		m.access.Unlock()
		return
	}
	session.closed = true
	session.closeCause = cause
	conns := session.conns
	session.conns = nil
	m.access.Unlock()
	session.conn.Close()
	for _, conn := range conns {
		conn.closeWithError(cause)
	}
}

func (m *udpMux) remove(session *udpSession, port uint16) {
	m.access.Lock()
	defer m.access.Unlock()
	delete(session.conns, port)
}

type udpConn struct {
	mux     *udpMux
	session *udpSession
	source  netip.AddrPort
	queue   chan udpFrame

	done         chan struct{}
	closeOnce    sync.Once
	closeCause   error
	readDeadline pipe.Deadline
	writeAccess  sync.Mutex
	writeBuffer  []byte
}

func (c *udpConn) closeWithError(cause error) {
	c.closeOnce.Do(func() {
		c.closeCause = cause
		close(c.done)
	})
}

func (c *udpConn) Close() error {
	c.closeWithError(net.ErrClosed)
	c.mux.remove(c.session, c.source.Port())
	return nil
}

func (c *udpConn) ReadFrom(p []byte) (int, net.Addr, error) {
	select {
	case frame := <-c.queue:
		n := copy(p, frame.payload)
		return n, net.UDPAddrFromAddrPort(frame.source), nil
	case <-c.done:
		return 0, nil, c.closeCause
	case <-c.readDeadline.Wait():
		return 0, nil, os.ErrDeadlineExceeded
	}
}

func (c *udpConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	select {
	case <-c.done:
		return 0, c.closeCause
	default:
	}
	if len(p) > maxUDPPayload {
		return 0, E.New("UDP payload too large: ", len(p))
	}
	destination, err := c.resolveDestination(M.SocksaddrFromNet(addr))
	if err != nil {
		return 0, err
	}
	c.writeAccess.Lock()
	c.writeBuffer = appendClientFrame(c.writeBuffer[:0], udpFrame{source: c.source, destination: destination, payload: p}, "")
	c.session.writeLock.Lock()
	_, err = c.session.conn.Write(c.writeBuffer)
	c.session.writeLock.Unlock()
	c.writeAccess.Unlock()
	if err != nil {
		c.mux.closeSession(c.session, err)
		return 0, err
	}
	return len(p), nil
}

func (c *udpConn) resolveDestination(destination M.Socksaddr) (netip.AddrPort, error) {
	if destination.IsFqdn() {
		addresses, err := c.mux.resolve(context.Background(), destination.Fqdn)
		if err != nil {
			return netip.AddrPort{}, E.Cause(err, "resolve ", destination.Fqdn)
		}
		if len(addresses) == 0 {
			return netip.AddrPort{}, E.New("no address found for ", destination.Fqdn)
		}
		return netip.AddrPortFrom(addresses[0], destination.Port), nil
	}
	return destination.AddrPort(), nil
}

func (c *udpConn) LocalAddr() net.Addr { return net.UDPAddrFromAddrPort(c.source) }

func (c *udpConn) SetDeadline(t time.Time) error {
	return c.SetReadDeadline(t)
}

func (c *udpConn) SetReadDeadline(t time.Time) error {
	c.readDeadline.Set(t)
	return nil
}

func (c *udpConn) SetWriteDeadline(time.Time) error { return nil }
