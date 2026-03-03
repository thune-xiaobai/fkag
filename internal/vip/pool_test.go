package vip

import (
	"net"
	"testing"
)

func TestNewPool_Basic(t *testing.T) {
	domains := []string{"api.example.com", "cdn.example.com"}
	p := NewPool(domains)

	// First domain gets 127.0.0.2
	ip, ok := p.Lookup("api.example.com")
	if !ok {
		t.Fatal("expected to find api.example.com")
	}
	if !ip.Equal(net.IPv4(127, 0, 0, 2)) {
		t.Fatalf("expected 127.0.0.2, got %s", ip)
	}

	// Second domain gets 127.0.0.3
	ip, ok = p.Lookup("cdn.example.com")
	if !ok {
		t.Fatal("expected to find cdn.example.com")
	}
	if !ip.Equal(net.IPv4(127, 0, 0, 3)) {
		t.Fatalf("expected 127.0.0.3, got %s", ip)
	}
}

func TestNewPool_Empty(t *testing.T) {
	p := NewPool(nil)
	if len(p.Mappings()) != 0 {
		t.Fatal("expected empty mappings for nil domains")
	}

	p = NewPool([]string{})
	if len(p.Mappings()) != 0 {
		t.Fatal("expected empty mappings for empty domains")
	}
}

func TestLookup_NotFound(t *testing.T) {
	p := NewPool([]string{"a.com"})
	_, ok := p.Lookup("b.com")
	if ok {
		t.Fatal("expected not found for unknown domain")
	}
}

func TestReverseLookup(t *testing.T) {
	domains := []string{"a.com", "b.com"}
	p := NewPool(domains)

	domain, ok := p.ReverseLookup(net.IPv4(127, 0, 0, 2))
	if !ok || domain != "a.com" {
		t.Fatalf("expected a.com, got %s (ok=%v)", domain, ok)
	}

	domain, ok = p.ReverseLookup(net.IPv4(127, 0, 0, 3))
	if !ok || domain != "b.com" {
		t.Fatalf("expected b.com, got %s (ok=%v)", domain, ok)
	}

	_, ok = p.ReverseLookup(net.IPv4(127, 0, 0, 99))
	if ok {
		t.Fatal("expected not found for unknown IP")
	}
}

func TestMappings_ReturnsCopy(t *testing.T) {
	p := NewPool([]string{"x.com"})
	m := p.Mappings()

	// Mutate the returned map
	m["injected.com"] = net.IPv4(1, 2, 3, 4)

	// Original pool should be unaffected
	_, ok := p.Lookup("injected.com")
	if ok {
		t.Fatal("Mappings() should return a copy; mutation leaked into pool")
	}
}

func TestMappings_IPCopyIndependence(t *testing.T) {
	p := NewPool([]string{"x.com"})
	m := p.Mappings()

	// Mutate the IP bytes in the returned map
	m["x.com"][3] = 255

	// Original pool IP should be unaffected
	ip, _ := p.Lookup("x.com")
	if ip.Equal(net.IPv4(127, 0, 0, 255)) {
		t.Fatal("Mappings() IP copy should be independent from pool")
	}
}
