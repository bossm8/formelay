package api

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseTrustedProxies(t *testing.T) {
	t.Run("CIDR and bare IP both parse, invalid entries are silently skipped", func(t *testing.T) {
		nets := ParseTrustedProxies([]string{"10.0.0.0/8", "192.168.1.1", "not-an-ip-or-cidr", "::1"})
		if len(nets) != 3 {
			t.Fatalf("expected 3 valid entries (1 CIDR, 1 bare IPv4, 1 bare IPv6), got %d: %v", len(nets), nets)
		}
	})

	t.Run("empty list yields no trusted networks", func(t *testing.T) {
		if nets := ParseTrustedProxies(nil); len(nets) != 0 {
			t.Fatalf("expected 0 entries, got %d", len(nets))
		}
	})

	t.Run("bare IPv4 becomes a /32", func(t *testing.T) {
		nets := ParseTrustedProxies([]string{"203.0.113.5"})
		if len(nets) != 1 {
			t.Fatalf("expected 1 entry, got %d", len(nets))
		}
		if nets[0].Contains(net.ParseIP("203.0.113.6")) {
			t.Fatal("a bare IPv4 entry must become a /32 (exact match only), not a wider range")
		}
		if !nets[0].Contains(net.ParseIP("203.0.113.5")) {
			t.Fatal("expected the /32 to contain its own address")
		}
	})
}

func TestClientIP(t *testing.T) {
	trusted := ParseTrustedProxies([]string{"10.0.0.0/8"})

	t.Run("untrusted peer: X-Forwarded-For is ignored", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		r.RemoteAddr = "203.0.113.9:12345" // not in trusted
		r.Header.Set("X-Forwarded-For", "1.2.3.4")
		if got := clientIP(r, trusted); got != "203.0.113.9" {
			t.Fatalf("clientIP = %q, want the untrusted peer's own address %q", got, "203.0.113.9")
		}
	})

	t.Run("trusted peer: X-Forwarded-For is honored, first entry wins", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		r.RemoteAddr = "10.0.0.1:12345" // in trusted 10.0.0.0/8
		r.Header.Set("X-Forwarded-For", "1.2.3.4, 5.6.7.8")
		if got := clientIP(r, trusted); got != "1.2.3.4" {
			t.Fatalf("clientIP = %q, want first XFF entry %q", got, "1.2.3.4")
		}
	})

	t.Run("trusted peer with no X-Forwarded-For falls back to RemoteAddr", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		r.RemoteAddr = "10.0.0.1:12345"
		if got := clientIP(r, trusted); got != "10.0.0.1" {
			t.Fatalf("clientIP = %q, want %q", got, "10.0.0.1")
		}
	})

	t.Run("no trusted proxies configured: XFF is always ignored, even from a plausible peer", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		r.RemoteAddr = "10.0.0.1:12345"
		r.Header.Set("X-Forwarded-For", "1.2.3.4")
		if got := clientIP(r, nil); got != "10.0.0.1" {
			t.Fatalf("clientIP = %q, want RemoteAddr %q: an empty trusted_proxies must never honor XFF", got, "10.0.0.1")
		}
	})
}
