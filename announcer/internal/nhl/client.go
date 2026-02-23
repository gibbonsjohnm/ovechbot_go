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
	OvechkinPlayerID    = 8471214
	CapitalsAbbrev      = "WSH"
	LandingURLFmt       = "https://api-web.nhle.com/v1/player/%d/landing"
	BoxscoreURLFmt      = "https://api-web.nhle.com/v1/gamecenter/%d/boxscore"
	ScheduleNowURL      = "https://api-web.nhle.com/v1/schedule/now"
	ClubScheduleSeason  = "https://api-web.nhle.com/v1/club-schedule-season/" + CapitalsAbbrev + "/now"
)

// venueJSON unmarshals venue from either a string or an object {"default": "Venue Name"}.
type venueJSON string

func (v *venueJSON) UnmarshalJSON(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	if data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*v = venueJSON(s)
		return nil
	}
	var o struct {
		Default string `json:"default"`
	}
	if err := json.Unmarshal(data, &o); err != nil {
		return err
	}
	*v = venueJSON(o.Default)
	return nil
}

// Client fetches NHL API data for Ovechkin (goals, last goal game).
type Client struct {
	httpClient *http.Client
}

// NewClient returns an NHL API client.
func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

// CareerGoals returns Ovechkin's career regular-season goal count.
func (c *Client) CareerGoals(ctx context.Context) (int, error) {
	url := fmt.Sprintf(LandingURLFmt, OvechkinPlayerID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("nhl api status %d: %s", resp.StatusCode, string(body))
	}
	var landing struct {
		CareerTotals struct {
			RegularSeason struct {
				Goals int `json:"goals"`
			} `json:"regularSeason"`
		} `json:"careerTotals"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&landing); err != nil {
		return 0, err
	}
	return landing.CareerTotals.RegularSeason.Goals, nil
}

// CurrentCapitalsGame holds the current or next Capitals game for bot status (HOME vs AWAY).
type CurrentCapitalsGame struct {
	HomeAbbrev string // e.g. "WSH"
	AwayAbbrev string // e.g. "PHI"
}

// InProgressGameStates are schedule gameState values meaning the game is on now (or pre-game).
var InProgressGameStates = map[string]bool{
	"LIVE": true, "PRE": true, "CRIT": true,
}

// CurrentCapitalsGame fetches the schedule and returns a Capitals game only when it is in progress (LIVE/PRE/CRIT).
// Returns nil when the Capitals are not playing right now (no WSH game in that state in the schedule window).
func (c *Client) CurrentCapitalsGame(ctx context.Context) (*CurrentCapitalsGame, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ScheduleNowURL, nil)
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
		return nil, fmt.Errorf("schedule api status %d", resp.StatusCode)
	}
	var sched struct {
		GameWeek []struct {
			Games []struct {
				GameState string `json:"gameState"`
				HomeTeam  struct {
					Abbrev string `json:"abbrev"`
				} `json:"homeTeam"`
				AwayTeam struct {
					Abbrev string `json:"abbrev"`
				} `json:"awayTeam"`
			} `json:"games"`
		} `json:"gameWeek"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sched); err != nil {
		return nil, err
	}
	for _, week := range sched.GameWeek {
		for _, g := range week.Games {
			if !InProgressGameStates[g.GameState] {
				continue
			}
			if g.HomeTeam.Abbrev == CapitalsAbbrev || g.AwayTeam.Abbrev == CapitalsAbbrev {
				return &CurrentCapitalsGame{
					HomeAbbrev: g.HomeTeam.Abbrev,
					AwayAbbrev: g.AwayTeam.Abbrev,
				}, nil
			}
		}
	}
	return nil, nil
}

// NextCapitalsGame holds the next (or current) Capitals game from the season schedule.
type NextCapitalsGame struct {
	GameID       int64     // for matching predictor's next_prediction
	HomeAbbrev   string    // e.g. "WSH"
	AwayAbbrev   string    // e.g. "PHI"
	StartTimeUTC time.Time // when the game starts or started
	GameState    string    // e.g. "FUT", "LIVE", "PRE", "CRIT", "FINAL"
	GameDate     string    // e.g. "2026-02-23"
	Venue        string    // e.g. "Capital One Arena"
}

