package main

import (
	"bufio"
	"encoding/binary"
	"io"
	"log"
	"net"
	"testing"
	"time"
)

func quiet() *log.Logger { return log.New(io.Discard, "", 0) }

func TestTCPRelayHeaderAndStream(t *testing.T) {
	up, _ := net.Listen("tcp", "127.0.0.1:0")
	defer up.Close()
	got := make(chan string, 1)
	go func() {
		c, err := up.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		br := bufio.NewReader(c)
		h := make([]byte, 28)
		io.ReadFull(br, h)
		ip := net.IP(h[16:20]).String()
		port := binary.BigEndian.Uint16(h[24:26])
		got <- ip + ":" + itoa(int64(port))
		c.Write([]byte("\\lc\\1\\challenge\\x\\final\\")) // the server speaks first
		line := make([]byte, 5)
		io.ReadFull(br, line)
		c.Write(line)
	}()
	front, _ := net.Listen("tcp", "127.0.0.1:0")
	defer front.Close()
	r := &TCPRelay{Name: "t", Upstream: up.Addr().String(), Stats: &stats{}, Log: quiet()}
	go r.Serve(front)

	c, err := net.Dial("tcp", front.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	want := "\\lc\\1\\challenge\\x\\final\\"
	greet := make([]byte, len(want))
	if _, err := io.ReadFull(c, greet); err != nil || string(greet) != want {
		t.Fatalf("server-first greeting %q %v", greet, err)
	}
	c.Write([]byte("hello"))
	echo := make([]byte, 5)
	if _, err := io.ReadFull(c, echo); err != nil || string(echo) != "hello" {
		t.Fatalf("echo %q %v", echo, err)
	}
	if src := <-got; src != c.LocalAddr().String() {
		t.Fatalf("header source %s, want %s", src, c.LocalAddr())
	}
}

func TestUDPRelayBothWaysAndUnsolicited(t *testing.T) {
	svc, _ := net.ListenPacket("udp", "127.0.0.1:0")
	defer svc.Close()
	pub, _ := net.ListenPacket("udp", "127.0.0.1:0")
	up, err := net.DialUDP("udp", nil, svc.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}
	r := &UDPRelay{Name: "u", Public: pub, Upstream: up, Stats: &stats{}, Log: quiet()}
	go r.Run()
	defer pub.Close()

	a, _ := net.ListenPacket("udp", "127.0.0.1:0") // player A
	defer a.Close()
	b, _ := net.ListenPacket("udp", "127.0.0.1:0") // player B, never talks first in this test
	defer b.Close()

	// The service: on a framed datagram from A, answer A and also send B a packet (the
	// unsolicited case: QR2 to a host, NATNEG to a peer).
	go func() {
		buf := make([]byte, 2048)
		n, relay, err := svc.ReadFrom(buf)
		if err != nil || n < 28 {
			return
		}
		srcIP := net.IP(buf[16:20])
		srcPort := int(binary.BigEndian.Uint16(buf[24:26]))
		payload := string(buf[28:n])
		local := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 27900}
		svc.WriteTo(append(encodeV2(false, local, &net.UDPAddr{IP: srcIP, Port: srcPort}), ("re:"+payload)...), relay)
		svc.WriteTo(append(encodeV2(false, local, b.LocalAddr().(*net.UDPAddr)), "to-B"...), relay)
		svc.WriteTo([]byte("garbage without a header"), relay)
	}()

	a.WriteTo([]byte("hi"), pub.LocalAddr())
	buf := make([]byte, 100)
	a.SetReadDeadline(time.Now().Add(3 * time.Second))
	n, from, err := a.ReadFrom(buf)
	if err != nil || string(buf[:n]) != "re:hi" || from.String() != pub.LocalAddr().String() {
		t.Fatalf("A got %q from %v (%v)", buf[:n], from, err)
	}
	b.SetReadDeadline(time.Now().Add(3 * time.Second))
	n, from, err = b.ReadFrom(buf)
	if err != nil || string(buf[:n]) != "to-B" || from.String() != pub.LocalAddr().String() {
		t.Fatalf("B got %q from %v (%v)", buf[:n], from, err)
	}
	time.Sleep(100 * time.Millisecond)
	if r.Stats.dropped.Load() != 1 {
		t.Fatalf("dropped %d, want 1 (the unframed datagram)", r.Stats.dropped.Load())
	}
	if line, ok := r.Stats.summary(); !ok || line != "in 1 out 2 conns 0 players 1 dropped 1" {
		t.Fatalf("summary %q", line)
	}
}

func TestMappings(t *testing.T) {
	m := mappings(" 39900=127.0.0.1:29950, 38910=127.0.0.1:28950,")
	if len(m) != 2 || m[0] != [2]string{"39900", "127.0.0.1:29950"} || m[1][1] != "127.0.0.1:28950" {
		t.Fatalf("%v", m)
	}
}
