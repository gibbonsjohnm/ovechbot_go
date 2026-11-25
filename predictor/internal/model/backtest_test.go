package model

import (
	"math"
	"testing"

	"ovechbot_go/predictor/internal/cache"
)

// Walk-forward backtest of the baseline on Ovechkin's real game log (testdata/game_log.json): every game from
// 2025-26 on is predicted only from the games before it, and scored with log-loss (lower is better).
// It guards against model changes that make predictions worse, which the unit tests can't see.

// backtestMaxLogLoss is the baseline's log-loss on the fixture when this test was written (0.5673; last-82 GPG
// scores 0.5955 and a constant 0.7536), plus a small margin. Raise it only with a backtest showing the change is worth it.
const backtestMaxLogLoss = 0.572

func logLoss(p float64, scored bool) float64 {
	p = math.Min(math.Max(p, 1e-4), 1-1e-4)
	if scored {
		return -math.Log(p)
	}
	return -math.Log(1 - p)
}

// walkForward returns the mean log-loss of predict over games from the 2025-26 season on.
func walkForward(t *testing.T, log []cache.GameLogEntry, predict func(prior []cache.GameLogEntry, next cache.GameLogEntry) float64) float64 {
	t.Helper()
	var sum float64
	var n int
	for i, e := range log {
		if seasonOf(e) < 2025 {
			continue
		}
		sum += logLoss(predict(log[:i], e), e.Goals > 0)
		n++
	}
	if n == 0 {
		t.Fatal("no games to backtest")
	}
	return sum / float64(n)
}

func TestBacktest_Baseline(t *testing.T) {
	log := loadFixtureGameLog(t)
	baseline := walkForward(t, log, func(prior []cache.GameLogEntry, _ cache.GameLogEntry) float64 {
		return 1 - math.Exp(-baselineGPGFrom(prior, baselineGamesMax))
	})
	// Reference 1: the pre-fix baseline, goals per game over the last 82 games.
	last82 := walkForward(t, log, func(prior []cache.GameLogEntry, _ cache.GameLogEntry) float64 {
		recent := lastN(prior, 82)
		return 1 - math.Exp(-float64(sumGoals(recent))/float64(len(recent)))
	})
	// Reference 2: a constant, the share of 2024-25 games he scored in.
	var scored, games float64
	for _, e := range log {
		if seasonOf(e) == 2024 {
			games++
			if e.Goals > 0 {
				scored++
			}
		}
	}
	constant := walkForward(t, log, func([]cache.GameLogEntry, cache.GameLogEntry) float64 { return scored / games })

	t.Logf("log-loss: baseline %.4f, last-82 GPG %.4f, constant %.4f", baseline, last82, constant)
	if baseline > backtestMaxLogLoss {
		t.Errorf("baseline log-loss %.4f > %.4f: this change makes historical predictions worse", baseline, backtestMaxLogLoss)
	}
	if baseline >= last82 || baseline >= constant {
		t.Errorf("baseline log-loss %.4f should beat last-82 GPG (%.4f) and a constant (%.4f)", baseline, last82, constant)
	}
}
