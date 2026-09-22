package nhl

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

// replaceHTTPClient swaps the package-level httpClient for the duration of a test.
func replaceHTTPClient(t *testing.T, server *httptest.Server) {
	t.Helper()
	orig := httpClient
	httpClient = &http.Client{Transport: &testRoundTripper{baseURL: server.URL}}
	t.Cleanup(func() { httpClient = orig })
}

// ---- LastCompletedGame tests ----

func TestLastCompletedGame_FINAL(t *testing.T) {
	// One recent FINAL game — should be returned.
	recentStart := time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339)
	schedJSON := fmt.Sprintf(`{"games": [
		{
			"id": 2025020042,
			"gameDate": "2026-02-01",
			"startTimeUTC": %q,
			"gameType": 2,
			"gameState": "FINAL",
			"homeTeam": {"abbrev": "WSH"},
			"awayTeam": {"abbrev": "PHI"}
		}
	]}`, recentStart)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(schedJSON))
	}))
	defer server.Close()
	replaceHTTPClient(t, server)

	g, err := LastCompletedGame(context.Background())
	if err != nil {
		t.Fatalf("LastCompletedGame: %v", err)
	}
	if g == nil {
		t.Fatal("expected non-nil game, got nil")
	}
	if g.GameID != 2025020042 {
		t.Errorf("GameID = %d; want 2025020042", g.GameID)
	}
	if g.OpponentAbbrev != "PHI" {
		t.Errorf("OpponentAbbrev = %q; want PHI", g.OpponentAbbrev)
	}
}

func TestLastCompletedGame_OFF(t *testing.T) {
	// Accepts OFF state as completed.
	recentStart := time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339)
	schedJSON := fmt.Sprintf(`{"games": [
		{
			"id": 2025020099,
			"gameDate": "2026-02-05",
			"startTimeUTC": %q,
			"gameType": 2,
			"gameState": "OFF",
			"homeTeam": {"abbrev": "NYR"},
			"awayTeam": {"abbrev": "WSH"}
		}
	]}`, recentStart)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(schedJSON))
	}))
	defer server.Close()
	replaceHTTPClient(t, server)

	g, err := LastCompletedGame(context.Background())
	if err != nil {
		t.Fatalf("LastCompletedGame: %v", err)
	}
	if g == nil {
		t.Fatal("expected non-nil game, got nil")
	}
	// WSH is away → opponent is home (NYR)
	if g.OpponentAbbrev != "NYR" {
		t.Errorf("OpponentAbbrev = %q; want NYR", g.OpponentAbbrev)
	}
}

func TestLastCompletedGame_PicksMostRecent(t *testing.T) {
	// Two recent FINAL games — should return the one with the later start time.
	now := time.Now().UTC()
	olderStart := now.Add(-72 * time.Hour).Format(time.RFC3339)
	newerStart := now.Add(-24 * time.Hour).Format(time.RFC3339)
	schedJSON := fmt.Sprintf(`{"games": [
		{
			"id": 111,
			"gameDate": "2026-01-10",
			"startTimeUTC": %q,
			"gameType": 2,
			"gameState": "FINAL",
			"homeTeam": {"abbrev": "WSH"},
			"awayTeam": {"abbrev": "BOS"}
		},
		{
			"id": 222,
			"gameDate": "2026-02-15",
			"startTimeUTC": %q,
			"gameType": 2,
			"gameState": "FINAL",
			"homeTeam": {"abbrev": "WSH"},
			"awayTeam": {"abbrev": "PHI"}
		}
	]}`, olderStart, newerStart)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(schedJSON))
	}))
	defer server.Close()
	replaceHTTPClient(t, server)

	g, err := LastCompletedGame(context.Background())
	if err != nil {
		t.Fatalf("LastCompletedGame: %v", err)
	}
	if g == nil {
		t.Fatal("expected non-nil game, got nil")
	}
	if g.GameID != 222 {
		t.Errorf("GameID = %d; want 222 (most recent)", g.GameID)
	}
	if g.OpponentAbbrev != "PHI" {
		t.Errorf("OpponentAbbrev = %q; want PHI", g.OpponentAbbrev)
	}
}

