package pf

import (
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"testing"
)

// mockPFController records calls and can return configured errors.
type mockPFController struct {
	loadCalls   []loadCall
	unloadCalls []string
	enableCalls int
	loadErr     error
	unloadErr   error
	enableErr   error
}

type loadCall struct {
	anchorName string
	rules      string
}

func (m *mockPFController) LoadRules(anchorName string, rules string) error {
	m.loadCalls = append(m.loadCalls, loadCall{anchorName, rules})
	return m.loadErr
}

func (m *mockPFController) UnloadAnchor(anchorName string) error {
	m.unloadCalls = append(m.unloadCalls, anchorName)
	return m.unloadErr
}

func (m *mockPFController) EnablePF() error {
	m.enableCalls++
	return m.enableErr
}

func TestNewAnchor_GeneratesCorrectRules(t *testing.T) {
	mappings := map[string]net.IP{
		"api.example.com": net.IPv4(127, 0, 0, 2),
	}
	ports := []int{80, 443}

	a := NewAnchorWithController(mappings, ports, &mockPFController{})
	rules := a.Rules()

	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}

	sort.Strings(rules)
	expected := []string{
		"rdr on lo0 inet proto tcp from any to 127.0.0.2 port 443 -> 127.0.0.2 port 10443",
		"rdr on lo0 inet proto tcp from any to 127.0.0.2 port 80 -> 127.0.0.2 port 10080",
	}
	sort.Strings(expected)

	for i, rule := range rules {
		if rule != expected[i] {
			t.Errorf("rule %d: got %q, want %q", i, rule, expected[i])
		}
	}
}

func TestNewAnchor_MultipleVIPs(t *testing.T) {
	mappings := map[string]net.IP{
		"a.example.com": net.IPv4(127, 0, 0, 2),
		"b.example.com": net.IPv4(127, 0, 0, 3),
	}
	ports := []int{443}

	a := NewAnchorWithController(mappings, ports, &mockPFController{})
	rules := a.Rules()

	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}

	sort.Strings(rules)
	expected := []string{
		"rdr on lo0 inet proto tcp from any to 127.0.0.2 port 443 -> 127.0.0.2 port 10443",
		"rdr on lo0 inet proto tcp from any to 127.0.0.3 port 443 -> 127.0.0.3 port 10443",
	}
	sort.Strings(expected)

	for i, rule := range rules {
		if rule != expected[i] {
			t.Errorf("rule %d: got %q, want %q", i, rule, expected[i])
		}
	}
}

func TestNewAnchor_EmptyMappings(t *testing.T) {
	a := NewAnchorWithController(map[string]net.IP{}, []int{80}, &mockPFController{})
	if len(a.Rules()) != 0 {
		t.Errorf("expected 0 rules for empty mappings, got %d", len(a.Rules()))
	}
}

func TestNewAnchor_EmptyPorts(t *testing.T) {
	mappings := map[string]net.IP{
		"a.example.com": net.IPv4(127, 0, 0, 2),
	}
	a := NewAnchorWithController(mappings, []int{}, &mockPFController{})
	if len(a.Rules()) != 0 {
		t.Errorf("expected 0 rules for empty ports, got %d", len(a.Rules()))
	}
}

func TestLoad_Success(t *testing.T) {
	ctrl := &mockPFController{}
	mappings := map[string]net.IP{
		"api.example.com": net.IPv4(127, 0, 0, 2),
	}
	a := NewAnchorWithController(mappings, []int{443}, ctrl)

	if err := a.Load(); err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if len(ctrl.loadCalls) != 1 {
		t.Fatalf("expected 1 LoadRules call, got %d", len(ctrl.loadCalls))
	}
	if ctrl.loadCalls[0].anchorName != "com.fkag" {
		t.Errorf("anchor name: got %q, want %q", ctrl.loadCalls[0].anchorName, "com.fkag")
	}
	expectedRule := "rdr on lo0 inet proto tcp from any to 127.0.0.2 port 443 -> 127.0.0.2 port 10443"
	if !strings.Contains(ctrl.loadCalls[0].rules, expectedRule) {
		t.Errorf("rules should contain %q, got %q", expectedRule, ctrl.loadCalls[0].rules)
	}
	if ctrl.enableCalls != 1 {
		t.Errorf("expected 1 EnablePF call, got %d", ctrl.enableCalls)
	}
}

