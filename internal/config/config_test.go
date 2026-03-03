package config

import "testing"

func TestParseProxyURL_ValidHTTP(t *testing.T) {
	scheme, host, port, err := ParseProxyURL("http://127.0.0.1:7897")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if scheme != "http" {
		t.Errorf("scheme = %q, want %q", scheme, "http")
	}
	if host != "127.0.0.1" {
		t.Errorf("host = %q, want %q", host, "127.0.0.1")
	}
	if port != 7897 {
		t.Errorf("port = %d, want %d", port, 7897)
	}
}

func TestParseProxyURL_ValidSOCKS5(t *testing.T) {
	scheme, host, port, err := ParseProxyURL("socks5://proxy.example.com:1080")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if scheme != "socks5" {
		t.Errorf("scheme = %q, want %q", scheme, "socks5")
	}
	if host != "proxy.example.com" {
		t.Errorf("host = %q, want %q", host, "proxy.example.com")
	}
	if port != 1080 {
		t.Errorf("port = %d, want %d", port, 1080)
	}
}

func TestParseProxyURL_UnsupportedScheme(t *testing.T) {
	_, _, _, err := ParseProxyURL("https://127.0.0.1:8080")
	if err == nil {
		t.Fatal("expected error for https scheme")
	}
}

func TestParseProxyURL_MissingHost(t *testing.T) {
	_, _, _, err := ParseProxyURL("http://:8080")
	if err == nil {
		t.Fatal("expected error for missing host")
	}
}

func TestParseProxyURL_MissingPort(t *testing.T) {
	_, _, _, err := ParseProxyURL("http://127.0.0.1")
	if err == nil {
		t.Fatal("expected error for missing port")
	}
}

func TestParseProxyURL_PortOutOfRange(t *testing.T) {
	_, _, _, err := ParseProxyURL("http://127.0.0.1:0")
	if err == nil {
		t.Fatal("expected error for port 0")
	}
	_, _, _, err = ParseProxyURL("http://127.0.0.1:65536")
	if err == nil {
		t.Fatal("expected error for port 65536")
	}
}

func TestParseProxyURL_EmptyString(t *testing.T) {
	_, _, _, err := ParseProxyURL("")
	if err == nil {
		t.Fatal("expected error for empty string")
	}
}

func TestParseProxyURL_PortBoundaries(t *testing.T) {
	// Port 1 - valid minimum
	_, _, port, err := ParseProxyURL("http://127.0.0.1:1")
	if err != nil {
		t.Fatalf("unexpected error for port 1: %v", err)
	}
	if port != 1 {
		t.Errorf("port = %d, want 1", port)
	}

	// Port 65535 - valid maximum
	_, _, port, err = ParseProxyURL("socks5://127.0.0.1:65535")
	if err != nil {
		t.Fatalf("unexpected error for port 65535: %v", err)
	}
	if port != 65535 {
		t.Errorf("port = %d, want 65535", port)
	}
}

