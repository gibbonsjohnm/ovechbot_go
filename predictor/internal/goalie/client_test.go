package goalie

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ovechbot_go/predictor/internal/schedule"
)

// testTransport rewrites the scheme+host to a local test server and forwards the path as-is.
type testTransport struct {
	baseURL string
}

func (t *testTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	newURL := t.baseURL + req.URL.RequestURI()
	newReq, err := http.NewRequest(req.Method, newURL, req.Body)
	if err != nil {
		return nil, err
	}
	newReq.Header = req.Header
	return http.DefaultTransport.RoundTrip(newReq)
}

// testClient returns a Client whose HTTP calls are redirected to the given server.
func testClient(server *httptest.Server) *Client {
	return &Client{
		http: &http.Client{
			Transport: &testTransport{baseURL: server.URL},
		},
	}
}

func makeGame(gameID int64, capsHome bool) *schedule.Game {
	if capsHome {
		return &schedule.Game{GameID: gameID, HomeAbbrev: "WSH", AwayAbbrev: "PHI", StartTimeUTC: time.Now()}
	}
	return &schedule.Game{GameID: gameID, HomeAbbrev: "PHI", AwayAbbrev: "WSH", StartTimeUTC: time.Now()}
}

// ---- opposingStarterFromBoxscore tests ----

func TestOpposingStarterFromBoxscore_CapsHome(t *testing.T) {
	// WSH is home → opponent is away team → we want the home goalie starter (PHI is home → actually we want away team's starter from home perspective).
	// Wait: caps are HOME means HomeAbbrev == "WSH". Opponent is AwayTeam (PHI).
	// The code: if box.AwayTeam.Abbrev == "WSH" → take HomeTeam goalies; else take AwayTeam goalies.
	// Caps are home: box.HomeTeam.Abbrev == "WSH", box.AwayTeam.Abbrev == "PHI".
	// So the "else" branch runs → take AwayTeam goalies (PHI's goalies). Correct.
	boxJSON := `{
		"awayTeam": {"abbrev": "PHI"},
		"homeTeam": {"abbrev": "WSH"},
		"playerByGameStats": {
			"awayTeam": {"goalies": [{"playerId": 8480945, "name": {"default": "S. Ersson"}, "starter": true}]},
			"homeTeam": {"goalies": [{"playerId": 9999999, "name": {"default": "C. Lindberg"}, "starter": true}]}
		}
	}`
	landingJSON := `{"featuredStats": {"regularSeason": {"subSeason": {"savePctg": 0.912}}}}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "boxscore") {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(boxJSON))
		} else {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(landingJSON))
		}
	}))
	defer server.Close()

	c := testClient(server)
	g := makeGame(20250001, true) // caps home
	info, err := c.opposingStarterFromBoxscore(context.Background(), g)
	if err != nil {
		t.Fatalf("opposingStarterFromBoxscore: %v", err)
	}
	if info == nil {
		t.Fatal("expected non-nil Info, got nil")
	}
	if info.Name != "S. Ersson" {
		t.Errorf("Name = %q; want S. Ersson", info.Name)
	}
	if info.SavePct != 0.912 {
		t.Errorf("SavePct = %v; want 0.912", info.SavePct)
	}
}

func TestOpposingStarterFromBoxscore_CapsAway(t *testing.T) {
	// WSH is away: box.AwayTeam.Abbrev == "WSH" → take HomeTeam goalies (PHI home goalie).
	boxJSON := `{
		"awayTeam": {"abbrev": "WSH"},
		"homeTeam": {"abbrev": "PHI"},
		"playerByGameStats": {
			"awayTeam": {"goalies": [{"playerId": 9999999, "name": {"default": "C. Lindberg"}, "starter": true}]},
			"homeTeam": {"goalies": [{"playerId": 8480945, "name": {"default": "S. Ersson"}, "starter": true}]}
		}
	}`
	landingJSON := `{"featuredStats": {"regularSeason": {"subSeason": {"savePctg": 0.905}}}}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "boxscore") {
			w.Write([]byte(boxJSON))
		} else {
			w.Write([]byte(landingJSON))
		}
	}))
	defer server.Close()

	c := testClient(server)
	g := makeGame(20250002, false) // caps away
	info, err := c.opposingStarterFromBoxscore(context.Background(), g)
	if err != nil {
		t.Fatalf("opposingStarterFromBoxscore: %v", err)
	}
	if info == nil {
		t.Fatal("expected non-nil Info, got nil")
	}
	if info.Name != "S. Ersson" {
		t.Errorf("Name = %q; want S. Ersson", info.Name)
	}
}

