package model

import (
	"math"

	"ovechbot_go/predictor/internal/cache"
	"ovechbot_go/predictor/internal/schedule"
)

const (
	minGamesForLogistic = 50
	logisticIters       = 400
	logisticLR          = 0.15
)

// LogisticPredict trains a logistic regression on the game log (features: home, opp GA ratio, baseline GPG, recent form)
// and returns predicted probability 0-100 for the upcoming game. Returns -1 if we don't have enough data to train.
func LogisticPredict(g *schedule.Game, gameLog []cache.GameLogEntry, standings map[string]cache.StandingsTeam) int {
	if len(gameLog) < minGamesForLogistic {
		return -1
	}
	leagueAvgGA := leagueAvgGAFromStandings(standings)
	// Build training samples from games that have enough prior history (last 6+ games before them).
	type sample struct {
		x []float64 // [1, home, oppGA/leagueAvg, baselineGPG, recentRatio]
		y float64   // 0 or 1
	}
	var samples []sample
	for i := 6; i < len(gameLog); i++ {
		e := gameLog[i]
		prior := gameLog[:i]
		baselineGPG := baselineGPGFrom(prior, baselineGamesMax)
		recentGoals := 0
		recentN := 5
		if len(prior) < recentN {
			recentN = len(prior)
		}
		for j := len(prior) - recentN; j < len(prior); j++ {
			recentGoals += prior[j].Goals
		}
		recentRatio := 1.0
		if baselineGPG > 0 && recentN > 0 {
			recentRatio = (float64(recentGoals) / float64(recentN)) / baselineGPG
		}
		oppGA := leagueAvgGA
		if t, ok := standings[e.OpponentAbbrev]; ok && t.GamesPlayed > 0 {
			oppGA = effectiveOppGAPerGameVenue(t, e.HomeRoadFlag == "H")
		}
		home := 0.0
		if e.HomeRoadFlag == "H" {
			home = 1.0
		}
		x := []float64{
			1.0,
			home,
			oppGA / leagueAvgGA,
			baselineGPG,
			recentRatio,
		}
		y := 0.0
		if e.Goals > 0 {
			y = 1.0
		}
		samples = append(samples, sample{x: x, y: y})
	}
	if len(samples) < 20 {
		return -1
	}
	// Train: gradient descent on log-loss. w has length 5.
	w := []float64{0.0, 0.0, 0.0, 0.0, 0.0}
	for iter := 0; iter < logisticIters; iter++ {
		for _, s := range samples {
			z := dot(w, s.x)
			p := sigmoid(z)
			// gradient of -[y*log(p)+(1-y)*log(1-p)] = (p-y)*x
			err := p - s.y
			for k := range w {
				w[k] -= logisticLR * err * s.x[k] / float64(len(samples))
			}
		}
	}
	// Predict for upcoming game g.
	baselineGPG := baselineGPGFrom(gameLog, baselineGamesMax)
	recentGoals := 0
	n := recentGames
	if len(gameLog) < n {
		n = len(gameLog)
	}
	start := len(gameLog) - n
	if start < 0 {
		start = 0
	}
	for i := start; i < len(gameLog); i++ {
		recentGoals += gameLog[i].Goals
	}
	recentRatio := 1.0
	if baselineGPG > 0 && n > 0 {
		recentRatio = (float64(recentGoals) / float64(n)) / baselineGPG
	}
	oppGA := leagueAvgGA
	if t, ok := standings[g.Opponent()]; ok && t.GamesPlayed > 0 {
		oppGA = effectiveOppGAPerGameVenue(t, g.IsHome())
	}
	home := 0.0
	if g.IsHome() {
		home = 1.0
	}
	x := []float64{1.0, home, oppGA / leagueAvgGA, baselineGPG, recentRatio}
	p := sigmoid(dot(w, x))
	pct := int(math.Round(p * 100))
	if pct < 15 {
		pct = 15
	}
	if pct > 75 {
		pct = 75
	}
	return pct
}

func leagueAvgGAFromStandings(standings map[string]cache.StandingsTeam) float64 {
	if len(standings) == 0 {
		return 3.0
	}
	var sumGA, sumGP int
	for _, t := range standings {
		sumGA += t.GoalAgainst
		sumGP += t.GamesPlayed
	}
	if sumGP == 0 {
		return 3.0
	}
	return float64(sumGA) / float64(sumGP)
}

func baselineGPGFrom(gameLog []cache.GameLogEntry, maxGames int) float64 {
	if len(gameLog) == 0 {
		return 0.4
	}
	start := 0
	if len(gameLog) > maxGames {
		start = len(gameLog) - maxGames
	}
	var goals int
	for i := start; i < len(gameLog); i++ {
		goals += gameLog[i].Goals
	}
	n := len(gameLog) - start
	if n == 0 {
		return 0.4
	}
	return float64(goals) / float64(n)
}

func sigmoid(z float64) float64 {
	if z > 20 {
		return 1.0
	}
	if z < -20 {
		return 0.0
	}
	return 1.0 / (1.0 + math.Exp(-z))
}

func dot(a, b []float64) float64 {
	var s float64
	for i := range a {
		s += a[i] * b[i]
	}
	return s
}
