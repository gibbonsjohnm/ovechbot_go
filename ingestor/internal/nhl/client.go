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
	CapitalsAbbrev   = "WSH"
	LandingURLFmt    = "https://api-web.nhle.com/v1/player/%d/landing"
	BoxscoreURLFmt   = "https://api-web.nhle.com/v1/gamecenter/%d/boxscore"
	PlayByPlayURLFmt = "https://api-web.nhle.com/v1/gamecenter/%d/play-by-play"
	ScoreNowURL      = "https://api-web.nhle.com/v1/score/now"
)

// LiveGameStates are states where we watch for live goals (score/now updates in real time).
var LiveGameStates = map[string]bool{"LIVE": true, "CRIT": true}

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
	req.Header.Set("User-Agent", "OvechBot/1.0")

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
	req.Header.Set("User-Agent", "OvechBot/1.0")
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
	req2, err := http.NewRequestWithContext(ctx, http.MethodGet, boxURL, nil)
	if err != nil {
		return &LastGoalGameInfo{Opponent: oppAbbrev}, nil
	}
	req2.Header.Set("Accept", "application/json")
	req2.Header.Set("User-Agent", "OvechBot/1.0")
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

// GameGoal is a single goal from the score/now API (subset of fields).
type GameGoal struct {
	PlayerID    int `json:"playerId"`
	GoalsToDate int `json:"goalsToDate"`
}

// CapsGame is the Washington Capitals game from score/now, when WSH is home or away.
type CapsGame struct {
	GameID     int        `json:"id"`
	GameState  string     `json:"gameState"`
	Goals      []GameGoal `json:"goals"`
	HomeAbbrev string     `json:"-"`
	AwayAbbrev string     `json:"-"`
}

// CapsGameFromScoreNow fetches score/now and returns the Capitals game if any (WSH home or away).
// Returns nil when there is no WSH game in the current score window.
func (c *Client) CapsGameFromScoreNow(ctx context.Context) (*CapsGame, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ScoreNowURL, nil)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "OvechBot/1.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("score/now api status %d: %s", resp.StatusCode, string(body))
	}

	var payload struct {
		Games []struct {
			ID         int    `json:"id"`
			GameState  string `json:"gameState"`
			AwayTeam   struct{ Abbrev string `json:"abbrev"` } `json:"awayTeam"`
			HomeTeam   struct{ Abbrev string `json:"abbrev"` } `json:"homeTeam"`
			Goals      []GameGoal `json:"goals"`
		} `json:"games"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode score/now: %w", err)
	}

	for _, g := range payload.Games {
		if g.AwayTeam.Abbrev != CapitalsAbbrev && g.HomeTeam.Abbrev != CapitalsAbbrev {
			continue
		}
		return &CapsGame{
			GameID:     g.ID,
			GameState:  g.GameState,
			Goals:      g.Goals,
			HomeAbbrev: g.HomeTeam.Abbrev,
			AwayAbbrev: g.AwayTeam.Abbrev,
		}, nil
	}
	return nil, nil
}

// GoalGameInfo fetches opponent and goalie for a specific game from its boxscore.
// Used to enrich real-time goal events when we already know the game ID.
func (c *Client) GoalGameInfo(ctx context.Context, gameID int) (*LastGoalGameInfo, error) {
	boxURL := fmt.Sprintf(BoxscoreURLFmt, gameID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, boxURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "OvechBot/1.0")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("boxscore status %d", resp.StatusCode)
	}
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
	if err := json.NewDecoder(resp.Body).Decode(&box); err != nil {
		return nil, err
	}
	var oppAbbrev, oppName, goalieName string
	if box.AwayTeam.Abbrev == CapitalsAbbrev {
		oppAbbrev = box.HomeTeam.Abbrev
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
		oppAbbrev = box.AwayTeam.Abbrev
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

// GoalieForGoal fetches play-by-play for the game and returns the display name of the goalie
// who was in net for the specific goal (scoringPlayerID + goalsToDate). Uses "goalieInNetId"
// from the goal event so we get the actual goalie on the ice, not the boxscore starter.
// Returns empty string if not found or on error.
func (c *Client) GoalieForGoal(ctx context.Context, gameID, scoringPlayerID, goalsToDate int) string {
	url := fmt.Sprintf(PlayByPlayURLFmt, gameID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "OvechBot/1.0")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	var pbp struct {
		Plays []struct {
			TypeCode int `json:"typeCode"`
			Details  *struct {
				ScoringPlayerID    int `json:"scoringPlayerId"`
				ScoringPlayerTotal int `json:"scoringPlayerTotal"`
				GoalieInNetID      int `json:"goalieInNetId"`
			} `json:"details"`
		} `json:"plays"`
		RosterSpots []struct {
			PlayerID     int    `json:"playerId"`
			PositionCode string `json:"positionCode"`
			FirstName    struct { Default string `json:"default"` } `json:"firstName"`
			LastName     struct { Default string `json:"default"` } `json:"lastName"`
		} `json:"rosterSpots"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&pbp); err != nil {
		return ""
	}
	var goalieInNetID int
	for _, play := range pbp.Plays {
		if play.TypeCode != 505 {
			continue
		}
		if play.Details == nil {
			continue
		}
		if play.Details.ScoringPlayerID == scoringPlayerID && play.Details.ScoringPlayerTotal == goalsToDate {
			goalieInNetID = play.Details.GoalieInNetID
			break
		}
	}
	if goalieInNetID == 0 {
		return ""
	}
	for _, r := range pbp.RosterSpots {
		if r.PlayerID != goalieInNetID {
			continue
		}
		first := r.FirstName.Default
		if len(first) > 0 {
			first = first[:1] + "."
		}
		return first + " " + r.LastName.Default
	}
	return ""
}
