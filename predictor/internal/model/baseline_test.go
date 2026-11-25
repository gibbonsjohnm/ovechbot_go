package model

import (
	"math"
	"testing"

	"ovechbot_go/predictor/internal/cache"
)

func TestBaselineGPGFrom_Empty(t *testing.T) {
	got := baselineGPGFrom(nil, 82)
	if got != 0.4 {
		t.Errorf("baselineGPGFrom(nil) = %v; want 0.4", got)
	}
}

func TestBaselineGPGFrom_Exact(t *testing.T) {
	// 10 games, 5 goals → 0.5 GPG
	log := make([]cache.GameLogEntry, 10)
	for i := range log {
		if i%2 == 0 {
			log[i].Goals = 1
		}
	}
	got := baselineGPGFrom(log, 82)
	if got != 0.5 {
		t.Errorf("baselineGPGFrom = %v; want 0.5", got)
	}
}

func TestBaselineGPGFrom_MaxCap(t *testing.T) {
	// 100 games, only last 82 count. Last 82 have 1 goal each → 1.0 GPG.
	log := make([]cache.GameLogEntry, 100)
	// First 18: 0 goals. Last 82: 1 goal each.
	for i := 18; i < 100; i++ {
		log[i].Goals = 1
	}
	got := baselineGPGFrom(log, 82)
	if got != 1.0 {
		t.Errorf("baselineGPGFrom = %v; want 1.0", got)
	}
}

// seasonLog builds games for one season (GameID prefix = startYear) with the given number of goals spread over the first games.
func seasonLog(startYear, games, goals int) []cache.GameLogEntry {
	log := make([]cache.GameLogEntry, games)
	for i := range log {
		log[i].GameID = startYear*1_000_000 + 20_000 + i + 1
		if i < goals {
			log[i].Goals = 1
		}
	}
	return log
}

func TestBaselineGPGFrom_CurrentSeasonShrunkTowardPrior(t *testing.T) {
	// Last season 32 goals in 82 games; this season 1 goal in 18. A flat 82-game window would give 28/82 ≈ 0.34,
	// barely reacting to the slump. The baseline should weight the current season and only shrink it toward last season.
	log := append(seasonLog(2025, 82, 32), seasonLog(2026, 18, 1)...)
	got := baselineGPGFrom(log, 82)
	want := (1 + baselinePriorGames*32.0/82) / (18 + baselinePriorGames)
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("baselineGPGFrom = %v; want %v", got, want)
	}
}

func TestBaselineGPGFrom_FirstGameOfSeasonLeansOnPrior(t *testing.T) {
	// One scoreless game into a new season must not crater the baseline.
	log := append(seasonLog(2025, 82, 41), seasonLog(2026, 1, 0)...)
	got := baselineGPGFrom(log, 82)
	if got < 0.45 {
		t.Errorf("baselineGPGFrom after one game = %v; want close to last season's 0.5", got)
	}
}

func TestBaselineGPGFrom_PriorUsesSeasonsBeforeCurrent(t *testing.T) {
	// The prior is the maxGames games before the current season, not including it.
	log := append(append(seasonLog(2024, 82, 0), seasonLog(2025, 82, 41)...), seasonLog(2026, 10, 5)...)
	got := baselineGPGFrom(log, 82)
	want := (5 + baselinePriorGames*0.5) / (10 + baselinePriorGames)
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("baselineGPGFrom = %v; want %v", got, want)
	}
}

// seasonLogShots is seasonLog with shots spread as evenly as possible across the games.
func seasonLogShots(startYear, games, goals, shots int) []cache.GameLogEntry {
	log := seasonLog(startYear, games, goals)
	for i := range log {
		log[i].Shots = shots / games
		if i < shots%games {
			log[i].Shots++
		}
	}
	return log
}

func TestBaselineGPGFrom_ShotBased(t *testing.T) {
	// Last season 32 goals on 244 shots in 82 games; this season 1 goal on 33 shots in 19 games.
	log := append(seasonLogShots(2025, 82, 32, 244), seasonLogShots(2026, 19, 1, 33)...)
	got := baselineGPGFrom(log, 82)
	shotsPerGame := (33 + baselineShotsPriorGames*244.0/82) / (19 + baselineShotsPriorGames)
	shootingPct := (1 + baselineShPctPriorShots*32.0/244) / (33 + baselineShPctPriorShots)
	if want := shotsPerGame * shootingPct; math.Abs(got-want) > 1e-9 {
		t.Errorf("baselineGPGFrom = %v; want %v", got, want)
	}
}

func TestBaselineGPGFrom_FewerShotsLowersBaseline(t *testing.T) {
	// Same 1 goal this season: 1.7 shots a game is a smaller role than 3 a game, and the baseline should say so.
	prior := seasonLogShots(2025, 82, 32, 244)
	fewShots := baselineGPGFrom(append(append([]cache.GameLogEntry{}, prior...), seasonLogShots(2026, 19, 1, 33)...), 82)
	manyShots := baselineGPGFrom(append(append([]cache.GameLogEntry{}, prior...), seasonLogShots(2026, 19, 1, 57)...), 82)
	if fewShots >= manyShots {
		t.Errorf("baseline on 33 shots = %v; want < baseline on 57 shots = %v", fewShots, manyShots)
	}
}

// makeGameLog builds n game log entries with alternating opponent abbrevs and a roughly
// constant scoring rate (~40% of games have 1 goal). Oldest game is at index 0.
func makeGameLog(n int) []cache.GameLogEntry {
	log := make([]cache.GameLogEntry, n)
	opponents := []string{"PHI", "NYR", "PIT", "BOS", "CBJ"}
	for i := range log {
		opp := opponents[i%len(opponents)]
		home := "H"
		if i%2 == 0 {
			home = "R"
		}
		goals := 0
		if i%5 == 0 || i%7 == 0 { // ~40% scoring rate
			goals = 1
		}
		log[i] = cache.GameLogEntry{
			GameDate:       "2026-01-01",
			OpponentAbbrev: opp,
			HomeRoadFlag:   home,
			Goals:          goals,
		}
	}
	return log
}
