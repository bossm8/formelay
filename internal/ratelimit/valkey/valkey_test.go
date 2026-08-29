package valkey

import "testing"

// The on_error decision itself needs no live or fake valkey.Client — see
// the Do()-failure branch in Allow(), which delegates to onErrorAllows().
// Real Do() calls against a live server are covered by the integration
// suite (integration_test.go, run via `make test-integration`).
func TestOnErrorAllows(t *testing.T) {
	cases := []struct {
		name    string
		onError OnError
		want    bool
	}{
		{"unset defaults to fail-open (allow)", "", true},
		{"explicit allow", OnErrorAllow, true},
		{"explicit deny", OnErrorDeny, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := &Store{onError: c.onError}
			if got := s.onErrorAllows(); got != c.want {
				t.Fatalf("onErrorAllows() = %v, want %v", got, c.want)
			}
		})
	}
}
