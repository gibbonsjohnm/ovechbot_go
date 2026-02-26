package goalie

import (
	"context"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"ovechbot_go/predictor/internal/schedule"
)

// opponentNameFragment is a substring that appears in Daily Faceoff matchup text (e.g. "Team at Team") for each opponent abbrev.
var opponentNameFragment = map[string]string{
	"ANA": "Anaheim", "BOS": "Boston", "BUF": "Buffalo", "CGY": "Calgary", "CAR": "Carolina",
	"CHI": "Chicago", "COL": "Colorado", "CBJ": "Columbus", "DAL": "Dallas", "DET": "Detroit",
	"EDM": "Edmonton", "FLA": "Florida", "LAK": "Los Angeles", "MIN": "Minnesota", "MTL": "Montreal",
	"NJD": "New Jersey", "NSH": "Nashville", "NYI": "New York Islanders", "NYR": "New York Rangers",
	"OTT": "Ottawa", "PHI": "Philadelphia", "PIT": "Pittsburgh", "SJS": "San Jose", "SEA": "Seattle",
	"STL": "St. Louis", "TBL": "Tampa Bay", "TOR": "Toronto", "UTA": "Utah", "VAN": "Vancouver",
	"VGK": "Vegas", "WPG": "Winnipeg", "WSH": "Washington",
}

const (
	dfoURLFmt     = "https://www.dailyfaceoff.com/starting-goalies/%s"
	dfoTimeout    = 15 * time.Second
	capitalsMatch = "Washington"
)

// OpposingStarterFromDFO fetches the Daily Faceoff starting-goalies page for the game's date and returns
// the opposing team's projected/confirmed starter name (e.g. "Dan Vladar"). Returns empty string if not found
// or on fetch/parse error. Used as fallback when NHL boxscore has no goalies yet.
func (c *Client) OpposingStarterFromDFO(ctx context.Context, g *schedule.Game) string {
	oppAbbrev := g.Opponent()
	frag, ok := opponentNameFragment[oppAbbrev]
	if !ok {
		return ""
	}
	// GameDate is YYYY-MM-DD (schedule uses venue/local date); DFO uses same date in URL.
	date := g.GameDate
	if date == "" {
		date = g.StartTimeUTC.Format("2006-01-02")
	}
	url := strings.Replace(dfoURLFmt, "%s", date, 1)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("User-Agent", "OvechBot/1.0 (NHL starting goalie fallback)")
	client := &http.Client{Timeout: dfoTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return ""
	}
	return parseDFOGoalieName(body, frag, g.IsHome())
}

// parseDFOGoalieName finds the matchup block that contains "Washington" and the opponent fragment,
// then returns the away goalie name if capsAreHome else the home goalie name.
// DFO lists games as "Away Team at Home Team" with away goalie first, home goalie second.
func parseDFOGoalieName(html []byte, opponentFragment string, capsAreHome bool) string {
	text := string(html)
	// Find block that contains both Washington and the opponent (e.g. "Philadelphia").
	if !strings.Contains(text, capitalsMatch) || !strings.Contains(text, opponentFragment) {
		return ""
	}
	// Match "FirstName LastName" patterns that look like goalie names (2+ word names).
	// DFO order: away goalie, then home goalie.
	namePat := regexp.MustCompile(`>[A-Z][a-z]+(?:-[A-Z][a-z]+)? [A-Z][a-z]+(?:-[A-Z][a-z]+)?<`)
	skip := map[string]bool{
		"Show More": true, "Line Combos": true, "Confirmed": true, "Likely": true,
		"Unknown": true, "Washington Capitals": true, "Philadelphia Flyers": true,
	}
	idx := strings.Index(text, capitalsMatch)
	if idx < 0 {
		return ""
	}
	idxOpp := strings.Index(text, opponentFragment)
	if idxOpp < 0 {
		return ""
	}
	// Same game block: capitals and opponent should be within ~200 chars (e.g. "Philadelphia Flyers at Washington Capitals").
	if idx > idxOpp+200 || idxOpp > idx+200 {
		// Try to find a block where both are close (e.g. "PHI at WSH" style).
		window := text
		if idxOpp > idx {
			window = text[idx:min(len(text), idxOpp+150)]
		} else {
			window = text[idxOpp:min(len(text), idx+150)]
		}
		if !strings.Contains(window, capitalsMatch) || !strings.Contains(window, opponentFragment) {
			return ""
		}
	}
	// From the start of this game block, collect first two goalie names in order (away, home).
	// We already collected all names; we need the two that appear after our matchup. Use a simple approach:
	// take the first two names that appear after the matchup text in the HTML.
	gameBlockStart := min(idx, idxOpp)
	block := text[gameBlockStart:]
	var inBlock []string
	for _, m := range namePat.FindAllString(block, -1) {
		name := strings.Trim(m, "><")
		if len(name) < 4 || skip[name] {
			continue
		}
		if strings.HasSuffix(name, "Capitals") || strings.HasSuffix(name, "Flyers") ||
			strings.HasSuffix(name, "Sabres") || strings.HasSuffix(name, "Devils") ||
			strings.HasSuffix(name, "Kraken") || strings.HasSuffix(name, "Stars") ||
			strings.HasSuffix(name, "Avalanche") || strings.HasSuffix(name, "Mammoth") ||
			strings.HasSuffix(name, "Jets") || strings.HasSuffix(name, "Canucks") ||
			strings.HasSuffix(name, "Knights") || strings.HasSuffix(name, "Kings") ||
			strings.HasSuffix(name, "Oilers") || strings.HasSuffix(name, "Ducks") {
			continue
		}
		inBlock = append(inBlock, name)
		if len(inBlock) >= 2 {
			break
		}
	}
	if len(inBlock) < 2 {
		return ""
	}
	if capsAreHome {
		return inBlock[0] // away goalie = opponent's starter
	}
	return inBlock[1] // home goalie = opponent's starter
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
