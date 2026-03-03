// Package config handles CLI parameter parsing, YAML config loading,
// and system proxy detection for fkag.
package config

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// CLIFlags 表示从命令行解析的原始参数
type CLIFlags struct {
	Proxy       string
	Domains     string // 逗号分隔
	Ports       string // 逗号分隔
	Config      string // 配置文件路径
	DNSListen   string
	Verbose     bool
	Command     string   // 子进程命令
	CommandArgs []string // 子进程参数
}

// YAMLConfig 表示 YAML 配置文件结构
type YAMLConfig struct {
	Proxy   string   `yaml:"proxy"`
	Domains []string `yaml:"domains"`
	Ports   []int    `yaml:"ports"`
	DNS     struct {
		Listen string `yaml:"listen"`
	} `yaml:"dns"`
}

// Config 保存 fkag 的完整运行配置
type Config struct {
	ProxyURL    string   // 上游代理地址
	Domains     []string // 目标域名列表
	Ports       []int    // 需要代理的端口列表，默认 [80, 443]
	DNSListen   string   // DNS 服务器监听地址，默认 "127.0.0.1:10053"
	Verbose     bool     // 是否启用详细日志
	Command     string   // 子进程命令
	CommandArgs []string // 子进程参数
}

// ProxyInfo 表示从 scutil --proxy 解析的代理信息
type ProxyInfo struct {
	HTTPSEnabled bool
	HTTPSProxy   string
	HTTPSPort    int
	SOCKSEnabled bool
	SOCKSProxy   string
	SOCKSPort    int
	HTTPEnabled  bool
	HTTPProxy    string
	HTTPPort     int
}

// ParseProxyURL 验证代理 URL 格式，支持 http:// 和 socks5://
func ParseProxyURL(rawURL string) (scheme, host string, port int, err error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", "", 0, fmt.Errorf("invalid proxy URL: %w", err)
	}

	if u.Scheme != "http" && u.Scheme != "socks5" {
		return "", "", 0, fmt.Errorf("unsupported proxy scheme %q: must be http or socks5", u.Scheme)
	}

	host = u.Hostname()
	if host == "" {
		return "", "", 0, fmt.Errorf("proxy URL missing host")
	}

	portStr := u.Port()
	if portStr == "" {
		return "", "", 0, fmt.Errorf("proxy URL missing port")
	}

	port, err = strconv.Atoi(portStr)
	if err != nil {
		return "", "", 0, fmt.Errorf("invalid proxy port %q: %w", portStr, err)
	}
	if port < 1 || port > 65535 {
		return "", "", 0, fmt.Errorf("proxy port %d out of range (1-65535)", port)
	}

	return u.Scheme, host, port, nil
}

// ParseDomains 解析逗号分隔的域名字符串为域名列表。
// 去除每个域名的前后空格，忽略空字符串。
func ParseDomains(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	var domains []string
	for _, p := range parts {
		d := strings.TrimSpace(p)
		if d != "" {
			domains = append(domains, d)
		}
	}
	return domains
}

// ParsePorts 解析逗号分隔的端口字符串为端口列表。
// 空字符串返回默认值 [80, 443]。
// 每个端口必须是 1-65535 范围内的整数。
func ParsePorts(raw string) ([]int, error) {
	if strings.TrimSpace(raw) == "" {
		return []int{80, 443}, nil
	}
	parts := strings.Split(raw, ",")
	var ports []int
	for _, p := range parts {
		s := strings.TrimSpace(p)
		if s == "" {
			continue
		}
		port, err := strconv.Atoi(s)
		if err != nil {
			return nil, fmt.Errorf("invalid port %q: %w", s, err)
		}
		if port < 1 || port > 65535 {
			return nil, fmt.Errorf("port %d out of range (1-65535)", port)
		}
		ports = append(ports, port)
	}
	return ports, nil
}

