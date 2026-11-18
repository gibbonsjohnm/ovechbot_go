package model

import (
	"testing"
	"time"

	"ovechbot_go/predictor/internal/cache"
	"ovechbot_go/predictor/internal/schedule"
)

func TestClampPct(t *testing.T) {
	cases := []struct{ in, want int }{
		{0, 5},
		{4, 5},
		{5, 5},
		{14, 14},
		{50, 50},
		{75, 75},
		{76, 75},
		{100, 75},
	}
	for _, tc := range cases {
		if got := ClampPct(tc.in); got != tc.want {
			t.Errorf("ClampPct(%d) = %d; want %d", tc.in, got, tc.want)
		}
	}
}

func TestRestFactor_EmptyLog(t *testing.T) {
	g := &schedule.Game{StartTimeUTC: time.Now()}
	got := restFactor(g, nil)
	if got != 1.0 {
		t.Errorf("restFactor(empty log) = %v; want 1.0", got)
	}
}

func TestRestFactor_BackToBack(t *testing.T) {
	yesterday := time.Now().UTC().Add(-24 * time.Hour)
	log := []cache.GameLogEntry{
		{GameDate: yesterday.Format("2006-01-02"), Goals: 0},
	}
	g := &schedule.Game{StartTimeUTC: time.Now().UTC()}
	got := restFactor(g, log)
	if got != 0.92 {
		t.Errorf("restFactor(back-to-back) = %v; want 0.92", got)
	}
}

func TestRestFactor_Rested(t *testing.T) {
	threeDaysAgo := time.Now().UTC().Add(-72 * time.Hour)
	log := []cache.GameLogEntry{
		{GameDate: threeDaysAgo.Format("2006-01-02"), Goals: 1},
	}
	g := &schedule.Game{StartTimeUTC: time.Now().UTC()}
	got := restFactor(g, log)
	if got != 1.02 {
		t.Errorf("restFactor(rested) = %v; want 1.02", got)
	}
}

func TestRestFactor_EveningGameBackToBack(t *testing.T) {
	// Nov 27 → Nov 28 at 7pm ET, which is 00:00 UTC Nov 29. Must use the local game date, not the UTC date.
	log := []cache.GameLogEntry{{GameDate: "2026-11-27"}}
	g := &schedule.Game{GameDate: "2026-11-28", StartTimeUTC: time.Date(2026, 11, 29, 0, 0, 0, 0, time.UTC)}
	if got := restFactor(g, log); got != 0.92 {
		t.Errorf("restFactor(evening back-to-back) = %v; want 0.92", got)
	}
}

func TestRestFactor_OneDayOffIsNeutral(t *testing.T) {
	// Nov 26 → Nov 28: one day off is normal rest, not "rested" (2+ days off).
	log := []cache.GameLogEntry{{GameDate: "2026-11-26"}}
	g := &schedule.Game{GameDate: "2026-11-28", StartTimeUTC: time.Date(2026, 11, 29, 0, 0, 0, 0, time.UTC)}
	if got := restFactor(g, log); got != 1.0 {
		t.Errorf("restFactor(one day off) = %v; want 1.0", got)
	}
}

func TestRestFactor_BadDate(t *testing.T) {
	log := []cache.GameLogEntry{
		{GameDate: "not-a-date", Goals: 0},
	}
	g := &schedule.Game{StartTimeUTC: time.Now().UTC()}
	got := restFactor(g, log)
	if got != 1.0 {
		t.Errorf("restFactor(bad date) = %v; want 1.0", got)
	}
}

func TestOviVsOpponentFactor_TooFewGames(t *testing.T) {
	log := []cache.GameLogEntry{
		{OpponentAbbrev: "PHI", Goals: 1},
		{OpponentAbbrev: "PHI", Goals: 1},
		// only 2 games vs PHI — need ≥3
	}
	got := oviVsOpponentFactor(log, "PHI", 0.5)
	if got != 1.0 {
		t.Errorf("oviVsOpponentFactor(< 3 games) = %v; want 1.0", got)
	}
}