func TestParseDomains_Basic(t *testing.T) {
	got := ParseDomains("api.example.com,cdn.example.com")
	want := []string{"api.example.com", "cdn.example.com"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestParseDomains_TrimSpaces(t *testing.T) {
	got := ParseDomains(" api.example.com , cdn.example.com ")
	want := []string{"api.example.com", "cdn.example.com"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestParseDomains_IgnoreEmpty(t *testing.T) {
	got := ParseDomains("api.example.com,,cdn.example.com,")
	want := []string{"api.example.com", "cdn.example.com"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestParseDomains_EmptyString(t *testing.T) {
	got := ParseDomains("")
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestParseDomains_Single(t *testing.T) {
	got := ParseDomains("example.com")
	if len(got) != 1 || got[0] != "example.com" {
		t.Errorf("got %v, want [example.com]", got)
	}
}

func TestParsePorts_Default(t *testing.T) {
	ports, err := ParsePorts("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ports) != 2 || ports[0] != 80 || ports[1] != 443 {
		t.Errorf("got %v, want [80 443]", ports)
	}
}

func TestParsePorts_DefaultWhitespace(t *testing.T) {
	ports, err := ParsePorts("  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ports) != 2 || ports[0] != 80 || ports[1] != 443 {
		t.Errorf("got %v, want [80 443]", ports)
	}
}

func TestParsePorts_Custom(t *testing.T) {
	ports, err := ParsePorts("8080,9090")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ports) != 2 || ports[0] != 8080 || ports[1] != 9090 {
		t.Errorf("got %v, want [8080 9090]", ports)
	}
}

func TestParsePorts_TrimSpaces(t *testing.T) {
	ports, err := ParsePorts(" 80 , 443 ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ports) != 2 || ports[0] != 80 || ports[1] != 443 {
		t.Errorf("got %v, want [80 443]", ports)
	}
}

func TestParsePorts_InvalidPort(t *testing.T) {
	_, err := ParsePorts("abc")
	if err == nil {
		t.Fatal("expected error for non-numeric port")
	}
}

func TestParsePorts_OutOfRange(t *testing.T) {
	_, err := ParsePorts("0")
	if err == nil {
		t.Fatal("expected error for port 0")
	}
	_, err = ParsePorts("65536")
	if err == nil {
		t.Fatal("expected error for port 65536")
	}
}

func TestParsePorts_Boundaries(t *testing.T) {
	ports, err := ParsePorts("1,65535")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ports) != 2 || ports[0] != 1 || ports[1] != 65535 {
		t.Errorf("got %v, want [1 65535]", ports)
	}
}

// --- ParseScutilOutput tests ---

func TestParseScutilOutput_AllEnabled(t *testing.T) {
	output := `<dictionary> {
  HTTPEnable : 1
  HTTPPort : 7897
  HTTPProxy : 127.0.0.1
  HTTPSEnable : 1
  HTTPSPort : 7897
  HTTPSProxy : 127.0.0.1
  SOCKSEnable : 1
  SOCKSPort : 7897
  SOCKSProxy : 127.0.0.1
}`
	info := ParseScutilOutput(output)
	if !info.HTTPSEnabled || info.HTTPSProxy != "127.0.0.1" || info.HTTPSPort != 7897 {
		t.Errorf("HTTPS: got enabled=%v proxy=%q port=%d", info.HTTPSEnabled, info.HTTPSProxy, info.HTTPSPort)
	}
	if !info.SOCKSEnabled || info.SOCKSProxy != "127.0.0.1" || info.SOCKSPort != 7897 {
		t.Errorf("SOCKS: got enabled=%v proxy=%q port=%d", info.SOCKSEnabled, info.SOCKSProxy, info.SOCKSPort)
	}
	if !info.HTTPEnabled || info.HTTPProxy != "127.0.0.1" || info.HTTPPort != 7897 {
		t.Errorf("HTTP: got enabled=%v proxy=%q port=%d", info.HTTPEnabled, info.HTTPProxy, info.HTTPPort)
	}
}

func TestParseScutilOutput_OnlyHTTPS(t *testing.T) {
	output := `<dictionary> {
  HTTPSEnable : 1
  HTTPSPort : 8080
  HTTPSProxy : proxy.example.com
}`
	info := ParseScutilOutput(output)
	if !info.HTTPSEnabled || info.HTTPSProxy != "proxy.example.com" || info.HTTPSPort != 8080 {
		t.Errorf("HTTPS: got enabled=%v proxy=%q port=%d", info.HTTPSEnabled, info.HTTPSProxy, info.HTTPSPort)
	}
	if info.SOCKSEnabled || info.HTTPEnabled {
		t.Errorf("expected SOCKS and HTTP disabled, got SOCKS=%v HTTP=%v", info.SOCKSEnabled, info.HTTPEnabled)
	}
}

func TestParseScutilOutput_NoneEnabled(t *testing.T) {
	output := `<dictionary> {
  HTTPEnable : 0
  HTTPSEnable : 0
  SOCKSEnable : 0
}`
	info := ParseScutilOutput(output)
	if info.HTTPSEnabled || info.SOCKSEnabled || info.HTTPEnabled {
		t.Errorf("expected all disabled, got HTTPS=%v SOCKS=%v HTTP=%v",
			info.HTTPSEnabled, info.SOCKSEnabled, info.HTTPEnabled)
	}
}

func TestParseScutilOutput_EmptyOutput(t *testing.T) {
	info := ParseScutilOutput("")
	if info.HTTPSEnabled || info.SOCKSEnabled || info.HTTPEnabled {
		t.Errorf("expected all disabled for empty output")
	}
}

// --- SelectProxy tests ---

func TestSelectProxy_HTTPSPriority(t *testing.T) {
	info := ProxyInfo{
		HTTPSEnabled: true, HTTPSProxy: "127.0.0.1", HTTPSPort: 7897,
		SOCKSEnabled: true, SOCKSProxy: "127.0.0.1", SOCKSPort: 7897,
		HTTPEnabled: true, HTTPProxy: "127.0.0.1", HTTPPort: 7897,
	}
	got, err := SelectProxy(info)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "http://127.0.0.1:7897" {
		t.Errorf("got %q, want %q", got, "http://127.0.0.1:7897")
	}
}

func TestSelectProxy_SOCKSFallback(t *testing.T) {
	info := ProxyInfo{
		SOCKSEnabled: true, SOCKSProxy: "127.0.0.1", SOCKSPort: 1080,
		HTTPEnabled: true, HTTPProxy: "127.0.0.1", HTTPPort: 8080,
	}
	got, err := SelectProxy(info)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "socks5://127.0.0.1:1080" {
		t.Errorf("got %q, want %q", got, "socks5://127.0.0.1:1080")
	}
}

func TestSelectProxy_HTTPFallback(t *testing.T) {
	info := ProxyInfo{
		HTTPEnabled: true, HTTPProxy: "192.168.1.1", HTTPPort: 3128,
	}
	got, err := SelectProxy(info)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "http://192.168.1.1:3128" {
		t.Errorf("got %q, want %q", got, "http://192.168.1.1:3128")
	}
}

func TestSelectProxy_NoneAvailable(t *testing.T) {
	info := ProxyInfo{}
	_, err := SelectProxy(info)
	if err == nil {
		t.Fatal("expected error when no proxy available")
	}
}

func TestSelectProxy_EnabledButMissingHost(t *testing.T) {
	// HTTPS enabled but no host → should fall through to next
	info := ProxyInfo{
		HTTPSEnabled: true, HTTPSProxy: "", HTTPSPort: 7897,
		HTTPEnabled: true, HTTPProxy: "127.0.0.1", HTTPPort: 8080,
	}
	got, err := SelectProxy(info)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "http://127.0.0.1:8080" {
		t.Errorf("got %q, want %q", got, "http://127.0.0.1:8080")
	}
}

func TestSelectProxy_EnabledButZeroPort(t *testing.T) {
	// HTTPS enabled but port 0 → should fall through
	info := ProxyInfo{
		HTTPSEnabled: true, HTTPSProxy: "127.0.0.1", HTTPSPort: 0,
		SOCKSEnabled: true, SOCKSProxy: "127.0.0.1", SOCKSPort: 1080,
	}
	got, err := SelectProxy(info)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "socks5://127.0.0.1:1080" {
		t.Errorf("got %q, want %q", got, "socks5://127.0.0.1:1080")
	}
}
