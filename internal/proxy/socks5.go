package proxy

import (
	"fmt"
	"net"
	"strconv"

	"golang.org/x/net/proxy"
)

// SOCKS5Dialer connects to a target through a SOCKS5 proxy.
type SOCKS5Dialer struct {
	proxyHost string
	proxyPort int
}

// NewSOCKS5Dialer creates a Dialer that uses the SOCKS5 protocol.
func NewSOCKS5Dialer(proxyHost string, proxyPort int) Dialer {
	return &SOCKS5Dialer{
		proxyHost: proxyHost,
		proxyPort: proxyPort,
	}
}

// Dial connects to the target through the SOCKS5 proxy.
func (d *SOCKS5Dialer) Dial(targetHost string, targetPort int) (net.Conn, error) {
	proxyAddr := net.JoinHostPort(d.proxyHost, strconv.Itoa(d.proxyPort))

	dialer, err := proxy.SOCKS5("tcp", proxyAddr, nil, proxy.Direct)
	if err != nil {
		return nil, fmt.Errorf("create SOCKS5 dialer for %s: %w", proxyAddr, err)
	}

	targetAddr := net.JoinHostPort(targetHost, strconv.Itoa(targetPort))
	conn, err := dialer.Dial("tcp", targetAddr)
	if err != nil {
		return nil, fmt.Errorf("SOCKS5 connect to %s via %s: %w", targetAddr, proxyAddr, err)
	}

	return conn, nil
}
