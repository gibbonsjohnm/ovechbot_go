package nhl

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	OvechkinPlayerID = 8471214
	GameLogURLFmt    = "https://api-web.nhle.com/v1/player/%d/game-log/%s/%d" // playerID, seasonID, gameTypeID
	StandingsNowURL  = "https://api-web.nhle.com/v1/standings/now"
	GameTypeRegular  = 2
)

// Client for free NHL API (game log, standings).
type Client struct {
	httpClient *http.Client
}

// NewClient returns a client with default timeout.
func NewClient() *Client {
	return &Client{httpClient: &http.Client{Timeout: 15 * time.Second}}
}

// GameLogEntry is one game in Ovechkin's game log (minimal for prediction).
type GameLogEntry struct {
	GameID          int    `json:"gameId"`
	GameDate        string `json:"gameDate"`
	OpponentAbbrev  string `json:"opponentAbbrev"`
	HomeRoadFlag    string `json:"homeRoadFlag"` // "H" or "R"
	Goals           int    `json:"goals"`
}

// GameLog fetches regular-season game log for the given season (e.g. "20242025").
func (c *Client) GameLog(ctx context.Context, seasonID string) ([]GameLogEntry, error) {
	url := fmt.Sprintf(GameLogURLFmt, OvechkinPlayerID, seasonID, GameTypeRegular)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("game log status %d: %s", resp.StatusCode, string(body))
	}
	var out struct {
		GameLog []struct {
			GameID         int    `json:"gameId"`
			GameDate       string `json:"gameDate"`
			OpponentAbbrev string `json:"opponentAbbrev"`
			HomeRoadFlag   string `json:"homeRoadFlag"`
			Goals          int    `json:"goals"`
		} `json:"gameLog"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	entries := make([]GameLogEntry, 0, len(out.GameLog))
	for _, g := range out.GameLog {
		entries = append(entries, GameLogEntry{
			GameID:         g.GameID,
			GameDate:       g.GameDate,
			OpponentAbbrev: g.OpponentAbbrev,
			HomeRoadFlag:   g.HomeRoadFlag,
			Goals:          g.Goals,
		})
	}
	return entries, nil
}

// StandingsTeam is per-team stats for opponent strength and form.
// Full-season: GA/GP, GF/GP, goal diff; L10 (last 10 games) for recent form; pointPctg for overall strength.
type StandingsTeam struct {
	TeamAbbrev        string  `json:"teamAbbrev"`
	GamesPlayed       int     `json:"gamesPlayed"`
	GoalAgainst       int     `json:"goalAgainst"`
	GoalsFor          int     `json:"goalFor"`
	GoalDifferential  int     `json:"goalDifferential"`
	GoalDifferentialPctg float64 `json:"goalDifferentialPctg"` // (GF-GA)/GP
	GoalsForPctg      float64 `json:"goalsForPctg"`           // GF/GP
	PointPctg         float64 `json:"pointPctg"`              // points percentage (0–1 scale in API)
	L10GamesPlayed    int     `json:"l10GamesPlayed"`
	L10GoalsAgainst   int     `json:"l10GoalsAgainst"`
	L10GoalsFor       int     `json:"l10GoalsFor"`
}

// teamAbbrevFrom extracts abbrev from API (can be string or object with default).
func teamAbbrevFrom(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	if m, ok := v.(map[string]interface{}); ok {
		if d, ok := m["default"].(string); ok {
			return d
		}
	}
	return ""
}

// Standings fetches current standings; returns team abbrev -> StandingsTeam for GA/GP lookup.
func (c *Client) Standings(ctx context.Context) (map[string]StandingsTeam, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, StandingsNowURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("standings status %d", resp.StatusCode)
	}
	var raw struct {
		Standings []struct {
			TeamAbbrev           interface{} `json:"teamAbbrev"`
			GamesPlayed          int         `json:"gamesPlayed"`
			GoalAgainst          int         `json:"goalAgainst"`
			GoalFor              int         `json:"goalFor"`
			GoalDifferential     int         `json:"goalDifferential"`
			GoalDifferentialPctg float64    `json:"goalDifferentialPctg"`
			GoalsForPctg         float64    `json:"goalsForPctg"`
			PointPctg            float64    `json:"pointPctg"`
			L10GamesPlayed       int         `json:"l10GamesPlayed"`
			L10GoalsAgainst      int         `json:"l10GoalsAgainst"`
			L10GoalsFor          int         `json:"l10GoalsFor"`
		} `json:"standings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	m := make(map[string]StandingsTeam)
	for _, t := range raw.Standings {
		abbrev := teamAbbrevFrom(t.TeamAbbrev)
		if abbrev == "" {
			continue
		}
		m[abbrev] = StandingsTeam{
			TeamAbbrev:           abbrev,
			GamesPlayed:          t.GamesPlayed,
			GoalAgainst:          t.GoalAgainst,
			GoalsFor:             t.GoalFor,
			GoalDifferential:     t.GoalDifferential,
			GoalDifferentialPctg: t.GoalDifferentialPctg,
			GoalsForPctg:         t.GoalsForPctg,
			PointPctg:            t.PointPctg,
			L10GamesPlayed:       t.L10GamesPlayed,
			L10GoalsAgainst:      t.L10GoalsAgainst,
			L10GoalsFor:          t.L10GoalsFor,
		}
	}
	return m, nil
}