func TestOviVsOpponentFactor_ZeroBaseline(t *testing.T) {
	log := []cache.GameLogEntry{
		{OpponentAbbrev: "PHI", Goals: 1},
		{OpponentAbbrev: "PHI", Goals: 1},
		{OpponentAbbrev: "PHI", Goals: 1},
	}
	got := oviVsOpponentFactor(log, "PHI", 0.0)
	if got != 1.0 {
		t.Errorf("oviVsOpponentFactor(zero baseline) = %v; want 1.0", got)
	}
}

func TestOviVsOpponentFactor_ClampHigh(t *testing.T) {
	// Ovi scores 3 goals/game vs PHI vs baseline of 0.3 → ratio 10 → clamped to 1.15
	log := make([]cache.GameLogEntry, 5)
	for i := range log {
		log[i] = cache.GameLogEntry{OpponentAbbrev: "PHI", Goals: 3}
	}
	got := oviVsOpponentFactor(log, "PHI", 0.3)
	if got != 1.15 {
		t.Errorf("oviVsOpponentFactor(high) = %v; want 1.15", got)
	}
}

func TestOviVsOpponentFactor_ClampLow(t *testing.T) {
	// Ovi scores 0 vs PHI but baseline 2.0 → ratio 0 → clamped to 0.85
	log := make([]cache.GameLogEntry, 5)
	for i := range log {
		log[i] = cache.GameLogEntry{OpponentAbbrev: "PHI", Goals: 0}
	}
	got := oviVsOpponentFactor(log, "PHI", 2.0)
	if got != 0.85 {
		t.Errorf("oviVsOpponentFactor(low) = %v; want 0.85", got)
	}
}

func TestPredict_EmptyLog(t *testing.T) {
	g := &schedule.Game{HomeAbbrev: "WSH", AwayAbbrev: "PHI", StartTimeUTC: time.Now().Add(24 * time.Hour)}
	got := Predict(g, nil, nil, 0)
	if got != 45 {
		t.Errorf("Predict(empty log) = %d; want 45", got)
	}
}

func TestPredict_HeuristicOnly(t *testing.T) {
	// 10 games — not enough for logistic (need 50), uses heuristic only
	g := &schedule.Game{HomeAbbrev: "WSH", AwayAbbrev: "PHI", StartTimeUTC: time.Now().Add(24 * time.Hour)}
	log := makeGameLog(10)
	got := Predict(g, log, makeStandings(), 0)
	if got < MinPct || got > MaxPct {
		t.Errorf("Predict(heuristic-only) = %d; want in [%d, %d]", got, MinPct, MaxPct)
	}
}

func TestPredict_LongLog(t *testing.T) {
	// 70 games; result should be clamped
	g := &schedule.Game{HomeAbbrev: "WSH", AwayAbbrev: "PHI", StartTimeUTC: time.Now().Add(24 * time.Hour)}
	log := makeGameLog(70)
	got := Predict(g, log, makeStandings(), 0)
	if got < MinPct || got > MaxPct {
		t.Errorf("Predict(long log) = %d; want in [%d, %d]", got, MinPct, MaxPct)
	}
}

func TestPredict_GoalieFactor_Strong(t *testing.T) {
	// Strong goalie (high SV%) should lower prediction vs no goalie factor
	g := &schedule.Game{HomeAbbrev: "WSH", AwayAbbrev: "PHI", StartTimeUTC: time.Now().Add(24 * time.Hour)}
	log := makeGameLog(30)
	standings := makeStandings()
	withAvgGoalie := Predict(g, log, standings, 0.905)   // league average — factor ~1.0
	withEliteGoalie := Predict(g, log, standings, 0.940) // elite — factor ~0.90 → lower
	// Elite goalie should give equal or lower prediction
	if withEliteGoalie > withAvgGoalie+2 { // allow small rounding
		t.Errorf("elite goalie prediction (%d) should be ≤ average goalie (%d)", withEliteGoalie, withAvgGoalie)
	}
}