func TestLastCompletedGame_NoCompletedGames(t *testing.T) {
	// Only future/in-progress games → returns nil.
	schedJSON := `{"games": [
		{
			"id": 999,
			"gameDate": "2030-01-01",
			"startTimeUTC": "2030-01-01T23:00:00Z",
			"gameType": 2,
			"gameState": "FUT",
			"homeTeam": {"abbrev": "WSH"},
			"awayTeam": {"abbrev": "PHI"}
		}
	]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(schedJSON))
	}))
	defer server.Close()
	replaceHTTPClient(t, server)

	g, err := LastCompletedGame(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if g != nil {
		t.Errorf("expected nil for no completed games, got: %+v", g)
	}
}

func TestLastCompletedGame_IgnoresStaleGame(t *testing.T) {
	// A FINAL game that finished long ago (e.g. last season, during the offseason)
	// must NOT be returned. Otherwise, if the evaluator's dedup key is ever lost
	// (TTL expiry or a Redis restart), it would re-publish a months-old post-game.
	staleStart := time.Now().UTC().Add(-200 * 24 * time.Hour).Format(time.RFC3339)
	schedJSON := fmt.Sprintf(`{"games": [
		{
			"id": 2024020042,
			"gameDate": "2025-04-01",
			"startTimeUTC": %q,
			"gameType": 2,
			"gameState": "FINAL",
			"homeTeam": {"abbrev": "WSH"},
			"awayTeam": {"abbrev": "PHI"}
		}
	]}`, staleStart)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(schedJSON))
	}))
	defer server.Close()
	replaceHTTPClient(t, server)

	g, err := LastCompletedGame(context.Background())
	if err != nil {
		t.Fatalf("LastCompletedGame: %v", err)
	}
	if g != nil {
		t.Errorf("expected nil for a stale (long-finished) game, got: %+v", g)
	}
}

func TestLastCompletedGame_NonOK(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	replaceHTTPClient(t, server)

	_, err := LastCompletedGame(context.Background())
	if err == nil {
		t.Error("expected error for non-200 status, got nil")
	}
}

func TestLastCompletedGame_IgnoresPreseasonGame(t *testing.T) {
	// A finished preseason game (gameType 1) must never produce a post-game report.
	recentStart := time.Now().UTC().Add(-3 * time.Hour).Format(time.RFC3339)
	schedJSON := fmt.Sprintf(`{"games": [
		{
			"id": 2026010020,
			"gameType": 1,
			"gameDate": "2026-09-21",
			"startTimeUTC": %q,
			"gameState": "FINAL",
			"homeTeam": {"abbrev": "WSH"},
			"awayTeam": {"abbrev": "PHI"}
		}
	]}`, recentStart)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(schedJSON))
	}))
	defer server.Close()
	replaceHTTPClient(t, server)

	g, err := LastCompletedGame(context.Background())
	if err != nil {
		t.Fatalf("LastCompletedGame: %v", err)
	}
	if g != nil {
		t.Errorf("expected nil for a preseason game, got: %+v", g)
	}
}

func TestLastCompletedGame_PrefersRegularSeasonOverNewerPlayoffGame(t *testing.T) {
	// Only regular-season (gameType 2) games count, even when a playoff game finished more recently.
	now := time.Now().UTC()
	regularStart := now.Add(-48 * time.Hour).Format(time.RFC3339)
	playoffStart := now.Add(-3 * time.Hour).Format(time.RFC3339)
	schedJSON := fmt.Sprintf(`{"games": [
		{
			"id": 2026021312,
			"gameType": 2,
			"gameDate": "2027-04-15",
			"startTimeUTC": %q,
			"gameState": "FINAL",
			"homeTeam": {"abbrev": "WSH"},
			"awayTeam": {"abbrev": "BOS"}
		},
		{
			"id": 2026030111,
			"gameType": 3,
			"gameDate": "2027-04-17",
			"startTimeUTC": %q,
			"gameState": "FINAL",
			"homeTeam": {"abbrev": "WSH"},
			"awayTeam": {"abbrev": "PIT"}
		}
	]}`, regularStart, playoffStart)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(schedJSON))
	}))
	defer server.Close()
	replaceHTTPClient(t, server)

	g, err := LastCompletedGame(context.Background())
	if err != nil {
		t.Fatalf("LastCompletedGame: %v", err)
	}
	if g == nil || g.GameID != 2026021312 {
		t.Fatalf("got %+v; want regular-season game 2026021312", g)
	}
}
