package main

import (
	"fmt"
	"math"
	"testing"
)

func calEntries(n, scored, predPct, uncalibratedPct int) []string {
	var out []string
	for i := 0; i < n; i++ {
		s := 0
		if i < scored {
			s = 1
		}
		if uncalibratedPct > 0 {
			out = append(out, fmt.Sprintf(`{"game_id":%d,"pred_pct":%d,"uncalibrated_pct":%d,"scored":%d}`, i, predPct, uncalibratedPct, s))
		} else {
			out = append(out, fmt.Sprintf(`{"game_id":%d,"pred_pct":%d,"scored":%d}`, i, predPct, s))
		}
	}
	return out
}

func TestCalibrationScaleFrom_UsesUncalibratedPrediction(t *testing.T) {
	// Model says 25%, true rate is 20%, and the posted (already scaled ×0.8) number was 20%.
	// Scale must stay 0.8; computing it from the posted 20% would swing back to 1.0 and oscillate.
	got := calibrationScaleFrom(calEntries(20, 4, 20, 25))
	if math.Abs(got-0.8) > 1e-9 {
		t.Errorf("calibrationScaleFrom = %v; want 0.8", got)
	}
}

func TestCalibrationScaleFrom_IgnoresLegacyEntries(t *testing.T) {
	// Entries without uncalibrated_pct logged an already-scaled number and can't be used.
	if got := calibrationScaleFrom(calEntries(50, 1, 30, 0)); got != 1.0 {
		t.Errorf("calibrationScaleFrom(legacy) = %v; want 1.0", got)
	}
}

func TestCalibrationScaleFrom_TooFewGames(t *testing.T) {
	if got := calibrationScaleFrom(calEntries(calibrationMinGames-1, 0, 30, 30)); got != 1.0 {
		t.Errorf("calibrationScaleFrom(too few) = %v; want 1.0", got)
	}
}

func TestCalibrationScaleFrom_Clamped(t *testing.T) {
	if got := calibrationScaleFrom(calEntries(20, 0, 30, 30)); got != 0.8 {
		t.Errorf("calibrationScaleFrom(no goals) = %v; want 0.8", got)
	}
	if got := calibrationScaleFrom(calEntries(20, 20, 30, 30)); got != 1.2 {
		t.Errorf("calibrationScaleFrom(all goals) = %v; want 1.2", got)
	}
}
