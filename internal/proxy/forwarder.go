// Package proxy implements the TCP transparent proxy that forwards traffic
// through upstream HTTP CONNECT or SOCKS5 proxies.
package proxy

import (
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"sync"

	"github.com/user/fkag/internal/vip"
)

// listenerMeta holds the domain and original port associated with a listener.
type listenerMeta struct {
	domain   string
	origPort int
}

// Forwarder listens on high ports for each Virtual IP and forwards
// TCP connections through an upstream proxy dialer.
type Forwarder struct {
	pool      *vip.Pool
	dialer    Dialer
	ports     []int
	listeners []net.Listener
	meta      map[net.Listener]listenerMeta
	mu        sync.Mutex

	// listenAddrFunc optionally overrides the listen address for each VIP+port.
	// If nil, the default <vip>:<port+10000> is used.
	// This is primarily for testing on systems where VIPs aren't bound.
	listenAddrFunc func(vipIP net.IP, highPort int) string
}

// NewForwarder creates a TCP forwarding proxy.
// The dialer is used to establish upstream connections through the proxy.
func NewForwarder(pool *vip.Pool, dialer Dialer, ports []int) *Forwarder {
	return &Forwarder{
		pool:   pool,
		dialer: dialer,
		ports:  ports,
		meta:   make(map[net.Listener]listenerMeta),
	}
}

// Start begins listening on <vip>:<port+10000> for every VIP and port combination.
// Connections are accepted in background goroutines. This method is non-blocking.
func (f *Forwarder) Start() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	mappings := f.pool.Mappings()

	for domain, ip := range mappings {
		for _, port := range f.ports {
			highPort := port + 10000
			var addr string
			if f.listenAddrFunc != nil {
				addr = f.listenAddrFunc(ip, highPort)
			} else {
				addr = net.JoinHostPort(ip.String(), strconv.Itoa(highPort))
			}
			ln, err := net.Listen("tcp", addr)
			if err != nil {
				// Close already-opened listeners on failure.
				for _, l := range f.listeners {
					l.Close()
				}
				f.listeners = nil
				f.meta = make(map[net.Listener]listenerMeta)
				return fmt.Errorf("listen on %s: %w", addr, err)
			}
			f.listeners = append(f.listeners, ln)
			f.meta[ln] = listenerMeta{domain: domain, origPort: port}
			go f.acceptLoop(ln)
		}
	}
	return nil
}

// Stop closes all listeners. In-flight connections will finish when their
// io.Copy goroutines complete.
func (f *Forwarder) Stop() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	var firstErr error
	for _, ln := range f.listeners {
		if err := ln.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	f.listeners = nil
	f.meta = make(map[net.Listener]listenerMeta)
	return firstErr
}

func (f *Forwarder) acceptLoop(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return // listener closed
		}
		go f.handleConn(conn, ln)
	}
}

func (f *Forwarder) handleConn(clientConn net.Conn, ln net.Listener) {
	f.mu.Lock()
	m, ok := f.meta[ln]
	f.mu.Unlock()

	if !ok {
		log.Printf("forwarder: no metadata for listener %s", ln.Addr())
		clientConn.Close()
		return
	}

	// Dial upstream through the proxy.
	upstreamConn, err := f.dialer.Dial(m.domain, m.origPort)
	if err != nil {
		log.Printf("forwarder: upstream dial %s:%d: %v", m.domain, m.origPort, err)
		clientConn.Close()
		return
	}

	// Bidirectional copy — when either direction finishes, close both.
	relay(clientConn, upstreamConn)
}

// relay copies data bidirectionally between two connections.
// When either direction finishes (EOF or error), both connections are closed.
func relay(a, b net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		io.Copy(b, a)
		if tc, ok := b.(*net.TCPConn); ok {
			tc.CloseWrite()
		}
	}()

	go func() {
		defer wg.Done()
		io.Copy(a, b)
		if tc, ok := a.(*net.TCPConn); ok {
			tc.CloseWrite()
		}
	}()

	wg.Wait()
	a.Close()
	b.Close()
}