func TestPredictHeuristic_StrongerOpponentLowersPrediction(t *testing.T) {
	// Same GA profile, different point %: the stronger team should be harder to score on.
	g := &schedule.Game{HomeAbbrev: "WSH", AwayAbbrev: "PHI", StartTimeUTC: time.Now().Add(24 * time.Hour)}
	log := makeGameLog(30)
	base := cache.StandingsTeam{GamesPlayed: 60, GoalAgainst: 180, HomeGamesPlayed: 30, HomeGoalsAgainst: 90, RoadGamesPlayed: 30, RoadGoalsAgainst: 90}
	strong, weak := base, base
	strong.PointPctg, weak.PointPctg = 0.75, 0.25
	vsStrong := predictHeuristic(g, log, map[string]cache.StandingsTeam{"PHI": strong}, 0)
	vsWeak := predictHeuristic(g, log, map[string]cache.StandingsTeam{"PHI": weak}, 0)
	if vsStrong >= vsWeak {
		t.Errorf("vs strong opponent = %d; want < vs weak opponent = %d", vsStrong, vsWeak)
	}
}

func TestPredictHeuristic_SlumpLowersPrediction(t *testing.T) {
	// 1 goal in 18 this season after a 32-goal season should predict well below last season's rate.
	g := &schedule.Game{HomeAbbrev: "WSH", AwayAbbrev: "PHI", StartTimeUTC: time.Now().Add(24 * time.Hour)}
	healthy := append(seasonLog(2025, 82, 32), seasonLog(2026, 18, 7)...)
	slump := append(seasonLog(2025, 82, 32), seasonLog(2026, 18, 1)...)
	// Keep recent form identical (last 5 scoreless) so only the baseline differs.
	for i := len(healthy) - 5; i < len(healthy); i++ {
		healthy[i].Goals = 0
	}
	h := predictHeuristic(g, healthy, nil, 0)
	s := predictHeuristic(g, slump, nil, 0)
	if s >= h {
		t.Errorf("slump prediction = %d; want < healthy-season prediction = %d", s, h)
	}
}

func TestPredict_SlumpCanGoBelowFifteen(t *testing.T) {
	// 1 goal on 33 shots in 19 games after a 32-goal season: the evidence supports well under 15%, and neither a
	// 15% floor nor a long-run model blended back in should hold it up.
	g := &schedule.Game{HomeAbbrev: "PHI", AwayAbbrev: "WSH", StartTimeUTC: time.Now().Add(24 * time.Hour)}
	log := append(seasonLogShots(2025, 82, 32, 244), seasonLogShots(2026, 19, 1, 33)...)
	if got := Predict(g, log, nil, 0); got >= 15 {
		t.Errorf("Predict(slump) = %d; want < 15", got)
	}
}

func TestPredictHeuristic_GoalOrderWithinSeasonDoesNotMatter(t *testing.T) {
	// Recent-form multipliers on the last 5 games hurt in backtests; the season baseline already reacts.
	g := &schedule.Game{HomeAbbrev: "WSH", AwayAbbrev: "PHI", StartTimeUTC: time.Now().Add(24 * time.Hour)}
	early := append(seasonLogShots(2025, 82, 32, 244), seasonLogShots(2026, 19, 3, 50)...)
	late := append(seasonLogShots(2025, 82, 32, 244), seasonLogShots(2026, 19, 0, 50)...)
	for i := len(late) - 3; i < len(late); i++ {
		late[i].Goals = 1
	}
	if a, b := predictHeuristic(g, early, nil, 0), predictHeuristic(g, late, nil, 0); a != b {
		t.Errorf("goals early in season = %d; goals in last 3 games = %d; want equal", a, b)
	}
}

func TestPredict_HomeVsAway(t *testing.T) {
	// Home game should give higher or equal prediction vs away (home factor 1.05 vs 0.95)
	log := makeGameLog(30)
	standings := makeStandings()
	homeGame := &schedule.Game{HomeAbbrev: "WSH", AwayAbbrev: "PHI", StartTimeUTC: time.Now().Add(24 * time.Hour)}
	awayGame := &schedule.Game{HomeAbbrev: "PHI", AwayAbbrev: "WSH", StartTimeUTC: time.Now().Add(24 * time.Hour)}
	homeResult := Predict(homeGame, log, standings, 0)
	awayResult := Predict(awayGame, log, standings, 0)
	if homeResult < awayResult-5 {
		t.Errorf("home prediction (%d) should not be much less than away (%d)", homeResult, awayResult)
	}
}
