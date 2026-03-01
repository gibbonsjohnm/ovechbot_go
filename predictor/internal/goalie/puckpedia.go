package goalie

import (
	"context"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"ovechbot_go/predictor/internal/schedule"
)

// PuckPedia starting goalies: https://depth-charts.puckpedia.com/starting-goalies
// dayCount=2 to include today and tomorrow (ET); page lists away goalie then home goalie per game.
const puckpediaURL = "https://depth-charts.puckpedia.com/starting-goalies?dayCount=2&timezone=America/New_York"

// opponentNameFragment is a substring that appears in matchup text (e.g. "Washington" vs "Montreal") for each opponent abbrev.
var opponentNameFragment = map[string]string{
	"ANA": "Anaheim", "BOS": "Boston", "BUF": "Buffalo", "CGY": "Calgary", "CAR": "Carolina",
	"CHI": "Chicago", "COL": "Colorado", "CBJ": "Columbus", "DAL": "Dallas", "DET": "Detroit",
	"EDM": "Edmonton", "FLA": "Florida", "LAK": "Los Angeles", "MIN": "Minnesota", "MTL": "Montreal",
	"NJD": "New Jersey", "NSH": "Nashville", "NYI": "New York Islanders", "NYR": "New York Rangers",
	"OTT": "Ottawa", "PHI": "Philadelphia", "PIT": "Pittsburgh", "SJS": "San Jose", "SEA": "Seattle",
	"STL": "St. Louis", "TBL": "Tampa Bay", "TOR": "Toronto", "UTA": "Utah", "VAN": "Vancouver",
	"VGK": "Vegas", "WPG": "Winnipeg", "WSH": "Washington",
}

const capitalsMatch = "Washington"

// puckPediaCapsFragments: page may use "WAS" or "Capitals" instead of "Washington".
var puckPediaCapsFragments = []string{capitalsMatch, "Capitals", "WAS"}

// puckPediaOpponentAlternatives: for some opponents, PuckPedia uses nickname or abbrev (e.g. "Canadiens", "MTL" not "Montreal").
var puckPediaOpponentAlternatives = map[string][]string{
	"Montreal":   {"Canadiens", "MTL"},
	"New Jersey": {"Devils", "NJD"},
	"San Jose":   {"Sharks", "SJS"},
	"Tampa Bay":  {"Lightning", "TBL"},
	"Los Angeles": {"Kings", "LAK"},
	"St. Louis":  {"Blues", "STL"},
}

// OpposingStarterFromPuckPedia fetches PuckPedia's starting-goalies page and returns the opposing
// team's starter name (e.g. "Jakub Dobes") for the given game. Returns empty string if not found.
// Page order: away goalie, then home goalie.
func (c *Client) OpposingStarterFromPuckPedia(ctx context.Context, g *schedule.Game) string {
	oppAbbrev := g.Opponent()
	frag, ok := opponentNameFragment[oppAbbrev]
	if !ok {
		return ""
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, puckpediaURL, nil)
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
	return parsePuckPediaGoalieName(body, frag, g.IsHome(), g.GameID)
}

// parsePuckPediaByGameID finds the game by ID in the embedded JSON and returns the opposing goalie's last name.
// PuckPedia embeds escaped JSON: \"lastName\":\"Dobes\" (home then away for that game).
// resolveGoalieByName accepts last name only and matches on roster.
func parsePuckPediaByGameID(text string, gameID int64, capsAreHome bool) string {
	idStr := strconv.FormatInt(gameID, 10)
	idx := strings.Index(text, idStr)
	if idx < 0 {
		return ""
	}
	block := text[idx:]
	if len(block) > 1500 {
		block = block[:1500]
	}
	// Match \"lastName\":\"Name\" or "lastName":"Name"
	re := regexp.MustCompile(`\\?"lastName\\?"\s*:\s*\\?"([^"\\]+)\\?"`)
	matches := re.FindAllStringSubmatch(block, 2)
	if len(matches) < 2 {
		return ""
	}
	homeLastName := strings.TrimSpace(matches[0][1])
	awayLastName := strings.TrimSpace(matches[1][1])
	if capsAreHome {
		return awayLastName // opponent is away
	}
	return homeLastName // opponent is home
}

