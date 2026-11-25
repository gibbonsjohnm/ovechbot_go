package model

import "testing"

// Direction tests check that an input moves the raw probability the way hockey says it should. A reversed factor
// fails here even when rounding would hide it. The model now adjusts only for the opposing goalie (see Predict).

func TestDirection_Goalie(t *testing.T) {
	log := loadFixtureGameLog(t)
	better := prob(log, Goalie{SavePct: 0.925, ShotsAgainst: 1200})
	worse := prob(log, Goalie{SavePct: 0.890, ShotsAgainst: 1200})
	if !(better < worse) {
		t.Errorf("vs .925 goalie = %.4f; want < vs .890 goalie = %.4f", better, worse)
	}
}

func TestDirection_ShotVolume(t *testing.T) {
	// Same goals, more shots this season: a bigger role should mean a higher chance.
	prior := seasonLogShots(2025, 82, 32, 244)
	few := prob(append(append(prior[:0:0], prior...), seasonLogShots(2026, 19, 1, 33)...), Goalie{})
	many := prob(append(append(prior[:0:0], prior...), seasonLogShots(2026, 19, 1, 57)...), Goalie{})
	if !(few < many) {
		t.Errorf("on 33 shots = %.4f; want < on 57 shots = %.4f", few, many)
	}
}
