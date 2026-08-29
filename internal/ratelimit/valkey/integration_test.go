//go:build integration

// Run against a real Valkey instance:
//
//	docker compose -f docker-compose.test.yml up -d valkey
//	VALKEY_TEST_ADDR=127.0.0.1:6379 go test -tags=integration ./internal/ratelimit/valkey/... -v
//
// or via `make test-integration`, which drives the whole thing through
// docker-compose so no local Valkey is needed.
package valkey

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
)

// testKey returns a fresh, collision-free bucket key per test run (already
// a dependency of this module elsewhere, so no new import for this).
func testKey(prefix string) string {
	return prefix + ulid.Make().String()
}

func testAddr(t *testing.T) string {
	t.Helper()
	addr := os.Getenv("VALKEY_TEST_ADDR")
	if addr == "" {
		addr = "127.0.0.1:6379"
	}
	return addr
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := New(Config{Addresses: []string{testAddr(t)}, DialTimeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("connect to valkey at %s: %v", testAddr(t), err)
	}
	t.Cleanup(st.Close)
	return st
}

func TestValkeyAllowBurstThenDeny(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	key := testKey("it:")

	for i := 0; i < 3; i++ {
		ok, err := st.Allow(ctx, key, 60, 3, time.Minute) // 60/min, burst 3
		if err != nil {
			t.Fatalf("request %d: unexpected error: %v", i, err)
		}
		if !ok {
			t.Fatalf("request %d: expected allow within burst", i)
		}
	}
	ok, err := st.Allow(ctx, key, 60, 3, time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatalf("expected deny once burst exhausted")
	}
}

func TestValkeyRefillsOverTime(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	key := testKey("it:")

	// burst of 1 at 60/min (1/sec refill).
	ok, err := st.Allow(ctx, key, 60, 1, time.Minute)
	if err != nil || !ok {
		t.Fatalf("expected first request allowed: ok=%v err=%v", ok, err)
	}
	ok, err = st.Allow(ctx, key, 60, 1, time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatalf("expected second request denied (burst exhausted)")
	}

	time.Sleep(1100 * time.Millisecond)

	ok, err = st.Allow(ctx, key, 60, 1, time.Minute)
	if err != nil || !ok {
		t.Fatalf("expected request allowed after refill: ok=%v err=%v", ok, err)
	}
}

// TestValkeySharedAcrossStoreInstances is the integration-level proof of
// the plan's core requirement for this backend: rate-limit state is shared
// state, not per-process — two independently-constructed Store instances
// (standing in for two formelay replicas talking to the same Valkey) must
// observe each other's consumption of the same bucket.
func TestValkeySharedAcrossStoreInstances(t *testing.T) {
	replicaA := newTestStore(t)
	replicaB, err := New(Config{Addresses: []string{testAddr(t)}, DialTimeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("connect replica B: %v", err)
	}
	t.Cleanup(replicaB.Close)

	ctx := context.Background()
	key := testKey("it:shared:")

	// Exhaust the burst of 2 via replica A.
	for i := 0; i < 2; i++ {
		ok, err := replicaA.Allow(ctx, key, 60, 2, time.Minute)
		if err != nil || !ok {
			t.Fatalf("replica A request %d: expected allow: ok=%v err=%v", i, ok, err)
		}
	}
	// Replica B, sharing the same Valkey-backed key, must see it as exhausted.
	ok, err := replicaB.Allow(ctx, key, 60, 2, time.Minute)
	if err != nil {
		t.Fatalf("replica B: unexpected error: %v", err)
	}
	if ok {
		t.Fatalf("replica B: expected deny — bucket was exhausted by replica A, proving shared state")
	}
}

// TestValkeyNewFailsFastOnUnreachableAddress documents (and locks in) that
// New() eagerly dials to determine cluster topology, so a Valkey that's
// unreachable at startup surfaces as a construction error immediately
// rather than being deferred to the first Allow() call. The on_error
// setting governs a *runtime* outage after a successful connection (a
// Do() call failing on an already-established client) — that decision
// logic is a pure function and is unit-tested directly in valkey_test.go
// (TestOnErrorAllows), without needing a live or fake client.
func TestValkeyNewFailsFastOnUnreachableAddress(t *testing.T) {
	_, err := New(Config{Addresses: []string{"127.0.0.1:1"}, DialTimeout: 200 * time.Millisecond})
	if err == nil {
		t.Fatalf("expected New() to fail fast against an unreachable address")
	}
}
