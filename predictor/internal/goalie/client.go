package goalie

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"ovechbot_go/predictor/internal/schedule"
)

const (
	boxscoreURLFmt  = "https://api-web.nhle.com/v1/gamecenter/%d/boxscore"
	playerLandingFmt = "https://api-web.nhle.com/v1/player/%d/landing"
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

// OpposingStarter returns the opposing team's starting goalie (name + season SV%) for the given game. Returns nil if boxscore has no goalies (e.g. game not yet opened) or fetch fails.
func (c *Client) OpposingStarter(ctx context.Context, g *schedule.Game) (*Info, error) {
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
	}
	if err := json.NewDecoder(resp.Body).Decode(&landing); err != nil {
		return 0, err
	}
	if landing.FeaturedStats != nil && landing.FeaturedStats.RegularSeason != nil && landing.FeaturedStats.RegularSeason.SubSeason != nil {
		return landing.FeaturedStats.RegularSeason.SubSeason.SavePctg, nil
	}
	return 0, nil
}
