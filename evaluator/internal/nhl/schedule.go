package nhl

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const scheduleURL = "https://api-web.nhle.com/v1/club-schedule-season/WSH/now"

// CompletedGame is a Caps game that has finished.
type CompletedGame struct {
	GameID          int64
	GameDate        string
	HomeAbbrev      string
	AwayAbbrev      string
	OpponentAbbrev  string
}

// CompletedGameStates are schedule gameState values for finished games (NHL API uses FINAL; OFF also accepted).
var CompletedGameStates = map[string]bool{"FINAL": true, "OFF": true}

// LastCompletedGame returns the most recent Capitals game with state FINAL or OFF (finished). Nil if none.
func LastCompletedGame(ctx context.Context) (*CompletedGame, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, scheduleURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("schedule status %d", resp.StatusCode)
	}
	var sched struct {
		Games []struct {
			ID           int64  `json:"id"`
			GameDate     string `json:"gameDate"`
			StartTimeUTC string `json:"startTimeUTC"`
			GameState    string `json:"gameState"`
			HomeTeam     struct{ Abbrev string `json:"abbrev"` } `json:"homeTeam"`
			AwayTeam     struct{ Abbrev string `json:"abbrev"` } `json:"awayTeam"`
		} `json:"games"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sched); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	var last *CompletedGame
	var lastStart time.Time
	for _, g := range sched.Games {
		if !CompletedGameStates[g.GameState] {
			continue
		}
		start, err := time.Parse(time.RFC3339, g.StartTimeUTC)
		if err != nil || start.After(now) {
			continue
		}
		// Pick the completed game with the latest start time (most recently finished).
		if last != nil && !start.After(lastStart) {
			continue
		}
		lastStart = start
		opp := g.AwayTeam.Abbrev
		if g.AwayTeam.Abbrev == "WSH" {
			opp = g.HomeTeam.Abbrev
		}
		last = &CompletedGame{
			GameID:         g.ID,
			GameDate:       g.GameDate,
			HomeAbbrev:     g.HomeTeam.Abbrev,
			AwayAbbrev:     g.AwayTeam.Abbrev,
			OpponentAbbrev: opp,
		}
	}
	return last, nil
}
