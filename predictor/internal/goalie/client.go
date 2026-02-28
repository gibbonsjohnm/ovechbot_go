package goalie

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"ovechbot_go/predictor/internal/schedule"
)

const (
	boxscoreURLFmt   = "https://api-web.nhle.com/v1/gamecenter/%d/boxscore"
	playerLandingFmt = "https://api-web.nhle.com/v1/player/%d/landing"
	rosterURLFmt     = "https://api-web.nhle.com/v1/roster/%s/current"
)

// Info is the opposing starter's name and season save percentage (0–1). When SavePct is 0, factor should be 1.0.
type Info struct {
	Name    string  // e.g. "S. Ersson"
	SavePct float64 // season save percentage, e.g. 0.905
}

// Client fetches opposing starting goalie and season SV% from the NHL API.
type Client struct {
	http *http.Client
}

// NewClient returns a client with default timeout.
func NewClient() *Client {
	return &Client{http: &http.Client{Timeout: 12 * time.Second}}
}

// OpposingStarter returns the opposing team's starting goalie (name + season SV%) for the given game.
// It tries PuckPedia first (no NHL game ID needed; uses opponent + home/away only). If that returns
// nothing, it falls back to the NHL boxscore (authoritative but often not available until near puck drop).
func (c *Client) OpposingStarter(ctx context.Context, g *schedule.Game) (*Info, error) {
	// Try PuckPedia first — does not use NHL game ID, only opponent and home/away from schedule.
	slog.Info("goalie: fetching from PuckPedia", "opponent", g.Opponent(), "caps_home", g.IsHome())
	name := c.OpposingStarterFromPuckPedia(ctx, g)
	if name != "" {
		playerID, displayName := c.resolveGoalieByName(ctx, g.Opponent(), name)
		if playerID != 0 {
			savePct, _ := c.playerSavePct(ctx, playerID)
			if displayName == "" {
				displayName = name
			}
			return &Info{Name: displayName, SavePct: savePct}, nil
		}
		slog.Warn("goalie: PuckPedia name not on opponent roster, discarding", "name", name, "opponent", g.Opponent())
	}
	// Fallback: NHL boxscore (uses game ID; often empty until near/after puck drop).
	info, err := c.opposingStarterFromBoxscore(ctx, g)
	if err != nil {
		return nil, err
	}
	if info != nil {
		return info, nil
	}
	slog.Info("goalie: none found", "opponent", g.Opponent(), "hint", "PuckPedia had no name and boxscore not yet published")
	return nil, nil
}

