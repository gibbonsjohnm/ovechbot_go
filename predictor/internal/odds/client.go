package odds

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"ovechbot_go/predictor/internal/schedule"
)

const (
	baseURL        = "https://api.the-odds-api.com/v4"
	sportKey       = "icehockey_nhl"
	anytimeMarket  = "player_goal_scorer_anytime"
	ovechkinSearch = "Ovechkin" // match "Alex Ovechkin" in description
)

// Client calls The Odds API for NHL anytime goal scorer odds.
type Client struct {
	apiKey string
	http   *http.Client
}

// NewClient returns a client. If apiKey is empty, all fetches will be skipped (no-op).
func NewClient(apiKey string) *Client {
	return &Client{
		apiKey: apiKey,
		http:   &http.Client{Timeout: 15 * time.Second},
	}
}

// Event from The Odds API.
type event struct {
	ID           string `json:"id"`
	CommenceTime string `json:"commence_time"`
	HomeTeam     string `json:"home_team"`
	AwayTeam     string `json:"away_team"`
}

// EventOdds response for one event.
type eventOdds struct {
	ID           string `json:"id"`
	CommenceTime string `json:"commence_time"`
	Bookmakers   []struct {
		Key     string `json:"key"`
		Markets []struct {
			Key      string `json:"key"`
			Outcomes []struct {
				Name        string  `json:"name"`
				Description string  `json:"description"`
				Price       int     `json:"price"`
				Point       float64 `json:"point,omitempty"`
			} `json:"outcomes"`
		} `json:"markets"`
	} `json:"bookmakers"`
}

// AnytimeOdds holds Ovechkin's anytime goal scorer line (American odds).
type AnytimeOdds struct {
	American string // e.g. "+140" or "-150"
	Price    int    // raw American price for implied prob
}

// ImpliedPct returns implied probability from American odds (0–100).
func ImpliedPct(american int) int {
	if american >= 0 {
		return 100 * 100 / (100 + american)
	}
	return 100 * (-american) / (100 + (-american))
}

// OvechkinAnytimeGoal fetches odds for the given game. Returns nil if API key is empty, game has no matching event, or Ovechkin line not found.
func (c *Client) OvechkinAnytimeGoal(ctx context.Context, g *schedule.Game) (*AnytimeOdds, error) {
	if c.apiKey == "" {
		return nil, nil
	}
	eventID, err := c.findEventID(ctx, g)
	if err != nil || eventID == "" {
		return nil, err
	}
	return c.fetchAnytimeOdds(ctx, eventID)
}

func (c *Client) findEventID(ctx context.Context, g *schedule.Game) (string, error) {
	u := baseURL + "/sports/" + sportKey + "/events?apiKey=" + url.QueryEscape(c.apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("odds events status %d", resp.StatusCode)
	}
	var events []event
	if err := json.NewDecoder(resp.Body).Decode(&events); err != nil {
		return "", err
	}
	gameStart := g.StartTimeUTC.UTC()
	for i := range events {
		e := &events[i]
		t, err := time.Parse(time.RFC3339, e.CommenceTime)
		if err != nil {
			continue
		}
		diff := gameStart.Sub(t.UTC())
		if diff < 0 {
			diff = -diff
		}
		if diff > 90*time.Minute {
			continue
		}
		home, away := strings.ToLower(e.HomeTeam), strings.ToLower(e.AwayTeam)
		if strings.Contains(home, "washington") || strings.Contains(away, "washington") ||
			strings.Contains(home, "capitals") || strings.Contains(away, "capitals") {
			return e.ID, nil
		}
	}
	return "", nil
}

func (c *Client) fetchAnytimeOdds(ctx context.Context, eventID string) (*AnytimeOdds, error) {
	u := baseURL + "/sports/" + sportKey + "/events/" + url.PathEscape(eventID) + "/odds?apiKey=" + url.QueryEscape(c.apiKey) +
		"&regions=us&markets=" + url.QueryEscape(anytimeMarket) + "&oddsFormat=american"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("event odds status %d", resp.StatusCode)
	}
	var data eventOdds
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	for _, b := range data.Bookmakers {
		for _, m := range b.Markets {
			if m.Key != anytimeMarket {
				continue
			}
			for _, o := range m.Outcomes {
				if strings.Contains(o.Description, ovechkinSearch) && (o.Name == "Yes" || o.Name == "Alex Ovechkin") {
					american := formatAmerican(o.Price)
					return &AnytimeOdds{American: american, Price: o.Price}, nil
				}
			}
		}
	}
	return nil, nil
}

func formatAmerican(price int) string {
	if price > 0 {
		return fmt.Sprintf("+%d", price)
	}
	return fmt.Sprintf("%d", price)
}
