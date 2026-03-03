// Package vip manages Virtual IP allocation and loopback alias lifecycle.
package vip

import (
	"fmt"
	"net"
	"os/exec"
)

// CommandExecutor abstracts system command execution for testability.
type CommandExecutor interface {
	Run(name string, args ...string) error
}

// RealExecutor executes real system commands.
type RealExecutor struct{}

// Run executes the named command with the given arguments.
func (RealExecutor) Run(name string, args ...string) error {
	return exec.Command(name, args...).Run()
}

// Pool manages domain-to-Virtual IP mappings and loopback alias lifecycle.
type Pool struct {
	forward  map[string]net.IP // domain -> virtual IP
	reverse  map[string]string // IP string -> domain
	domains  []string          // ordered domain list for deterministic iteration
	executor CommandExecutor
}

// NewPool allocates Virtual IPs for the given domains starting from 127.0.0.2.
func NewPool(domains []string) *Pool {
	return newPool(domains, RealExecutor{})
}

// NewPoolWithExecutor creates a Pool with a custom CommandExecutor (for testing).
func NewPoolWithExecutor(domains []string, exec CommandExecutor) *Pool {
	return newPool(domains, exec)
}

func newPool(domains []string, executor CommandExecutor) *Pool {
	p := &Pool{
		forward:  make(map[string]net.IP, len(domains)),
		reverse:  make(map[string]string, len(domains)),
		domains:  domains,
		executor: executor,
	}
	for i, domain := range domains {
		ip := net.IPv4(127, 0, 0, byte(2+i)).To4()
		p.forward[domain] = ip
		p.reverse[ip.String()] = domain
	}
	return p
}

// Setup adds loopback aliases for all Virtual IPs (ifconfig lo0 alias <ip>).
// If any alias fails, already-added aliases are removed before returning the error.
func (p *Pool) Setup() error {
	var added []net.IP
	for _, domain := range p.domains {
		ip := p.forward[domain]
		if err := p.executor.Run("ifconfig", "lo0", "alias", ip.String()); err != nil {
			// Rollback already-added aliases
			for _, a := range added {
				_ = p.executor.Run("ifconfig", "lo0", "-alias", a.String())
			}
			return fmt.Errorf("failed to add loopback alias for %s (%s): %w", domain, ip, err)
		}
		added = append(added, ip)
	}
	return nil
}

// Teardown removes all loopback aliases (ifconfig lo0 -alias <ip>).
// Errors are collected but do not stop removal of remaining aliases.
func (p *Pool) Teardown() error {
	var firstErr error
	for _, domain := range p.domains {
		ip := p.forward[domain]
		if err := p.executor.Run("ifconfig", "lo0", "-alias", ip.String()); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("failed to remove loopback alias for %s (%s): %w", domain, ip, err)
			}
		}
	}
	return firstErr
}

// Lookup returns the Virtual IP for the given domain.
func (p *Pool) Lookup(domain string) (net.IP, bool) {
	ip, ok := p.forward[domain]
	return ip, ok
}

// ReverseLookup returns the domain for the given Virtual IP.
func (p *Pool) ReverseLookup(ip net.IP) (string, bool) {
	domain, ok := p.reverse[ip.String()]
	return domain, ok
}

// Mappings returns a read-only copy of all domain-to-IP mappings.
func (p *Pool) Mappings() map[string]net.IP {
	out := make(map[string]net.IP, len(p.forward))
	for d, ip := range p.forward {
		cp := make(net.IP, len(ip))
		copy(cp, ip)
		out[d] = cp
	}
	return out
}
