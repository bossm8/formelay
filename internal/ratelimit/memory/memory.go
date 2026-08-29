// Package memory implements ratelimit.Store as an in-process, sharded
// token-bucket map — the default backend, correct for a single running
// instance, with a janitor goroutine bounding memory via idle eviction.
package memory

import (
	"context"
	"hash/fnv"
	"sync"
	"time"
)

const numShards = 32

type bucket struct {
	mu       sync.Mutex
	tokens   float64
	lastSeen time.Time
}

func (b *bucket) allow(rate, burst float64, window time.Duration, now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.lastSeen.IsZero() {
		b.tokens = burst
	} else {
		elapsed := now.Sub(b.lastSeen).Seconds()
		refillPerSecond := rate / window.Seconds()
		b.tokens = min(burst, b.tokens+elapsed*refillPerSecond)
	}
	b.lastSeen = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func (b *bucket) idleSince(now time.Time) time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	return now.Sub(b.lastSeen)
}

type shard struct {
	mu      sync.Mutex
	buckets map[string]*bucket
}

// Store is a sharded, in-process token-bucket ratelimit.Store.
type Store struct {
	shards  [numShards]*shard
	idleTTL time.Duration
}

// New creates a Store. Call StartJanitor to begin evicting buckets idle
// longer than idleTTL; without it, memory grows with the number of distinct
// keys ever seen.
func New(idleTTL time.Duration) *Store {
	s := &Store{idleTTL: idleTTL}
	for i := range s.shards {
		s.shards[i] = &shard{buckets: map[string]*bucket{}}
	}
	return s
}

func (s *Store) shardFor(key string) *shard {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return s.shards[h.Sum32()%numShards]
}

func (s *Store) Allow(_ context.Context, key string, rate, burst float64, window time.Duration) (bool, error) {
	sh := s.shardFor(key)
	sh.mu.Lock()
	b, ok := sh.buckets[key]
	if !ok {
		b = &bucket{}
		sh.buckets[key] = b
	}
	sh.mu.Unlock()
	return b.allow(rate, burst, window, time.Now()), nil
}

// StartJanitor runs until ctx is cancelled, evicting buckets idle longer
// than idleTTL every interval.
func (s *Store) StartJanitor(ctx context.Context, interval time.Duration) {
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-t.C:
				s.evictIdle(now)
			}
		}
	}()
}

func (s *Store) evictIdle(now time.Time) {
	for _, sh := range s.shards {
		sh.mu.Lock()
		for k, b := range sh.buckets {
			if b.idleSince(now) > s.idleTTL {
				delete(sh.buckets, k)
			}
		}
		sh.mu.Unlock()
	}
}

// ActiveBuckets reports the current total bucket count, across shards, for
// the formelay_ratelimit_buckets_active metric.
func (s *Store) ActiveBuckets() int {
	total := 0
	for _, sh := range s.shards {
		sh.mu.Lock()
		total += len(sh.buckets)
		sh.mu.Unlock()
	}
	return total
}
