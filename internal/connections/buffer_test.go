package connections

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestBuffer(t *testing.T, maxSize int) (*Buffer, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rdb.Close() })
	return NewBuffer(rdb, maxSize), mr
}

func TestBufferPushAndGetFrames(t *testing.T) {
	buf, _ := newTestBuffer(t, 10)
	ctx := context.Background()

	buf.Push(ctx, "alice", "c2s", 1, []byte("hello"))
	buf.Push(ctx, "alice", "s2c", 1, []byte("world"))

	frames, err := buf.GetFrames(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 2 {
		t.Fatalf("expected 2 frames, got %d", len(frames))
	}
	if frames[0].Direction != "c2s" {
		t.Errorf("expected direction c2s, got %s", frames[0].Direction)
	}
	if frames[1].Direction != "s2c" {
		t.Errorf("expected direction s2c, got %s", frames[1].Direction)
	}
}

func TestBufferRingCap(t *testing.T) {
	buf, _ := newTestBuffer(t, 3)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		buf.Push(ctx, "bob", "c2s", 1, []byte{byte(i)})
	}

	frames, err := buf.GetFrames(ctx, "bob")
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 3 {
		t.Fatalf("expected 3 frames (capped), got %d", len(frames))
	}
}

func TestBufferClear(t *testing.T) {
	buf, _ := newTestBuffer(t, 10)
	ctx := context.Background()

	buf.Push(ctx, "carol", "c2s", 1, []byte("data"))
	buf.Clear(ctx, "carol")

	frames, err := buf.GetFrames(ctx, "carol")
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 0 {
		t.Fatalf("expected 0 frames after clear, got %d", len(frames))
	}
}

func TestBufferNilSafe(t *testing.T) {
	var buf *Buffer
	ctx := context.Background()

	// None of these should panic
	buf.Push(ctx, "user", "c2s", 1, []byte("data"))
	buf.Clear(ctx, "user")

	frames, err := buf.GetFrames(ctx, "user")
	if err != nil {
		t.Fatal(err)
	}
	if frames != nil {
		t.Fatalf("expected nil frames from nil buffer, got %v", frames)
	}
}

func TestBufferIsolation(t *testing.T) {
	buf, _ := newTestBuffer(t, 10)
	ctx := context.Background()

	buf.Push(ctx, "alice", "c2s", 1, []byte("alice-msg"))
	buf.Push(ctx, "bob", "c2s", 1, []byte("bob-msg"))

	aliceFrames, _ := buf.GetFrames(ctx, "alice")
	bobFrames, _ := buf.GetFrames(ctx, "bob")

	if len(aliceFrames) != 1 || len(bobFrames) != 1 {
		t.Fatalf("expected 1 frame each, got alice=%d bob=%d", len(aliceFrames), len(bobFrames))
	}
}
