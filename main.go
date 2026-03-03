package main

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/user/fkag/internal/config"
	"github.com/user/fkag/internal/dns"
	"github.com/user/fkag/internal/pf"
	"github.com/user/fkag/internal/proxy"
	"github.com/user/fkag/internal/runner"
	"github.com/user/fkag/internal/vip"
)

func main() {
	// childExitCode is set by run() when the child process exits with a non-zero
	// code. We call os.Exit *after* rootCmd.Execute() returns so that all cleanup
	// defers inside run() have already been executed.
	childExitCode := 0

	rootCmd := &cobra.Command{
		Use:   "fkag [flags] -- <command> [args...]",
		Short: "macOS domain-level transparent proxy tool",
		Long: `fkag is a macOS domain-level transparent proxy CLI tool.
It wraps a target command, routing specified domains through an upstream proxy
(HTTP CONNECT or SOCKS5), and cleans up all system configuration on exit.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          func(cmd *cobra.Command, args []string) error { return run(cmd, args, &childExitCode) },
	}

	flags := rootCmd.Flags()
	flags.String("proxy", "", "upstream proxy URL (http://host:port or socks5://host:port)")
	flags.String("domain", "", "target domains, comma-separated")
	flags.String("port", "", "ports to proxy, comma-separated (default 80,443)")
	flags.String("config", "", "YAML config file path")
	flags.String("dns-listen", "", "local DNS listen address (default 127.0.0.1:10053)")
	flags.Bool("verbose", false, "enable verbose logging")

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	// All defers inside run() have now completed. Propagate child exit code.
	if childExitCode != 0 {
		os.Exit(childExitCode)
	}
}

func run(cmd *cobra.Command, args []string, childExitCode *int) error {
	// Check root privileges
	if os.Getuid() != 0 {
		return fmt.Errorf("fkag requires root privileges, please run with sudo")
	}

	// Build CLIFlags from cobra flags
	proxyFlag, _ := cmd.Flags().GetString("proxy")
	domainFlag, _ := cmd.Flags().GetString("domain")
	portFlag, _ := cmd.Flags().GetString("port")
	configFlag, _ := cmd.Flags().GetString("config")
	dnsListenFlag, _ := cmd.Flags().GetString("dns-listen")
	verboseFlag, _ := cmd.Flags().GetBool("verbose")

	cliFlags := config.CLIFlags{
		Proxy:     proxyFlag,
		Domains:   domainFlag,
		Ports:     portFlag,
		Config:    configFlag,
		DNSListen: dnsListenFlag,
		Verbose:   verboseFlag,
	}

	// Extract child process command from args (everything after --)
	if len(args) > 0 {
		cliFlags.Command = args[0]
		if len(args) > 1 {
			cliFlags.CommandArgs = args[1:]
		}
	}

	// Load and validate configuration
	cfg, err := config.Load(cliFlags)
	if err != nil {
		return err
	}

	// Detect proxy if not specified
	if cfg.ProxyURL == "" {
		if cfg.Verbose {
			fmt.Fprintf(os.Stderr, "[verbose] No proxy specified, detecting system proxy...\n")
		}
		proxyURL, err := config.DetectSystemProxy()
		if err != nil {
			return fmt.Errorf("no proxy specified and auto-detection failed: %w", err)
		}
		cfg.ProxyURL = proxyURL
		fmt.Fprintf(os.Stderr, "Detected system proxy: %s\n", cfg.ProxyURL)
	}

	// Parse proxy URL to create dialer
	scheme, proxyHost, proxyPort, err := config.ParseProxyURL(cfg.ProxyURL)
	if err != nil {
		return fmt.Errorf("invalid proxy URL: %w", err)
	}

	var dialer proxy.Dialer
	switch scheme {
	case "http":
		dialer = proxy.NewHTTPConnectDialer(proxyHost, proxyPort)
	case "socks5":
		dialer = proxy.NewSOCKS5Dialer(proxyHost, proxyPort)
	default:
		return fmt.Errorf("unsupported proxy scheme: %s", scheme)
	}

	// Parse DNS port from listen address
	_, dnsPortStr, err := net.SplitHostPort(cfg.DNSListen)
	if err != nil {
		return fmt.Errorf("invalid dns-listen address %q: %w", cfg.DNSListen, err)
	}
	dnsPort, err := strconv.Atoi(dnsPortStr)
	if err != nil {
		return fmt.Errorf("invalid dns port %q: %w", dnsPortStr, err)
	}

	// Cleanup stack: each step pushes its cleanup function; on failure or exit, run in reverse
	var cleanups []func() error
	defer func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			if cerr := cleanups[i](); cerr != nil {
				fmt.Fprintf(os.Stderr, "cleanup error: %v\n", cerr)
			}
		}
	}()

	// Clean stale resources from previous runs
	if cfg.Verbose {
		fmt.Fprintf(os.Stderr, "[verbose] Cleaning stale resources from previous runs...\n")
	}

	// Clean stale resolver files
	staleResolverMgr := dns.NewResolverManager(cfg.Domains, dnsPort)
	if err := staleResolverMgr.CleanStale(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to clean stale resolver files: %v\n", err)
	} else if cfg.Verbose {
		fmt.Fprintf(os.Stderr, "[verbose] Stale resolver files cleaned\n")
	}

	// Clean stale pf anchor
	staleAnchor := pf.NewAnchor(nil, nil)
	if err := staleAnchor.Unload(); err != nil {
		// Ignore error - anchor may not exist from a previous run
		if cfg.Verbose {
			fmt.Fprintf(os.Stderr, "[verbose] No stale pf anchor to clean (or failed): %v\n", err)
		}
	} else if cfg.Verbose {
		fmt.Fprintf(os.Stderr, "[verbose] Stale pf anchor cleaned\n")
	}

	// Clean stale loopback aliases
	stalePool := vip.NewPool(cfg.Domains)
	if err := stalePool.Teardown(); err != nil {
		// Ignore error - aliases may not exist from a previous run
		if cfg.Verbose {
			fmt.Fprintf(os.Stderr, "[verbose] No stale loopback aliases to clean (or failed): %v\n", err)
		}
	} else if cfg.Verbose {
		fmt.Fprintf(os.Stderr, "[verbose] Stale loopback aliases cleaned\n")
	}

	// Step 1: Allocate VIPs
	pool := vip.NewPool(cfg.Domains)

	// Step 2: Setup loopback aliases
	if cfg.Verbose {
		fmt.Fprintf(os.Stderr, "[verbose] Setting up loopback aliases...\n")
	}
	if err := pool.Setup(); err != nil {
		return fmt.Errorf("failed to setup loopback aliases: %w", err)
	}
	cleanups = append(cleanups, pool.Teardown)
	// Give the kernel a moment to fully bind the new loopback aliases before
	// we try net.Listen on those addresses (mirrors the shell script's sleep 0.5).
	time.Sleep(100 * time.Millisecond)

	// Step 3: Start DNS server
	if cfg.Verbose {
		fmt.Fprintf(os.Stderr, "[verbose] Starting DNS server on %s...\n", cfg.DNSListen)
	}
	dnsServer := dns.NewServer(cfg.DNSListen, pool)
	if err := dnsServer.Start(); err != nil {
		return fmt.Errorf("failed to start DNS server: %w", err)
	}
	cleanups = append(cleanups, dnsServer.Stop)

	// Step 4: Start TCP proxy (Forwarder)
	if cfg.Verbose {
		fmt.Fprintf(os.Stderr, "[verbose] Starting TCP proxy forwarder...\n")
	}
	forwarder := proxy.NewForwarder(pool, dialer, cfg.Ports)
	if err := forwarder.Start(); err != nil {
		return fmt.Errorf("failed to start TCP proxy: %w", err)
	}
	cleanups = append(cleanups, forwarder.Stop)

	// Step 5: Load pf anchor rules
	anchor := pf.NewAnchor(pool.Mappings(), cfg.Ports)
	if cfg.Verbose {
		fmt.Fprintf(os.Stderr, "[verbose] Loading pf anchor rules:\n")
		for _, rule := range anchor.Rules() {
			fmt.Fprintf(os.Stderr, "[verbose]   %s\n", rule)
		}
	}
	if err := anchor.Load(); err != nil {
		return fmt.Errorf("failed to load pf rules: %w", err)
	}
	cleanups = append(cleanups, anchor.Unload)

	// Step 6: Create resolver files
	if cfg.Verbose {
		fmt.Fprintf(os.Stderr, "[verbose] Creating resolver files...\n")
	}
	resolverMgr := dns.NewResolverManager(cfg.Domains, dnsPort)
	if err := resolverMgr.Setup(); err != nil {
		return fmt.Errorf("failed to create resolver files: %w", err)
	}
	cleanups = append(cleanups, resolverMgr.Teardown)

	// Step 7: Flush DNS cache (already done by resolverMgr.Setup, but explicit for clarity)
	// resolverMgr.Setup already flushes the cache

	// Print domain-VIP mapping table and proxy address
	fmt.Fprintf(os.Stderr, "fkag - domain-level transparent proxy\n")
	fmt.Fprintf(os.Stderr, "  Proxy: %s\n", cfg.ProxyURL)
	fmt.Fprintf(os.Stderr, "  DNS:   %s\n", cfg.DNSListen)
	fmt.Fprintf(os.Stderr, "  Domains:\n")
	for _, domain := range cfg.Domains {
		if ip, ok := pool.Lookup(domain); ok {
			fmt.Fprintf(os.Stderr, "    %s -> %s\n", domain, ip)
		}
	}
	fmt.Fprintf(os.Stderr, "  Ports: %v\n", cfg.Ports)

	// Step 8: 启动子进程（可选）或进入守护模式
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	// childExitCode is passed in from main() so it survives after run() returns.

	if cfg.Command != "" {
		// 有子命令：启动子进程，透明代理其流量
		if cfg.Verbose {
			fmt.Fprintf(os.Stderr, "[verbose] Starting child process: %s %v\n", cfg.Command, cfg.CommandArgs)
		}
		r := runner.NewRunner(cfg.Command, cfg.CommandArgs)
		if err := r.Start(); err != nil {
			return fmt.Errorf("failed to start child process: %w", err)
		}

		// 收到信号时转发给子进程
		go func() {
			sig := <-sigCh
			fmt.Fprintf(os.Stderr, "\nReceived %s, shutting down...\n", sig)
			r.Signal(sig)
		}()

		// 等待子进程退出，以子进程 exit code 退出
		exitCode, err := r.Wait()
		if err != nil {
			return fmt.Errorf("child process error: %w", err)
		}
		*childExitCode = exitCode
	} else {
		// 守护模式：不启动子进程，保持运行直到收到信号
		fmt.Fprintf(os.Stderr, "Running in daemon mode. Press Ctrl+C to stop and clean up.\n")
		sig := <-sigCh
		fmt.Fprintf(os.Stderr, "\nReceived %s, shutting down...\n", sig)
	}

	return nil
}
