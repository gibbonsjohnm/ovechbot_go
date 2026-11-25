package model

import (
	"math"

	"ovechbot_go/predictor/internal/cache"
)

const (
	baselineGamesMax = 82
	// baselineShotsPriorGames is how many games' weight last season's shots per game gets against this season's.
	baselineShotsPriorGames = 3
	// baselineShPctPriorShots is how many shots' weight last season's shooting % gets against this season's.
	baselineShPctPriorShots = 40
	// baselinePriorGames is the prior weight in games for the goals-per-game fallback when the log has no shot data.
	baselinePriorGames = 15
	// MinPct and MaxPct bound every published probability.
	MinPct = 5
	MaxPct = 75
	// League-average save percentage; the opposing goalie's save % is shrunk toward it.
	leagueAvgSavePct = 0.905
	// goalieSavePctPriorShots is how many shots' weight the league average gets against the goalie's own record.
	// Save % needs well over a season of shots to settle, so a goalie with a few hundred shots stays near average.
	goalieSavePctPriorShots = 1000
	goalieFactorMin         = 0.94
	goalieFactorMax         = 1.06
)

// Goalie is the opposing starter's season save percentage (0–1) and the shots faced behind it.
// The zero value means unknown, and no goalie factor is applied.
type Goalie struct {
	SavePct      float64
	ShotsAgainst int
}

// Predict returns estimated probability (MinPct–MaxPct) that Ovechkin scores in his next game.
//
// It is his baseline (this season's shots per game × shooting %, each shrunk toward last season) adjusted only for
// the opposing goalie. Opponent defense, home ice, head-to-head history, opponent point %, pace and rest were
// removed: in a walk-forward backtest over 2024-25 to 2026-27 using standings as of the day before each game, none
// improved on the baseline in both full seasons, and together they made log-loss worse (0.6575 vs 0.6291).
func Predict(gameLog []cache.GameLogEntry, goalie Goalie) int {
	if len(gameLog) == 0 {
		return 45
	}
	return ClampPct(int(math.Round(prob(gameLog, goalie) * 100)))
}

// prob is the unrounded, unclamped probability (0–1) behind Predict.
func prob(gameLog []cache.GameLogEntry, goalie Goalie) float64 {
	baseProb := 1 - math.Exp(-baselineGPGFrom(gameLog, baselineGamesMax))
	return baseProb * goalieFactor(goalie)
}

// goalieFactor scales for the opposing goalie: league-average save % over his save % after shrinking it toward the
// league average by goalieSavePctPriorShots, within goalieFactorMin–goalieFactorMax. Untested historically (no
// past starter data), so it is kept narrow.
func goalieFactor(g Goalie) float64 {
	if g.SavePct <= 0 || g.SavePct >= 1 || g.ShotsAgainst <= 0 {
		return 1.0
	}
	shots := float64(g.ShotsAgainst)
	shrunk := (g.SavePct*shots + leagueAvgSavePct*goalieSavePctPriorShots) / (shots + goalieSavePctPriorShots)
	return math.Min(goalieFactorMax, math.Max(goalieFactorMin, leagueAvgSavePct/shrunk))
}

// ClampPct bounds a probability percentage to [MinPct, MaxPct].
func ClampPct(pct int) int {
	if pct < MinPct {
		return MinPct
	}
	if pct > MaxPct {
		return MaxPct
	}
	return pct
}
