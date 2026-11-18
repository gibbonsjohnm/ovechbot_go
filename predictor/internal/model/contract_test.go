package model

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"ovechbot_go/predictor/internal/cache"
	"ovechbot_go/predictor/internal/schedule"
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

func TestFixtureGameLog_RestFactorUsesLocalDates(t *testing.T) {
	// Last game in the fixture is Tue 2026-11-17. Evening ET starts are already the next day in UTC.
	log := loadFixtureGameLog(t)
	backToBack := &schedule.Game{GameDate: "2026-11-18", StartTimeUTC: time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC)}
	if got := restFactor(backToBack, log); got != 0.92 {
		t.Errorf("restFactor(next night, 7pm ET) = %v; want 0.92", got)
	}
	oneDayOff := &schedule.Game{GameDate: "2026-11-19", StartTimeUTC: time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)}
	if got := restFactor(oneDayOff, log); got != 1.0 {
		t.Errorf("restFactor(one day off, 7pm ET) = %v; want 1.0", got)
	}
}
