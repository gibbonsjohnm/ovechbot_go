package nhl

import (
	"context"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestGameLogSeasons(t *testing.T) {
	cases := []struct {
		now  time.Time
		want []string
	}{
		// Mid-season (November): current season started this calendar year.
		{time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC), []string{"20242025", "20252026", "20262027"}},
		// Late season (March): current season started last calendar year.
		{time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC), []string{"20242025", "20252026", "20262027"}},
		// Offseason (July): roll over to the upcoming season so it is picked up as soon as games exist.
		{time.Date(2027, 7, 1, 0, 0, 0, 0, time.UTC), []string{"20252026", "20262027", "20272028"}},
		{time.Date(2027, 6, 30, 0, 0, 0, 0, time.UTC), []string{"20242025", "20252026", "20262027"}},
	}
	for _, tc := range cases {
		if got := GameLogSeasons(tc.now, 3); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("GameLogSeasons(%s, 3) = %v; want %v", tc.now.Format("2006-01-02"), got, tc.want)
		}
	}
}

type stubTransport string

func (s stubTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(s))), Header: http.Header{}}, nil
}

func TestGameLog_IncludesShots(t *testing.T) {
	// The predictor's baseline is shots per game x shooting %, so shots must survive into the cached log.
	c := &Client{httpClient: &http.Client{Transport: stubTransport(
		`{"gameLog":[{"gameId":2026020334,"gameDate":"2026-11-17","opponentAbbrev":"TBL","homeRoadFlag":"R","goals":0,"shots":3}]}`)}}
	got, err := c.GameLog(context.Background(), "20262027")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Shots != 3 {
		t.Errorf("GameLog = %+v; want one entry with Shots 3", got)
	}
}

func TestSortGameLog_OldestFirst(t *testing.T) {
	// The NHL game-log endpoint returns each season newest-first; merged seasons must come out oldest-first.
	entries := []GameLogEntry{
		{GameID: 2025020010, GameDate: "2025-10-20"},
		{GameID: 2025020001, GameDate: "2025-10-08"},
		{GameID: 2026020020, GameDate: "2026-11-17"},
		{GameID: 2026020002, GameDate: "2026-10-02"},
	}
	SortGameLog(entries)
	var got []string
	for _, e := range entries {
		got = append(got, e.GameDate)
	}
	want := []string{"2025-10-08", "2025-10-20", "2026-10-02", "2026-11-17"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SortGameLog dates = %v; want %v", got, want)
	}
}
