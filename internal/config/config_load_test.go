package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeYAMLFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoad_CLIOnly(t *testing.T) {
	flags := CLIFlags{
		Proxy:       "http://127.0.0.1:7897",
		Domains:     "api.example.com,cdn.example.com",
		Ports:       "443,8080",
		DNSListen:   "127.0.0.1:5353",
		Verbose:     true,
		Command:     "curl",
		CommandArgs: []string{"-v", "https://api.example.com"},
	}

	cfg, err := Load(flags)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.ProxyURL != "http://127.0.0.1:7897" {
		t.Errorf("ProxyURL = %q, want %q", cfg.ProxyURL, "http://127.0.0.1:7897")
	}
	if len(cfg.Domains) != 2 || cfg.Domains[0] != "api.example.com" || cfg.Domains[1] != "cdn.example.com" {
		t.Errorf("Domains = %v, want [api.example.com cdn.example.com]", cfg.Domains)
	}
	if len(cfg.Ports) != 2 || cfg.Ports[0] != 443 || cfg.Ports[1] != 8080 {
		t.Errorf("Ports = %v, want [443 8080]", cfg.Ports)
	}
	if cfg.DNSListen != "127.0.0.1:5353" {
		t.Errorf("DNSListen = %q, want %q", cfg.DNSListen, "127.0.0.1:5353")
	}
	if !cfg.Verbose {
		t.Error("Verbose = false, want true")
	}
	if cfg.Command != "curl" {
		t.Errorf("Command = %q, want %q", cfg.Command, "curl")
	}
}

func TestLoad_YAMLOnly(t *testing.T) {
	yamlContent := `
proxy: socks5://10.0.0.1:1080
domains:
  - "a.example.com"
  - "b.example.com"
ports: [80, 443, 8443]
dns:
  listen: "127.0.0.1:9053"
`
	path := writeYAMLFile(t, yamlContent)

	flags := CLIFlags{
		Config:  path,
		Command: "wget",
	}

	cfg, err := Load(flags)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.ProxyURL != "socks5://10.0.0.1:1080" {
		t.Errorf("ProxyURL = %q, want %q", cfg.ProxyURL, "socks5://10.0.0.1:1080")
	}
	if len(cfg.Domains) != 2 || cfg.Domains[0] != "a.example.com" {
		t.Errorf("Domains = %v, want [a.example.com b.example.com]", cfg.Domains)
	}
	if len(cfg.Ports) != 3 || cfg.Ports[0] != 80 || cfg.Ports[2] != 8443 {
		t.Errorf("Ports = %v, want [80 443 8443]", cfg.Ports)
	}
	if cfg.DNSListen != "127.0.0.1:9053" {
		t.Errorf("DNSListen = %q, want %q", cfg.DNSListen, "127.0.0.1:9053")
	}
}

func TestLoad_CLIOverridesYAML(t *testing.T) {
	yamlContent := `
proxy: socks5://10.0.0.1:1080
domains:
  - "yaml.example.com"
ports: [8080]
dns:
  listen: "127.0.0.1:9053"
`
	path := writeYAMLFile(t, yamlContent)

	flags := CLIFlags{
		Config:    path,
		Proxy:     "http://127.0.0.1:7897",
		Domains:   "cli.example.com",
		Ports:     "443",
		DNSListen: "127.0.0.1:5353",
		Command:   "test-cmd",
	}

	cfg, err := Load(flags)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.ProxyURL != "http://127.0.0.1:7897" {
		t.Errorf("ProxyURL = %q, want CLI value", cfg.ProxyURL)
	}
	if len(cfg.Domains) != 1 || cfg.Domains[0] != "cli.example.com" {
		t.Errorf("Domains = %v, want [cli.example.com]", cfg.Domains)
	}
	if len(cfg.Ports) != 1 || cfg.Ports[0] != 443 {
		t.Errorf("Ports = %v, want [443]", cfg.Ports)
	}
	if cfg.DNSListen != "127.0.0.1:5353" {
		t.Errorf("DNSListen = %q, want CLI value", cfg.DNSListen)
	}
}

func TestLoad_DefaultPorts(t *testing.T) {
	flags := CLIFlags{
		Domains: "example.com",
		Command: "test-cmd",
	}

	cfg, err := Load(flags)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cfg.Ports) != 2 || cfg.Ports[0] != 80 || cfg.Ports[1] != 443 {
		t.Errorf("Ports = %v, want default [80 443]", cfg.Ports)
	}
}

func TestLoad_DefaultDNSListen(t *testing.T) {
	flags := CLIFlags{
		Domains: "example.com",
		Command: "test-cmd",
	}

	cfg, err := Load(flags)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.DNSListen != "127.0.0.1:10053" {
		t.Errorf("DNSListen = %q, want default %q", cfg.DNSListen, "127.0.0.1:10053")
	}
}

func TestLoad_ErrorNoDomains(t *testing.T) {
	flags := CLIFlags{
		Command: "test-cmd",
	}

	_, err := Load(flags)
	if err == nil {
		t.Fatal("expected error for missing domains, got nil")
	}
}

func TestLoad_ErrorNoCommand(t *testing.T) {
	flags := CLIFlags{
		Domains: "example.com",
	}

	_, err := Load(flags)
	if err == nil {
		t.Fatal("expected error for missing command, got nil")
	}
}

func TestLoad_ErrorInvalidConfigFile(t *testing.T) {
	flags := CLIFlags{
		Config:  "/nonexistent/path/config.yaml",
		Domains: "example.com",
		Command: "test-cmd",
	}

	_, err := Load(flags)
	if err == nil {
		t.Fatal("expected error for nonexistent config file, got nil")
	}
}

func TestLoad_ErrorInvalidYAML(t *testing.T) {
	path := writeYAMLFile(t, "{{invalid yaml")

	flags := CLIFlags{
		Config:  path,
		Domains: "example.com",
		Command: "test-cmd",
	}

	_, err := Load(flags)
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

func TestLoad_ErrorInvalidPorts(t *testing.T) {
	flags := CLIFlags{
		Domains: "example.com",
		Ports:   "abc",
		Command: "test-cmd",
	}

	_, err := Load(flags)
	if err == nil {
		t.Fatal("expected error for invalid ports, got nil")
	}
}

func TestLoad_YAMLPortsFallbackToDefault(t *testing.T) {
	yamlContent := `
domains:
  - "example.com"
`
	path := writeYAMLFile(t, yamlContent)

	flags := CLIFlags{
		Config:  path,
		Command: "test-cmd",
	}

	cfg, err := Load(flags)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cfg.Ports) != 2 || cfg.Ports[0] != 80 || cfg.Ports[1] != 443 {
		t.Errorf("Ports = %v, want default [80 443]", cfg.Ports)
	}
}

func TestLoad_CommandArgs(t *testing.T) {
	flags := CLIFlags{
		Domains:     "example.com",
		Command:     "curl",
		CommandArgs: []string{"-v", "--proxy", "none", "https://example.com"},
	}

	cfg, err := Load(flags)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Command != "curl" {
		t.Errorf("Command = %q, want %q", cfg.Command, "curl")
	}
	if len(cfg.CommandArgs) != 4 || cfg.CommandArgs[0] != "-v" {
		t.Errorf("CommandArgs = %v, want [-v --proxy none https://example.com]", cfg.CommandArgs)
	}
}
