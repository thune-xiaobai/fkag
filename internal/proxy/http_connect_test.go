package proxy

import (
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
)

// mockProxy starts a TCP listener that simulates an HTTP CONNECT proxy.
// It reads the CONNECT request, validates it, and responds with the given status code.
// If status is 200, it echoes back any data sent through the tunnel.
func mockProxy(t *testing.T, statusCode int) (string, int, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start mock proxy: %v", err)
	}

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return // listener closed
			}
			go handleMockConn(conn, statusCode)
		}
	}()

	addr := ln.Addr().(*net.TCPAddr)
	return addr.IP.String(), addr.Port, func() { ln.Close() }
}

func handleMockConn(conn net.Conn, statusCode int) {
	defer conn.Close()

	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		return
	}
	request := string(buf[:n])

	// Validate it looks like a CONNECT request
	if !strings.HasPrefix(request, "CONNECT ") {
		conn.Write([]byte("HTTP/1.1 400 Bad Request\r\n\r\n"))
		return
	}

	// Send response
	resp := fmt.Sprintf("HTTP/1.1 %d %s\r\n\r\n", statusCode, statusText(statusCode))
	conn.Write([]byte(resp))

	if statusCode == 200 {
		// Echo mode: copy data back for tunnel verification
		io.Copy(conn, conn)
	}
}

func statusText(code int) string {
	switch code {
	case 200:
		return "Connection Established"
	case 403:
		return "Forbidden"
	case 502:
		return "Bad Gateway"
	default:
		return "Unknown"
	}
}

func TestHTTPConnectDialer_Success(t *testing.T) {
	host, port, cleanup := mockProxy(t, 200)
	defer cleanup()

	dialer := NewHTTPConnectDialer(host, port)
	conn, err := dialer.Dial("example.com", 443)
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	defer conn.Close()

	// Verify the tunnel works by sending data and reading the echo
	msg := []byte("hello tunnel")
	if _, err := conn.Write(msg); err != nil {
		t.Fatalf("write through tunnel: %v", err)
	}

	buf := make([]byte, len(msg))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read from tunnel: %v", err)
	}
	if string(buf) != string(msg) {
		t.Fatalf("expected %q, got %q", msg, buf)
	}
}

func TestHTTPConnectDialer_Rejected(t *testing.T) {
	host, port, cleanup := mockProxy(t, 403)
	defer cleanup()

	dialer := NewHTTPConnectDialer(host, port)
	conn, err := dialer.Dial("blocked.com", 443)
	if err == nil {
		conn.Close()
		t.Fatal("expected error for rejected CONNECT")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Fatalf("expected 403 in error, got: %v", err)
	}
}

func TestHTTPConnectDialer_ProxyUnreachable(t *testing.T) {
	// Use a port that's almost certainly not listening
	dialer := NewHTTPConnectDialer("127.0.0.1", 1)
	conn, err := dialer.Dial("example.com", 443)
	if err == nil {
		conn.Close()
		t.Fatal("expected error for unreachable proxy")
	}
	if !strings.Contains(err.Error(), "connect to proxy") {
		t.Fatalf("expected 'connect to proxy' in error, got: %v", err)
	}
}

// mockProxyCapture starts a mock proxy that captures the raw CONNECT request.
func mockProxyCapture(t *testing.T) (string, int, func(), <-chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start mock proxy: %v", err)
	}

	captured := make(chan string, 1)

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		buf := make([]byte, 4096)
		n, err := conn.Read(buf)
		if err != nil {
			return
		}
		captured <- string(buf[:n])
		conn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
	}()

	addr := ln.Addr().(*net.TCPAddr)
	return addr.IP.String(), addr.Port, func() { ln.Close() }, captured
}

func TestHTTPConnectDialer_RequestFormat(t *testing.T) {
	host, port, cleanup, captured := mockProxyCapture(t)
	defer cleanup()

	dialer := NewHTTPConnectDialer(host, port)
	conn, err := dialer.Dial("api.example.com", 443)
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	conn.Close()

	req := <-captured
	expected := "CONNECT api.example.com:443 HTTP/1.1\r\nHost: api.example.com:443\r\n\r\n"
	if req != expected {
		t.Fatalf("unexpected CONNECT request:\ngot:  %q\nwant: %q", req, expected)
	}
}

func TestHTTPConnectDialer_Port80(t *testing.T) {
	host, port, cleanup, captured := mockProxyCapture(t)
	defer cleanup()

	dialer := NewHTTPConnectDialer(host, port)
	conn, err := dialer.Dial("cdn.example.com", 80)
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	conn.Close()

	req := <-captured
	expected := "CONNECT cdn.example.com:80 HTTP/1.1\r\nHost: cdn.example.com:80\r\n\r\n"
	if req != expected {
		t.Fatalf("unexpected CONNECT request:\ngot:  %q\nwant: %q", req, expected)
	}
}
