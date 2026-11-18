package nhl

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"
)

const (
	OvechkinPlayerID = 8471214
	GameLogURLFmt    = "https://api-web.nhle.com/v1/player/%d/game-log/%s/%d" // playerID, seasonID, gameTypeID
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
	GameID         int    `json:"gameId"`
	GameDate       string `json:"gameDate"`
	OpponentAbbrev string `json:"opponentAbbrev"`
	HomeRoadFlag   string `json:"homeRoadFlag"` // "H" or "R"
	Goals          int    `json:"goals"`
	Shots          int    `json:"shots"`
}

// GameLogSeasons returns the season IDs (e.g. "20262027") to fetch as of now: the current season and the n-1 before it, oldest first.
// The season rolls over on July 1 so the upcoming season is included before its first game.
func GameLogSeasons(now time.Time, n int) []string {
	start := now.Year()
	if now.Month() < time.July {
		start--
	}
	seasons := make([]string, 0, n)
	for y := start - n + 1; y <= start; y++ {
		seasons = append(seasons, fmt.Sprintf("%d%d", y, y+1))
	}
	return seasons
}

// SortGameLog orders entries oldest-first, which is what the predictor expects.
// The NHL game-log endpoint returns each season newest-first.
func SortGameLog(entries []GameLogEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].GameDate != entries[j].GameDate {
			return entries[i].GameDate < entries[j].GameDate
		}
		return entries[i].GameID < entries[j].GameID
	})
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
			Shots          int    `json:"shots"`
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
			Shots:          g.Shots,
		})
	}
	return entries, nil
}
