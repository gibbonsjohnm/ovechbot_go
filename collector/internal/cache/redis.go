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
	GameLogKey    = "ovechkin:game_log"
	StandingsKey  = "standings:now"
	GameLogTTL    = 12 * time.Hour
	StandingsTTL  = 1 * time.Hour
)

// Cache writes game log and standings to Redis for the predictor.
type Cache struct {
	client *redis.Client
}

// New returns a Cache that uses the given Redis client.
func New(client *redis.Client) *Cache {
	return &Cache{client: client}
}

// WriteGameLog stores the merged game log (all seasons) as JSON.
func (c *Cache) WriteGameLog(ctx context.Context, entries []nhl.GameLogEntry) error {
	b, err := json.Marshal(entries)
	if err != nil {
		return fmt.Errorf("marshal game log: %w", err)
	}
	return c.client.Set(ctx, GameLogKey, string(b), GameLogTTL).Err()
}

// WriteStandings stores standings as JSON (map teamAbbrev -> {gamesPlayed, goalAgainst, goalFor}).
func (c *Cache) WriteStandings(ctx context.Context, standings map[string]nhl.StandingsTeam) error {
	b, err := json.Marshal(standings)
	if err != nil {
		return fmt.Errorf("marshal standings: %w", err)
	}
	return c.client.Set(ctx, StandingsKey, string(b), StandingsTTL).Err()
}
