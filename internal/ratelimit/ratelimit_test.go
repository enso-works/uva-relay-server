package ratelimit

import (
	"context"
	"testing"
	"time"
)

func TestConnectionRateLimit(t *testing.T) {
	l := NewLimiter(nil, 3, 100)
	ctx := context.Background()
	ip := "192.168.1.1"

	for i := 0; i < 3; i++ {
		if !l.AllowConnection(ctx, ip) {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}

	if l.AllowConnection(ctx, ip) {
		t.Fatal("4th request should be rejected")
	}

	// Different IP should still be allowed
	if !l.AllowConnection(ctx, "10.0.0.1") {
		t.Fatal("different IP should be allowed")
	}
}

func TestMessageRateLimit(t *testing.T) {
	l := NewLimiter(nil, 100, 5)
	ctx := context.Background()
	pairID := "pair-1"

	for i := 0; i < 5; i++ {
		if !l.AllowMessage(ctx, pairID) {
			t.Fatalf("message %d should be allowed", i+1)
		}
	}

	if l.AllowMessage(ctx, pairID) {
		t.Fatal("6th message should be rejected")
	}
}

func TestRateLimitResetsAfterWindow(t *testing.T) {
	l := NewLimiter(nil, 100, 2)
	ctx := context.Background()
	pairID := "pair-reset"

	// Use up the limit
	for i := 0; i < 2; i++ {
		if !l.AllowMessage(ctx, pairID) {
			t.Fatalf("message %d should be allowed", i+1)
		}
	}
	if l.AllowMessage(ctx, pairID) {
		t.Fatal("should be rejected at limit")
	}

	// Manually expire the entries to simulate window passing
	l.mu.Lock()
	past := time.Now().Add(-2 * time.Second)
	l.msgWindows[pairID] = []time.Time{past, past}
	l.mu.Unlock()

	if !l.AllowMessage(ctx, pairID) {
		t.Fatal("should be allowed after window passes")
	}
}

func TestZeroLimitAllowsAll(t *testing.T) {
	l := NewLimiter(nil, 0, 0)
	ctx := context.Background()

	for i := 0; i < 100; i++ {
		if !l.AllowConnection(ctx, "any-ip") {
			t.Fatal("zero limit should allow all connections")
		}
		if !l.AllowMessage(ctx, "any-pair") {
			t.Fatal("zero limit should allow all messages")
		}
	}
}
