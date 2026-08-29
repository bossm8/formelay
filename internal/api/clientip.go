package api

import (
	"net"
	"net/http"
	"strings"
)

// clientIP returns the request's client IP, honoring X-Forwarded-For only
// when the immediate peer (RemoteAddr) is in trustedProxies.
func clientIP(r *http.Request, trustedProxies []*net.IPNet) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer := net.ParseIP(host)

	if peer != nil && isTrusted(peer, trustedProxies) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			candidate := strings.TrimSpace(parts[0])
			if ip := net.ParseIP(candidate); ip != nil {
				return candidate
			}
		}
	}
	return host
}

func isTrusted(ip net.IP, trusted []*net.IPNet) bool {
	for _, n := range trusted {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// ParseTrustedProxies parses the server.trusted_proxies YAML entries
// (CIDRs, or bare IPs treated as a /32 or /128), skipping invalid ones.
func ParseTrustedProxies(entries []string) []*net.IPNet {
	var out []*net.IPNet
	for _, e := range entries {
		if !strings.Contains(e, "/") {
			if ip := net.ParseIP(e); ip != nil {
				bits := 32
				if ip.To4() == nil {
					bits = 128
				}
				e = e + "/" + itoa(bits)
			}
		}
		_, n, err := net.ParseCIDR(e)
		if err == nil {
			out = append(out, n)
		}
	}
	return out
}

func itoa(n int) string {
	if n == 32 {
		return "32"
	}
	return "128"
}
