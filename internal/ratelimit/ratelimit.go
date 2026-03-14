package ratelimit

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// Limiter provides rate limiting with Redis backend and in-memory fallback.
type Limiter struct {
	rdb        *redis.Client // nil for in-memory only
	connPerMin int
	msgPerSec  int

	// In-memory fallback
	mu          sync.Mutex
	connWindows map[string][]time.Time // IP -> timestamps
	msgWindows  map[string][]time.Time // pairID -> timestamps
}

// NewLimiter creates a rate limiter. If rdb is nil, only in-memory limiting is used.
func NewLimiter(rdb *redis.Client, connPerMin, msgPerSec int) *Limiter {
	l := &Limiter{
		rdb:         rdb,
		connPerMin:  connPerMin,
		msgPerSec:   msgPerSec,
		connWindows: make(map[string][]time.Time),
		msgWindows:  make(map[string][]time.Time),
	}
	go l.cleanup()
	return l
}

// AllowConnection checks per-IP connection rate using a 1-minute sliding window.
func (l *Limiter) AllowConnection(ctx context.Context, ip string) bool {
	if l.connPerMin <= 0 {
		return true
	}
	if l.rdb != nil {
		allowed, err := l.redisAllow(ctx, fmt.Sprintf("relay:ratelimit:conn:%s", ip), l.connPerMin, time.Minute)
		if err == nil {
			return allowed
		}
		// Fall back to in-memory on Redis error
	}
	return l.memAllow(&l.connWindows, ip, l.connPerMin, time.Minute)
}

// AllowMessage checks per-pair message rate using a 1-second sliding window.
func (l *Limiter) AllowMessage(ctx context.Context, pairID string) bool {
	if l.msgPerSec <= 0 {
		return true
	}
	if l.rdb != nil {
		allowed, err := l.redisAllow(ctx, fmt.Sprintf("relay:ratelimit:msg:%s", pairID), l.msgPerSec, time.Second)
		if err == nil {
			return allowed
		}
	}
	return l.memAllow(&l.msgWindows, pairID, l.msgPerSec, time.Second)
}

func (l *Limiter) redisAllow(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	now := time.Now()
	nowNano := float64(now.UnixNano())
	cutoff := float64(now.Add(-window).UnixNano())

	pipe := l.rdb.Pipeline()
	pipe.ZRemRangeByScore(ctx, key, "-inf", fmt.Sprintf("%f", cutoff))
	pipe.ZAdd(ctx, key, redis.Z{Score: nowNano, Member: nowNano})
	cardCmd := pipe.ZCard(ctx, key)
	pipe.Expire(ctx, key, 2*window)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return false, err
	}

	count := cardCmd.Val()
	if count > int64(limit) {
		// Over limit - remove the entry we just added
		l.rdb.ZRem(ctx, key, nowNano)
		return false, nil
	}
	return true, nil
}

func (l *Limiter) memAllow(windows *map[string][]time.Time, key string, limit int, window time.Duration) bool {
	now := time.Now()
	cutoff := now.Add(-window)

	l.mu.Lock()
	defer l.mu.Unlock()

	entries := (*windows)[key]

	// Remove expired entries
	valid := entries[:0]
	for _, t := range entries {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}

	if len(valid) >= limit {
		(*windows)[key] = valid
		return false
	}

	(*windows)[key] = append(valid, now)
	return true
}

// cleanup periodically removes stale in-memory entries.
func (l *Limiter) cleanup() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		l.mu.Lock()
		now := time.Now()
		cleanMap(l.connWindows, now, 2*time.Minute)
		cleanMap(l.msgWindows, now, 2*time.Second)
		l.mu.Unlock()
	}
}

func cleanMap(m map[string][]time.Time, now time.Time, maxAge time.Duration) {
	cutoff := now.Add(-maxAge)
	for key, entries := range m {
		valid := entries[:0]
		for _, t := range entries {
			if t.After(cutoff) {
				valid = append(valid, t)
			}
		}
		if len(valid) == 0 {
			delete(m, key)
		} else {
			m[key] = valid
		}
	}
}
