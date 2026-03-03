package vip

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// mockCall records a single command invocation.
type mockCall struct {
	Name string
	Args []string
}

func (c mockCall) String() string {
	return c.Name + " " + strings.Join(c.Args, " ")
}

// mockExecutor records calls and can inject failures at specific invocations.
type mockExecutor struct {
	calls  []mockCall
	failAt map[int]error // call index -> error to return
}

func newMockExecutor() *mockExecutor {
	return &mockExecutor{failAt: make(map[int]error)}
}

func (m *mockExecutor) Run(name string, args ...string) error {
	idx := len(m.calls)
	m.calls = append(m.calls, mockCall{Name: name, Args: args})
	if err, ok := m.failAt[idx]; ok {
		return err
	}
	return nil
}

func TestSetup_AllSucceed(t *testing.T) {
	exec := newMockExecutor()
	p := NewPoolWithExecutor([]string{"a.com", "b.com"}, exec)

	if err := p.Setup(); err != nil {
		t.Fatalf("Setup() unexpected error: %v", err)
	}

	if len(exec.calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(exec.calls))
	}
	expect := []string{
		"ifconfig lo0 alias 127.0.0.2",
		"ifconfig lo0 alias 127.0.0.3",
	}
	for i, want := range expect {
		got := exec.calls[i].String()
		if got != want {
			t.Errorf("call[%d] = %q, want %q", i, got, want)
		}
	}
}

func TestSetup_FailRollsBack(t *testing.T) {
	exec := newMockExecutor()
	// Fail on the second alias (index 1)
	exec.failAt[1] = errors.New("alias failed")
	p := NewPoolWithExecutor([]string{"a.com", "b.com"}, exec)

	err := p.Setup()
	if err == nil {
		t.Fatal("Setup() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "b.com") {
		t.Errorf("error should mention b.com: %v", err)
	}

	// Expect: call 0 = alias a.com, call 1 = alias b.com (failed), call 2 = -alias a.com (rollback)
	if len(exec.calls) != 3 {
		t.Fatalf("expected 3 calls (2 alias + 1 rollback), got %d: %v", len(exec.calls), exec.calls)
	}
	rollback := exec.calls[2].String()
	if rollback != "ifconfig lo0 -alias 127.0.0.2" {
		t.Errorf("rollback call = %q, want ifconfig lo0 -alias 127.0.0.2", rollback)
	}
}

func TestSetup_FirstFailNoRollback(t *testing.T) {
	exec := newMockExecutor()
	exec.failAt[0] = errors.New("alias failed")
	p := NewPoolWithExecutor([]string{"a.com"}, exec)

	err := p.Setup()
	if err == nil {
		t.Fatal("Setup() expected error, got nil")
	}
	// Only 1 call (the failed alias), no rollback needed
	if len(exec.calls) != 1 {
		t.Fatalf("expected 1 call, got %d: %v", len(exec.calls), exec.calls)
	}
}

func TestSetup_EmptyDomains(t *testing.T) {
	exec := newMockExecutor()
	p := NewPoolWithExecutor([]string{}, exec)

	if err := p.Setup(); err != nil {
		t.Fatalf("Setup() on empty pool: %v", err)
	}
	if len(exec.calls) != 0 {
		t.Fatalf("expected 0 calls, got %d", len(exec.calls))
	}
}

func TestTeardown_AllSucceed(t *testing.T) {
	exec := newMockExecutor()
	p := NewPoolWithExecutor([]string{"a.com", "b.com"}, exec)

	if err := p.Teardown(); err != nil {
		t.Fatalf("Teardown() unexpected error: %v", err)
	}

	if len(exec.calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(exec.calls))
	}
	expect := []string{
		"ifconfig lo0 -alias 127.0.0.2",
		"ifconfig lo0 -alias 127.0.0.3",
	}
	for i, want := range expect {
		got := exec.calls[i].String()
		if got != want {
			t.Errorf("call[%d] = %q, want %q", i, got, want)
		}
	}
}

func TestTeardown_ContinuesOnError(t *testing.T) {
	exec := newMockExecutor()
	// Fail on the first -alias (index 0)
	exec.failAt[0] = errors.New("remove failed")
	p := NewPoolWithExecutor([]string{"a.com", "b.com"}, exec)

	err := p.Teardown()
	if err == nil {
		t.Fatal("Teardown() expected error, got nil")
	}
	// Both calls should still be made despite the first failure
	if len(exec.calls) != 2 {
		t.Fatalf("expected 2 calls (continue on error), got %d: %v", len(exec.calls), exec.calls)
	}
}

func TestTeardown_AllFail(t *testing.T) {
	exec := newMockExecutor()
	exec.failAt[0] = errors.New("fail 1")
	exec.failAt[1] = errors.New("fail 2")
	p := NewPoolWithExecutor([]string{"a.com", "b.com"}, exec)

	err := p.Teardown()
	if err == nil {
		t.Fatal("Teardown() expected error, got nil")
	}
	// Returns the first error
	if !strings.Contains(err.Error(), "a.com") {
		t.Errorf("error should mention a.com (first failure): %v", err)
	}
	// Both calls attempted
	if len(exec.calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(exec.calls))
	}
}

func TestTeardown_EmptyDomains(t *testing.T) {
	exec := newMockExecutor()
	p := NewPoolWithExecutor([]string{}, exec)

	if err := p.Teardown(); err != nil {
		t.Fatalf("Teardown() on empty pool: %v", err)
	}
	if len(exec.calls) != 0 {
		t.Fatalf("expected 0 calls, got %d", len(exec.calls))
	}
}

func TestSetup_FailMiddleRollsBackAll(t *testing.T) {
	exec := newMockExecutor()
	// 3 domains, fail on the third (index 2)
	exec.failAt[2] = errors.New("alias failed")
	p := NewPoolWithExecutor([]string{"a.com", "b.com", "c.com"}, exec)

	err := p.Setup()
	if err == nil {
		t.Fatal("Setup() expected error, got nil")
	}

	// Expect: 3 alias attempts + 2 rollback calls = 5 total
	if len(exec.calls) != 5 {
		for i, c := range exec.calls {
			fmt.Printf("  call[%d]: %s\n", i, c)
		}
		t.Fatalf("expected 5 calls, got %d", len(exec.calls))
	}
	// Rollback calls should remove a.com and b.com
	if exec.calls[3].String() != "ifconfig lo0 -alias 127.0.0.2" {
		t.Errorf("rollback[0] = %q", exec.calls[3])
	}
	if exec.calls[4].String() != "ifconfig lo0 -alias 127.0.0.3" {
		t.Errorf("rollback[1] = %q", exec.calls[4])
	}
}