// parsePuckPediaGoalieName finds the Caps game and returns the opposing goalie name.
// It first tries JSON extraction by game ID (page embeds matchupSummaries with "id":"2025020940", home/away goalie lastName).
// If that fails, it falls back to HTML parsing (Caps + opponent block, then #N FirstName LastName or two-word names).
func parsePuckPediaGoalieName(html []byte, opponentFragment string, capsAreHome bool, gameID int64) string {
	text := string(html)
	if gameID != 0 {
		if name := parsePuckPediaByGameID(text, gameID, capsAreHome); name != "" {
			return name
		}
	}
	textLower := strings.ToLower(text)
	oppLower := strings.ToLower(opponentFragment)
	// Page may use "WAS"/"Capitals" not "Washington", and "Canadiens"/"MTL" not "Montreal".
	hasCapsInPage := false
	for _, f := range puckPediaCapsFragments {
		if strings.Contains(text, f) || strings.Contains(textLower, strings.ToLower(f)) {
			hasCapsInPage = true
			break
		}
	}
	if !hasCapsInPage {
		return ""
	}
	hasOppInPage := strings.Contains(textLower, oppLower)
	if !hasOppInPage && puckPediaOpponentAlternatives[opponentFragment] != nil {
		for _, alt := range puckPediaOpponentAlternatives[opponentFragment] {
			if strings.Contains(text, alt) || strings.Contains(textLower, strings.ToLower(alt)) {
				hasOppInPage = true
				break
			}
		}
	}
	if !hasOppInPage {
		return ""
	}
	// Find block: Caps fragment and opponent fragment within 250 chars.
	const matchupWindow = 250
	gameBlockStart := -1
	windowLen := matchupWindow
	if len(text) < windowLen {
		windowLen = len(text)
	}
	for i := 0; i <= len(text)-windowLen; i++ {
		window := text[i : i+windowLen]
		windowLower := strings.ToLower(window)
		hasCaps := false
		for _, f := range puckPediaCapsFragments {
			if strings.Contains(window, f) || strings.Contains(windowLower, strings.ToLower(f)) {
				hasCaps = true
				break
			}
		}
		hasOpp := strings.Contains(windowLower, oppLower)
		if !hasOpp && puckPediaOpponentAlternatives[opponentFragment] != nil {
			for _, alt := range puckPediaOpponentAlternatives[opponentFragment] {
				if strings.Contains(window, alt) || strings.Contains(windowLower, strings.ToLower(alt)) {
					hasOpp = true
					break
				}
			}
		}
		if hasCaps && hasOpp {
			gameBlockStart = i
			break
		}
	}
	if gameBlockStart < 0 {
		return ""
	}
	const blockLen = 3000
	blockEnd := gameBlockStart + blockLen
	if blockEnd > len(text) {
		blockEnd = len(text)
	}
	block := text[gameBlockStart:blockEnd]

	// Full name: "#79 Charlie Lindgren" or "Charlie Lindgren" with CONFIRMED/PROJECTED nearby.
	// Prefer #\d+ FirstName LastName so we get the exact card name.
	fullNamePat := regexp.MustCompile(`#\d+\s+([A-Z][a-z]+(?:\s+[A-Z][a-z\-]+)+)`)
	matches := fullNamePat.FindAllStringSubmatch(block, -1)
	var names []string
	seen := make(map[string]bool)
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		name := strings.TrimSpace(m[1])
		if len(name) < 4 || seen[name] {
			continue
		}
		// Must have CONFIRMED or PROJECTED within 400 chars after this match (goalie status).
		idx := strings.Index(block, m[0])
		if idx < 0 {
			continue
		}
		after := block[idx:]
		if len(after) > 400 {
			after = after[:400]
		}
		afterLower := strings.ToLower(after)
		if !strings.Contains(afterLower, "confirmed") && !strings.Contains(afterLower, "projected") {
			continue
		}
		// Skip team names / non-goalies.
		if strings.HasSuffix(strings.ToLower(name), "capitals") || strings.HasSuffix(strings.ToLower(name), "flyers") ||
			strings.HasSuffix(strings.ToLower(name), "canadiens") || strings.HasSuffix(strings.ToLower(name), "rangers") {
			continue
		}
		seen[name] = true
		names = append(names, name)
		if len(names) >= 2 {
			break
		}
	}
	if len(names) < 2 {
		// Fallback: two-word names without # prefix (e.g. "Charlie Lindgren" near CONFIRMED).
		twoWordPat := regexp.MustCompile(`\b([A-Z][a-z]+(?:-[A-Z][a-z]+)?\s+[A-Z][a-z]+(?:-[A-Z][a-z]+)?)\b`)
		twoMatches := twoWordPat.FindAllStringSubmatch(block, -1)
		for _, m := range twoMatches {
			if len(m) < 2 {
				continue
			}
			name := strings.TrimSpace(m[1])
			if len(name) < 4 || seen[name] {
				continue
			}
			if strings.HasSuffix(strings.ToLower(name), "capitals") || strings.HasSuffix(strings.ToLower(name), "flyers") ||
				strings.HasSuffix(strings.ToLower(name), "canadiens") {
				continue
			}
			seen[name] = true
			names = append(names, name)
			if len(names) >= 2 {
				break
			}
		}
	}
	if len(names) < 2 {
		return ""
	}
	if capsAreHome {
		return names[0] // away goalie = opponent
	}
	return names[1] // home goalie = opponent
}
