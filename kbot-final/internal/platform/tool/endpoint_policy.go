package tool

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// EndpointPolicy 控制 Tool 可以访问的网络目标。显式 allowlist 可放行课堂 Compose 服务。
type EndpointPolicy struct {
	allowedHosts map[string]struct{}
	allowPrivate bool
	resolver     *net.Resolver
}

func NewEndpointPolicy(allowedHosts []string, allowPrivate bool) *EndpointPolicy {
	p := &EndpointPolicy{allowedHosts: make(map[string]struct{}), allowPrivate: allowPrivate, resolver: net.DefaultResolver}
	for _, host := range allowedHosts {
		if host = strings.ToLower(strings.TrimSpace(host)); host != "" {
			p.allowedHosts[host] = struct{}{}
		}
	}
	return p
}

func (p *EndpointPolicy) hostAllowed(host string) bool {
	_, ok := p.allowedHosts[strings.ToLower(strings.TrimSuffix(host, "."))]
	return ok
}

func (p *EndpointPolicy) validateURL(ctx context.Context, rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		return fmt.Errorf("invalid tool endpoint URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("tool endpoint scheme must be http or https")
	}
	if u.User != nil {
		return fmt.Errorf("tool endpoint URL must not contain credentials")
	}
	return p.validateHost(ctx, u.Hostname())
}

// ValidateURL 校验可配置的服务端 HTTP 出口地址。
func (p *EndpointPolicy) ValidateURL(ctx context.Context, rawURL string) error {
	return p.validateURL(ctx, rawURL)
}

func (p *EndpointPolicy) validateHost(ctx context.Context, host string) error {
	if p == nil || p.allowPrivate || p.hostAllowed(host) {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil {
		if unsafeIP(ip) {
			return fmt.Errorf("tool endpoint resolves to a private or local network address")
		}
		return nil
	}
	addrs, err := p.resolver.LookupIPAddr(ctx, host)
	if err != nil || len(addrs) == 0 {
		return fmt.Errorf("resolve tool endpoint host: %w", err)
	}
	for _, addr := range addrs {
		if unsafeIP(addr.IP) {
			return fmt.Errorf("tool endpoint resolves to a private or local network address")
		}
	}
	return nil
}

func unsafeIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast()
}

func (p *EndpointPolicy) validateConfig(ctx context.Context, sourceType string, endpoint map[string]any) error {
	if p == nil {
		return nil
	}
	var fields []string
	switch sourceType {
	case "rest_api":
		fields = []string{"url"}
	case "mcp_server":
		transport, _ := endpoint["transport"].(string)
		if transport == "" && strings.TrimSpace(stringValue(endpoint["url"])) == "" {
			transport = "stdio"
		}
		if transport == "stdio" {
			return fmt.Errorf("MCP stdio is disabled in app/worker; deploy an isolated connector runner and use streamable_http")
		}
		fields = []string{"url"}
	case "a2a":
		fields = []string{"card_url", "url"}
	default:
		return nil
	}
	seen := false
	for _, field := range fields {
		raw, _ := endpoint[field].(string)
		if raw == "" {
			continue
		}
		seen = true
		if err := p.validateURL(ctx, raw); err != nil {
			return fmt.Errorf("%s: %w", field, err)
		}
	}
	if !seen {
		return fmt.Errorf("tool endpoint URL is required")
	}
	return nil
}

func stringValue(value any) string {
	result, _ := value.(string)
	return result
}

// HTTPClient 在连接建立前再次解析并校验 IP，覆盖 DNS rebinding 与重定向场景。
func (p *EndpointPolicy) HTTPClient(timeout time.Duration) *http.Client {
	if p == nil {
		return &http.Client{Timeout: timeout, CheckRedirect: redirectPolicy(nil)}
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		if err := p.validateHost(ctx, host); err != nil {
			return nil, err
		}
		return dialer.DialContext(ctx, network, net.JoinHostPort(host, port))
	}
	return &http.Client{Timeout: timeout, Transport: transport, CheckRedirect: redirectPolicy(p)}
}

func redirectPolicy(policy *EndpointPolicy) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("tool endpoint stopped after 5 redirects")
		}
		if policy != nil {
			if err := policy.validateURL(req.Context(), req.URL.String()); err != nil {
				return fmt.Errorf("redirect target: %w", err)
			}
		}
		if len(via) > 0 && !sameOrigin(via[0].URL, req.URL) {
			return fmt.Errorf("tool endpoint cross-origin redirect is blocked")
		}
		return nil
	}
}

func sameOrigin(left, right *url.URL) bool {
	return strings.EqualFold(left.Scheme, right.Scheme) &&
		strings.EqualFold(strings.TrimSuffix(left.Hostname(), "."), strings.TrimSuffix(right.Hostname(), ".")) &&
		effectivePort(left) == effectivePort(right)
}

func effectivePort(value *url.URL) string {
	if port := value.Port(); port != "" {
		return port
	}
	if strings.EqualFold(value.Scheme, "https") {
		return "443"
	}
	if strings.EqualFold(value.Scheme, "http") {
		return "80"
	}
	return ""
}
