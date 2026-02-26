package goalie

import (
	"context"
	"testing"
	"time"

	"ovechbot_go/predictor/internal/schedule"
)

func TestParseDFOGoalieName(t *testing.T) {
	// Simulated HTML fragment: Philadelphia Flyers at Washington Capitals, then two goalies (away=PHI, home=WSH).
	html := []byte(`
	<div>Philadelphia Flyers at Washington Capitals</div>
	<div>2026-02-26T00:00:00.000Z</div>
	<a href="/">Dan Vladar</a>
	<span>Confirmed</span>
	<a href="/">Logan Thompson</a>
	<span>Confirmed</span>
	`)
	// Caps home → we want away goalie (PHI) = Dan Vladar.
	got := parseDFOGoalieName(html, "Philadelphia", true)
	if got != "Dan Vladar" {
		t.Errorf("Caps home: got %q, want Dan Vladar", got)
	}
	// Caps away → we want home goalie (opponent's home) = Logan Thompson.
	got = parseDFOGoalieName(html, "Philadelphia", false)
	if got != "Logan Thompson" {
		t.Errorf("Caps away (opponent PHI home): got %q, want Logan Thompson", got)
	}
}

func TestParseDFOGoalieName_noMatch(t *testing.T) {
	html := []byte(`<div>Buffalo Sabres at New Jersey Devils</div><a>Ukko-Pekka Luukkonen</a><a>Jake Allen</a>`)
	got := parseDFOGoalieName(html, "Philadelphia", true)
	if got != "" {
		t.Errorf("wrong game block: got %q, want empty", got)
	}
}

// TestOpposingStarterFromDFO_live fetches the real Daily Faceoff page and verifies we can
// extract the opposing starter for a Caps game. Skips if the page is unavailable.
func TestOpposingStarterFromDFO_live(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// PHI @ WSH on 2026-02-25 (game date); DFO URL uses that date.
	g := &schedule.Game{
		GameID:       0,
		HomeAbbrev:   "WSH",
		AwayAbbrev:   "PHI",
		StartTimeUTC: time.Date(2026, 2, 26, 0, 0, 0, 0, time.UTC),
		GameState:    "FUT",
		GameDate:     "2026-02-25",
	}
	client := NewClient()
	got := client.OpposingStarterFromDFO(ctx, g)
	if got == "" {
		t.Skip("DFO fetch returned no goalie (page may have changed or be unavailable)")
	}
	// We expect the opponent (PHI) starter; Caps are home so away goalie = PHI.
	// At the time of writing this was "Dan Vladar"; allow any non-empty name.
	if len(got) < 3 {
		t.Errorf("OpposingStarterFromDFO: got %q, expected a full goalie name", got)
	}
	t.Logf("DFO returned opposing starter: %s", got)
}
