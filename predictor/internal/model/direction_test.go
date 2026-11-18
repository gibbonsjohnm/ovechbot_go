package model

import (
	"fmt"
	"testing"
	"time"

	"ovechbot_go/predictor/internal/cache"
	"ovechbot_go/predictor/internal/schedule"
)

// Each test changes one input and checks the raw probability moves the way hockey says it should.
// A reversed factor (e.g. stronger opponents raising the odds) fails here even when rounding would hide it.

func leagueTeam(abbrev string) cache.StandingsTeam {
	return cache.StandingsTeam{
		TeamAbbrev: abbrev, GamesPlayed: 20, GoalAgainst: 60, GoalsFor: 60, PointPctg: 0.5,
		HomeGamesPlayed: 10, HomeGoalsAgainst: 30, RoadGamesPlayed: 10, RoadGoalsAgainst: 30,
		L10GamesPlayed: 10, L10GoalsAgainst: 30, L10GoalsFor: 30,
	}
}

// league returns 32 average teams (3.0 GA/GP) with PHI replaced by phi.
func league(phi cache.StandingsTeam) map[string]cache.StandingsTeam {
	m := map[string]cache.StandingsTeam{}
	for i := 0; i < 31; i++ {
		abbrev := fmt.Sprintf("T%02d", i)
		m[abbrev] = leagueTeam(abbrev)
	}
	m["PHI"] = phi
	return m
}

// homeGame vs PHI two days after the fixture's last game (one day off: neutral rest).
func homeGame() *schedule.Game {
	return &schedule.Game{HomeAbbrev: "WSH", AwayAbbrev: "PHI", GameDate: "2026-11-19", StartTimeUTC: time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)}
}

func assertLower(t *testing.T, what string, lower, higher float64) {
	t.Helper()
	if !(lower < higher) {
		t.Errorf("%s: got %.4f, want < %.4f", what, lower, higher)
	}
}

func TestDirection_OpponentGoalsAgainst(t *testing.T) {
	log := loadFixtureGameLog(t)
	stingy, leaky := leagueTeam("PHI"), leagueTeam("PHI")
	stingy.RoadGoalsAgainst, stingy.L10GoalsAgainst = 24, 24 // 2.4 GA/GP on the road
	leaky.RoadGoalsAgainst, leaky.L10GoalsAgainst = 36, 36   // 3.6 GA/GP on the road
	assertLower(t, "vs stingy defense", heuristicProb(homeGame(), log, league(stingy), 0), heuristicProb(homeGame(), log, league(leaky), 0))
}

func TestDirection_OpponentStrength(t *testing.T) {
	log := loadFixtureGameLog(t)
	strong, weak := leagueTeam("PHI"), leagueTeam("PHI")
	strong.PointPctg, weak.PointPctg = 0.7, 0.3
	assertLower(t, "vs strong team", heuristicProb(homeGame(), log, league(strong), 0), heuristicProb(homeGame(), log, league(weak), 0))
}

func TestDirection_OpponentPace(t *testing.T) {
	log := loadFixtureGameLog(t)
	slow, fast := leagueTeam("PHI"), leagueTeam("PHI")
	slow.L10GoalsFor, slow.L10GoalsAgainst = 22, 25
	fast.L10GoalsFor, fast.L10GoalsAgainst = 40, 35
	assertLower(t, "vs low-event team", heuristicProb(homeGame(), log, league(slow), 0), heuristicProb(homeGame(), log, league(fast), 0))
}

func TestDirection_Goalie(t *testing.T) {
	log := loadFixtureGameLog(t)
	st := league(leagueTeam("PHI"))
	assertLower(t, "vs .925 goalie", heuristicProb(homeGame(), log, st, 0.925), heuristicProb(homeGame(), log, st, 0.890))
}

func TestDirection_HomeIce(t *testing.T) {
	log := loadFixtureGameLog(t)
	st := league(leagueTeam("PHI"))
	away := homeGame()
	away.HomeAbbrev, away.AwayAbbrev = "PHI", "WSH"
	assertLower(t, "away", heuristicProb(away, log, st, 0), heuristicProb(homeGame(), log, st, 0))
}

func TestDirection_Rest(t *testing.T) {
	log := loadFixtureGameLog(t) // last game 2026-11-17
	st := league(leagueTeam("PHI"))
	at := func(date string) float64 {
		g := homeGame()
		g.GameDate = date
		return heuristicProb(g, log, st, 0)
	}
	b2b, normal, rested := at("2026-11-18"), at("2026-11-19"), at("2026-11-21")
	assertLower(t, "back-to-back vs one day off", b2b, normal)
	assertLower(t, "one day off vs rested", normal, rested)
}
