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
	// OvechkinPlayerID is Alex Ovechkin's NHL API player ID.
	OvechkinPlayerID = 8471214
	// LandingURLFmt is the NHL API landing endpoint for a player.
	LandingURLFmt = "https://api-web.nhle.com/v1/player/%d/landing"
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
