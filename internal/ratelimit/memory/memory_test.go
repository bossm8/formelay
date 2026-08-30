package memory

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestBucketAllowBurstThenDeny(t *testing.T) {
	s := New(time.Minute)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		ok, err := s.Allow(ctx, "k", 60, 3, time.Minute) // 60/min, burst 3
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ok {
			t.Fatalf("request %d: expected allow within burst", i)
		}
	}
	ok, _ := s.Allow(ctx, "k", 60, 3, time.Minute)
	if ok {
		t.Fatalf("expected deny once burst exhausted")
	}
}

func TestBucketRefillsOverTime(t *testing.T) {
	b := &bucket{}
	now := time.Now()
	// Exhaust burst of 2.
	if !b.allow(120, 2, time.Minute, now) {
		t.Fatal("expected first token allowed")
	}
	if !b.allow(120, 2, time.Minute, now) {
		t.Fatal("expected second token allowed")
	}
	if b.allow(120, 2, time.Minute, now) {
		t.Fatal("expected third token denied (burst exhausted)")
	}
	// 120/min = 2/sec refill; after 1 second one token should be available.
	later := now.Add(1 * time.Second)
	if !b.allow(120, 2, time.Minute, later) {
		t.Fatal("expected token available after refill window")
	}
}

func totalBuckets(s *Store) int {
	total := 0
	for _, n := range s.ActiveBucketsByScope() {
		total += n
	}
	return total
}

func TestJanitorEvictsIdleBuckets(t *testing.T) {
	s := New(10 * time.Millisecond)
	ctx := context.Background()
	if _, err := s.Allow(ctx, "k", 10, 10, time.Minute); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if totalBuckets(s) != 1 {
		t.Fatalf("expected 1 active bucket, got %d", totalBuckets(s))
	}
	time.Sleep(20 * time.Millisecond)
	s.evictIdle(time.Now())
	if totalBuckets(s) != 0 {
		t.Fatalf("expected bucket to be evicted, got %d active", totalBuckets(s))
	}
}

func TestActiveBucketsByScope(t *testing.T) {
	s := New(time.Minute)
	ctx := context.Background()
	keys := []string{"global", "ip:formA:1.2.3.4", "form:formA"}
	for _, k := range keys {
		if _, err := s.Allow(ctx, k, 10, 10, time.Minute); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	want := map[string]int{"global": 1, "ip": 1, "form": 1}
	if got := s.ActiveBucketsByScope(); !reflect.DeepEqual(got, want) {
		t.Fatalf("ActiveBucketsByScope() = %v, want %v", got, want)
	}
}
