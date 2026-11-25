package model

import (
	"ovechbot_go/predictor/internal/cache"
)

// baselineGPGFrom returns Ovi's expected goals per game after the given (oldest-first) log, as shots per game times
// shooting percentage, each from the current season shrunk toward the last maxGames games before it.
// Shot volume settles fast, so it gets a light prior (baselineShotsPriorGames); shooting % is noisy, so its prior is
// worth baselineShPctPriorShots shots. A drop in shots and ice time shows up here before goals catch up.
// Logs without shot data fall back to shrinking goals per game (baselinePriorGames).
// Backtested on 2024-25 through 2026-27 against goal-only baselines, a last-5-games form multiplier, and a logistic
// blend; this was the best of those across every season.
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
	curGames, curGoals, curShots := float64(len(current)), float64(sumGoals(current)), float64(sumShots(current))
	priorGPG := float64(sumGoals(prior)) / float64(len(prior))
	priorShots := sumShots(prior)
	if priorShots == 0 {
		return (curGoals + baselinePriorGames*priorGPG) / (curGames + baselinePriorGames)
	}
	priorShotsPerGame := float64(priorShots) / float64(len(prior))
	priorShootingPct := float64(sumGoals(prior)) / float64(priorShots)
	shotsPerGame := (curShots + baselineShotsPriorGames*priorShotsPerGame) / (curGames + baselineShotsPriorGames)
	shootingPct := (curGoals + baselineShPctPriorShots*priorShootingPct) / (curShots + baselineShPctPriorShots)
	return shotsPerGame * shootingPct
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

func sumShots(gameLog []cache.GameLogEntry) int {
	var shots int
	for _, e := range gameLog {
		shots += e.Shots
	}
	return shots
}
