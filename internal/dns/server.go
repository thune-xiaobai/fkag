// Package dns implements the local DNS server and /etc/resolver/ file management.
package dns

import (
	"fmt"
	"net"
	"strings"
	"sync"

	"github.com/miekg/dns"
	"github.com/user/fkag/internal/vip"
)

// Server is the local DNS server that resolves target domains to Virtual IPs.
type Server struct {
	listenAddr string
	pool       *vip.Pool
	server     *dns.Server
	mu         sync.Mutex
}

// NewServer creates a new DNS server instance.
func NewServer(listenAddr string, pool *vip.Pool) *Server {
	return &Server{
		listenAddr: listenAddr,
		pool:       pool,
	}
}

// Start launches the UDP DNS server in a non-blocking manner.
// It waits for the server to be ready before returning.
func (s *Server) Start() error {
	mux := dns.NewServeMux()
	mux.HandleFunc(".", s.handleQuery)

	started := make(chan struct{})
	s.mu.Lock()
	s.server = &dns.Server{
		Addr:    s.listenAddr,
		Net:     "udp",
		Handler: mux,
		NotifyStartedFunc: func() {
			close(started)
		},
	}
	s.mu.Unlock()

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.server.ListenAndServe()
	}()

	select {
	case <-started:
		return nil
	case err := <-errCh:
		return fmt.Errorf("dns server failed to start: %w", err)
	}
}

// Stop shuts down the DNS server.
func (s *Server) Stop() error {
	s.mu.Lock()
	srv := s.server
	s.mu.Unlock()
	if srv == nil {
		return nil
	}
	return srv.Shutdown()
}

// handleQuery processes incoming DNS queries.
func (s *Server) handleQuery(w dns.ResponseWriter, r *dns.Msg) {
	msg := new(dns.Msg)
	msg.SetReply(r)
	msg.Authoritative = true

	if len(r.Question) == 0 {
		w.WriteMsg(msg)
		return
	}

	q := r.Question[0]
	// Remove trailing dot from the queried name
	domain := strings.TrimSuffix(q.Name, ".")

	switch q.Qtype {
	case dns.TypeA:
		ip, ok := s.pool.Lookup(domain)
		if !ok {
			msg.Rcode = dns.RcodeNameError // NXDOMAIN
		} else {
			msg.Answer = append(msg.Answer, &dns.A{
				Hdr: dns.RR_Header{
					Name:   q.Name,
					Rrtype: dns.TypeA,
					Class:  dns.ClassINET,
					Ttl:    60,
				},
				A: ip.To4(),
			})
		}
	case dns.TypeAAAA:
		// Return empty response for AAAA queries (no IPv6 addresses)
		// msg.Rcode stays NoError, just no answers
	default:
		// For other query types: NXDOMAIN for unknown domains, empty for known
		if _, ok := s.pool.Lookup(domain); !ok {
			msg.Rcode = dns.RcodeNameError
		}
	}

	w.WriteMsg(msg)
}

// Addr returns the actual address the server is listening on.
// Useful when the server was started on port 0 (random port).
func (s *Server) Addr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.server != nil {
		return s.server.PacketConn.LocalAddr()
	}
	return nil
}