func TestLoad_EmptyRules_Noop(t *testing.T) {
	ctrl := &mockPFController{}
	a := NewAnchorWithController(map[string]net.IP{}, []int{}, ctrl)

	if err := a.Load(); err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if len(ctrl.loadCalls) != 0 {
		t.Errorf("expected 0 LoadRules calls for empty rules, got %d", len(ctrl.loadCalls))
	}
	if ctrl.enableCalls != 0 {
		t.Errorf("expected 0 EnablePF calls for empty rules, got %d", ctrl.enableCalls)
	}
}

func TestLoad_LoadRulesError(t *testing.T) {
	ctrl := &mockPFController{loadErr: errors.New("pfctl failed")}
	mappings := map[string]net.IP{
		"api.example.com": net.IPv4(127, 0, 0, 2),
	}
	a := NewAnchorWithController(mappings, []int{443}, ctrl)

	err := a.Load()
	if err == nil {
		t.Fatal("expected error from Load()")
	}
	if !strings.Contains(err.Error(), "failed to load pf anchor") {
		t.Errorf("unexpected error message: %v", err)
	}
	// EnablePF should not be called if LoadRules fails
	if ctrl.enableCalls != 0 {
		t.Errorf("EnablePF should not be called after LoadRules failure, got %d calls", ctrl.enableCalls)
	}
}

func TestLoad_EnablePFError(t *testing.T) {
	ctrl := &mockPFController{enableErr: errors.New("enable failed")}
	mappings := map[string]net.IP{
		"api.example.com": net.IPv4(127, 0, 0, 2),
	}
	a := NewAnchorWithController(mappings, []int{443}, ctrl)

	err := a.Load()
	if err == nil {
		t.Fatal("expected error from Load()")
	}
	if !strings.Contains(err.Error(), "failed to enable pf") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestUnload_Success(t *testing.T) {
	ctrl := &mockPFController{}
	mappings := map[string]net.IP{
		"api.example.com": net.IPv4(127, 0, 0, 2),
	}
	a := NewAnchorWithController(mappings, []int{443}, ctrl)

	if err := a.Unload(); err != nil {
		t.Fatalf("Unload() returned error: %v", err)
	}
	if len(ctrl.unloadCalls) != 1 {
		t.Fatalf("expected 1 UnloadAnchor call, got %d", len(ctrl.unloadCalls))
	}
	if ctrl.unloadCalls[0] != "com.fkag" {
		t.Errorf("anchor name: got %q, want %q", ctrl.unloadCalls[0], "com.fkag")
	}
}

func TestUnload_Error(t *testing.T) {
	ctrl := &mockPFController{unloadErr: errors.New("unload failed")}
	mappings := map[string]net.IP{
		"api.example.com": net.IPv4(127, 0, 0, 2),
	}
	a := NewAnchorWithController(mappings, []int{443}, ctrl)

	err := a.Unload()
	if err == nil {
		t.Fatal("expected error from Unload()")
	}
	if !strings.Contains(err.Error(), "failed to unload pf anchor") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestRules_ReturnsCopy(t *testing.T) {
	mappings := map[string]net.IP{
		"api.example.com": net.IPv4(127, 0, 0, 2),
	}
	a := NewAnchorWithController(mappings, []int{443}, &mockPFController{})

	rules1 := a.Rules()
	rules2 := a.Rules()

	// Mutating the returned slice should not affect the anchor's internal state
	if len(rules1) > 0 {
		rules1[0] = "modified"
		if rules2[0] == "modified" {
			t.Error("Rules() should return a copy, not a reference to internal state")
		}
	}
}

func TestRuleFormat(t *testing.T) {
	mappings := map[string]net.IP{
		"test.example.com": net.IPv4(127, 0, 0, 5),
	}
	a := NewAnchorWithController(mappings, []int{8080}, &mockPFController{})
	rules := a.Rules()

	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}

	expected := "rdr on lo0 inet proto tcp from any to 127.0.0.5 port 8080 -> 127.0.0.5 port 18080"
	if rules[0] != expected {
		t.Errorf("got %q, want %q", rules[0], expected)
	}
}

func TestRuleCount_EqualsVIPsTimesPorts(t *testing.T) {
	mappings := map[string]net.IP{}
	for i := 0; i < 5; i++ {
		domain := fmt.Sprintf("d%d.example.com", i)
		mappings[domain] = net.IPv4(127, 0, 0, byte(2+i))
	}
	ports := []int{80, 443, 8080}

	a := NewAnchorWithController(mappings, ports, &mockPFController{})
	rules := a.Rules()

	expected := len(mappings) * len(ports)
	if len(rules) != expected {
		t.Errorf("expected %d rules (5 VIPs × 3 ports), got %d", expected, len(rules))
	}
}
