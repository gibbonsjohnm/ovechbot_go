package model

import (
	"math"
	"time"

	"ovechbot_go/predictor/internal/cache"
	"ovechbot_go/predictor/internal/schedule"
)

const (
	baselineGamesMax = 82
	recentGames      = 5
	// CalibrationScale can be tuned from historical hit rate (e.g. compare predicted % to actual over past seasons).
	CalibrationScale = 1.0
	// League-average save percentage; used for goalie strength factor when we have opposing starter SV%.
	leagueAvgSavePct = 0.905
	goalieFactorMin  = 0.88
	goalieFactorMax  = 1.12
)

// Predict returns estimated probability (0-100) that Ovechkin scores in the given game.
// When we have enough game-log history (50+ games), the result is a 50/50 blend of the heuristic and a logistic model trained on the same log.
// goalieSavePct is the opposing starter's season save percentage (0–1); 0 means unknown and no goalie factor is applied.
func Predict(g *schedule.Game, gameLog []cache.GameLogEntry, standings map[string]cache.StandingsTeam, goalieSavePct float64) int {
	if len(gameLog) == 0 {
		return 45
	}
	heuristic := predictHeuristic(g, gameLog, standings, goalieSavePct)
	if logPct := LogisticPredict(g, gameLog, standings); logPct >= 0 {
		// Blend heuristic and logistic
		return clampPct((heuristic + logPct) / 2)
	}
	return heuristic
}

func predictHeuristic(g *schedule.Game, gameLog []cache.GameLogEntry, standings map[string]cache.StandingsTeam, goalieSavePct float64) int {

	// Baseline GPG from last N games only (e.g. one season) so it reflects "current" Ovi.
	baselineStart := 0
	if len(gameLog) > baselineGamesMax {
		baselineStart = len(gameLog) - baselineGamesMax
	}
	var totalGoals int
	for i := baselineStart; i < len(gameLog); i++ {
		totalGoals += gameLog[i].Goals
	}
	baselineLen := len(gameLog) - baselineStart
	baselineGPG := float64(totalGoals) / float64(baselineLen)
	baseProb := 1 - math.Exp(-baselineGPG)

	// League-average GA (full-season) so opponent factor is relative to league.
	leagueAvgGA := leagueAvgGAFromStandings(standings)

	// Opponent factor: more goals allowed by opponent → higher Ovi chance. Ratio vs league avg.
	oppFactor := 1.0
	if t, ok := standings[g.Opponent()]; ok && t.GamesPlayed > 0 {
		gaPerGame := effectiveOppGAPerGame(t)
		oppFactor = gaPerGame / leagueAvgGA
		if oppFactor > 1.35 {
			oppFactor = 1.35
		}
		if oppFactor < 0.75 {
			oppFactor = 0.75
		}
	}

	homeFactor := 0.95
	if g.IsHome() {
		homeFactor = 1.05
	}

	// Recent form: last N games (game log is chronological oldest-first, so take from the end).
	n := recentGames
	if len(gameLog) < n {
		n = len(gameLog)
	}
	var recentGoals int
	start := len(gameLog) - n
	if start < 0 {
		start = 0
	}
	for i := start; i < len(gameLog); i++ {
		recentGoals += gameLog[i].Goals
	}
	recentFactor := 1.0
	if n > 0 && baselineGPG > 0 {
		recentFactor = (float64(recentGoals) / float64(n)) / baselineGPG
		if recentFactor > 1.4 {
			recentFactor = 1.4
		}
		if recentFactor < 0.6 {
			recentFactor = 0.6
		}
	}

	// Back-to-back and rest: compare next game date to Caps' last game (from Ovi's game log).
	restFactor := restFactor(g, gameLog)

	// Opposing goalie strength: season SV% vs league average only (no "Ovi vs this goalie" history; would require goalie-faced per game).
	goalieFactor := 1.0
	if goalieSavePct > 0 && goalieSavePct < 1 {
		goalieFactor = leagueAvgSavePct / goalieSavePct
		if goalieFactor < goalieFactorMin {
			goalieFactor = goalieFactorMin
		}
		if goalieFactor > goalieFactorMax {
			goalieFactor = goalieFactorMax
		}
	}

	prob := baseProb * oppFactor * homeFactor * recentFactor * restFactor * goalieFactor * CalibrationScale
	return clampPct(int(math.Round(prob * 100)))
}

// effectiveOppGAPerGame returns goals-against per game for the opponent, blending full-season with
// last-10 when available so recent defensive form is reflected.
func effectiveOppGAPerGame(t cache.StandingsTeam) float64 {
	if t.GamesPlayed == 0 {
		return 3.0
	}
	full := float64(t.GoalAgainst) / float64(t.GamesPlayed)
	if t.L10GamesPlayed < 5 {
		return full
	}
	l10 := float64(t.L10GoalsAgainst) / float64(t.L10GamesPlayed)
	return 0.7*full + 0.3*l10
}

func clampPct(pct int) int {
	if pct < 15 {
		return 15
	}
	if pct > 75 {
		return 75
	}
	return pct
}

// restFactor returns 0.92 for back-to-back (game next day or same day after last), 1.02 for 2+ days rest, else 1.0.
func restFactor(g *schedule.Game, gameLog []cache.GameLogEntry) float64 {
	if len(gameLog) == 0 {
		return 1.0
	}
	last := gameLog[len(gameLog)-1]
	lastDate, err := time.Parse("2006-01-02", last.GameDate)
	if err != nil {
		return 1.0
	}
	nextDate := g.StartTimeUTC.UTC().Truncate(24 * time.Hour)
	lastDateUTC := time.Date(lastDate.Year(), lastDate.Month(), lastDate.Day(), 0, 0, 0, 0, time.UTC)
	daysBetween := int(nextDate.Sub(lastDateUTC).Hours() / 24)
	switch {
	case daysBetween <= 1:
		return 0.92 // back-to-back
	case daysBetween >= 2:
		return 1.02 // rested
	default:
		return 1.0
	}
}
