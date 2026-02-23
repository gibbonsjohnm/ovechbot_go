package nhl

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const ovechkinPlayerID = 8471214
const boxscoreURLFmt = "https://api-web.nhle.com/v1/gamecenter/%d/boxscore"

// PlayerGameStats is Ovechkin's line for one game.
type PlayerGameStats struct {
	Goals   int
	Assists int
	Points  int
	TOI     string
	Shifts  int
	SOG     int
}

// OvechkinGameStats fetches the boxscore for the game and returns Ovechkin's stats. Nil if not found.
func OvechkinGameStats(ctx context.Context, gameID int64) (*PlayerGameStats, error) {
	url := fmt.Sprintf(boxscoreURLFmt, gameID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
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
		return nil, fmt.Errorf("boxscore status %d", resp.StatusCode)
	}
	var box struct {
		PlayerByGameStats struct {
			AwayTeam struct {
				Forwards []struct {
					PlayerID int    `json:"playerId"`
					Goals    int    `json:"goals"`
					Assists  int    `json:"assists"`
					Points   int    `json:"points"`
					TOI      string `json:"toi"`
					Shifts   int    `json:"shifts"`
					SOG      int    `json:"sog"`
				} `json:"forwards"`
				Defense []struct {
					PlayerID int    `json:"playerId"`
					Goals    int    `json:"goals"`
					Assists  int    `json:"assists"`
					Points   int    `json:"points"`
					TOI      string `json:"toi"`
					Shifts   int    `json:"shifts"`
					SOG      int    `json:"sog"`
				} `json:"defense"`
			} `json:"awayTeam"`
			HomeTeam struct {
				Forwards []struct {
					PlayerID int    `json:"playerId"`
					Goals    int    `json:"goals"`
					Assists  int    `json:"assists"`
					Points   int    `json:"points"`
					TOI      string `json:"toi"`
					Shifts   int    `json:"shifts"`
					SOG      int    `json:"sog"`
				} `json:"forwards"`
				Defense []struct {
					PlayerID int    `json:"playerId"`
					Goals    int    `json:"goals"`
					Assists  int    `json:"assists"`
					Points   int    `json:"points"`
					TOI      string `json:"toi"`
					Shifts   int    `json:"shifts"`
					SOG      int    `json:"sog"`
				} `json:"defense"`
			} `json:"homeTeam"`
		} `json:"playerByGameStats"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&box); err != nil {
		return nil, err
	}
	pb := &box.PlayerByGameStats
	for _, p := range pb.AwayTeam.Forwards {
		if p.PlayerID == ovechkinPlayerID {
			return &PlayerGameStats{Goals: p.Goals, Assists: p.Assists, Points: p.Points, TOI: p.TOI, Shifts: p.Shifts, SOG: p.SOG}, nil
		}
	}
	for _, p := range pb.AwayTeam.Defense {
		if p.PlayerID == ovechkinPlayerID {
			return &PlayerGameStats{Goals: p.Goals, Assists: p.Assists, Points: p.Points, TOI: p.TOI, Shifts: p.Shifts, SOG: p.SOG}, nil
		}
	}
	for _, p := range pb.HomeTeam.Forwards {
		if p.PlayerID == ovechkinPlayerID {
			return &PlayerGameStats{Goals: p.Goals, Assists: p.Assists, Points: p.Points, TOI: p.TOI, Shifts: p.Shifts, SOG: p.SOG}, nil
		}
	}
	for _, p := range pb.HomeTeam.Defense {
		if p.PlayerID == ovechkinPlayerID {
			return &PlayerGameStats{Goals: p.Goals, Assists: p.Assists, Points: p.Points, TOI: p.TOI, Shifts: p.Shifts, SOG: p.SOG}, nil
		}
	}
	return nil, nil
}