// Load 从 CLI 参数和可选的 YAML 配置文件加载配置，CLI 参数优先
func Load(cliFlags CLIFlags) (*Config, error) {
	var yamlCfg YAMLConfig

	// 1. 如果指定了配置文件，读取并解析 YAML
	if cliFlags.Config != "" {
		data, err := os.ReadFile(cliFlags.Config)
		if err != nil {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
		if err := yaml.Unmarshal(data, &yamlCfg); err != nil {
			return nil, fmt.Errorf("failed to parse config file: %w", err)
		}
	}

	// 2. 合并：CLI 参数优先于 YAML 配置
	cfg := &Config{
		Verbose: cliFlags.Verbose,
	}

	// Proxy
	if cliFlags.Proxy != "" {
		cfg.ProxyURL = cliFlags.Proxy
	} else {
		cfg.ProxyURL = yamlCfg.Proxy
	}

	// Domains
	if cliFlags.Domains != "" {
		cfg.Domains = ParseDomains(cliFlags.Domains)
	} else {
		cfg.Domains = yamlCfg.Domains
	}

	// Ports
	if cliFlags.Ports != "" {
		ports, err := ParsePorts(cliFlags.Ports)
		if err != nil {
			return nil, fmt.Errorf("invalid ports: %w", err)
		}
		cfg.Ports = ports
	} else if len(yamlCfg.Ports) > 0 {
		cfg.Ports = yamlCfg.Ports
	} else {
		cfg.Ports = []int{80, 443}
	}

	// DNSListen
	if cliFlags.DNSListen != "" {
		cfg.DNSListen = cliFlags.DNSListen
	} else if yamlCfg.DNS.Listen != "" {
		cfg.DNSListen = yamlCfg.DNS.Listen
	} else {
		cfg.DNSListen = "127.0.0.1:10053"
	}

	// Command
	cfg.Command = cliFlags.Command
	cfg.CommandArgs = cliFlags.CommandArgs

	// 3. 验证：域名不能为空（命令为可选，不传则进入守护模式）
	if len(cfg.Domains) == 0 {
		return nil, fmt.Errorf("no domains specified: use --domain or config file")
	}

	return cfg, nil
}

// ParseScutilOutput 解析 scutil --proxy 的输出，提取代理配置信息。
// 输出格式为 key-value 对，如 "HTTPSEnable : 1"、"HTTPSPort : 7897"。
func ParseScutilOutput(output string) ProxyInfo {
	var info ProxyInfo
	kv := make(map[string]string)

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		kv[key] = val
	}

	// HTTPS proxy
	if kv["HTTPSEnable"] == "1" {
		info.HTTPSEnabled = true
		info.HTTPSProxy = kv["HTTPSProxy"]
		info.HTTPSPort, _ = strconv.Atoi(kv["HTTPSPort"])
	}

	// SOCKS proxy
	if kv["SOCKSEnable"] == "1" {
		info.SOCKSEnabled = true
		info.SOCKSProxy = kv["SOCKSProxy"]
		info.SOCKSPort, _ = strconv.Atoi(kv["SOCKSPort"])
	}

	// HTTP proxy
	if kv["HTTPEnable"] == "1" {
		info.HTTPEnabled = true
		info.HTTPProxy = kv["HTTPProxy"]
		info.HTTPPort, _ = strconv.Atoi(kv["HTTPPort"])
	}

	return info
}

// SelectProxy 根据 ProxyInfo 按优先级选择代理 URL。
// 优先级：HTTPS > SOCKS5 > HTTP。
// HTTPS 和 HTTP 返回 "http://host:port"，SOCKS5 返回 "socks5://host:port"。
func SelectProxy(info ProxyInfo) (string, error) {
	if info.HTTPSEnabled && info.HTTPSProxy != "" && info.HTTPSPort > 0 {
		return fmt.Sprintf("http://%s:%d", info.HTTPSProxy, info.HTTPSPort), nil
	}
	if info.SOCKSEnabled && info.SOCKSProxy != "" && info.SOCKSPort > 0 {
		return fmt.Sprintf("socks5://%s:%d", info.SOCKSProxy, info.SOCKSPort), nil
	}
	if info.HTTPEnabled && info.HTTPProxy != "" && info.HTTPPort > 0 {
		return fmt.Sprintf("http://%s:%d", info.HTTPProxy, info.HTTPPort), nil
	}
	return "", fmt.Errorf("no system proxy available: use --proxy to specify manually")
}

// DetectSystemProxy 通过 scutil --proxy 探测 macOS 系统代理。
// 优先级：HTTPS > SOCKS5 > HTTP。
func DetectSystemProxy() (string, error) {
	out, err := exec.Command("scutil", "--proxy").Output()
	if err != nil {
		return "", fmt.Errorf("failed to run scutil --proxy: %w", err)
	}
	info := ParseScutilOutput(string(out))
	return SelectProxy(info)
}
