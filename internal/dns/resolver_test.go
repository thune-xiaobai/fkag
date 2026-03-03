package dns

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- Mock FileSystem ---

type mockFS struct {
	dirs     map[string]bool
	files    map[string][]byte
	globFunc func(pattern string) ([]string, error)

	mkdirErr   error
	writeErr   error
	removeErr  error
	removeErrs map[string]error // per-path remove errors
}

func newMockFS() *mockFS {
	return &mockFS{
		dirs:  make(map[string]bool),
		files: make(map[string][]byte),
	}
}

func (m *mockFS) MkdirAll(path string, perm os.FileMode) error {
	if m.mkdirErr != nil {
		return m.mkdirErr
	}
	m.dirs[path] = true
	return nil
}

func (m *mockFS) WriteFile(name string, data []byte, perm os.FileMode) error {
	if m.writeErr != nil {
		return m.writeErr
	}
	m.files[name] = append([]byte(nil), data...)
	return nil
}

func (m *mockFS) Remove(name string) error {
	if m.removeErrs != nil {
		if err, ok := m.removeErrs[name]; ok {
			return err
		}
	}
	if m.removeErr != nil {
		return m.removeErr
	}
	delete(m.files, name)
	return nil
}

func (m *mockFS) Glob(pattern string) ([]string, error) {
	if m.globFunc != nil {
		return m.globFunc(pattern)
	}
	// Simple glob: return all files whose directory matches the pattern's dir
	dir := filepath.Dir(pattern)
	var result []string
	for path := range m.files {
		if filepath.Dir(path) == dir {
			result = append(result, path)
		}
	}
	return result, nil
}

// --- Mock CommandExecutor ---

type mockExecutor struct {
	calls [][]string
	err   error
}

func (m *mockExecutor) Run(name string, args ...string) error {
	call := append([]string{name}, args...)
	m.calls = append(m.calls, call)
	return m.err
}

// --- Tests ---

func TestNewResolverManager(t *testing.T) {
	domains := []string{"a.com", "b.com"}
	rm := NewResolverManager(domains, 10053)
	if rm == nil {
		t.Fatal("expected non-nil ResolverManager")
	}
	if len(rm.domains) != 2 {
		t.Fatalf("expected 2 domains, got %d", len(rm.domains))
	}
	if rm.dnsPort != 10053 {
		t.Fatalf("expected port 10053, got %d", rm.dnsPort)
	}
	if rm.baseDir != "/etc/resolver" {
		t.Fatalf("expected default baseDir /etc/resolver, got %s", rm.baseDir)
	}
}

func TestSetup_CreatesDirectoryAndFiles(t *testing.T) {
	fs := newMockFS()
	exec := &mockExecutor{}
	rm := NewResolverManagerWithDeps([]string{"a.com", "b.com"}, 10053, "/tmp/resolver", fs, exec)

	if err := rm.Setup(); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	// Directory created
	if !fs.dirs["/tmp/resolver"] {
		t.Fatal("expected resolver directory to be created")
	}

	// Files created with correct content
	expected := "nameserver 127.0.0.1\nport 10053\n"
	for _, domain := range []string{"a.com", "b.com"} {
		path := filepath.Join("/tmp/resolver", domain)
		data, ok := fs.files[path]
		if !ok {
			t.Fatalf("expected file %s to be created", path)
		}
		if string(data) != expected {
			t.Fatalf("file %s: expected %q, got %q", path, expected, string(data))
		}
	}

	// Created list tracked
	created := rm.Created()
	if len(created) != 2 {
		t.Fatalf("expected 2 created files, got %d", len(created))
	}

	// DNS cache flushed (2 commands)
	if len(exec.calls) != 2 {
		t.Fatalf("expected 2 executor calls, got %d", len(exec.calls))
	}
	if exec.calls[0][0] != "dscacheutil" {
		t.Fatalf("expected dscacheutil call, got %v", exec.calls[0])
	}
	if exec.calls[1][0] != "killall" {
		t.Fatalf("expected killall call, got %v", exec.calls[1])
	}
}

