package model

import (
	"testing"
	"time"

	"ovechbot_go/predictor/internal/cache"
	"ovechbot_go/predictor/internal/schedule"
)

func TestSigmoid(t *testing.T) {
	// z > 20 clamps to exactly 1.0; z < -20 clamps to exactly 0.0; z == ±20 goes through exp.
	if got := sigmoid(0); got != 0.5 {
		t.Errorf("sigmoid(0) = %v; want 0.5", got)
	}
	if got := sigmoid(25); got != 1.0 {
		t.Errorf("sigmoid(25) = %v; want 1.0", got)
	}
	if got := sigmoid(-25); got != 0.0 {
		t.Errorf("sigmoid(-25) = %v; want 0.0", got)
	}
	// At the boundary z == 20 / -20 the exponential is computed (not clamped).
	if got := sigmoid(20); got <= 0.99 || got > 1.0 {
		t.Errorf("sigmoid(20) = %v; want > 0.99 and ≤ 1.0", got)
	}
	if got := sigmoid(-20); got < 0 || got >= 0.01 {
		t.Errorf("sigmoid(-20) = %v; want ≥ 0 and < 0.01", got)
	}
	// Near-zero: sigmoid(1) should be ~0.731
	s1 := sigmoid(1.0)
	if s1 < 0.7 || s1 > 0.8 {
		t.Errorf("sigmoid(1.0) = %v; want ~0.731", s1)
	}
}

func TestDot(t *testing.T) {
	if got := dot([]float64{1, 2, 3}, []float64{4, 5, 6}); got != 32 {
		t.Errorf("dot = %v; want 32", got)
	}
	if got := dot([]float64{0}, []float64{99}); got != 0 {
		t.Errorf("dot = %v; want 0", got)
	}
}

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

func TestLeagueAvgGAFromStandings_Empty(t *testing.T) {
	got := leagueAvgGAFromStandings(nil)
	if got != 3.0 {
		t.Errorf("leagueAvgGAFromStandings(nil) = %v; want 3.0", got)
	}
}

func TestLeagueAvgGAFromStandings_ZeroGP(t *testing.T) {
	standings := map[string]cache.StandingsTeam{
		"TST": {GoalAgainst: 100, GamesPlayed: 0},
	}
	got := leagueAvgGAFromStandings(standings)
	if got != 3.0 {
		t.Errorf("leagueAvgGAFromStandings(zero GP) = %v; want 3.0", got)
	}
}

func TestLeagueAvgGAFromStandings_Normal(t *testing.T) {
	standings := map[string]cache.StandingsTeam{
		"A": {GoalAgainst: 300, GamesPlayed: 100},
		"B": {GoalAgainst: 200, GamesPlayed: 100},
	}
	got := leagueAvgGAFromStandings(standings)
	// (300+200)/(100+100) = 2.5
	if got != 2.5 {
		t.Errorf("leagueAvgGAFromStandings = %v; want 2.5", got)
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

func makeStandings() map[string]cache.StandingsTeam {
	return map[string]cache.StandingsTeam{
		"PHI": {GamesPlayed: 60, GoalAgainst: 180, HomeGamesPlayed: 30, HomeGoalsAgainst: 90, RoadGamesPlayed: 30, RoadGoalsAgainst: 90},
		"NYR": {GamesPlayed: 60, GoalAgainst: 150, HomeGamesPlayed: 30, HomeGoalsAgainst: 75, RoadGamesPlayed: 30, RoadGoalsAgainst: 75},
		"PIT": {GamesPlayed: 60, GoalAgainst: 210, HomeGamesPlayed: 30, HomeGoalsAgainst: 105, RoadGamesPlayed: 30, RoadGoalsAgainst: 105},
		"BOS": {GamesPlayed: 60, GoalAgainst: 160, HomeGamesPlayed: 30, HomeGoalsAgainst: 80, RoadGamesPlayed: 30, RoadGoalsAgainst: 80},
		"CBJ": {GamesPlayed: 60, GoalAgainst: 240, HomeGamesPlayed: 30, HomeGoalsAgainst: 120, RoadGamesPlayed: 30, RoadGoalsAgainst: 120},
	}
}

func TestLogisticPredict_InsufficientData(t *testing.T) {
	g := &schedule.Game{HomeAbbrev: "WSH", AwayAbbrev: "PHI", StartTimeUTC: time.Now().Add(24 * time.Hour)}
	standings := makeStandings()

	// 49 games — one short of threshold
	got := LogisticPredict(g, makeGameLog(49), standings)
	if got != -1 {
		t.Errorf("LogisticPredict with 49 games = %d; want -1", got)
	}
}

func TestLogisticPredict_SufficientData(t *testing.T) {
	g := &schedule.Game{HomeAbbrev: "WSH", AwayAbbrev: "PHI", StartTimeUTC: time.Now().Add(24 * time.Hour)}
	standings := makeStandings()

	// 70 games — enough for logistic (>50 samples, >20 training samples after skip)
	got := LogisticPredict(g, makeGameLog(70), standings)
	if got < 15 || got > 75 {
		t.Errorf("LogisticPredict = %d; want in [15, 75]", got)
	}
}

func TestLogisticPredict_AwayGame(t *testing.T) {
	// WSH is away team
	g := &schedule.Game{HomeAbbrev: "PHI", AwayAbbrev: "WSH", StartTimeUTC: time.Now().Add(48 * time.Hour)}
	standings := makeStandings()

	got := LogisticPredict(g, makeGameLog(70), standings)
	if got != -1 && (got < 15 || got > 75) {
		t.Errorf("LogisticPredict (away) = %d; want in [15,75] or -1", got)
	}
}

func TestLogisticPredict_Clamped(t *testing.T) {
	// Build a log where Ovi scores every single game to push probability high.
	// Result should still be clamped to ≤75.
	log := make([]cache.GameLogEntry, 80)
	for i := range log {
		log[i] = cache.GameLogEntry{
			GameDate: "2026-01-01", OpponentAbbrev: "PHI",
			HomeRoadFlag: "H", Goals: 2,
		}
	}
	g := &schedule.Game{HomeAbbrev: "WSH", AwayAbbrev: "PHI", StartTimeUTC: time.Now().Add(24 * time.Hour)}
	got := LogisticPredict(g, log, makeStandings())
	if got != -1 && got > 75 {
		t.Errorf("LogisticPredict (always scores) = %d; want ≤75", got)
	}
}
