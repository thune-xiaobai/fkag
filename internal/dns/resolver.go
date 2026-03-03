package dns

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/user/fkag/internal/vip"
)

// FileSystem abstracts file system operations for testability.
type FileSystem interface {
	MkdirAll(path string, perm os.FileMode) error
	WriteFile(name string, data []byte, perm os.FileMode) error
	Remove(name string) error
	Glob(pattern string) ([]string, error)
}

// OSFileSystem implements FileSystem using real OS calls.
type OSFileSystem struct{}

func (OSFileSystem) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

func (OSFileSystem) WriteFile(name string, data []byte, perm os.FileMode) error {
	return os.WriteFile(name, data, perm)
}

func (OSFileSystem) Remove(name string) error {
	return os.Remove(name)
}

func (OSFileSystem) Glob(pattern string) ([]string, error) {
	return filepath.Glob(pattern)
}

// ResolverManager manages /etc/resolver/ files for macOS DNS resolution.
type ResolverManager struct {
	domains  []string
	dnsPort  int
	created  []string // file paths created, for cleanup
	baseDir  string   // resolver directory, default "/etc/resolver"
	fs       FileSystem
	executor vip.CommandExecutor
}

// NewResolverManager creates a resolver manager with default settings.
func NewResolverManager(domains []string, dnsPort int) *ResolverManager {
	return &ResolverManager{
		domains:  domains,
		dnsPort:  dnsPort,
		baseDir:  "/etc/resolver",
		fs:       OSFileSystem{},
		executor: vip.RealExecutor{},
	}
}

// NewResolverManagerWithDeps creates a resolver manager with custom dependencies (for testing).
func NewResolverManagerWithDeps(domains []string, dnsPort int, baseDir string, fs FileSystem, executor vip.CommandExecutor) *ResolverManager {
	return &ResolverManager{
		domains:  domains,
		dnsPort:  dnsPort,
		baseDir:  baseDir,
		fs:       fs,
		executor: executor,
	}
}

// Setup creates resolver files and flushes the DNS cache.
func (r *ResolverManager) Setup() error {
	if err := r.fs.MkdirAll(r.baseDir, 0755); err != nil {
		return fmt.Errorf("failed to create resolver directory %s: %w", r.baseDir, err)
	}

	content := fmt.Sprintf("nameserver 127.0.0.1\nport %d\n", r.dnsPort)

	for _, domain := range r.domains {
		path := filepath.Join(r.baseDir, domain)
		if err := r.fs.WriteFile(path, []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to write resolver file %s: %w", path, err)
		}
		r.created = append(r.created, path)
	}

	return FlushDNSCacheWith(r.executor)
}

// Teardown removes all created resolver files and flushes the DNS cache.
func (r *ResolverManager) Teardown() error {
	var firstErr error
	for _, path := range r.created {
		if err := r.fs.Remove(path); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("failed to remove resolver file %s: %w", path, err)
			}
		}
	}
	r.created = nil

	if err := FlushDNSCacheWith(r.executor); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

// CleanStale removes resolver files matching target domains that may be left over from a previous run.
func (r *ResolverManager) CleanStale() error {
	matches, err := r.fs.Glob(filepath.Join(r.baseDir, "*"))
	if err != nil {
		return fmt.Errorf("failed to glob resolver directory: %w", err)
	}

	domainSet := make(map[string]struct{}, len(r.domains))
	for _, d := range r.domains {
		domainSet[d] = struct{}{}
	}

	var firstErr error
	for _, path := range matches {
		name := filepath.Base(path)
		if _, ok := domainSet[name]; ok {
			if err := r.fs.Remove(path); err != nil && firstErr == nil {
				firstErr = fmt.Errorf("failed to remove stale resolver file %s: %w", path, err)
			}
		}
	}
	return firstErr
}

// FlushDNSCache executes dscacheutil -flushcache and killall -HUP mDNSResponder.
func FlushDNSCache() error {
	return FlushDNSCacheWith(vip.RealExecutor{})
}

// FlushDNSCacheWith flushes the DNS cache using the provided executor.
func FlushDNSCacheWith(executor vip.CommandExecutor) error {
	if err := executor.Run("dscacheutil", "-flushcache"); err != nil {
		return fmt.Errorf("failed to flush DNS cache (dscacheutil): %w", err)
	}
	if err := executor.Run("killall", "-HUP", "mDNSResponder"); err != nil {
		return fmt.Errorf("failed to flush DNS cache (mDNSResponder): %w", err)
	}
	return nil
}

// Created returns the list of resolver file paths that have been created.
func (r *ResolverManager) Created() []string {
	out := make([]string, len(r.created))
	copy(out, r.created)
	return out
}