// NextCapitalsGame fetches the Capitals season schedule and returns the next game (or the one on now).
// Returns nil if no upcoming/in-progress game is found (e.g. season over or schedule empty).
func (c *Client) NextCapitalsGame(ctx context.Context) (*NextCapitalsGame, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ClubScheduleSeason, nil)
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
		return nil, fmt.Errorf("club schedule api status %d", resp.StatusCode)
	}
	var sched struct {
		Games []struct {
			ID           int64     `json:"id"`
			GameDate     string    `json:"gameDate"`
			StartTimeUTC string    `json:"startTimeUTC"`
			GameState    string    `json:"gameState"`
			Venue        venueJSON `json:"venue"`
			HomeTeam     struct{ Abbrev string `json:"abbrev"` } `json:"homeTeam"`
			AwayTeam     struct{ Abbrev string `json:"abbrev"` } `json:"awayTeam"`
		} `json:"games"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sched); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	var inProgress, firstFuture *NextCapitalsGame
	for _, g := range sched.Games {
		start, _ := time.Parse(time.RFC3339, g.StartTimeUTC)
		n := &NextCapitalsGame{
			GameID:       g.ID,
			HomeAbbrev:   g.HomeTeam.Abbrev,
			AwayAbbrev:   g.AwayTeam.Abbrev,
			StartTimeUTC: start,
			GameState:    g.GameState,
			GameDate:     g.GameDate,
			Venue:        string(g.Venue),
		}
		if InProgressGameStates[g.GameState] {
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

// LastGoalGame holds info about the most recent game in which Ovechkin scored.
type LastGoalGame struct {
	GameDate   string // e.g. "2026-02-05"
	Opponent   string // e.g. "NSH"
	OpponentName string // e.g. "Predators"
	GoalieName string // opposing starter, e.g. "J. Annunen"
}

// LastGoalGame fetches the most recent game (from last 5) where Ovechkin scored, plus opponent and goalie from boxscore.
func (c *Client) LastGoalGame(ctx context.Context) (*LastGoalGame, error) {
	url := fmt.Sprintf(LandingURLFmt, OvechkinPlayerID)
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
		return nil, fmt.Errorf("nhl api status %d", resp.StatusCode)
	}
	var landing struct {
		Last5Games []struct {
			GameDate        string `json:"gameDate"`
			GameID          int    `json:"gameId"`
			OpponentAbbrev  string `json:"opponentAbbrev"`
			Goals           int    `json:"goals"`
		} `json:"last5Games"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&landing); err != nil {
		return nil, err
	}
	var gameID int
	var gameDate, oppAbbrev string
	for _, g := range landing.Last5Games {
		if g.Goals > 0 {
			gameID = g.GameID
			gameDate = g.GameDate
			oppAbbrev = g.OpponentAbbrev
			break
		}
	}
	if gameID == 0 {
		return nil, fmt.Errorf("no recent game with a goal in last 5 games")
	}

	// Fetch boxscore for opponent name and goalie
	boxURL := fmt.Sprintf(BoxscoreURLFmt, gameID)
	req2, _ := http.NewRequestWithContext(ctx, http.MethodGet, boxURL, nil)
	req2.Header.Set("Accept", "application/json")
	resp2, err := c.httpClient.Do(req2)
	if err != nil {
		return &LastGoalGame{GameDate: gameDate, Opponent: oppAbbrev}, nil // partial
	}
	defer resp2.Body.Close()
	var box struct {
		AwayTeam struct {
			Abbrev     string `json:"abbrev"`
			CommonName struct {
				Default string `json:"default"`
			} `json:"commonName"`
		} `json:"awayTeam"`
		HomeTeam struct {
			Abbrev     string `json:"abbrev"`
			CommonName struct {
				Default string `json:"default"`
			} `json:"commonName"`
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
		return &LastGoalGame{GameDate: gameDate, Opponent: oppAbbrev}, nil
	}
	// WSH is Capitals; opponent is the other team
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
	return &LastGoalGame{
		GameDate:     gameDate,
		Opponent:     oppAbbrev,
		OpponentName: oppName,
		GoalieName:   goalieName,
	}, nil
}
