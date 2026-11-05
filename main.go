package main

import (
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// version is set at build time.
var version = "dev"

// mappings parses "listenPort=upstreamHost:port,..." .
func mappings(v string) [][2]string {
	var out [][2]string
	for _, m := range strings.Split(v, ",") {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		l, u, ok := strings.Cut(m, "=")
		if !ok {
			log.Fatalf("bad mapping %q (want listenPort=host:port)", m)
		}
		out = append(out, [2]string{strings.TrimSpace(l), strings.TrimSpace(u)})
	}
	return out
}

func main() {
	logger := log.New(os.Stderr, "", log.LstdFlags)
	type named struct {
		name string
		s    *stats
	}
	var all []named
	for _, m := range mappings(os.Getenv("FRONT_TCP")) {
		l, err := net.Listen("tcp", ":"+m[0])
		if err != nil {
			logger.Fatalf("tcp %s: %v", m[0], err)
		}
		r := &TCPRelay{Name: m[0] + "->" + m[1], Upstream: m[1], Stats: &stats{}, Log: logger}
		all = append(all, named{"tcp " + r.Name, r.Stats})
		go func() { logger.Fatalf("tcp %s: %v", r.Name, r.Serve(l)) }()
		logger.Printf("tcp %s relaying with PROXY v2", r.Name)
	}
	for _, m := range mappings(os.Getenv("FRONT_UDP")) {
		pub, err := net.ListenPacket("udp", ":"+m[0])
		if err != nil {
			logger.Fatalf("udp %s: %v", m[0], err)
		}
		ua, err := net.ResolveUDPAddr("udp", m[1])
		if err != nil {
			logger.Fatalf("udp %s: %v", m[1], err)
		}
		up, err := net.DialUDP("udp", nil, ua)
		if err != nil {
			logger.Fatalf("udp %s: %v", m[1], err)
		}
		r := &UDPRelay{Name: m[0] + "->" + m[1], Public: pub, Upstream: up, Stats: &stats{}, Log: logger}
		all = append(all, named{"udp " + r.Name, r.Stats})
		go func() { r.Run(); logger.Fatalf("udp %s stopped", r.Name) }()
		logger.Printf("udp %s relaying with PROXY v2 frames", r.Name)
	}
	if len(all) == 0 {
		logger.Fatal("nothing to relay: set FRONT_TCP and/or FRONT_UDP")
	}
	logger.Printf("wfc-front %s up", version)
	go func() {
		for range time.Tick(time.Minute) {
			for _, n := range all {
				if line, ok := n.s.summary(); ok {
					logger.Printf("%s last minute: %s", n.name, line)
				}
			}
		}
	}()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	logger.Printf("stopped")
}
