package model

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"ovechbot_go/predictor/internal/cache"
)

// loadFixtureGameLog reads testdata/game_log.json: the collector's real output for Ovechkin's 2024-25 to 2026-27
// seasons (as of 2026-11-17), kept in sync by the collector's TestGameLog_MatchesPredictorFixture.
func loadFixtureGameLog(t *testing.T) []cache.GameLogEntry {
	t.Helper()
	b, err := os.ReadFile("testdata/game_log.json")
	if err != nil {
		t.Fatal(err)
	}
	var log []cache.GameLogEntry
	if err := json.Unmarshal(b, &log); err != nil {
		t.Fatal(err)
	}
	return log
}

func TestFixtureGameLog_MeetsModelAssumptions(t *testing.T) {
	log := loadFixtureGameLog(t)
	for i, e := range log {
		if i > 0 && e.GameDate < log[i-1].GameDate {
			t.Fatalf("game %d (%s) before game %d (%s); the model reads the log oldest-first", i, e.GameDate, i-1, log[i-1].GameDate)
		}
		// seasonOf decodes the season from the game ID; it must agree with the date.
		d, err := time.Parse("2006-01-02", e.GameDate)
		if err != nil {
			t.Fatalf("game %d: bad gameDate %q", i, e.GameDate)
		}
		wantSeason := d.Year()
		if d.Month() < time.July {
			wantSeason--
		}
		if got := seasonOf(e); got != wantSeason {
			t.Fatalf("game %d (%s): seasonOf = %d; want %d", i, e.GameDate, got, wantSeason)
		}
	}
	if sumShots(log) == 0 {
		t.Error("fixture has no shots; baselineGPGFrom would fall back to goals only")
	}
}
