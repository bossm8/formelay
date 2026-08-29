package memory

import (
	"context"
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

func TestJanitorEvictsIdleBuckets(t *testing.T) {
	s := New(10 * time.Millisecond)
	ctx := context.Background()
	if _, err := s.Allow(ctx, "k", 10, 10, time.Minute); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.ActiveBuckets() != 1 {
		t.Fatalf("expected 1 active bucket, got %d", s.ActiveBuckets())
	}
	time.Sleep(20 * time.Millisecond)
	s.evictIdle(time.Now())
	if s.ActiveBuckets() != 0 {
		t.Fatalf("expected bucket to be evicted, got %d active", s.ActiveBuckets())
	}
}
