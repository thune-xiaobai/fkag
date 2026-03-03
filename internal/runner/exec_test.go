package runner

import (
	"os"
	"syscall"
	"testing"
	"time"
)

func TestNewRunner(t *testing.T) {
	r := NewRunner("echo", []string{"hello"})
	if r.command != "echo" {
		t.Errorf("expected command 'echo', got %q", r.command)
	}
	if len(r.args) != 1 || r.args[0] != "hello" {
		t.Errorf("expected args [hello], got %v", r.args)
	}
}

func TestStartAndWait_Success(t *testing.T) {
	r := NewRunner("echo", []string{"hello"})
	if err := r.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	code, err := r.Wait()
	if err != nil {
		t.Fatalf("Wait returned error: %v", err)
	}
	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}
}

func TestStartAndWait_NonZeroExit(t *testing.T) {
	r := NewRunner("sh", []string{"-c", "exit 42"})
	if err := r.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	code, err := r.Wait()
	if err != nil {
		t.Fatalf("Wait returned error: %v", err)
	}
	if code != 42 {
		t.Errorf("expected exit code 42, got %d", code)
	}
}

func TestStartAndWait_ExitCode1(t *testing.T) {
	r := NewRunner("sh", []string{"-c", "exit 1"})
	if err := r.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	code, err := r.Wait()
	if err != nil {
		t.Fatalf("Wait returned error: %v", err)
	}
	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}
}

func TestStartAndWait_ExitCode255(t *testing.T) {
	r := NewRunner("sh", []string{"-c", "exit 255"})
	if err := r.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	code, err := r.Wait()
	if err != nil {
		t.Fatalf("Wait returned error: %v", err)
	}
	if code != 255 {
		t.Errorf("expected exit code 255, got %d", code)
	}
}

func TestSignal(t *testing.T) {
	r := NewRunner("sleep", []string{"10"})
	if err := r.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	// Give the process a moment to start
	time.Sleep(50 * time.Millisecond)

	if err := r.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("Signal failed: %v", err)
	}
	code, _ := r.Wait()
	// Process killed by signal typically returns -1 or non-zero
	if code == 0 {
		t.Error("expected non-zero exit code after SIGTERM")
	}
}

func TestSignal_NotStarted(t *testing.T) {
	r := NewRunner("echo", []string{"hello"})
	err := r.Signal(os.Kill)
	if err == nil {
		t.Error("expected error when signaling unstarted process")
	}
}

func TestStart_InvalidCommand(t *testing.T) {
	r := NewRunner("nonexistent_command_xyz", nil)
	err := r.Start()
	if err == nil {
		t.Error("expected error for invalid command")
	}
}
