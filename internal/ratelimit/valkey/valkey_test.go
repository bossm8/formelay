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

// handleBackendError is the only place a Do() failure is observable at
// all (Allow never returns it to its caller), so this is the seam
// formelay_ratelimit_backend_errors_total hooks into.
func TestHandleBackendErrorInvokesCallback(t *testing.T) {
	t.Run("callback set: fires exactly once per call", func(t *testing.T) {
		var calls int
		s := &Store{cfg: Config{OnBackendError: func() { calls++ }}}
		s.handleBackendError()
		if calls != 1 {
			t.Fatalf("OnBackendError called %d times, want 1", calls)
		}
	})

	t.Run("callback unset: no panic, still resolves onError", func(t *testing.T) {
		s := &Store{onError: OnErrorDeny}
		if got := s.handleBackendError(); got != false {
			t.Fatalf("handleBackendError() = %v, want false (OnErrorDeny)", got)
		}
	})
}
