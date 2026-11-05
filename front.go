// wfc-front receives the address-sensitive Wii/DS game ports (GPCM, server browser, QR2,
// NATNEG) on a pasta network, so it sees players' real public addresses (like openpak-edge),
// and relays them to the services' loopback-published proxy ports with PROXY protocol v2
// (prds#149):
//
//   - TCP: one v2 header (STREAM) at the start of each connection, then bytes both ways;
//   - UDP: each datagram framed with a v2 header (DGRAM) naming the player; the service frames
//     what it sends with the player as destination, and the relay sends it from the public
//     port, so replies and unsolicited packets (QR2 to a host, NATNEG to both peers) reach the
//     player from the port the player knows.
package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

var proxySig = []byte("\r\n\r\n\x00\r\nQUIT\n")

func encodeV2(stream bool, src, dst *net.UDPAddr) []byte {
	out := append([]byte(nil), proxySig...)
	proto := byte(0x2)
	if stream {
		proto = 0x1
	}
	if s4, d4 := src.IP.To4(), dst.IP.To4(); s4 != nil && d4 != nil {
		out = append(out, 0x21, 0x10|proto)
		out = binary.BigEndian.AppendUint16(out, 12)
		out = append(out, s4...)
		out = append(out, d4...)
	} else {
		out = append(out, 0x21, 0x20|proto)
		out = binary.BigEndian.AppendUint16(out, 36)
		out = append(out, src.IP.To16()...)
		out = append(out, dst.IP.To16()...)
	}
	out = binary.BigEndian.AppendUint16(out, uint16(src.Port))
	out = binary.BigEndian.AppendUint16(out, uint16(dst.Port))
	return out
}

var errNotV2 = errors.New("not a PROXY v2 frame")

// decodeV2Dst reads the destination and payload of a framed datagram from the service.
func decodeV2Dst(b []byte) (*net.UDPAddr, []byte, error) {
	if len(b) < 16 || !bytes.Equal(b[:12], proxySig) || b[12] != 0x21 {
		return nil, nil, errNotV2
	}
	n := int(binary.BigEndian.Uint16(b[14:16]))
	if len(b) < 16+n {
		return nil, nil, errNotV2
	}
	a := b[16 : 16+n]
	switch b[13] >> 4 {
	case 1:
		if n < 12 {
			return nil, nil, errNotV2
		}
		return &net.UDPAddr{IP: net.IP(append([]byte(nil), a[4:8]...)), Port: int(binary.BigEndian.Uint16(a[10:12]))}, b[16+n:], nil
	case 2:
		if n < 36 {
			return nil, nil, errNotV2
		}
		return &net.UDPAddr{IP: net.IP(append([]byte(nil), a[16:32]...)), Port: int(binary.BigEndian.Uint16(a[34:36]))}, b[16+n:], nil
	}
	return nil, nil, errNotV2
}

// stats are one mapping's counters, logged once a minute.
type stats struct {
	in, out, conns, dropped atomic.Int64
	mu                      sync.Mutex
	players                 map[string]struct{}
}

func (s *stats) seen(addr string) {
	s.mu.Lock()
	if s.players == nil {
		s.players = map[string]struct{}{}
	}
	s.players[addr] = struct{}{}
	s.mu.Unlock()
}

// summary returns the line and resets the counters (no player addresses).
func (s *stats) summary() (string, bool) {
	in, out, conns, dropped := s.in.Swap(0), s.out.Swap(0), s.conns.Swap(0), s.dropped.Swap(0)
	s.mu.Lock()
	players := len(s.players)
	s.players = nil
	s.mu.Unlock()
	if in+out+conns+dropped == 0 {
		return "", false
	}
	return "in " + itoa(in) + " out " + itoa(out) + " conns " + itoa(conns) + " players " + itoa(int64(players)) + " dropped " + itoa(dropped), true
}

