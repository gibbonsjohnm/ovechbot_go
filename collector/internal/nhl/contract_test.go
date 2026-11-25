package nhl

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

// predictorGameLogFixture is the predictor's copy of what this collector writes to Redis. The predictor's tests
// (including its backtest) read it, so this test is the contract between the two services.
const predictorGameLogFixture = "../../../predictor/internal/model/testdata/game_log.json"

var update = flag.Bool("update", false, "rewrite the predictor's game log fixture from the NHL API fixtures")

// fixtureTransport serves testdata/gamelog_<season>.json for game-log URLs, i.e. real NHL API responses.
type fixtureTransport struct{}

func (fixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	// /v1/player/<id>/game-log/<season>/<gameType>
	parts := strings.Split(r.URL.Path, "/")
	season := parts[len(parts)-2]
	b, err := os.ReadFile("testdata/gamelog_" + season + ".json")
	if err != nil {
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}}, nil
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(b)), Header: http.Header{}}, nil
}

// collectFixtures runs the collector's game-log path (fetch each season, merge, sort) against the NHL fixtures.
func collectFixtures(t *testing.T) []GameLogEntry {
	t.Helper()
	c := &Client{httpClient: &http.Client{Transport: fixtureTransport{}}}
	var all []GameLogEntry
	for _, season := range []string{"20242025", "20252026", "20262027"} {
		entries, err := c.GameLog(context.Background(), season)
		if err != nil {
			t.Fatalf("GameLog(%s): %v", season, err)
		}
		all = append(all, entries...)
	}
	SortGameLog(all)
	return all
}

func TestGameLog_RealResponsesOldestFirstWithShots(t *testing.T) {
	// The NHL API returns each season newest-first; the predictor assumes oldest-first.
	log := collectFixtures(t)
	if len(log) != 166 {
		t.Fatalf("got %d games; want 166 (65 + 82 + 19)", len(log))
	}
	for i := 1; i < len(log); i++ {
		if log[i].GameDate < log[i-1].GameDate {
			t.Fatalf("game %d (%s) is before game %d (%s); log must be oldest-first", i, log[i].GameDate, i-1, log[i-1].GameDate)
		}
	}
	if first, last := log[0].GameDate, log[len(log)-1].GameDate; first != "2024-10-12" || last != "2026-11-17" {
		t.Errorf("log spans %s..%s; want 2024-10-12..2026-11-17", first, last)
	}
	var shots int
	for _, e := range log {
		shots += e.Shots
	}
	if shots == 0 {
		t.Error("no shots in the log; the predictor's baseline needs them")
	}
}

func TestGameLog_MatchesPredictorFixture(t *testing.T) {
	got, err := json.Marshal(collectFixtures(t))
	if err != nil {
		t.Fatal(err)
	}
	if *update {
		if err := os.WriteFile(predictorGameLogFixture, append(got, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(predictorGameLogFixture)
	if err != nil {
		t.Fatalf("read predictor fixture: %v (run: go test ./internal/nhl -run MatchesPredictorFixture -update)", err)
	}
	if !bytes.Equal(got, bytes.TrimSpace(want)) {
		t.Errorf("collector output no longer matches %s.\nIf the change is intended, regenerate it with -update and make sure the predictor still reads it correctly.", predictorGameLogFixture)
	}
}
