// Package pf manages macOS pf firewall anchor rules for port redirection.
package pf

import (
	"bytes"
	"fmt"
	"net"
	"os/exec"
	"strings"
)

// PFController abstracts pfctl operations for testability.
type PFController interface {
	LoadRules(anchorName string, rules string) error
	UnloadAnchor(anchorName string) error
	EnablePF() error
}

// RealPFController executes real pfctl commands.
type RealPFController struct{}

// LoadRules pipes rules to pfctl -a <anchor> -f - via stdin.
func (RealPFController) LoadRules(anchorName string, rules string) error {
	cmd := exec.Command("pfctl", "-a", anchorName, "-f", "-")
	cmd.Stdin = strings.NewReader(rules)
	return cmd.Run()
}

// UnloadAnchor runs pfctl -a <anchor> -F all.
func (RealPFController) UnloadAnchor(anchorName string) error {
	return exec.Command("pfctl", "-a", anchorName, "-F", "all").Run()
}

// EnablePF runs pfctl -e to ensure pf is enabled.
// On macOS, if pf is already enabled, pfctl -e exits with status 1 and
// prints "pf already enabled" — we treat that as success.
func (RealPFController) EnablePF() error {
	cmd := exec.Command("pfctl", "-e")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		// "pf already enabled" is not an error
		if strings.Contains(stderr.String(), "already enabled") {
			return nil
		}
		return err
	}
	return nil
}

// Anchor manages pf firewall anchor rules for port redirection.
type Anchor struct {
	name       string // anchor name, fixed to "com.fkag"
	rules      []string
	controller PFController
}

// NewAnchor generates pf rdr rules for each VIP and port.
// Rule format: rdr on lo0 inet proto tcp from any to <vip> port <port> -> <vip> port <port+10000>
func NewAnchor(mappings map[string]net.IP, ports []int) *Anchor {
	return newAnchor(mappings, ports, RealPFController{})
}

// NewAnchorWithController creates an Anchor with a custom PFController (for testing).
func NewAnchorWithController(mappings map[string]net.IP, ports []int, ctrl PFController) *Anchor {
	return newAnchor(mappings, ports, ctrl)
}

func newAnchor(mappings map[string]net.IP, ports []int, ctrl PFController) *Anchor {
	var rules []string
	for _, ip := range mappings {
		for _, port := range ports {
			rule := fmt.Sprintf(
				"rdr on lo0 inet proto tcp from any to %s port %d -> %s port %d",
				ip.String(), port, ip.String(), port+10000,
			)
			rules = append(rules, rule)
		}
	}
	return &Anchor{
		name:       "com.fkag",
		rules:      rules,
		controller: ctrl,
	}
}

// Load pipes the generated rules to pfctl and ensures pf is enabled.
func (a *Anchor) Load() error {
	if len(a.rules) == 0 {
		return nil
	}
	rulesText := strings.Join(a.rules, "\n") + "\n"
	if err := a.controller.LoadRules(a.name, rulesText); err != nil {
		return fmt.Errorf("failed to load pf anchor %s: %w", a.name, err)
	}
	if err := a.controller.EnablePF(); err != nil {
		return fmt.Errorf("failed to enable pf: %w", err)
	}
	return nil
}

// Unload flushes all rules from the pf anchor.
func (a *Anchor) Unload() error {
	if err := a.controller.UnloadAnchor(a.name); err != nil {
		return fmt.Errorf("failed to unload pf anchor %s: %w", a.name, err)
	}
	return nil
}

// Rules returns the generated pf rdr rules list.
func (a *Anchor) Rules() []string {
	out := make([]string, len(a.rules))
	copy(out, a.rules)
	return out
}
