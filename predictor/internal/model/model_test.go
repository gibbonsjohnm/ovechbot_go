package model

import (
	"testing"
	"time"

	"ovechbot_go/predictor/internal/cache"
	"ovechbot_go/predictor/internal/schedule"
)

func TestClampPct(t *testing.T) {
	cases := []struct{ in, want int }{
		{0, 15},
		{14, 15},
		{15, 15},
		{50, 50},
		{75, 75},
		{76, 75},
		{100, 75},
	}
	for _, tc := range cases {
		if got := clampPct(tc.in); got != tc.want {
			t.Errorf("clampPct(%d) = %d; want %d", tc.in, got, tc.want)
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
	if got < 15 || got > 75 {
		t.Errorf("Predict(heuristic-only) = %d; want in [15, 75]", got)
	}
}

func TestPredict_BlendedWithLogistic(t *testing.T) {
	// 70 games — enough for logistic; result should be blended and clamped
	g := &schedule.Game{HomeAbbrev: "WSH", AwayAbbrev: "PHI", StartTimeUTC: time.Now().Add(24 * time.Hour)}
	log := makeGameLog(70)
	got := Predict(g, log, makeStandings(), 0)
	if got < 15 || got > 75 {
		t.Errorf("Predict(blended) = %d; want in [15, 75]", got)
	}
}

func TestPredict_GoalieFactor_Strong(t *testing.T) {
	// Strong goalie (high SV%) should lower prediction vs no goalie factor
	g := &schedule.Game{HomeAbbrev: "WSH", AwayAbbrev: "PHI", StartTimeUTC: time.Now().Add(24 * time.Hour)}
	log := makeGameLog(30)
	standings := makeStandings()
	withAvgGoalie := Predict(g, log, standings, 0.905)  // league average — factor ~1.0
	withEliteGoalie := Predict(g, log, standings, 0.940) // elite — factor ~0.90 → lower
	// Elite goalie should give equal or lower prediction
	if withEliteGoalie > withAvgGoalie+2 { // allow small rounding
		t.Errorf("elite goalie prediction (%d) should be ≤ average goalie (%d)", withEliteGoalie, withAvgGoalie)
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
