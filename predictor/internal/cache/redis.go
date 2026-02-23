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
}

// StandingsTeam matches collector's nhl.StandingsTeam (includes L10 and strength metrics).
type StandingsTeam struct {
	TeamAbbrev           string  `json:"teamAbbrev"`
	GamesPlayed          int     `json:"gamesPlayed"`
	GoalAgainst          int     `json:"goalAgainst"`
	GoalsFor             int     `json:"goalFor"`
	GoalDifferential     int     `json:"goalDifferential"`
	GoalDifferentialPctg float64 `json:"goalDifferentialPctg"`
	GoalsForPctg         float64 `json:"goalsForPctg"`
	PointPctg            float64 `json:"pointPctg"`
	L10GamesPlayed       int     `json:"l10GamesPlayed"`
	L10GoalsAgainst      int     `json:"l10GoalsAgainst"`
	L10GoalsFor          int     `json:"l10GoalsFor"`
}

const (
	GameLogKey   = "ovechkin:game_log"
	StandingsKey = "standings:now"
)

// Reader reads game log and standings from Redis (written by collector).
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

// ReadStandings returns standings map or nil if missing/invalid.
func (r *Reader) ReadStandings(ctx context.Context) (map[string]StandingsTeam, error) {
	b, err := r.client.Get(ctx, StandingsKey).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out map[string]StandingsTeam
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("unmarshal standings: %w", err)
	}
	return out, nil
}
