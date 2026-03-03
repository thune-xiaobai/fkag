package proxy

import (
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/user/fkag/internal/vip"
)

// mockDialer implements Dialer for testing. It connects to a local echo server
// instead of going through a real proxy.
type mockDialer struct {
	// targetAddr maps "host:port" to a local listener address to connect to.
	targets map[string]string
	mu      sync.Mutex
	calls   []string // recorded dial calls
}

func newMockDialer() *mockDialer {
	return &mockDialer{targets: make(map[string]string)}
}

func (d *mockDialer) addTarget(host string, port int, localAddr string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	key := net.JoinHostPort(host, strconv.Itoa(port))
	d.targets[key] = localAddr
}

func (d *mockDialer) Dial(targetHost string, targetPort int) (net.Conn, error) {
	d.mu.Lock()
	key := net.JoinHostPort(targetHost, strconv.Itoa(targetPort))
	addr, ok := d.targets[key]
	d.calls = append(d.calls, key)
	d.mu.Unlock()

	if !ok {
		return nil, fmt.Errorf("mock dialer: no target for %s", key)
	}
	return net.DialTimeout("tcp", addr, 2*time.Second)
}

func (d *mockDialer) getCalls() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]string, len(d.calls))
	copy(out, d.calls)
	return out
}

// echoServer starts a TCP listener that echoes back everything it receives.
func echoServer(t *testing.T) (string, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("echoServer listen: %v", err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				io.Copy(c, c)
			}(conn)
		}
	}()
	return ln.Addr().String(), func() { ln.Close() }
}

// noopExecutor satisfies vip.CommandExecutor without running real commands.
type noopExecutor struct{}

func (noopExecutor) Run(name string, args ...string) error { return nil }

// testListenAddr overrides VIP addresses to 127.0.0.1:0 for testing.
func testListenAddr(_ net.IP, _ int) string {
	return "127.0.0.1:0"
}

// getForwarderAddr returns the actual listen address for the first listener.
func getForwarderAddr(f *Forwarder, index int) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if index >= len(f.listeners) {
		return ""
	}
	return f.listeners[index].Addr().String()
}

func TestForwarder_BidirectionalRelay(t *testing.T) {
	pool := vip.NewPoolWithExecutor([]string{"example.com"}, noopExecutor{})
	echoAddr, echoCleanup := echoServer(t)
	defer echoCleanup()

	dialer := newMockDialer()
	dialer.addTarget("example.com", 443, echoAddr)

	fwd := NewForwarder(pool, dialer, []int{443})
	fwd.listenAddrFunc = testListenAddr
	if err := fwd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer fwd.Stop()

	// Connect to the forwarder's actual listen address.
	addr := getForwarderAddr(fwd, 0)
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("connect to forwarder: %v", err)
	}
	defer conn.Close()

	// Send data and verify echo.
	msg := []byte("hello from client")
	if _, err := conn.Write(msg); err != nil {
		t.Fatalf("write: %v", err)
	}

	buf := make([]byte, len(msg))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(buf) != string(msg) {
		t.Fatalf("expected %q, got %q", msg, buf)
	}

	// Verify the dialer was called with the correct target.
	calls := dialer.getCalls()
	if len(calls) != 1 || calls[0] != "example.com:443" {
		t.Fatalf("expected dial to example.com:443, got %v", calls)
	}
}

func TestForwarder_MultipleDomainsAndPorts(t *testing.T) {
	domains := []string{"a.example.com", "b.example.com"}
	pool := vip.NewPoolWithExecutor(domains, noopExecutor{})

	echoAddr, echoCleanup := echoServer(t)
	defer echoCleanup()

	dialer := newMockDialer()
	for _, d := range domains {
		for _, p := range []int{80, 443} {
			dialer.addTarget(d, p, echoAddr)
		}
	}

	fwd := NewForwarder(pool, dialer, []int{80, 443})
	fwd.listenAddrFunc = testListenAddr
	if err := fwd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer fwd.Stop()

	// We have 2 domains × 2 ports = 4 listeners.
	fwd.mu.Lock()
	numListeners := len(fwd.listeners)
	fwd.mu.Unlock()
	if numListeners != 4 {
		t.Fatalf("expected 4 listeners, got %d", numListeners)
	}

	// Test each listener.
	for i := 0; i < numListeners; i++ {
		addr := getForwarderAddr(fwd, i)
		conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
		if err != nil {
			t.Fatalf("connect to listener %d (%s): %v", i, addr, err)
		}

		msg := []byte(fmt.Sprintf("test-listener-%d", i))
		conn.Write(msg)

		buf := make([]byte, len(msg))
		io.ReadFull(conn, buf)
		if string(buf) != string(msg) {
			t.Errorf("listener %d: expected %q, got %q", i, msg, buf)
		}
		conn.Close()
	}
}

func TestForwarder_UpstreamDialFailure(t *testing.T) {
	pool := vip.NewPoolWithExecutor([]string{"fail.example.com"}, noopExecutor{})

	// Dialer with no targets — all dials will fail.
	dialer := newMockDialer()

	fwd := NewForwarder(pool, dialer, []int{443})
	fwd.listenAddrFunc = testListenAddr
	if err := fwd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer fwd.Stop()

	addr := getForwarderAddr(fwd, 0)
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("connect to forwarder: %v", err)
	}

	// The forwarder should close the connection after dial failure.
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1)
	_, err = conn.Read(buf)
	if err == nil {
		t.Fatal("expected connection to be closed after upstream dial failure")
	}
	conn.Close()
}

func TestForwarder_StopClosesListeners(t *testing.T) {
	pool := vip.NewPoolWithExecutor([]string{"stop.example.com"}, noopExecutor{})
	dialer := newMockDialer()

	fwd := NewForwarder(pool, dialer, []int{443})
	fwd.listenAddrFunc = testListenAddr
	if err := fwd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	addr := getForwarderAddr(fwd, 0)

	if err := fwd.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// After stop, connecting should fail.
	_, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
	if err == nil {
		t.Fatal("expected connection to fail after Stop")
	}
}
