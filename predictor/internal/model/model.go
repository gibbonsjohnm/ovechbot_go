package model

import (
	"math"
	"time"

	"ovechbot_go/predictor/internal/cache"
	"ovechbot_go/predictor/internal/schedule"
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
	// CalibrationScale can be tuned from historical hit rate (e.g. compare predicted % to actual over past seasons).
	CalibrationScale = 1.0
	// League-average save percentage; used for goalie strength factor when we have opposing starter SV%.
	leagueAvgSavePct = 0.905
	goalieFactorMin  = 0.88
	goalieFactorMax  = 1.12
)

// Predict returns estimated probability (0-100) that Ovechkin scores in the given game.
// goalieSavePct is the opposing starter's season save percentage (0–1); 0 means unknown and no goalie factor is applied.
func Predict(g *schedule.Game, gameLog []cache.GameLogEntry, standings map[string]cache.StandingsTeam, goalieSavePct float64) int {
	if len(gameLog) == 0 {
		return 45
	}
	return predictHeuristic(g, gameLog, standings, goalieSavePct)
}

func predictHeuristic(g *schedule.Game, gameLog []cache.GameLogEntry, standings map[string]cache.StandingsTeam, goalieSavePct float64) int {

	// Baseline: this season's shots per game x shooting %, each shrunk toward last season, so it reflects "current" Ovi.
	// No separate recent-form factor: it made backtests worse, and the season baseline already reacts.
	baselineGPG := baselineGPGFrom(gameLog, baselineGamesMax)
	baseProb := 1 - math.Exp(-baselineGPG)

	// League-average GA (full-season) so opponent factor is relative to league.
	leagueAvgGA := leagueAvgGAFromStandings(standings)

	// Opponent factor: venue-specific GA when available (Caps home → use opp road GA; Caps away → use opp home GA).
	oppFactor := 1.0
	if t, ok := standings[g.Opponent()]; ok && t.GamesPlayed > 0 {
		gaPerGame := effectiveOppGAPerGameVenue(t, g.IsHome())
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

	// Ovi vs this opponent: his historical GPG vs this team vs baseline (last 10 meetings or all).
	oviVsOppFactor := oviVsOpponentFactor(gameLog, g.Opponent(), baselineGPG)

	// Opponent team strength: point % (stronger teams slightly harder to score on, same GA).
	pointStrengthFactor := 1.0
	if t, ok := standings[g.Opponent()]; ok && t.PointPctg > 0 {
		pointStrengthFactor = 1.04 - 0.08*t.PointPctg // .500 team → 1.0; stronger → lower
		if pointStrengthFactor < 0.92 {
			pointStrengthFactor = 0.92
		}
		if pointStrengthFactor > 1.08 {
			pointStrengthFactor = 1.08
		}
	}

	// Pace: high-event opponent (L10 GF+GA) → slightly more chances both ways.
	paceFactor := paceFactorForOpponent(standings, g.Opponent())

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

	prob := baseProb * oppFactor * homeFactor * oviVsOppFactor * pointStrengthFactor * paceFactor * restFactor * goalieFactor * CalibrationScale
	return ClampPct(int(math.Round(prob * 100)))
}

// effectiveOppGAPerGameVenue returns venue-specific GA/GP when available: Caps home → opponent's road GA; Caps away → opponent's home GA.
// Blends venue GA with L10 when L10 is available; falls back to full-season GA otherwise.
func effectiveOppGAPerGameVenue(t cache.StandingsTeam, capsHome bool) float64 {
	if t.GamesPlayed == 0 {
		return 3.0
	}
	var venueGA, venueGP int
	if capsHome {
		venueGA, venueGP = t.RoadGoalsAgainst, t.RoadGamesPlayed
	} else {
		venueGA, venueGP = t.HomeGoalsAgainst, t.HomeGamesPlayed
	}
	full := float64(t.GoalAgainst) / float64(t.GamesPlayed)
	if venueGP >= 5 {
		venuePerGame := float64(venueGA) / float64(venueGP)
		if t.L10GamesPlayed >= 5 {
			l10 := float64(t.L10GoalsAgainst) / float64(t.L10GamesPlayed)
			return 0.7*venuePerGame + 0.3*l10
		}
		return venuePerGame
	}
	if t.L10GamesPlayed >= 5 {
		l10 := float64(t.L10GoalsAgainst) / float64(t.L10GamesPlayed)
		return 0.7*full + 0.3*l10
	}
	return full
}

// oviVsOpponentFactor returns a multiplier from Ovi's historical GPG vs this opponent vs his baseline (0.85–1.15).
func oviVsOpponentFactor(gameLog []cache.GameLogEntry, opponent string, baselineGPG float64) float64 {
	const maxVsOpp = 10
	var goals int
	var games int
	for i := len(gameLog) - 1; i >= 0 && games < maxVsOpp; i-- {
		if gameLog[i].OpponentAbbrev != opponent {
			continue
		}
		games++
		goals += gameLog[i].Goals
	}
	if games < 3 || baselineGPG <= 0 {
		return 1.0
	}
	gpgVsOpp := float64(goals) / float64(games)
	ratio := gpgVsOpp / baselineGPG
	if ratio < 0.85 {
		ratio = 0.85
	}
	if ratio > 1.15 {
		ratio = 1.15
	}
	return ratio
}

// paceFactorForOpponent returns a multiplier from opponent's L10 event rate vs league (0.97–1.03).
func paceFactorForOpponent(standings map[string]cache.StandingsTeam, opponent string) float64 {
	t, ok := standings[opponent]
	if !ok || t.L10GamesPlayed < 5 {
		return 1.0
	}
	oppPace := (float64(t.L10GoalsFor) + float64(t.L10GoalsAgainst)) / (2 * float64(t.L10GamesPlayed))
	leaguePace := leagueAvgPaceFromStandings(standings)
	if leaguePace <= 0 {
		return 1.0
	}
	ratio := oppPace / leaguePace
	if ratio < 0.97 {
		ratio = 0.97
	}
	if ratio > 1.03 {
		ratio = 1.03
	}
	return ratio
}

func leagueAvgPaceFromStandings(standings map[string]cache.StandingsTeam) float64 {
	if len(standings) == 0 {
		return 3.0
	}
	var sumGF, sumGA, sumGP int
	for _, t := range standings {
		sumGF += t.GoalsFor
		sumGA += t.GoalAgainst
		sumGP += t.GamesPlayed
	}
	if sumGP == 0 {
		return 3.0
	}
	return (float64(sumGF) + float64(sumGA)) / float64(2*sumGP)
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

// restFactor returns 0.92 for back-to-back (game next day or same day after last), 1.02 for 2+ days rest, else 1.0.
// Both dates are local game dates: a 7pm ET start is already the next day in UTC, which would hide back-to-backs.
func restFactor(g *schedule.Game, gameLog []cache.GameLogEntry) float64 {
	if len(gameLog) == 0 {
		return 1.0
	}
	last := gameLog[len(gameLog)-1]
	lastDate, err := time.Parse("2006-01-02", last.GameDate)
	if err != nil {
		return 1.0
	}
	nextDate, err := time.Parse("2006-01-02", g.GameDate)
	if err != nil {
		nextDate = g.StartTimeUTC.UTC().Truncate(24 * time.Hour)
	}
	daysBetween := int(nextDate.Sub(lastDate).Hours() / 24)
	switch {
	case daysBetween <= 1:
		return 0.92 // back-to-back
	case daysBetween >= 3:
		return 1.02 // rested (2+ days off)
	default:
		return 1.0
	}
}
