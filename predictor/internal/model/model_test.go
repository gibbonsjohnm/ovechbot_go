package model

import (
	"math"
	"testing"
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

func TestPredict_EmptyLog(t *testing.T) {
	if got := Predict(nil, Goalie{}); got != 45 {
		t.Errorf("Predict(empty log) = %d; want 45", got)
	}
}

func TestPredict_InRange(t *testing.T) {
	for _, n := range []int{10, 70} {
		if got := Predict(makeGameLog(n), Goalie{}); got < MinPct || got > MaxPct {
			t.Errorf("Predict(%d games) = %d; want in [%d, %d]", n, got, MinPct, MaxPct)
		}
	}
}

func TestPredict_SlumpLowersPrediction(t *testing.T) {
	// 1 goal in 18 this season after a 32-goal season should predict well below last season's rate.
	healthy := append(seasonLog(2025, 82, 32), seasonLog(2026, 18, 7)...)
	slump := append(seasonLog(2025, 82, 32), seasonLog(2026, 18, 1)...)
	if h, s := Predict(healthy, Goalie{}), Predict(slump, Goalie{}); s >= h {
		t.Errorf("slump prediction = %d; want < healthy-season prediction = %d", s, h)
	}
}

func TestPredict_SlumpNotHeldUpByFloor(t *testing.T) {
	// 0 goals on 25 shots in 19 games after a 32-goal season: neither a 15% floor nor a long-run model blended back
	// in should hold the published number above what the evidence says.
	log := append(seasonLogShots(2025, 82, 32, 244), seasonLogShots(2026, 19, 0, 25)...)
	got := Predict(log, Goalie{})
	if got >= 15 {
		t.Errorf("Predict(deep slump) = %d; want < 15", got)
	}
	if raw := int(math.Round(prob(log, Goalie{}) * 100)); got != raw {
		t.Errorf("Predict(deep slump) = %d; want the model's own %d (nothing above MinPct should clamp it)", got, raw)
	}
}

func TestPredict_GoalOrderWithinSeasonDoesNotMatter(t *testing.T) {
	// Recent-form multipliers on the last 5 games hurt in backtests; the season baseline already reacts.
	early := append(seasonLogShots(2025, 82, 32, 244), seasonLogShots(2026, 19, 3, 50)...)
	late := append(seasonLogShots(2025, 82, 32, 244), seasonLogShots(2026, 19, 0, 50)...)
	for i := len(late) - 3; i < len(late); i++ {
		late[i].Goals = 1
	}
	if a, b := Predict(early, Goalie{}), Predict(late, Goalie{}); a != b {
		t.Errorf("goals early in season = %d; goals in last 3 games = %d; want equal", a, b)
	}
}

func TestGoalieFactor_UnknownIsNeutral(t *testing.T) {
	for _, g := range []Goalie{{}, {SavePct: 0.930}, {ShotsAgainst: 500}, {SavePct: 1, ShotsAgainst: 10}} {
		if got := goalieFactor(g); got != 1.0 {
			t.Errorf("goalieFactor(%+v) = %v; want 1.0", g, got)
		}
	}
}

func TestGoalieFactor_ShrinksSmallSamples(t *testing.T) {
	// .950 on 40 shots (a couple of starts) says little; the same .950 on 1500 shots says a lot more.
	small := goalieFactor(Goalie{SavePct: 0.950, ShotsAgainst: 40})
	large := goalieFactor(Goalie{SavePct: 0.950, ShotsAgainst: 1500})
	if !(small < 1 && large < small) {
		t.Errorf("goalieFactor(.950 on 40 shots) = %v, (.950 on 1500) = %v; want large < small < 1", small, large)
	}
	want := leagueAvgSavePct / ((0.950*40 + leagueAvgSavePct*goalieSavePctPriorShots) / (40 + goalieSavePctPriorShots))
	if math.Abs(small-want) > 1e-12 {
		t.Errorf("goalieFactor(.950 on 40 shots) = %v; want %v", small, want)
	}
}

func TestGoalieFactor_Clamped(t *testing.T) {
	if got := goalieFactor(Goalie{SavePct: 0.990, ShotsAgainst: 100000}); got != goalieFactorMin {
		t.Errorf("goalieFactor(elite, huge sample) = %v; want %v", got, goalieFactorMin)
	}
	if got := goalieFactor(Goalie{SavePct: 0.800, ShotsAgainst: 100000}); got != goalieFactorMax {
		t.Errorf("goalieFactor(awful, huge sample) = %v; want %v", got, goalieFactorMax)
	}
}
