package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"ovechbot_go/collector/internal/nhl"

	"github.com/redis/go-redis/v9"
)

const (
	GameLogKey = "ovechkin:game_log"
	minTTL     = 1 * time.Hour
)

// TTLFor returns how long written keys live when the collector refreshes every interval.
// It is twice the interval so one failed run doesn't empty the cache; a TTL shorter than the interval
// once left the predictor without data ~5 of every 6 hours.
func TTLFor(interval time.Duration) time.Duration {
	if ttl := 2 * interval; ttl > minTTL {
		return ttl
	}
	return minTTL
}

// Cache writes the game log to Redis for the predictor.
type Cache struct {
	client *redis.Client
	ttl    time.Duration
}

// New returns a Cache that uses the given Redis client, sized for a collector that refreshes every interval.
func New(client *redis.Client, interval time.Duration) *Cache {
	return &Cache{client: client, ttl: TTLFor(interval)}
}

// WriteGameLog stores the merged game log (all seasons) as JSON.
func (c *Cache) WriteGameLog(ctx context.Context, entries []nhl.GameLogEntry) error {
	b, err := json.Marshal(entries)
	if err != nil {
		return fmt.Errorf("marshal game log: %w", err)
	}
	return c.client.Set(ctx, GameLogKey, string(b), c.ttl).Err()
}
