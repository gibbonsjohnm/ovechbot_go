package odds

import (
	"context"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"
)

func TestImpliedProb(t *testing.T) {
	cases := []struct {
		in   string
		want float64
		ok   bool
	}{
		{"+350", 100.0 / 450, true},
		{"-150", 150.0 / 250, true},
		{"+100", 0.5, true},
		{"", 0, false},
		{"abc", 0, false},
		{"0", 0, false},
	}
	for _, tc := range cases {
		got, ok := impliedProb(tc.in)
		if math.Abs(got-tc.want) > 1e-12 || ok != tc.ok {
			t.Errorf("impliedProb(%q) = (%v, %v); want (%v, %v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestMarketPct_RemovesVigWhenBothSidesQuoted(t *testing.T) {
	// +350 implies 22.2% and -500 implies 83.3%: 105.6% total, so the fair Yes is 22.2/105.6 = 21%.
	got, ok := AnytimeOdds{American: "+350", NoAmerican: "-500"}.MarketPct()
	if !ok || got != 21 {
		t.Errorf("MarketPct(+350/-500) = (%d, %v); want (21, true)", got, ok)
	}
}

func TestMarketPct_YesOnlyUsesImplied(t *testing.T) {
	// Without the No side the margin can't be measured, so the raw implied probability is used.
	got, ok := AnytimeOdds{American: "+350"}.MarketPct()
	if !ok || got != 22 {
		t.Errorf("MarketPct(+350) = (%d, %v); want (22, true)", got, ok)
	}
	if _, ok := (AnytimeOdds{}).MarketPct(); ok {
		t.Error("MarketPct(empty) ok = true; want false")
	}
}

func TestCacheRoundTrip(t *testing.T) {
	o := AnytimeOdds{American: "+350", NoAmerican: "-500"}
	if got := DecodeCache(o.EncodeCache()); got != o {
		t.Errorf("DecodeCache(EncodeCache(%+v)) = %+v", o, got)
	}
	// Values cached before this format were the bare Yes price.
	if got := DecodeCache("+350"); got != (AnytimeOdds{American: "+350"}) {
		t.Errorf("DecodeCache(legacy) = %+v; want American +350", got)
	}
}

type stubTransport string

func (s stubTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(s))), Header: http.Header{}}, nil
}

func TestFetchAnytimeOdds_PrefersBookWithBothSides(t *testing.T) {
	// The Odds API's event-odds shape for player_goal_scorer_anytime: Yes/No outcomes with the player in description.
	body := `{"id":"e1","bookmakers":[
		{"key":"bookA","markets":[{"key":"player_goal_scorer_anytime","outcomes":[
			{"name":"Yes","description":"Alex Ovechkin","price":340}]}]},
		{"key":"bookB","markets":[{"key":"player_goal_scorer_anytime","outcomes":[
			{"name":"Yes","description":"Tom Wilson","price":200},
			{"name":"Yes","description":"Alex Ovechkin","price":350},
			{"name":"No","description":"Alex Ovechkin","price":-500}]}]}]}`
	c := &Client{apiKey: "k", http: &http.Client{Transport: stubTransport(body)}}
	got, err := c.fetchAnytimeOdds(context.Background(), "e1")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || *got != (AnytimeOdds{American: "+350", NoAmerican: "-500"}) {
		t.Errorf("fetchAnytimeOdds = %+v; want bookB's +350/-500", got)
	}
}

func TestFetchAnytimeOdds_YesOnly(t *testing.T) {
	body := `{"id":"e1","bookmakers":[{"key":"bookA","markets":[{"key":"player_goal_scorer_anytime","outcomes":[
		{"name":"Yes","description":"Alex Ovechkin","price":340}]}]}]}`
	c := &Client{apiKey: "k", http: &http.Client{Transport: stubTransport(body)}}
	got, err := c.fetchAnytimeOdds(context.Background(), "e1")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || *got != (AnytimeOdds{American: "+340"}) {
		t.Errorf("fetchAnytimeOdds = %+v; want +340 with no No side", got)
	}
}
