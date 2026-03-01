package nhl

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestCareerGoals_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/player/8471214/landing" && r.URL.Path != "/landing" {
			t.Logf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"careerTotals":{"regularSeason":{"goals":919}}}`))
	}))
	defer server.Close()

	c := &Client{
		httpClient: server.Client(),
		baseURL:    server.URL + "/v1/player/8471214/landing",
	}
	ctx := context.Background()

	goals, err := c.CareerGoals(ctx)
	if err != nil {
		t.Fatalf("CareerGoals: %v", err)
	}
	if goals != 919 {
		t.Errorf("goals = %d; want 919", goals)
	}
}

func TestCareerGoals_Non200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("server error"))
	}))
	defer server.Close()

	c := &Client{
		httpClient: server.Client(),
		baseURL:    server.URL,
	}
	ctx := context.Background()

	goals, err := c.CareerGoals(ctx)
	if err == nil {
		t.Fatal("expected error")
	}
	if goals != 0 {
		t.Errorf("goals = %d; want 0 on error", goals)
	}
	if err.Error() != "nhl api status 500: server error" {
		t.Errorf("err = %v", err)
	}
}

func TestCareerGoals_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{invalid`))
	}))
	defer server.Close()

	c := &Client{
		httpClient: server.Client(),
		baseURL:    server.URL,
	}
	ctx := context.Background()

	_, err := c.CareerGoals(ctx)
	if err == nil {
		t.Fatal("expected decode error")
	}
}

func TestNewClient_BaseURL(t *testing.T) {
	c := NewClient()
	if c.baseURL != "https://api-web.nhle.com/v1/player/8471214/landing" {
		t.Errorf("baseURL = %s", c.baseURL)
	}
	if c.httpClient == nil {
		t.Error("httpClient is nil")
	}
}

func TestCapsGameFromScoreNow_Found(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/score/now" {
			t.Errorf("path = %s; want /v1/score/now", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"games":[{"id":2025020940,"gameState":"LIVE","awayTeam":{"abbrev":"WSH"},"homeTeam":{"abbrev":"MTL"},"goals":[{"playerId":8471214,"goalsToDate":23}]}]}`))
	}))
	defer server.Close()

	// Client uses ScoreNowURL (api-web.nhle.com); redirect that host to test server
	transport := &redirectHostRoundTripper{redirectBase: server.URL}
	c := &Client{httpClient: &http.Client{Transport: transport, Timeout: server.Client().Timeout}, baseURL: "https://api-web.nhle.com/v1/player/8471214/landing"}

	ctx := context.Background()
	caps, err := c.CapsGameFromScoreNow(ctx)
	if err != nil {
		t.Fatalf("CapsGameFromScoreNow: %v", err)
	}
	if caps == nil {
		t.Fatal("caps is nil; want WSH game")
	}
	if caps.GameID != 2025020940 || caps.GameState != "LIVE" || caps.AwayAbbrev != "WSH" || caps.HomeAbbrev != "MTL" {
		t.Errorf("caps = %+v", caps)
	}
	if len(caps.Goals) != 1 || caps.Goals[0].PlayerID != OvechkinPlayerID || caps.Goals[0].GoalsToDate != 23 {
		t.Errorf("caps.Goals = %+v", caps.Goals)
	}
}

// redirectHostRoundTripper sends requests to redirectBase (e.g. httptest.Server.URL) for testing.
type redirectHostRoundTripper struct {
	redirectBase string
}

func (r *redirectHostRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req2 := req.Clone(req.Context())
	u, _ := url.Parse(r.redirectBase)
	u.Path = req.URL.Path
	u.RawQuery = req.URL.RawQuery
	req2.URL = u
	return http.DefaultTransport.RoundTrip(req2)
}
