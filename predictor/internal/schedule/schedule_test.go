package schedule

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// testRoundTripper redirects all HTTP calls to a local test server.
type testRoundTripper struct {
	baseURL string
}

func (t *testRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	newURL := t.baseURL + req.URL.RequestURI()
	newReq, err := http.NewRequest(req.Method, newURL, req.Body)
	if err != nil {
		return nil, err
	}
	newReq.Header = req.Header
	return http.DefaultTransport.RoundTrip(newReq)
}

// serveSchedule swaps the package-level httpClient for one that serves schedJSON.
func serveSchedule(t *testing.T, schedJSON string) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(schedJSON))
	}))
	t.Cleanup(server.Close)
	orig := httpClient
	httpClient = &http.Client{Transport: &testRoundTripper{baseURL: server.URL}}
	t.Cleanup(func() { httpClient = orig })
}

func TestNextGame_ReturnsFirstFutureRegularSeasonGame(t *testing.T) {
	future := time.Now().UTC().Add(48 * time.Hour).Format(time.RFC3339)
	serveSchedule(t, fmt.Sprintf(`{"games":[
		{"id":2026020018,"gameType":2,"gameDate":"2026-10-02","startTimeUTC":%q,"gameState":"FUT","homeTeam":{"abbrev":"CAR"},"awayTeam":{"abbrev":"WSH"}}
	]}`, future))

	g, err := NextGame(context.Background())
	if err != nil {
		t.Fatalf("NextGame: %v", err)
	}
	if g == nil || g.GameID != 2026020018 {
		t.Fatalf("got %+v; want game 2026020018", g)
	}
}

func TestNextGame_SkipsPreseasonGame(t *testing.T) {
	// Regression: on 2026-09-21 the bot posted a reminder for preseason PHI @ WSH (gameType 1).
	// Preseason games precede the regular season in club-schedule-season and must be ignored.
	now := time.Now().UTC()
	preseason := now.Add(1 * time.Hour).Format(time.RFC3339)
	regular := now.Add(10 * 24 * time.Hour).Format(time.RFC3339)
	serveSchedule(t, fmt.Sprintf(`{"games":[
		{"id":2026010020,"gameType":1,"gameDate":"2026-09-21","startTimeUTC":%q,"gameState":"FUT","homeTeam":{"abbrev":"WSH"},"awayTeam":{"abbrev":"PHI"}},
		{"id":2026020018,"gameType":2,"gameDate":"2026-10-02","startTimeUTC":%q,"gameState":"FUT","homeTeam":{"abbrev":"CAR"},"awayTeam":{"abbrev":"WSH"}}
	]}`, preseason, regular))

	g, err := NextGame(context.Background())
	if err != nil {
		t.Fatalf("NextGame: %v", err)
	}
	if g == nil || g.GameID != 2026020018 {
		t.Fatalf("got %+v; want regular-season game 2026020018", g)
	}
}

func TestNextGame_SkipsLivePreseasonGame(t *testing.T) {
	// A preseason game that is in progress must not be preferred over the next regular-season game.
	regular := time.Now().UTC().Add(10 * 24 * time.Hour).Format(time.RFC3339)
	serveSchedule(t, fmt.Sprintf(`{"games":[
		{"id":2026010020,"gameType":1,"gameDate":"2026-09-21","startTimeUTC":"2026-09-21T23:00:00Z","gameState":"LIVE","homeTeam":{"abbrev":"WSH"},"awayTeam":{"abbrev":"PHI"}},
		{"id":2026020018,"gameType":2,"gameDate":"2026-10-02","startTimeUTC":%q,"gameState":"FUT","homeTeam":{"abbrev":"CAR"},"awayTeam":{"abbrev":"WSH"}}
	]}`, regular))

	g, err := NextGame(context.Background())
	if err != nil {
		t.Fatalf("NextGame: %v", err)
	}
	if g == nil || g.GameID != 2026020018 {
		t.Fatalf("got %+v; want regular-season game 2026020018", g)
	}
}

func TestNextGame_SkipsPlayoffGame(t *testing.T) {
	future := time.Now().UTC().Add(48 * time.Hour).Format(time.RFC3339)
	serveSchedule(t, fmt.Sprintf(`{"games":[
		{"id":2026030111,"gameType":3,"gameDate":"2027-04-20","startTimeUTC":%q,"gameState":"FUT","homeTeam":{"abbrev":"WSH"},"awayTeam":{"abbrev":"PIT"}}
	]}`, future))

	g, err := NextGame(context.Background())
	if err != nil {
		t.Fatalf("NextGame: %v", err)
	}
	if g != nil {
		t.Fatalf("got %+v; want nil (playoff games are ignored)", g)
	}
}
