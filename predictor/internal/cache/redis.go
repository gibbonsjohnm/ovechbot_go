package cache

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// GameLogEntry matches collector's nhl.GameLogEntry (minimal).
type GameLogEntry struct {
	GameID         int    `json:"gameId"`
	GameDate       string `json:"gameDate"`
	OpponentAbbrev string `json:"opponentAbbrev"`
	HomeRoadFlag   string `json:"homeRoadFlag"`
	Goals          int    `json:"goals"`
	Shots          int    `json:"shots"`
}

const GameLogKey = "ovechkin:game_log"

// Reader reads the game log from Redis (written by collector).
type Reader struct {
	client *redis.Client
}

// NewReader returns a Reader.
func NewReader(client *redis.Client) *Reader {
	return &Reader{client: client}
}

// ReadGameLog returns the merged game log or nil if missing/invalid.
func (r *Reader) ReadGameLog(ctx context.Context) ([]GameLogEntry, error) {
	b, err := r.client.Get(ctx, GameLogKey).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []GameLogEntry
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("unmarshal game log: %w", err)
	}
	return out, nil
}
