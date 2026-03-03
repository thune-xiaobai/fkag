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
	// GetNATRules returns the current top-level NAT/RDR anchor rules.
	GetNATRules() (string, error)
	// LoadMainRules loads top-level NAT/RDR rules (e.g. rdr-anchor references).
	LoadMainRules(rules string) error
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

// GetNATRules returns the current top-level NAT/RDR rules via pfctl -s nat.
func (RealPFController) GetNATRules() (string, error) {
	out, err := exec.Command("pfctl", "-s", "nat").Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// LoadMainRules loads top-level NAT/RDR rules via pfctl -Nf -.
// The -N flag tells pfctl to only load NAT rules (not filter rules),
// so existing filter rules are not flushed.
func (RealPFController) LoadMainRules(rules string) error {
	cmd := exec.Command("pfctl", "-Nf", "-")
	cmd.Stdin = strings.NewReader(rules)
	return cmd.Run()
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

// Load registers the rdr-anchor in the main pf ruleset, pipes the generated
// rules to the anchor, and ensures pf is enabled.
//
// macOS pf requires a top-level "rdr-anchor" reference for the engine to
// evaluate rdr rules inside a named anchor. Without this reference, the
// anchor's rules are silently ignored.
func (a *Anchor) Load() error {
	if len(a.rules) == 0 {
		return nil
	}

	// Step 1: Register rdr-anchor in the main NAT ruleset (if not already present).
	anchorRef := fmt.Sprintf(`rdr-anchor "%s"`, a.name)
	if err := a.ensureAnchorRef(anchorRef); err != nil {
		return fmt.Errorf("failed to register rdr-anchor reference: %w", err)
	}

	// Step 2: Load rules into the anchor.
	rulesText := strings.Join(a.rules, "\n") + "\n"
	if err := a.controller.LoadRules(a.name, rulesText); err != nil {
		return fmt.Errorf("failed to load pf anchor %s: %w", a.name, err)
	}

	// Step 3: Ensure pf is enabled.
	if err := a.controller.EnablePF(); err != nil {
		return fmt.Errorf("failed to enable pf: %w", err)
	}
	return nil
}

// ensureAnchorRef adds the rdr-anchor reference to the main NAT rules if missing.
func (a *Anchor) ensureAnchorRef(anchorRef string) error {
	current, err := a.controller.GetNATRules()
	if err != nil {
		// If we can't read current rules, try to load just our anchor ref.
		return a.controller.LoadMainRules(anchorRef + "\n")
	}

	// Check if our anchor reference already exists.
	for _, line := range strings.Split(current, "\n") {
		// pfctl -s nat outputs rdr-anchor with " all" suffix, e.g.:
		//   rdr-anchor "com.fkag" all
		if strings.Contains(line, fmt.Sprintf(`"%s"`, a.name)) && strings.HasPrefix(strings.TrimSpace(line), "rdr-anchor") {
			return nil // already registered
		}
	}

	// Collect existing rdr-anchor / nat-anchor lines and append ours.
	var lines []string
	for _, line := range strings.Split(current, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	lines = append(lines, anchorRef)
	return a.controller.LoadMainRules(strings.Join(lines, "\n") + "\n")
}

// Unload flushes all rules from the pf anchor and removes the rdr-anchor
// reference from the main NAT ruleset.
func (a *Anchor) Unload() error {
	// Step 1: Flush rules inside the anchor.
	if err := a.controller.UnloadAnchor(a.name); err != nil {
		return fmt.Errorf("failed to unload pf anchor %s: %w", a.name, err)
	}

	// Step 2: Remove our rdr-anchor reference from the main NAT rules.
	if err := a.removeAnchorRef(); err != nil {
		// Non-fatal: the anchor is already empty, so the dangling reference is harmless.
		return nil
	}
	return nil
}

// removeAnchorRef removes the rdr-anchor reference for this anchor from the main NAT rules.
func (a *Anchor) removeAnchorRef() error {
	current, err := a.controller.GetNATRules()
	if err != nil {
		return err
	}

	var remaining []string
	for _, line := range strings.Split(current, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// Skip our anchor reference line.
		if strings.Contains(line, fmt.Sprintf(`"%s"`, a.name)) && strings.HasPrefix(trimmed, "rdr-anchor") {
			continue
		}
		remaining = append(remaining, trimmed)
	}

	if len(remaining) == 0 {
		// No remaining rules — load an empty ruleset to clear.
		return a.controller.LoadMainRules("\n")
	}
	return a.controller.LoadMainRules(strings.Join(remaining, "\n") + "\n")
}

// Rules returns the generated pf rdr rules list.
func (a *Anchor) Rules() []string {
	out := make([]string, len(a.rules))
	copy(out, a.rules)
	return out
}
