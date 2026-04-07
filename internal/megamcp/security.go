package megamcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

// ValidateBaseURL rejects non-HTTPS URLs and literal private/loopback IPs.
// Does NOT perform DNS resolution — that check happens at request time via
// SafeDialer to prevent DNS rebinding attacks. This function validates the
// URL format and catches obviously unsafe literal IP addresses.
func ValidateBaseURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	if parsed.Scheme != "https" {
		return fmt.Errorf("URL must use HTTPS, got %q", parsed.Scheme)
	}

	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("URL has no hostname")
	}

	// If the host is a literal IP address, check it directly.
	// Hostname-based URLs are checked at request time via SafeDialer.
	if ip := net.ParseIP(host); ip != nil {
		if err := checkIP(ip); err != nil {
			return fmt.Errorf("IP address %s: %w", host, err)
		}
	}

	return nil
}

// checkIP rejects private, loopback, link-local, and metadata IPs.
func checkIP(ip net.IP) error {
	if ip.IsLoopback() {
		return fmt.Errorf("loopback address rejected")
	}
	if ip.IsPrivate() {
		return fmt.Errorf("private IP address rejected")
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return fmt.Errorf("link-local address rejected")
	}
	// Cloud metadata endpoint (169.254.169.254).
	if ip.Equal(net.ParseIP("169.254.169.254")) {
		return fmt.Errorf("cloud metadata address rejected")
	}
	return nil
}

// SafeHTTPClient returns an http.Client with a custom dialer that rejects
// connections to private, loopback, link-local, and cloud metadata IPs.
// This is the runtime SSRF guard that ValidateBaseURL defers DNS checks to.
func SafeHTTPClient(timeout time.Duration) *http.Client {
	dialer := &safeDialer{
		inner: &net.Dialer{Timeout: 10 * time.Second},
	}
	transport := &http.Transport{
		Proxy:       http.ProxyFromEnvironment,
		DialContext: dialer.DialContext,
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}
}

// safeDialer wraps a net.Dialer and checks resolved IPs before connecting.
type safeDialer struct {
	inner *net.Dialer
}

// DialContext resolves the hostname, validates all IPs, then connects.
func (d *safeDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("invalid address %q: %w", addr, err)
	}

	// Resolve hostname to IPs.
	ips, err := net.DefaultResolver.LookupHost(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve %q: %w", host, err)
	}

	// Check every resolved IP.
	for _, ipStr := range ips {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			continue
		}
		if err := checkIP(ip); err != nil {
			return nil, fmt.Errorf("hostname %q resolves to %s: %w", host, ipStr, err)
		}
	}

	// All IPs are safe — connect to the first one.
	safeAddr := net.JoinHostPort(ips[0], port)
	return d.inner.DialContext(ctx, network, safeAddr)
}

// SanitizeText strips control characters (except newline and tab),
// and truncates to maxLen.
func SanitizeText(s string, maxLen int) string {
	var b strings.Builder
	for _, r := range s {
		if r == '\n' || r == '\t' {
			b.WriteRune(r)
			continue
		}
		if unicode.IsControl(r) {
			continue
		}
		b.WriteRune(r)
	}
	result := b.String()
	if maxLen > 0 && len(result) > maxLen {
		result = result[:maxLen]
	}
	return result
}

// VerifyChecksum compares the SHA-256 hash of data against an expected
// checksum string in the format "sha256:<hex>".
func VerifyChecksum(data []byte, expected string) error {
	if expected == "" {
		return fmt.Errorf("expected checksum is empty")
	}
	if !strings.HasPrefix(expected, "sha256:") {
		return fmt.Errorf("unsupported checksum format: %q (expected sha256:<hex>)", expected)
	}
	expectedHex := strings.TrimPrefix(expected, "sha256:")
	actual := sha256.Sum256(data)
	actualHex := hex.EncodeToString(actual[:])
	if actualHex != expectedHex {
		return fmt.Errorf("checksum mismatch: expected sha256:%s, got sha256:%s", expectedHex, actualHex)
	}
	return nil
}

// ComputeChecksum returns the SHA-256 checksum of data in "sha256:<hex>" format.
func ComputeChecksum(data []byte) string {
	h := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(h[:])
}

// ValidateSlug rejects slugs that contain path traversal characters or are empty.
func ValidateSlug(slug string) error {
	if slug == "" {
		return fmt.Errorf("slug is empty")
	}
	if strings.Contains(slug, "..") {
		return fmt.Errorf("slug contains path traversal: %q", slug)
	}
	if strings.Contains(slug, "/") {
		return fmt.Errorf("slug contains forward slash: %q", slug)
	}
	if strings.Contains(slug, "\\") {
		return fmt.Errorf("slug contains backslash: %q", slug)
	}
	return nil
}

// ValidateCachePath verifies that the resolved path is under cacheRoot.
// Belt-and-suspenders defense against path traversal.
func ValidateCachePath(path, cacheRoot string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("cannot resolve path %q: %w", path, err)
	}
	absRoot, err := filepath.Abs(cacheRoot)
	if err != nil {
		return fmt.Errorf("cannot resolve cache root %q: %w", cacheRoot, err)
	}
	// Ensure the path is under the root with a trailing separator.
	if !strings.HasPrefix(absPath, absRoot+string(filepath.Separator)) && absPath != absRoot {
		return fmt.Errorf("path %q escapes cache root %q", absPath, absRoot)
	}
	return nil
}
