package connections

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/anthropics/uvame-relay/internal/protocol"
	"github.com/redis/go-redis/v9"
)

const bufferTTL = 1 * time.Hour

// Buffer stores recent messages per username for replay on reconnect.
// Uses Redis lists as a ring buffer capped at maxSize.
// If rdb is nil, all operations are no-ops.
type Buffer struct {
	rdb     *redis.Client
	maxSize int
	ttl     time.Duration
}

// NewBuffer creates a new Buffer. If rdb is nil, the buffer is a no-op.
func NewBuffer(rdb *redis.Client, maxSize int) *Buffer {
	if rdb == nil {
		return nil
	}
	return &Buffer{
		rdb:     rdb,
		maxSize: maxSize,
		ttl:     bufferTTL,
	}
}

func bufferKey(username string) string {
	return "relay:buffer:" + username
}

// Push adds a frame to the ring buffer for the given username.
func (b *Buffer) Push(ctx context.Context, username, direction string, msgType int, data []byte) {
	if b == nil || b.rdb == nil {
		return
	}

	frame := protocol.BufferedFrame{
		Timestamp: time.Now().UnixMilli(),
		Direction: direction,
		MsgType:   msgType,
		Data:      base64.StdEncoding.EncodeToString(data),
	}

	encoded, err := json.Marshal(frame)
	if err != nil {
		slog.Warn("buffer: failed to marshal frame", "err", err)
		return
	}

	key := bufferKey(username)
	pipe := b.rdb.Pipeline()
	pipe.RPush(ctx, key, encoded)
	pipe.LTrim(ctx, key, int64(-b.maxSize), -1)
	pipe.Expire(ctx, key, b.ttl)
	if _, err := pipe.Exec(ctx); err != nil {
		slog.Warn("buffer: failed to push frame", "err", err)
	}
}

// GetFrames returns all buffered frames for a username.
func (b *Buffer) GetFrames(ctx context.Context, username string) ([]protocol.BufferedFrame, error) {
	if b == nil || b.rdb == nil {
		return nil, nil
	}

	key := bufferKey(username)
	vals, err := b.rdb.LRange(ctx, key, 0, -1).Result()
	if err != nil {
		return nil, err
	}

	frames := make([]protocol.BufferedFrame, 0, len(vals))
	for _, v := range vals {
		var f protocol.BufferedFrame
		if err := json.Unmarshal([]byte(v), &f); err != nil {
			continue
		}
		frames = append(frames, f)
	}
	return frames, nil
}

// Clear removes all buffered frames for a username.
func (b *Buffer) Clear(ctx context.Context, username string) {
	if b == nil || b.rdb == nil {
		return
	}
	b.rdb.Del(ctx, bufferKey(username))
}
