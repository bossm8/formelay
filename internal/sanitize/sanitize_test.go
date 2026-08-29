package sanitize

import "testing"

func TestField(t *testing.T) {
	// "e" + combining acute accent (U+0065 U+0301, decomposed form)
	// NFC-normalizes to the single precomposed U+00E9 ("e-acute") —
	// used below to verify the golang.org/x/text/unicode/norm pass
	// actually runs, not just that ordinary strings survive unchanged.
	decomposed := "café"
	precomposed := "café"

	cases := []struct {
		name      string
		in        string
		multiline bool
		maxLen    int
		want      string
		wantErr   bool
	}{
		{"plain", "hello", false, 0, "hello", false},
		{"strips control chars", "he\x00llo\x07", false, 0, "hello", false},
		{"newline collapsed when not multiline", "line1\nline2", false, 0, "line1 line2", false},
		{"newline kept when multiline", "line1\nline2", true, 0, "line1\nline2", false},
		{"crlf normalized", "line1\r\nline2", true, 0, "line1\nline2", false},
		{"tab preserved", "a\tb", false, 0, "a\tb", false},
		{"truncates", "hello world", false, 5, "hello", false},
		{"invalid utf8 rejected", string([]byte{0xff, 0xfe}), false, 0, "", true},
		{"unicode NFC normalization", decomposed, false, 0, precomposed, false},
		{"strips script tag", "<script>alert(1)</script>hello", false, 0, "hello", false},
		{"strips dangerous attribute", "<img src=x onerror=alert(1)>", false, 0, "", false},
		{"strips formatting tags but keeps text", "<b>bold</b> text", false, 0, "bold text", false},
		{"plain less-than/greater-than not mangled", "5 < 10 and > 3", false, 0, "5 < 10 and > 3", false},
		{"plain ampersand not mangled", "Bob & Alice", false, 0, "Bob & Alice", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Field(c.in, c.multiline, c.maxLen)
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected error, got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Fatalf("got %q (bytes %x), want %q (bytes %x)", got, []byte(got), c.want, []byte(c.want))
			}
		})
	}
}

func TestHeaderSafeAddress(t *testing.T) {
	if _, err := HeaderSafeAddress("user@example.com"); err != nil {
		t.Fatalf("expected valid address to pass: %v", err)
	}
	// Classic SMTP header injection attempt via embedded CRLF.
	injected := "user@example.com\r\nBcc: attacker@evil.com"
	if _, err := HeaderSafeAddress(injected); err == nil {
		t.Fatalf("expected header-injection attempt to be rejected")
	}
}