// opposingStarterFromBoxscore returns the opponent's starter from the NHL game boxscore, or nil if not yet published.
func (c *Client) opposingStarterFromBoxscore(ctx context.Context, g *schedule.Game) (*Info, error) {
	url := fmt.Sprintf(boxscoreURLFmt, g.GameID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil // lineup not yet published for this game
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("boxscore status %d", resp.StatusCode)
	}
	var box struct {
		AwayTeam struct {
			Abbrev string `json:"abbrev"`
		} `json:"awayTeam"`
		HomeTeam struct {
			Abbrev string `json:"abbrev"`
		} `json:"homeTeam"`
		PlayerByGameStats struct {
			AwayTeam struct {
				Goalies []struct {
					PlayerID int    `json:"playerId"`
					Name     struct { Default string `json:"default"` } `json:"name"`
					Starter  bool   `json:"starter"`
				} `json:"goalies"`
			} `json:"awayTeam"`
			HomeTeam struct {
				Goalies []struct {
					PlayerID int    `json:"playerId"`
					Name     struct { Default string `json:"default"` } `json:"name"`
					Starter  bool   `json:"starter"`
				} `json:"goalies"`
			} `json:"homeTeam"`
		} `json:"playerByGameStats"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&box); err != nil {
		return nil, err
	}
	// Caps are WSH; opponent is the other team. We want the opponent's starter.
	var goaliePlayerID int
	var goalieName string
	if box.AwayTeam.Abbrev == "WSH" {
		for _, gk := range box.PlayerByGameStats.HomeTeam.Goalies {
			if gk.Starter {
				goaliePlayerID = gk.PlayerID
				goalieName = gk.Name.Default
				break
			}
		}
		if goaliePlayerID == 0 && len(box.PlayerByGameStats.HomeTeam.Goalies) > 0 {
			gk := box.PlayerByGameStats.HomeTeam.Goalies[0]
			goaliePlayerID = gk.PlayerID
			goalieName = gk.Name.Default
		}
	} else {
		for _, gk := range box.PlayerByGameStats.AwayTeam.Goalies {
			if gk.Starter {
				goaliePlayerID = gk.PlayerID
				goalieName = gk.Name.Default
				break
			}
		}
		if goaliePlayerID == 0 && len(box.PlayerByGameStats.AwayTeam.Goalies) > 0 {
			gk := box.PlayerByGameStats.AwayTeam.Goalies[0]
			goaliePlayerID = gk.PlayerID
			goalieName = gk.Name.Default
		}
	}
	if goaliePlayerID == 0 {
		return nil, nil
	}
	savePct, err := c.playerSavePct(ctx, goaliePlayerID)
	if err != nil || savePct <= 0 {
		return &Info{Name: goalieName, SavePct: 0}, nil
	}
	return &Info{Name: goalieName, SavePct: savePct}, nil
}

// resolveGoalieByName fetches the opponent's roster from the NHL API and returns the goalie's player ID and display name (e.g. "D. Vladar") that matches the given full name (e.g. "Dan Vladar").
func (c *Client) resolveGoalieByName(ctx context.Context, teamAbbrev, fullName string) (playerID int, displayName string) {
	url := fmt.Sprintf(rosterURLFmt, teamAbbrev)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, ""
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, ""
	}
	var roster struct {
		Goalies []struct {
			ID        int `json:"id"`
			FirstName struct {
				Default string `json:"default"`
			} `json:"firstName"`
			LastName struct {
				Default string `json:"default"`
			} `json:"lastName"`
		} `json:"goalies"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&roster); err != nil {
		return 0, ""
	}
	fullName = strings.TrimSpace(fullName)
	parts := strings.SplitN(fullName, " ", 2)
	var first, last string
	if len(parts) == 2 {
		first, last = parts[0], parts[1]
	} else {
		last = fullName
	}
	for _, g := range roster.Goalies {
		rosterLast := g.LastName.Default
		rosterFirst := g.FirstName.Default
		if strings.EqualFold(rosterLast, last) && (first == "" || strings.EqualFold(rosterFirst, first) || (len(rosterFirst) > 0 && len(first) > 0 && rosterFirst[0] == first[0])) {
			if len(rosterFirst) > 0 {
				displayName = rosterFirst[:1] + ". " + rosterLast
			} else {
				displayName = rosterLast
			}
			return g.ID, displayName
		}
	}
	return 0, ""
}

func (c *Client) playerSavePct(ctx context.Context, playerID int) (float64, error) {
	url := fmt.Sprintf(playerLandingFmt, playerID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("player landing status %d", resp.StatusCode)
	}
	var landing struct {
		FeaturedStats *struct {
			RegularSeason *struct {
				SubSeason *struct {
					SavePctg float64 `json:"savePctg"`
				} `json:"subSeason"`
			} `json:"regularSeason"`
		} `json:"featuredStats"`
		SeasonTotals []struct {
			Season     int     `json:"season"`
			GameTypeID int     `json:"gameTypeId"`
			SavePctg   float64 `json:"savePctg"`
		} `json:"seasonTotals"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&landing); err != nil {
		return 0, err
	}
	if landing.FeaturedStats != nil && landing.FeaturedStats.RegularSeason != nil && landing.FeaturedStats.RegularSeason.SubSeason != nil {
		if pct := landing.FeaturedStats.RegularSeason.SubSeason.SavePctg; pct > 0 {
			return pct, nil
		}
	}
	// featuredStats is absent for backup/inactive goalies; fall back to the most recent regular-season entry.
	var bestSeason int
	var bestPct float64
	for _, s := range landing.SeasonTotals {
		if s.GameTypeID != 2 { // 2 = regular season
			continue
		}
		if s.Season > bestSeason && s.SavePctg > 0 {
			bestSeason = s.Season
			bestPct = s.SavePctg
		}
	}
	return bestPct, nil
}
