package odds

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
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
	American   string `json:"american"`              // Yes price, e.g. "+140" or "-150"
	NoAmerican string `json:"no_american,omitempty"` // No price from the same book, when quoted
}

// MarketPct returns the market's probability (0–100) that he scores. With both sides quoted it removes the book's
// margin (Yes implied / (Yes implied + No implied)); with only Yes it is the raw implied probability, which runs a
// few points high.
func (o AnytimeOdds) MarketPct() (int, bool) {
	yes, ok := impliedProb(o.American)
	if !ok {
		return 0, false
	}
	if no, ok := impliedProb(o.NoAmerican); ok && yes+no > 0 {
		return int(math.Round(100 * yes / (yes + no))), true
	}
	return int(math.Round(100 * yes)), true
}

// EncodeCache serializes the odds for the per-game Redis cache.
func (o AnytimeOdds) EncodeCache() string {
	b, _ := json.Marshal(o)
	return string(b)
}

// DecodeCache reads a cached value; values cached before EncodeCache existed are the bare Yes price.
func DecodeCache(s string) AnytimeOdds {
	var o AnytimeOdds
	if strings.HasPrefix(s, "{") && json.Unmarshal([]byte(s), &o) == nil {
		return o
	}
	return AnytimeOdds{American: s}
}

// impliedProb parses American odds and returns the implied probability (0–1).
func impliedProb(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	price, err := strconv.Atoi(strings.TrimPrefix(s, "+"))
	if err != nil || price == 0 {
		return 0, false
	}
	if price > 0 {
		return 100 / (100 + float64(price)), true
	}
	return float64(-price) / (100 + float64(-price)), true
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
	// Prefer a book quoting both Yes and No (so MarketPct can remove the margin); otherwise the first Yes line.
	var yesOnly *AnytimeOdds
	for _, b := range data.Bookmakers {
		for _, m := range b.Markets {
			if m.Key != anytimeMarket {
				continue
			}
			var line AnytimeOdds
			for _, o := range m.Outcomes {
				if !strings.Contains(o.Description, ovechkinSearch) {
					continue
				}
				switch o.Name {
				case "Yes", "Alex Ovechkin":
					line.American = formatAmerican(o.Price)
				case "No":
					line.NoAmerican = formatAmerican(o.Price)
				}
			}
			if line.American == "" {
				continue
			}
			if line.NoAmerican != "" {
				return &line, nil
			}
			if yesOnly == nil {
				yesOnly = &line
			}
		}
	}
	return yesOnly, nil
}

func formatAmerican(price int) string {
	if price > 0 {
		return fmt.Sprintf("+%d", price)
	}
	return fmt.Sprintf("%d", price)
}