func TestSetup_MkdirError(t *testing.T) {
	fs := newMockFS()
	fs.mkdirErr = fmt.Errorf("permission denied")
	exec := &mockExecutor{}
	rm := NewResolverManagerWithDeps([]string{"a.com"}, 10053, "/tmp/resolver", fs, exec)

	err := rm.Setup()
	if err == nil {
		t.Fatal("expected error from Setup")
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("expected permission denied error, got: %v", err)
	}
}

func TestSetup_WriteFileError(t *testing.T) {
	fs := newMockFS()
	fs.writeErr = fmt.Errorf("disk full")
	exec := &mockExecutor{}
	rm := NewResolverManagerWithDeps([]string{"a.com"}, 10053, "/tmp/resolver", fs, exec)

	err := rm.Setup()
	if err == nil {
		t.Fatal("expected error from Setup")
	}
	if !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("expected disk full error, got: %v", err)
	}
}

func TestSetup_FlushCacheError(t *testing.T) {
	fs := newMockFS()
	exec := &mockExecutor{err: fmt.Errorf("flush failed")}
	rm := NewResolverManagerWithDeps([]string{"a.com"}, 10053, "/tmp/resolver", fs, exec)

	err := rm.Setup()
	if err == nil {
		t.Fatal("expected error from Setup when flush fails")
	}
	if !strings.Contains(err.Error(), "flush failed") {
		t.Fatalf("expected flush failed error, got: %v", err)
	}
}

func TestTeardown_RemovesFilesAndFlushes(t *testing.T) {
	fs := newMockFS()
	exec := &mockExecutor{}
	rm := NewResolverManagerWithDeps([]string{"a.com", "b.com"}, 10053, "/tmp/resolver", fs, exec)

	// Setup first
	if err := rm.Setup(); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	exec.calls = nil // reset

	// Teardown
	if err := rm.Teardown(); err != nil {
		t.Fatalf("Teardown failed: %v", err)
	}

	// Files removed
	if len(fs.files) != 0 {
		t.Fatalf("expected all files removed, got %d remaining", len(fs.files))
	}

	// Created list cleared
	if len(rm.Created()) != 0 {
		t.Fatalf("expected created list to be empty after teardown")
	}

	// DNS cache flushed
	if len(exec.calls) != 2 {
		t.Fatalf("expected 2 executor calls for flush, got %d", len(exec.calls))
	}
}

func TestTeardown_RemoveError_ContinuesAndFlushes(t *testing.T) {
	fs := newMockFS()
	exec := &mockExecutor{}
	rm := NewResolverManagerWithDeps([]string{"a.com", "b.com"}, 10053, "/tmp/resolver", fs, exec)

	if err := rm.Setup(); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	exec.calls = nil

	// Make remove fail for first file only
	fs.removeErrs = map[string]error{
		filepath.Join("/tmp/resolver", "a.com"): fmt.Errorf("busy"),
	}

	err := rm.Teardown()
	if err == nil {
		t.Fatal("expected error from Teardown")
	}
	if !strings.Contains(err.Error(), "busy") {
		t.Fatalf("expected busy error, got: %v", err)
	}

	// b.com should still be removed
	if _, ok := fs.files[filepath.Join("/tmp/resolver", "b.com")]; ok {
		t.Fatal("expected b.com to be removed despite a.com failure")
	}

	// Flush should still be called
	if len(exec.calls) != 2 {
		t.Fatalf("expected flush calls even after remove error, got %d", len(exec.calls))
	}
}

func TestCleanStale_RemovesMatchingFiles(t *testing.T) {
	fs := newMockFS()
	exec := &mockExecutor{}

	// Pre-populate stale files
	fs.files["/tmp/resolver/a.com"] = []byte("old")
	fs.files["/tmp/resolver/b.com"] = []byte("old")
	fs.files["/tmp/resolver/other.com"] = []byte("unrelated")

	rm := NewResolverManagerWithDeps([]string{"a.com", "b.com"}, 10053, "/tmp/resolver", fs, exec)

	if err := rm.CleanStale(); err != nil {
		t.Fatalf("CleanStale failed: %v", err)
	}

	// Target domain files removed
	if _, ok := fs.files["/tmp/resolver/a.com"]; ok {
		t.Fatal("expected a.com to be removed")
	}
	if _, ok := fs.files["/tmp/resolver/b.com"]; ok {
		t.Fatal("expected b.com to be removed")
	}

	// Unrelated file preserved
	if _, ok := fs.files["/tmp/resolver/other.com"]; !ok {
		t.Fatal("expected other.com to be preserved")
	}
}

