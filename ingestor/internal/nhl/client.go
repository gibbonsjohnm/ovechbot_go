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
	LandingURLFmt    = "https://api-web.nhle.com/v1/player/%d/landing"
	BoxscoreURLFmt   = "https://api-web.nhle.com/v1/gamecenter/%d/boxscore"
)

// Client polls the NHL API for player stats.
type Client struct {
	httpClient *http.Client
	baseURL    string
}

// NewClient returns an NHL API client with default timeout.
func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 15 * time.Second},
		baseURL:    fmt.Sprintf(LandingURLFmt, OvechkinPlayerID),
	}
}

// LandingResponse represents the NHL player landing API response (subset we need).
type LandingResponse struct {
	CareerTotals struct {
		RegularSeason struct {
			Goals int `json:"goals"`
		} `json:"regularSeason"`
	} `json:"careerTotals"`
}

// CareerGoals returns the current career regular-season goal count for the player.
func (c *Client) CareerGoals(ctx context.Context) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL, nil)
	if err != nil {
		return 0, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("nhl api status %d: %s", resp.StatusCode, string(body))
	}

	var landing LandingResponse
	if err := json.NewDecoder(resp.Body).Decode(&landing); err != nil {
		return 0, fmt.Errorf("decode response: %w", err)
	}

	return landing.CareerTotals.RegularSeason.Goals, nil
}

// LastGoalGameInfo holds opponent and goalie for the most recent game in which the player scored (from last 5 games).
type LastGoalGameInfo struct {
	Opponent     string // e.g. "NSH"
	OpponentName string // e.g. "Predators"
	GoalieName   string // opposing starter
}

// LastGoalGameInfo fetches the most recent game (from last 5) where the player scored and returns opponent + goalie from boxscore.
// Returns nil if no recent game with a goal, or on error (caller can still emit without enrichment).
func (c *Client) LastGoalGameInfo(ctx context.Context) (*LastGoalGameInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL, nil)
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
		return nil, fmt.Errorf("nhl api status %d", resp.StatusCode)
	}
	var landing struct {
		Last5Games []struct {
			GameID         int    `json:"gameId"`
			OpponentAbbrev string `json:"opponentAbbrev"`
			Goals          int    `json:"goals"`
		} `json:"last5Games"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&landing); err != nil {
		return nil, err
	}
	var gameID int
	var oppAbbrev string
	for _, g := range landing.Last5Games {
		if g.Goals > 0 {
			gameID = g.GameID
			oppAbbrev = g.OpponentAbbrev
			break
		}
	}
	if gameID == 0 {
		return nil, nil
	}
	boxURL := fmt.Sprintf(BoxscoreURLFmt, gameID)
	req2, _ := http.NewRequestWithContext(ctx, http.MethodGet, boxURL, nil)
	req2.Header.Set("Accept", "application/json")
	resp2, err := c.httpClient.Do(req2)
	if err != nil {
		return &LastGoalGameInfo{Opponent: oppAbbrev}, nil
	}
	defer resp2.Body.Close()
	var box struct {
		AwayTeam struct {
			Abbrev     string `json:"abbrev"`
			CommonName struct { Default string `json:"default"` } `json:"commonName"`
		} `json:"awayTeam"`
		HomeTeam struct {
			Abbrev     string `json:"abbrev"`
			CommonName struct { Default string `json:"default"` } `json:"commonName"`
		} `json:"homeTeam"`
		PlayerByGameStats struct {
			AwayTeam struct {
				Goalies []struct {
					Name    struct { Default string `json:"default"` } `json:"name"`
					Starter bool   `json:"starter"`
				} `json:"goalies"`
			} `json:"awayTeam"`
			HomeTeam struct {
				Goalies []struct {
					Name    struct { Default string `json:"default"` } `json:"name"`
					Starter bool   `json:"starter"`
				} `json:"goalies"`
			} `json:"homeTeam"`
		} `json:"playerByGameStats"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&box); err != nil {
		return &LastGoalGameInfo{Opponent: oppAbbrev}, nil
	}
	var oppName, goalieName string
	if box.AwayTeam.Abbrev == "WSH" {
		oppName = box.HomeTeam.CommonName.Default
		for _, g := range box.PlayerByGameStats.HomeTeam.Goalies {
			if g.Starter {
				goalieName = g.Name.Default
				break
			}
		}
		if goalieName == "" && len(box.PlayerByGameStats.HomeTeam.Goalies) > 0 {
			goalieName = box.PlayerByGameStats.HomeTeam.Goalies[0].Name.Default
		}
	} else {
		oppName = box.AwayTeam.CommonName.Default
		for _, g := range box.PlayerByGameStats.AwayTeam.Goalies {
			if g.Starter {
				goalieName = g.Name.Default
				break
			}
		}
		if goalieName == "" && len(box.PlayerByGameStats.AwayTeam.Goalies) > 0 {
			goalieName = box.PlayerByGameStats.AwayTeam.Goalies[0].Name.Default
		}
	}
	if oppName == "" {
		oppName = oppAbbrev
	}
	return &LastGoalGameInfo{
		Opponent:     oppAbbrev,
		OpponentName: oppName,
		GoalieName:   goalieName,
	}, nil
}