func TestOpposingStarterFromBoxscore_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	c := testClient(server)
	info, err := c.opposingStarterFromBoxscore(context.Background(), makeGame(99, true))
	if err != nil {
		t.Errorf("expected nil error for 404, got: %v", err)
	}
	if info != nil {
		t.Errorf("expected nil Info for 404, got: %+v", info)
	}
}

func TestOpposingStarterFromBoxscore_NoGoalies(t *testing.T) {
	boxJSON := `{
		"awayTeam": {"abbrev": "PHI"},
		"homeTeam": {"abbrev": "WSH"},
		"playerByGameStats": {
			"awayTeam": {"goalies": []},
			"homeTeam": {"goalies": []}
		}
	}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(boxJSON))
	}))
	defer server.Close()

	c := testClient(server)
	info, err := c.opposingStarterFromBoxscore(context.Background(), makeGame(20250003, true))
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if info != nil {
		t.Errorf("expected nil when no goalies, got: %+v", info)
	}
}

// ---- resolveGoalieByName tests ----

func TestResolveGoalieByName_Found(t *testing.T) {
	roster := map[string]interface{}{
		"goalies": []map[string]interface{}{
			{
				"id":        8480945,
				"firstName": map[string]string{"default": "Samuel"},
				"lastName":  map[string]string{"default": "Ersson"},
			},
		},
	}
	rosterJSON, _ := json.Marshal(roster)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(rosterJSON)
	}))
	defer server.Close()

	c := testClient(server)
	id, display := c.resolveGoalieByName(context.Background(), "PHI", "Samuel Ersson")
	if id != 8480945 {
		t.Errorf("id = %d; want 8480945", id)
	}
	if display != "S. Ersson" {
		t.Errorf("display = %q; want S. Ersson", display)
	}
}

func TestResolveGoalieByName_NotFound(t *testing.T) {
	roster := map[string]interface{}{
		"goalies": []map[string]interface{}{
			{
				"id":        8480945,
				"firstName": map[string]string{"default": "Samuel"},
				"lastName":  map[string]string{"default": "Ersson"},
			},
		},
	}
	rosterJSON, _ := json.Marshal(roster)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(rosterJSON)
	}))
	defer server.Close()

	c := testClient(server)
	id, display := c.resolveGoalieByName(context.Background(), "PHI", "Unknown Goalie")
	if id != 0 || display != "" {
		t.Errorf("expected (0, \"\"), got (%d, %q)", id, display)
	}
}

func TestResolveGoalieByName_NonOK(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	c := testClient(server)
	id, display := c.resolveGoalieByName(context.Background(), "PHI", "Samuel Ersson")
	if id != 0 || display != "" {
		t.Errorf("expected (0, \"\") for non-200, got (%d, %q)", id, display)
	}
}

// ---- playerSavePct tests ----

func TestPlayerSavePct_Found(t *testing.T) {
	landingJSON := `{"featuredStats": {"regularSeason": {"subSeason": {"savePctg": 0.923}}}}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(landingJSON))
	}))
	defer server.Close()

	c := testClient(server)
	pct, err := c.playerSavePct(context.Background(), 8480945)
	if err != nil {
		t.Fatalf("playerSavePct: %v", err)
	}
	if pct != 0.923 {
		t.Errorf("savePct = %v; want 0.923", pct)
	}
}

func TestPlayerSavePct_MissingNestedStats(t *testing.T) {
	// featuredStats is null → returns 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"featuredStats": null}`))
	}))
	defer server.Close()

	c := testClient(server)
	pct, err := c.playerSavePct(context.Background(), 8480945)
	if err != nil {
		t.Fatalf("playerSavePct: %v", err)
	}
	if pct != 0 {
		t.Errorf("savePct = %v; want 0 when stats missing", pct)
	}
}

func TestPlayerSavePct_NonOK(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	c := testClient(server)
	_, err := c.playerSavePct(context.Background(), 8480945)
	if err == nil {
		t.Error("expected error for non-200 status, got nil")
	}
}
