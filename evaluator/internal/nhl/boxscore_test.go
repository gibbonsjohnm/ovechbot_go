package nhl

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ---- OvechkinGameStats tests ----
// Uses the shared replaceHTTPClient + testRoundTripper from schedule_test.go.

func TestOvechkinGameStats_FoundInAwayForwards(t *testing.T) {
	// Ovi is in the away team forwards list.
	boxJSON := `{
		"playerByGameStats": {
			"awayTeam": {
				"forwards": [
					{"playerId": 8471214, "goals": 2, "assists": 1, "points": 3, "toi": "20:12", "shifts": 22, "sog": 5}
				],
				"defense": []
			},
			"homeTeam": {
				"forwards": [],
				"defense": []
			}
		}
	}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(boxJSON))
	}))
	defer server.Close()
	replaceHTTPClient(t, server)

	stats, err := OvechkinGameStats(context.Background(), 20250001)
	if err != nil {
		t.Fatalf("OvechkinGameStats: %v", err)
	}
	if stats == nil {
		t.Fatal("expected non-nil stats, got nil")
	}
	if stats.Goals != 2 {
		t.Errorf("Goals = %d; want 2", stats.Goals)
	}
	if stats.Assists != 1 {
		t.Errorf("Assists = %d; want 1", stats.Assists)
	}
	if stats.TOI != "20:12" {
		t.Errorf("TOI = %q; want 20:12", stats.TOI)
	}
	if stats.SOG != 5 {
		t.Errorf("SOG = %d; want 5", stats.SOG)
	}
}

func TestOvechkinGameStats_FoundInHomeDefense(t *testing.T) {
	// Unlikely but possible — Ovi is in home team defense (tests all four list scans).
	boxJSON := `{
		"playerByGameStats": {
			"awayTeam": {"forwards": [], "defense": []},
			"homeTeam": {
				"forwards": [],
				"defense": [
					{"playerId": 8471214, "goals": 1, "assists": 0, "points": 1, "toi": "18:30", "shifts": 19, "sog": 3}
				]
			}
		}
	}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(boxJSON))
	}))
	defer server.Close()
	replaceHTTPClient(t, server)

	stats, err := OvechkinGameStats(context.Background(), 20250002)
	if err != nil {
		t.Fatalf("OvechkinGameStats: %v", err)
	}
	if stats == nil {
		t.Fatal("expected non-nil stats, got nil")
	}
	if stats.Goals != 1 {
		t.Errorf("Goals = %d; want 1", stats.Goals)
	}
}

func TestOvechkinGameStats_NotFound(t *testing.T) {
	// Ovi not in any list → returns nil, nil.
	boxJSON := `{
		"playerByGameStats": {
			"awayTeam": {
				"forwards": [{"playerId": 9999999, "goals": 0, "assists": 0, "points": 0, "toi": "15:00", "shifts": 15, "sog": 0}],
				"defense": []
			},
			"homeTeam": {"forwards": [], "defense": []}
		}
	}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(boxJSON))
	}))
	defer server.Close()
	replaceHTTPClient(t, server)

	stats, err := OvechkinGameStats(context.Background(), 20250003)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if stats != nil {
		t.Errorf("expected nil stats when Ovi not found, got: %+v", stats)
	}
}

func TestOvechkinGameStats_NonOK(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	replaceHTTPClient(t, server)

	_, err := OvechkinGameStats(context.Background(), 9999)
	if err == nil {
		t.Error("expected error for non-200 status, got nil")
	}
}
