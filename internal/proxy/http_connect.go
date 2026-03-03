// Package proxy implements the TCP transparent proxy that forwards traffic
// through upstream HTTP CONNECT or SOCKS5 proxies.
package proxy

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
)

// Dialer abstracts upstream proxy connection establishment.
type Dialer interface {
	Dial(targetHost string, targetPort int) (net.Conn, error)
}

// bufferedConn wraps a net.Conn with a bufio.Reader so that any bytes already
// pre-read into the buffer are not lost when the connection is used for relay.
type bufferedConn struct {
	net.Conn
	r io.Reader // may be a *bufio.Reader with buffered data
}

func (c *bufferedConn) Read(b []byte) (int, error) {
	return c.r.Read(b)
}

// HTTPConnectDialer connects to a target through an HTTP CONNECT proxy.
type HTTPConnectDialer struct {
	proxyHost string
	proxyPort int
}

// NewHTTPConnectDialer creates a Dialer that uses HTTP CONNECT tunneling.
func NewHTTPConnectDialer(proxyHost string, proxyPort int) Dialer {
	return &HTTPConnectDialer{
		proxyHost: proxyHost,
		proxyPort: proxyPort,
	}
}

// Dial connects to the proxy, sends a CONNECT request for the target,
// and returns the tunneled connection on success.
//
// The returned connection wraps the underlying TCP conn with the bufio.Reader
// used to parse the HTTP response, so any bytes already pre-read into the
// buffer are not lost during subsequent relay.
func (d *HTTPConnectDialer) Dial(targetHost string, targetPort int) (net.Conn, error) {
	proxyAddr := net.JoinHostPort(d.proxyHost, strconv.Itoa(d.proxyPort))
	conn, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		return nil, fmt.Errorf("connect to proxy %s: %w", proxyAddr, err)
	}

	target := net.JoinHostPort(targetHost, strconv.Itoa(targetPort))
	req := "CONNECT " + target + " HTTP/1.1\r\nHost: " + target + "\r\n\r\n"

	if _, err := conn.Write([]byte(req)); err != nil {
		conn.Close()
		return nil, fmt.Errorf("send CONNECT request: %w", err)
	}

	// Use a bufio.Reader only to parse the HTTP response headers.
	// After ReadResponse returns, the reader may have pre-buffered some bytes
	// that belong to the tunneled stream. Wrap conn in bufferedConn so those
	// bytes are replayed before reads fall through to the raw conn.
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("read CONNECT response: %w", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		conn.Close()
		return nil, fmt.Errorf("CONNECT to %s failed: %s", target, resp.Status)
	}

	// If bufio pre-read any tunnel bytes, expose them via bufferedConn.
	// When the buffer is empty, br.Read falls through directly to conn,
	// so there is no performance penalty in the common case.
	return &bufferedConn{Conn: conn, r: br}, nil
}
