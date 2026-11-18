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
	logisticL2          = 0.01 // L2 regularization strength; bias term (w[0]) is excluded
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

	// Z-score normalize features 1–4 (leave the bias at index 0 unchanged).
	// Features are on different scales (home ∈ {0,1} vs baselineGPG ∈ ~[0.3,0.8]),
	// so a single learning rate would converge unevenly without normalization.
	nFeatures := len(samples[0].x)
	means := make([]float64, nFeatures)
	stds := make([]float64, nFeatures)
	for j := 1; j < nFeatures; j++ {
		var sum float64
		for _, s := range samples {
			sum += s.x[j]
		}
		means[j] = sum / float64(len(samples))
	}
	for j := 1; j < nFeatures; j++ {
		var sumSq float64
		for _, s := range samples {
			d := s.x[j] - means[j]
			sumSq += d * d
		}
		v := sumSq / float64(len(samples))
		if v > 0 {
			stds[j] = math.Sqrt(v)
		} else {
			stds[j] = 1.0 // constant feature: avoid division by zero
		}
	}
	normalize := func(x []float64) []float64 {
		xn := make([]float64, nFeatures)
		xn[0] = 1.0
		for j := 1; j < nFeatures; j++ {
			xn[j] = (x[j] - means[j]) / stds[j]
		}
		return xn
	}
	scaled := make([]sample, len(samples))
	for i, s := range samples {
		scaled[i] = sample{x: normalize(s.x), y: s.y}
	}

	// Train: batch gradient descent on log-loss with L2 regularization.
	// The full-batch gradient (summed over all samples, then divided by N) is applied once
	// per epoch. The original code divided by N inside the per-sample loop, which made the
	// effective learning rate N× too small and prevented proper convergence.
	w := make([]float64, nFeatures)
	grad := make([]float64, nFeatures)
	nSamples := float64(len(scaled))
	for iter := 0; iter < logisticIters; iter++ {
		for k := range grad {
			grad[k] = 0
		}
		for _, s := range scaled {
			z := dot(w, s.x)
			p := sigmoid(z)
			e := p - s.y // gradient of -[y*log(p)+(1-y)*log(1-p)] w.r.t. z is (p-y)*x
			for k := range w {
				grad[k] += e * s.x[k]
			}
		}
		for k := range w {
			l2 := 0.0
			if k > 0 { // do not regularize the bias term
				l2 = 2 * logisticL2 * w[k]
			}
			w[k] -= logisticLR * (grad[k]/nSamples + l2)
		}
	}

	// Predict for upcoming game g using the same feature construction and normalization.
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
	p := sigmoid(dot(w, normalize(x)))
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

// baselineGPGFrom returns Ovi's baseline goals per game after the given (oldest-first) log: the current season's GPG
// shrunk toward the GPG of the last maxGames games before it, with that prior weighted as baselinePriorGames games.
// Early in a season this leans on last season; as games accumulate, the current season dominates.
// (A flat last-82-games window barely reacts to a slump: 1 goal in 18 games after a 32-goal season still reads 0.34 GPG.)
func baselineGPGFrom(gameLog []cache.GameLogEntry, maxGames int) float64 {
	if len(gameLog) == 0 {
		return 0.4
	}
	split := len(gameLog)
	cur := seasonOf(gameLog[split-1])
	for split > 0 && seasonOf(gameLog[split-1]) == cur {
		split--
	}
	current, prior := lastN(gameLog[split:], maxGames), lastN(gameLog[:split], maxGames)
	if len(prior) == 0 {
		return float64(sumGoals(current)) / float64(len(current))
	}
	priorGPG := float64(sumGoals(prior)) / float64(len(prior))
	return (float64(sumGoals(current)) + baselinePriorGames*priorGPG) / (float64(len(current)) + baselinePriorGames)
}

// seasonOf returns the season start year encoded in an NHL game ID (2026020334 → 2026).
func seasonOf(e cache.GameLogEntry) int {
	return e.GameID / 1_000_000
}

func lastN(gameLog []cache.GameLogEntry, n int) []cache.GameLogEntry {
	if len(gameLog) > n {
		return gameLog[len(gameLog)-n:]
	}
	return gameLog
}

func sumGoals(gameLog []cache.GameLogEntry) int {
	var goals int
	for _, e := range gameLog {
		goals += e.Goals
	}
	return goals
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
