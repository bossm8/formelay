package render

import "testing"

func TestTruncateFunc(t *testing.T) {
	truncate := FuncMap["truncate"].(func(int, string) string)

	t.Run("no-op when under the limit", func(t *testing.T) {
		if got := truncate(10, "short"); got != "short" {
			t.Fatalf("got %q, want unchanged %q", got, "short")
		}
	})

	t.Run("truncates ASCII to the exact rune count", func(t *testing.T) {
		if got := truncate(3, "abcdef"); got != "abc" {
			t.Fatalf("got %q, want %q", got, "abc")
		}
	})

	t.Run("rune-safe on multi-byte UTF-8, matching sanitize.Field's truncation semantics", func(t *testing.T) {
		// Each of these runes is multi-byte in UTF-8; truncating by bytes
		// instead of runes would corrupt the output.
		if got := truncate(2, "日本語"); got != "日本" {
			t.Fatalf("got %q, want %q", got, "日本")
		}
	})
}
