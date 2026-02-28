package schedule

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const clubScheduleURL = "https://api-web.nhle.com/v1/club-schedule-season/WSH/now"

var httpClient = &http.Client{Timeout: 15 * time.Second}

// Game is the next (or current) Capitals game with ID for reminder idempotency.
type Game struct {
	GameID       int64
	HomeAbbrev   string
	AwayAbbrev   string
	StartTimeUTC time.Time
	GameState    string
	GameDate     string
}

// Opponent returns the opponent abbrev (the non-WSH team).
func (g *Game) Opponent() string {
	if g.HomeAbbrev == "WSH" {
		return g.AwayAbbrev
	}
	return g.HomeAbbrev
}

// IsHome returns true if Capitals are home.
func (g *Game) IsHome() bool {
	return g.HomeAbbrev == "WSH"
}

var inProgressStates = map[string]bool{"LIVE": true, "PRE": true, "CRIT": true}

// NextGame fetches the Capitals schedule and returns the next game (or in-progress).
func NextGame(ctx context.Context) (*Game, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, clubScheduleURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "OvechBot/1.0")
	resp, err := httpClient.Do(req)
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
	var inProgress, firstFuture *Game
	for _, g := range sched.Games {
		start, _ := time.Parse(time.RFC3339, g.StartTimeUTC)
		n := &Game{
			GameID:       g.ID,
			HomeAbbrev:   g.HomeTeam.Abbrev,
			AwayAbbrev:   g.AwayTeam.Abbrev,
			StartTimeUTC: start,
			GameState:    g.GameState,
			GameDate:     g.GameDate,
		}
		if inProgressStates[g.GameState] {
			if inProgress == nil {
				inProgress = n
			}
		}
		if g.GameState == "FUT" && !start.Before(now) && firstFuture == nil {
			firstFuture = n
		}
	}
	if inProgress != nil {
		return inProgress, nil
	}
	return firstFuture, nil
}
