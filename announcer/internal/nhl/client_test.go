package nhl

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCareerGoals_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"careerTotals":{"regularSeason":{"goals":919}}}`))
	}))
	defer server.Close()

	client := &Client{
		httpClient: &http.Client{
			Transport: &roundTripperFunc{fn: func(req *http.Request) (*http.Response, error) {
				req.URL.Host = server.Listener.Addr().String()
				req.URL.Scheme = "http"
				return http.DefaultTransport.RoundTrip(req)
			}},
		},
	}
	ctx := context.Background()
	goals, err := client.CareerGoals(ctx)
	if err != nil {
		t.Fatalf("CareerGoals: %v", err)
	}
	if goals != 919 {
		t.Errorf("goals = %d; want 919", goals)
	}
}

func TestCareerGoals_WithBaseURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"careerTotals":{"regularSeason":{"goals":900}}}`))
	}))
	defer server.Close()

	// Use custom transport to hit test server; Client has no baseURL, so we need to inject.
	// CareerGoals uses LandingURLFmt with OvechkinPlayerID - we can't change that without refactor.
	// So use a wrapper: create a Client that uses DefaultClient but we need to override the URL.
	// Easiest: create Client with custom RoundTripper that redirects to our server.
	client := &Client{
		httpClient: &http.Client{
			Transport: &roundTripperFunc{fn: func(req *http.Request) (*http.Response, error) {
				req.URL.Host = server.Listener.Addr().String()
				req.URL.Scheme = "http"
				return http.DefaultTransport.RoundTrip(req)
			}},
		},
	}
	ctx := context.Background()
	goals, err := client.CareerGoals(ctx)
	if err != nil {
		t.Fatalf("CareerGoals: %v", err)
	}
	if goals != 900 {
		t.Errorf("goals = %d; want 900", goals)
	}
}

type roundTripperFunc struct {
	fn func(*http.Request) (*http.Response, error)
}

func (r *roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return r.fn(req)
}

func TestCareerGoals_Non200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := &Client{
		httpClient: &http.Client{
			Transport: &roundTripperFunc{fn: func(req *http.Request) (*http.Response, error) {
				req.URL.Host = server.Listener.Addr().String()
				req.URL.Scheme = "http"
				return http.DefaultTransport.RoundTrip(req)
			}},
		},
	}
	ctx := context.Background()
	_, err := client.CareerGoals(ctx)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCurrentCapitalsGame_Found(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"gameWeek":[{"games":[{"gameState":"LIVE","homeTeam":{"abbrev":"WSH"},"awayTeam":{"abbrev":"PHI"}}]}]}`))
	}))
	defer server.Close()

	client := &Client{
		httpClient: &http.Client{
			Transport: &roundTripperFunc{fn: func(req *http.Request) (*http.Response, error) {
				req.URL.Host = server.Listener.Addr().String()
				req.URL.Scheme = "http"
				return http.DefaultTransport.RoundTrip(req)
			}},
		},
	}
	ctx := context.Background()
	game, err := client.CurrentCapitalsGame(ctx)
	if err != nil {
		t.Fatalf("CurrentCapitalsGame: %v", err)
	}
	if game == nil {
		t.Fatal("expected game")
	}
	if game.HomeAbbrev != "WSH" || game.AwayAbbrev != "PHI" {
		t.Errorf("game = %+v", game)
	}
}

func TestCurrentCapitalsGame_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"gameWeek":[{"games":[{"gameState":"LIVE","homeTeam":{"abbrev":"BOS"},"awayTeam":{"abbrev":"MTL"}}]}]}`))
	}))
	defer server.Close()

	client := &Client{
		httpClient: &http.Client{
			Transport: &roundTripperFunc{fn: func(req *http.Request) (*http.Response, error) {
				req.URL.Host = server.Listener.Addr().String()
				req.URL.Scheme = "http"
				return http.DefaultTransport.RoundTrip(req)
			}},
		},
	}
	ctx := context.Background()
	game, err := client.CurrentCapitalsGame(ctx)
	if err != nil {
		t.Fatalf("CurrentCapitalsGame: %v", err)
	}
	if game != nil {
		t.Errorf("expected nil game, got %+v", game)
	}
}

func TestCurrentCapitalsGame_NotInProgress(t *testing.T) {
	// WSH vs PHI but gameState is FUT (future) - should not count as "watching"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"gameWeek":[{"games":[{"gameState":"FUT","homeTeam":{"abbrev":"WSH"},"awayTeam":{"abbrev":"PHI"}}]}]}`))
	}))
	defer server.Close()

	client := &Client{
		httpClient: &http.Client{
			Transport: &roundTripperFunc{fn: func(req *http.Request) (*http.Response, error) {
				req.URL.Host = server.Listener.Addr().String()
				req.URL.Scheme = "http"
				return http.DefaultTransport.RoundTrip(req)
			}},
		},
	}
	ctx := context.Background()
	game, err := client.CurrentCapitalsGame(ctx)
	if err != nil {
		t.Fatalf("CurrentCapitalsGame: %v", err)
	}
	if game != nil {
		t.Errorf("expected nil when game is FUT, got %+v", game)
	}
}