func itoa(n int64) string {
	b := []byte{}
	if n == 0 {
		return "0"
	}
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// TCPRelay accepts on l and relays each connection to upstream with a PROXY v2 header.
type TCPRelay struct {
	Name     string
	Upstream string
	Stats    *stats
	Log      *log.Logger
}

func (r *TCPRelay) Serve(l net.Listener) error {
	for {
		c, err := l.Accept()
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			return err
		}
		go r.handle(c)
	}
}

func (r *TCPRelay) handle(c net.Conn) {
	defer c.Close()
	start := time.Now()
	u, err := net.DialTimeout("tcp", r.Upstream, 5*time.Second)
	if err != nil {
		r.Stats.dropped.Add(1)
		r.Log.Printf("tcp %s: upstream dial: %v", r.Name, err)
		return
	}
	defer u.Close()
	src, _ := c.RemoteAddr().(*net.TCPAddr)
	dst, _ := c.LocalAddr().(*net.TCPAddr)
	if src == nil || dst == nil {
		return
	}
	if _, err := u.Write(encodeV2(true, &net.UDPAddr{IP: src.IP, Port: src.Port}, &net.UDPAddr{IP: dst.IP, Port: dst.Port})); err != nil {
		return
	}
	if tc, ok := c.(*net.TCPConn); ok {
		_ = tc.SetKeepAlive(true)
	}
	r.Stats.conns.Add(1)
	r.Stats.seen(src.IP.String())
	var wg sync.WaitGroup
	var up, down int64
	wg.Add(1)
	go func() {
		defer wg.Done()
		down, _ = io.Copy(c, u)
		if cw, ok := c.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		}
	}()
	up, _ = io.Copy(u, c)
	if cw, ok := u.(interface{ CloseWrite() error }); ok {
		_ = cw.CloseWrite()
	}
	wg.Wait()
	r.Stats.in.Add(up)
	r.Stats.out.Add(down)
	r.Log.Printf("tcp %s: connection closed after %s (%d bytes in, %d out)", r.Name, time.Since(start).Round(time.Millisecond), up, down)
}

// UDPRelay relays datagrams between the public socket and the service's proxy socket.
type UDPRelay struct {
	Name     string
	Public   net.PacketConn
	Upstream *net.UDPConn // connected to the service's proxy socket
	Stats    *stats
	Log      *log.Logger
}

// Run relays until either socket fails.
func (r *UDPRelay) Run() {
	local, _ := r.Public.LocalAddr().(*net.UDPAddr)
	done := make(chan struct{}, 2)
	go func() { // player -> service
		defer func() { done <- struct{}{} }()
		buf := make([]byte, 2048)
		for {
			n, from, err := r.Public.ReadFrom(buf)
			if err != nil {
				r.Log.Printf("udp %s: public read: %v", r.Name, err)
				return
			}
			src, ok := from.(*net.UDPAddr)
			if !ok {
				continue
			}
			frame := append(encodeV2(false, src, local), buf[:n]...)
			if _, err := r.Upstream.Write(frame); err != nil {
				r.Stats.dropped.Add(1)
				continue
			}
			r.Stats.in.Add(1)
			r.Stats.seen(src.IP.String())
		}
	}()
	go func() { // service -> player
		defer func() { done <- struct{}{} }()
		buf := make([]byte, 2048)
		for {
			n, err := r.Upstream.Read(buf)
			if err != nil {
				var ne net.Error
				if errors.As(err, &ne) && !ne.Timeout() && !errors.Is(err, net.ErrClosed) {
					// ICMP "port unreachable" while the service restarts: keep going.
					time.Sleep(100 * time.Millisecond)
					continue
				}
				if errors.Is(err, net.ErrClosed) {
					return
				}
				time.Sleep(100 * time.Millisecond)
				continue
			}
			dst, payload, err := decodeV2Dst(buf[:n])
			if err != nil {
				r.Stats.dropped.Add(1)
				continue
			}
			if _, err := r.Public.WriteTo(payload, dst); err != nil {
				r.Stats.dropped.Add(1)
				continue
			}
			r.Stats.out.Add(1)
		}
	}()
	<-done
}
