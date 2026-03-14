package connections

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const (
	redisKeyPrefix  = "relay:"
	waitingPrefix   = redisKeyPrefix + "waiting:"
	peerPrefix      = redisKeyPrefix + "peer:"
	waitingSetKey   = redisKeyPrefix + "waiting_usernames"
	pairedSetKey    = redisKeyPrefix + "paired_connids"
	defaultRedisTTL = 5 * time.Minute
)

// Lua scripts for atomic operations.
var (
	// setWaitingScript atomically gets the old waiting connID, sets the new one,
	// and adds the username to the waiting set.
	setWaitingScript = redis.NewScript(`
		local old = redis.call("GET", KEYS[1])
		redis.call("SET", KEYS[1], ARGV[1], "EX", ARGV[2])
		redis.call("SADD", KEYS[2], ARGV[3])
		if old == false then return "" end
		if old == ARGV[1] then return "" end
		return old
	`)

	// removeWaitingScript atomically checks if the waiting connID matches
	// and removes it if so.
	removeWaitingScript = redis.NewScript(`
		if redis.call("GET", KEYS[1]) == ARGV[1] then
			redis.call("DEL", KEYS[1])
			redis.call("SREM", KEYS[2], ARGV[2])
			return 1
		end
		return 0
	`)

	// removePairScript atomically gets the peer, deletes both peer entries,
	// and removes both from the paired set.
	removePairScript = redis.NewScript(`
		local peer = redis.call("GET", KEYS[1])
		if peer == false then return "" end
		redis.call("DEL", KEYS[1])
		redis.call("DEL", KEYS[2]..peer)
		redis.call("SREM", KEYS[3], ARGV[1], peer)
		return peer
	`)
)

type redisStore struct {
	rdb *redis.Client
	ttl time.Duration
}

// NewRedisStore creates a Redis-backed Store implementation.
func NewRedisStore(rdb *redis.Client) Store {
	return &redisStore{
		rdb: rdb,
		ttl: defaultRedisTTL,
	}
}

func (s *redisStore) SetWaiting(ctx context.Context, username, connID string) (string, error) {
	result, err := setWaitingScript.Run(ctx, s.rdb,
		[]string{waitingPrefix + username, waitingSetKey},
		connID, int(s.ttl.Seconds()), username,
	).Text()
	if err == redis.Nil {
		return "", nil
	}
	return result, err
}

func (s *redisStore) GetWaiting(ctx context.Context, username string) (string, bool) {
	val, err := s.rdb.Get(ctx, waitingPrefix+username).Result()
	if err != nil {
		return "", false
	}
	// Refresh TTL on access
	s.rdb.Expire(ctx, waitingPrefix+username, s.ttl)
	return val, true
}

func (s *redisStore) RemoveWaiting(ctx context.Context, username, connID string) bool {
	result, err := removeWaitingScript.Run(ctx, s.rdb,
		[]string{waitingPrefix + username, waitingSetKey},
		connID, username,
	).Int()
	return err == nil && result == 1
}

func (s *redisStore) CreatePair(ctx context.Context, serverConnID, clientConnID, username string) (string, error) {
	pairID := uuid.NewString()

	pipe := s.rdb.TxPipeline()
	pipe.Del(ctx, waitingPrefix+username)
	pipe.SRem(ctx, waitingSetKey, username)
	pipe.Set(ctx, peerPrefix+serverConnID, clientConnID, s.ttl)
	pipe.Set(ctx, peerPrefix+clientConnID, serverConnID, s.ttl)
	pipe.SAdd(ctx, pairedSetKey, serverConnID, clientConnID)
	_, err := pipe.Exec(ctx)

	return pairID, err
}

func (s *redisStore) GetPeer(ctx context.Context, connID string) (string, bool) {
	val, err := s.rdb.Get(ctx, peerPrefix+connID).Result()
	if err != nil {
		return "", false
	}
	// Refresh TTL on activity for both sides
	pipe := s.rdb.Pipeline()
	pipe.Expire(ctx, peerPrefix+connID, s.ttl)
	pipe.Expire(ctx, peerPrefix+val, s.ttl)
	pipe.Exec(ctx)
	return val, true
}

func (s *redisStore) RemovePair(ctx context.Context, connID string) (string, bool) {
	result, err := removePairScript.Run(ctx, s.rdb,
		[]string{peerPrefix + connID, peerPrefix, pairedSetKey},
		connID,
	).Text()
	if err != nil || result == "" {
		return "", false
	}
	return result, true
}

func (s *redisStore) IsPaired(ctx context.Context, connID string) bool {
	n, err := s.rdb.Exists(ctx, peerPrefix+connID).Result()
	return err == nil && n > 0
}

func (s *redisStore) IsWaiting(ctx context.Context, username string) bool {
	n, err := s.rdb.Exists(ctx, waitingPrefix+username).Result()
	return err == nil && n > 0
}

func (s *redisStore) Stats(ctx context.Context) (int, int) {
	w, _ := s.rdb.SCard(ctx, waitingSetKey).Result()
	p, _ := s.rdb.SCard(ctx, pairedSetKey).Result()
	return int(w), int(p) / 2
}

func (s *redisStore) GetWaitingUsernames(ctx context.Context) []string {
	names, _ := s.rdb.SMembers(ctx, waitingSetKey).Result()
	return names
}