func TestLastGoalGame_FromLanding(t *testing.T) {
	landingCalled := false
	boxscoreCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if strings.Contains(r.URL.Path, "landing") {
			landingCalled = true
			_, _ = w.Write([]byte(`{"last5Games":[{"gameDate":"2026-02-05","gameId":2025020911,"opponentAbbrev":"PHI","goals":1}]}`))
			return
		}
		if strings.Contains(r.URL.Path, "boxscore") {
			boxscoreCalled = true
			_, _ = w.Write([]byte(`{"awayTeam":{"abbrev":"PHI","commonName":{"default":"Flyers"}},"homeTeam":{"abbrev":"WSH","commonName":{"default":"Capitals"}},"playerByGameStats":{"awayTeam":{"goalies":[{"name":{"default":"S. Ersson"},"starter":true}]},"homeTeam":{"goalies":[]}}}`))
			return
		}
		t.Logf("unexpected path: %s", r.URL.Path)
	}))
	defer server.Close()

	client := &Client{
		httpClient: &http.Client{
			Transport: &roundTripperFunc{fn: func(req *http.Request) (*http.Response, error) {
				req.URL.Host = server.Listener.Addr().String()
				req.URL.Scheme = "http"
				return http.DefaultTransport.RoundTrip(req)
			}},
		},
	}
	ctx := context.Background()
	info, err := client.LastGoalGame(ctx)
	if err != nil {
		t.Fatalf("LastGoalGame: %v", err)
	}
	if info == nil {
		t.Fatal("expected info")
	}
	if !landingCalled {
		t.Error("landing not called")
	}
	if !boxscoreCalled {
		t.Error("boxscore not called")
	}
	if info.GameDate != "2026-02-05" || info.Opponent != "PHI" {
		t.Errorf("info = %+v", info)
	}
}

func TestNextCapitalsGame_Future(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "club-schedule-season") {
			t.Logf("unexpected path: %s", r.URL.Path)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"games":[{"gameDate":"2026-02-25","startTimeUTC":"2026-02-25T00:30:00Z","gameState":"FUT","venue":"Capital One Arena","homeTeam":{"abbrev":"WSH"},"awayTeam":{"abbrev":"PHI"}}]}`))
	}))
	defer server.Close()

	client := &Client{
		httpClient: &http.Client{
			Transport: &roundTripperFunc{fn: func(req *http.Request) (*http.Response, error) {
				req.URL.Host = server.Listener.Addr().String()
				req.URL.Scheme = "http"
				return http.DefaultTransport.RoundTrip(req)
			}},
		},
	}
	ctx := context.Background()
	game, err := client.NextCapitalsGame(ctx)
	if err != nil {
		t.Fatalf("NextCapitalsGame: %v", err)
	}
	if game == nil {
		t.Fatal("expected game")
	}
	if game.HomeAbbrev != "WSH" || game.AwayAbbrev != "PHI" || game.GameState != "FUT" {
		t.Errorf("game = %+v", game)
	}
	if game.Venue != "Capital One Arena" || game.GameDate != "2026-02-25" {
		t.Errorf("game = %+v", game)
	}
}

func TestNextCapitalsGame_InProgressPreferred(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "club-schedule-season") {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// LIVE game first in list; FUT later. Should return LIVE.
		_, _ = w.Write([]byte(`{"games":[{"gameDate":"2026-02-22","startTimeUTC":"2026-02-22T00:00:00Z","gameState":"LIVE","venue":"Wells Fargo Center","homeTeam":{"abbrev":"PHI"},"awayTeam":{"abbrev":"WSH"}},{"gameDate":"2026-02-25","startTimeUTC":"2026-02-25T00:30:00Z","gameState":"FUT","venue":"Capital One Arena","homeTeam":{"abbrev":"WSH"},"awayTeam":{"abbrev":"PHI"}}]}`))
	}))
	defer server.Close()

	client := &Client{
		httpClient: &http.Client{
			Transport: &roundTripperFunc{fn: func(req *http.Request) (*http.Response, error) {
				req.URL.Host = server.Listener.Addr().String()
				req.URL.Scheme = "http"
				return http.DefaultTransport.RoundTrip(req)
			}},
		},
	}
	ctx := context.Background()
	game, err := client.NextCapitalsGame(ctx)
	if err != nil {
		t.Fatalf("NextCapitalsGame: %v", err)
	}
	if game == nil {
		t.Fatal("expected game")
	}
	if game.GameState != "LIVE" || game.AwayAbbrev != "WSH" || game.HomeAbbrev != "PHI" {
		t.Errorf("expected LIVE WSH @ PHI, got %+v", game)
	}
}

func TestNewClient(t *testing.T) {
	c := NewClient()
	if c == nil || c.httpClient == nil {
		t.Error("NewClient failed")
	}
}