func TestCleanStale_GlobError(t *testing.T) {
	fs := newMockFS()
	fs.globFunc = func(pattern string) ([]string, error) {
		return nil, fmt.Errorf("glob error")
	}
	exec := &mockExecutor{}
	rm := NewResolverManagerWithDeps([]string{"a.com"}, 10053, "/tmp/resolver", fs, exec)

	err := rm.CleanStale()
	if err == nil {
		t.Fatal("expected error from CleanStale")
	}
	if !strings.Contains(err.Error(), "glob error") {
		t.Fatalf("expected glob error, got: %v", err)
	}
}

func TestCleanStale_NoMatchingFiles(t *testing.T) {
	fs := newMockFS()
	fs.files["/tmp/resolver/other.com"] = []byte("data")
	exec := &mockExecutor{}
	rm := NewResolverManagerWithDeps([]string{"a.com"}, 10053, "/tmp/resolver", fs, exec)

	if err := rm.CleanStale(); err != nil {
		t.Fatalf("CleanStale should succeed with no matching files: %v", err)
	}

	// Unrelated file preserved
	if _, ok := fs.files["/tmp/resolver/other.com"]; !ok {
		t.Fatal("expected other.com to be preserved")
	}
}

func TestFlushDNSCacheWith_Success(t *testing.T) {
	exec := &mockExecutor{}
	if err := FlushDNSCacheWith(exec); err != nil {
		t.Fatalf("FlushDNSCacheWith failed: %v", err)
	}
	if len(exec.calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(exec.calls))
	}
	// First: dscacheutil -flushcache
	if exec.calls[0][0] != "dscacheutil" || exec.calls[0][1] != "-flushcache" {
		t.Fatalf("unexpected first call: %v", exec.calls[0])
	}
	// Second: killall -HUP mDNSResponder
	if exec.calls[1][0] != "killall" || exec.calls[1][1] != "-HUP" || exec.calls[1][2] != "mDNSResponder" {
		t.Fatalf("unexpected second call: %v", exec.calls[1])
	}
}

func TestFlushDNSCacheWith_FirstCommandFails(t *testing.T) {
	exec := &mockExecutor{err: fmt.Errorf("command not found")}
	err := FlushDNSCacheWith(exec)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "dscacheutil") {
		t.Fatalf("expected dscacheutil error, got: %v", err)
	}
}

func TestSetup_DifferentPort(t *testing.T) {
	fs := newMockFS()
	exec := &mockExecutor{}
	rm := NewResolverManagerWithDeps([]string{"x.com"}, 5353, "/tmp/resolver", fs, exec)

	if err := rm.Setup(); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	expected := "nameserver 127.0.0.1\nport 5353\n"
	data := fs.files[filepath.Join("/tmp/resolver", "x.com")]
	if string(data) != expected {
		t.Fatalf("expected %q, got %q", expected, string(data))
	}
}

func TestSetup_EmptyDomains(t *testing.T) {
	fs := newMockFS()
	exec := &mockExecutor{}
	rm := NewResolverManagerWithDeps([]string{}, 10053, "/tmp/resolver", fs, exec)

	if err := rm.Setup(); err != nil {
		t.Fatalf("Setup with empty domains should succeed: %v", err)
	}

	// Directory still created
	if !fs.dirs["/tmp/resolver"] {
		t.Fatal("expected directory to be created even with no domains")
	}

	// No files created
	if len(fs.files) != 0 {
		t.Fatalf("expected no files, got %d", len(fs.files))
	}

	// Flush still called
	if len(exec.calls) != 2 {
		t.Fatalf("expected flush calls, got %d", len(exec.calls))
	}
}
