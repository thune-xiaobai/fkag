package dns

import (
	"net"
	"testing"

	mdns "github.com/miekg/dns"
	"github.com/user/fkag/internal/vip"
)

// helper to start a server on a random port and return the address.
func startTestServer(t *testing.T, domains []string) (*Server, string) {
	t.Helper()
	pool := vip.NewPool(domains)
	srv := NewServer("127.0.0.1:0", pool)
	if err := srv.Start(); err != nil {
		t.Fatalf("failed to start dns server: %v", err)
	}
	addr := srv.Addr().String()
	t.Cleanup(func() { srv.Stop() })
	return srv, addr
}

func query(t *testing.T, addr, domain string, qtype uint16) *mdns.Msg {
	t.Helper()
	c := new(mdns.Client)
	m := new(mdns.Msg)
	m.SetQuestion(mdns.Fqdn(domain), qtype)
	r, _, err := c.Exchange(m, addr)
	if err != nil {
		t.Fatalf("dns query failed: %v", err)
	}
	return r
}

func TestARecordTargetDomain(t *testing.T) {
	_, addr := startTestServer(t, []string{"example.com", "api.test.io"})

	r := query(t, addr, "example.com", mdns.TypeA)
	if r.Rcode != mdns.RcodeSuccess {
		t.Fatalf("expected success, got rcode %d", r.Rcode)
	}
	if len(r.Answer) != 1 {
		t.Fatalf("expected 1 answer, got %d", len(r.Answer))
	}
	a, ok := r.Answer[0].(*mdns.A)
	if !ok {
		t.Fatal("answer is not an A record")
	}
	if !a.A.Equal(net.IPv4(127, 0, 0, 2)) {
		t.Fatalf("expected 127.0.0.2, got %s", a.A)
	}
	if a.Hdr.Ttl != 60 {
		t.Fatalf("expected TTL 60, got %d", a.Hdr.Ttl)
	}
}

func TestARecordSecondDomain(t *testing.T) {
	_, addr := startTestServer(t, []string{"example.com", "api.test.io"})

	r := query(t, addr, "api.test.io", mdns.TypeA)
	if r.Rcode != mdns.RcodeSuccess {
		t.Fatalf("expected success, got rcode %d", r.Rcode)
	}
	if len(r.Answer) != 1 {
		t.Fatalf("expected 1 answer, got %d", len(r.Answer))
	}
	a := r.Answer[0].(*mdns.A)
	if !a.A.Equal(net.IPv4(127, 0, 0, 3)) {
		t.Fatalf("expected 127.0.0.3, got %s", a.A)
	}
}

func TestARecordUnknownDomain(t *testing.T) {
	_, addr := startTestServer(t, []string{"example.com"})

	r := query(t, addr, "unknown.com", mdns.TypeA)
	if r.Rcode != mdns.RcodeNameError {
		t.Fatalf("expected NXDOMAIN, got rcode %d", r.Rcode)
	}
	if len(r.Answer) != 0 {
		t.Fatalf("expected 0 answers, got %d", len(r.Answer))
	}
}

func TestAAAAQueryReturnsEmpty(t *testing.T) {
	_, addr := startTestServer(t, []string{"example.com"})

	// AAAA for a target domain → empty response, no error
	r := query(t, addr, "example.com", mdns.TypeAAAA)
	if r.Rcode != mdns.RcodeSuccess {
		t.Fatalf("expected success for AAAA, got rcode %d", r.Rcode)
	}
	if len(r.Answer) != 0 {
		t.Fatalf("expected 0 answers for AAAA, got %d", len(r.Answer))
	}
}

func TestAAAAQueryUnknownDomain(t *testing.T) {
	_, addr := startTestServer(t, []string{"example.com"})

	// AAAA for unknown domain → also empty (AAAA always returns empty)
	r := query(t, addr, "unknown.com", mdns.TypeAAAA)
	if r.Rcode != mdns.RcodeSuccess {
		t.Fatalf("expected success for AAAA on unknown domain, got rcode %d", r.Rcode)
	}
	if len(r.Answer) != 0 {
		t.Fatalf("expected 0 answers, got %d", len(r.Answer))
	}
}

func TestOtherQueryTypeKnownDomain(t *testing.T) {
	_, addr := startTestServer(t, []string{"example.com"})

	// MX query for known domain → empty response (no error)
	r := query(t, addr, "example.com", mdns.TypeMX)
	if r.Rcode != mdns.RcodeSuccess {
		t.Fatalf("expected success for MX on known domain, got rcode %d", r.Rcode)
	}
	if len(r.Answer) != 0 {
		t.Fatalf("expected 0 answers, got %d", len(r.Answer))
	}
}

func TestOtherQueryTypeUnknownDomain(t *testing.T) {
	_, addr := startTestServer(t, []string{"example.com"})

	// MX query for unknown domain → NXDOMAIN
	r := query(t, addr, "unknown.com", mdns.TypeMX)
	if r.Rcode != mdns.RcodeNameError {
		t.Fatalf("expected NXDOMAIN for MX on unknown domain, got rcode %d", r.Rcode)
	}
}

func TestStopIdempotent(t *testing.T) {
	srv, _ := startTestServer(t, []string{"example.com"})
	// Stop is called by cleanup, but calling it again should not error
	if err := srv.Stop(); err != nil {
		t.Fatalf("second stop should not error: %v", err)
	}
}
