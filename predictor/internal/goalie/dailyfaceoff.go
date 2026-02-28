package goalie

import (
	"context"
	"io"
	"net/http"
	"regexp"
	"strings"

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
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; OvechBot/1.0; +https://github.com/ovechbot) Chrome/120.0.0.0")
	resp, err := c.http.Do(req)
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
	textLower := strings.ToLower(text)
	oppLower := strings.ToLower(opponentFragment)
	// Find block that contains both Washington and the opponent (e.g. "Philadelphia").
	if !strings.Contains(text, capitalsMatch) || !strings.Contains(textLower, oppLower) {
		return ""
	}
	// Match goalie names: "FirstName LastName" or "I. LastName" (DFO sometimes abbreviates).
	// DFO order: away goalie, then home goalie.
	namePat := regexp.MustCompile(`>(?:[A-Z][a-z]+(?:-[A-Z][a-z]+)? [A-Z][a-z]+(?:-[A-Z][a-z]+)?|[A-Z]\. [A-Z][a-z]+(?:-[A-Z][a-z]+)?)<`)
	skip := map[string]bool{
		"Show More": true, "Line Combos": true, "Confirmed": true, "Likely": true,
		"Unknown": true, "Washington Capitals": true, "Philadelphia Flyers": true,
		"Tarik El-Bashir": true, // Caps reporter often in DFO source links; not a goalie
	}
	// Find the matchup row: both "Washington" and opponent in the same window (HTML can have tags between words).
	const matchupWindow = 220
	gameBlockStart := -1
	windowLen := matchupWindow
	if len(text) < windowLen {
		windowLen = len(text)
	}
	for i := 0; i <= len(text)-windowLen; i++ {
		window := text[i : i+windowLen]
		windowLower := strings.ToLower(window)
		if strings.Contains(window, capitalsMatch) && strings.Contains(windowLower, oppLower) {
			gameBlockStart = i
			break
		}
	}
	if gameBlockStart < 0 {
		return ""
	}
	// Limit block so we don't pick goalies from the next game.
	const gameBlockLen = 2800
	blockEnd := gameBlockStart + gameBlockLen
	if blockEnd > len(text) {
		blockEnd = len(text)
	}
	block := text[gameBlockStart:blockEnd]
	nameMatches := namePat.FindAllStringIndex(block, -1)
	var inBlock []string
	for _, loc := range nameMatches {
		name := strings.Trim(block[loc[0]:loc[1]], "><")
		if len(name) < 4 || skip[name] {
			continue
		}
		if strings.HasSuffix(name, "Capitals") || strings.HasSuffix(name, "Flyers") ||
			strings.HasSuffix(name, "Sabres") || strings.HasSuffix(name, "Devils") ||
			strings.HasSuffix(name, "Kraken") || strings.HasSuffix(name, "Stars") ||
			strings.HasSuffix(name, "Avalanche") || strings.HasSuffix(name, "Mammoth") ||
			strings.HasSuffix(name, "Jets") || strings.HasSuffix(name, "Canucks") ||
			strings.HasSuffix(name, "Knights") || strings.HasSuffix(name, "Kings") ||
			strings.HasSuffix(name, "Oilers") || strings.HasSuffix(name, "Ducks") ||
			strings.HasSuffix(name, "Bruins") || strings.HasSuffix(name, "Canadiens") ||
			strings.HasSuffix(name, "Senators") || strings.HasSuffix(name, "Leafs") ||
			strings.HasSuffix(name, "Rangers") || strings.HasSuffix(name, "Islanders") ||
			strings.HasSuffix(name, "Hurricanes") || strings.HasSuffix(name, "Panthers") ||
			strings.HasSuffix(name, "Lightning") || strings.HasSuffix(name, "Jackets") ||
			strings.HasSuffix(name, "Wings") || strings.HasSuffix(name, "Predators") ||
			strings.HasSuffix(name, "Blues") || strings.HasSuffix(name, "Wild") ||
			strings.HasSuffix(name, "Flames") || strings.HasSuffix(name, "Sharks") ||
			strings.HasSuffix(name, "Penguins") {
			continue
		}
		// Require a goalie status word within 400 chars after this name — filters out
		// journalist names, analyst bylines, and other non-goalie two-word strings.
		lookaheadEnd := loc[1] + 400
		if lookaheadEnd > len(block) {
			lookaheadEnd = len(block)
		}
		lookahead := block[loc[1]:lookaheadEnd]
		if !strings.Contains(lookahead, "Confirmed") && !strings.Contains(lookahead, "Likely") &&
			!strings.Contains(lookahead, "Unconfirmed") && !strings.Contains(lookahead, "Projected") {
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
