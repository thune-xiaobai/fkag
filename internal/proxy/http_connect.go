// Package proxy implements the TCP transparent proxy that forwards traffic
// through upstream HTTP CONNECT or SOCKS5 proxies.
package proxy

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"strconv"
)

// Dialer abstracts upstream proxy connection establishment.
type Dialer interface {
	Dial(targetHost string, targetPort int) (net.Conn, error)
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

	return conn, nil
}
