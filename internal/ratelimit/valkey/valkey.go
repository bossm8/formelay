// Package valkey implements ratelimit.Store against a Valkey (or
// Redis-protocol-compatible) server, for sharing rate-limit state across
// multiple formelay replicas. Bucket state is updated atomically server-side
// via a Lua script, and keys self-expire (no separate janitor needed).
package valkey

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/valkey-io/valkey-go"
)

// tokenBucketScript atomically refills and consumes one token from the
// bucket stored under KEYS[1], returning 1 (allowed) or 0 (denied).
const tokenBucketScript = `
local key = KEYS[1]
local rate = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local burst = tonumber(ARGV[3])
local now = tonumber(ARGV[4])
local ttl = tonumber(ARGV[5])

local data = redis.call('HMGET', key, 'tokens', 'ts')
local tokens = tonumber(data[1])
local last = tonumber(data[2])

if tokens == nil then
  tokens = burst
  last = now
end

local elapsed = now - last
if elapsed < 0 then elapsed = 0 end
local refill = rate / window
tokens = math.min(burst, tokens + elapsed * refill)

local allowed = 0
if tokens >= 1 then
  tokens = tokens - 1
  allowed = 1
end

redis.call('HSET', key, 'tokens', tostring(tokens), 'ts', tostring(now))
redis.call('EXPIRE', key, ttl)

return allowed
`

// OnError selects behavior when the Valkey server itself is unreachable or
// times out — deliberately configurable, not hardcoded (see plan Security
// Model / AI classifier on_error precedent).
type OnError string

const (
	OnErrorAllow OnError = "allow"
	OnErrorDeny  OnError = "deny"
)

type Config struct {
	Addresses   []string
	Password    string
	DB          int
	DialTimeout time.Duration
	KeyPrefix   string
	OnError     OnError
}

// Store implements ratelimit.Store against Valkey.
type Store struct {
	client  valkey.Client
	cfg     Config
	onError OnError
}

func New(cfg Config) (*Store, error) {
	opt := valkey.ClientOption{
		InitAddress: cfg.Addresses,
		Password:    cfg.Password,
		SelectDB:    cfg.DB,
	}
	if cfg.DialTimeout > 0 {
		opt.Dialer = net.Dialer{Timeout: cfg.DialTimeout}
	}
	client, err := valkey.NewClient(opt)
	if err != nil {
		return nil, fmt.Errorf("ratelimit/valkey: connect: %w", err)
	}
	onError := cfg.OnError
	if onError == "" {
		onError = OnErrorAllow
	}
	return &Store{client: client, cfg: cfg, onError: onError}, nil
}

func (s *Store) Close() {
	s.client.Close()
}

func (s *Store) Allow(ctx context.Context, key string, rate, burst float64, window time.Duration) (bool, error) {
	fullKey := s.cfg.KeyPrefix + key
	ttlSeconds := int(window.Seconds()) * 4
	if ttlSeconds < 1 {
		ttlSeconds = 1
	}
	now := float64(time.Now().UnixNano()) / 1e9

	cmd := s.client.B().Eval().
		Script(tokenBucketScript).
		Numkeys(1).
		Key(fullKey).
		Arg(
			fmt.Sprintf("%f", rate),
			fmt.Sprintf("%f", window.Seconds()),
			fmt.Sprintf("%f", burst),
			fmt.Sprintf("%f", now),
			fmt.Sprintf("%d", ttlSeconds),
		).
		Build()

	resp := s.client.Do(ctx, cmd)
	allowed, err := resp.ToInt64()
	if err != nil {
		return s.onErrorAllows(), nil
	}
	return allowed == 1, nil
}

// onErrorAllows resolves the OnError setting to a boolean, isolated as its
// own method so the on_error decision (allow vs. deny on a Do() failure) is
// unit-testable without a live or fake valkey.Client — see valkey_test.go.
func (s *Store) onErrorAllows() bool {
	return s.onError != OnErrorDeny
}
