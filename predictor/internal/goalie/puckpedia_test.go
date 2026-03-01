package goalie

import (
	"testing"
)

func TestParsePuckPediaGoalieName(t *testing.T) {
	// Simulated HTML: Washington/Capitals vs Montreal, #79 Charlie Lindgren (away), #75 Jakub Dobes (home).
	html := []byte(`
	<div>Washington Capitals at Montreal Canadiens 7:00PM</div>
	<span>#79 Charlie Lindgren</span><span>CONFIRMED</span>
	<span>#75 Jakub Dobes</span><span>CONFIRMED</span>
	`)
	// Caps away @ MTL → we want home goalie = Jakub Dobes. Pass 0 to skip JSON path.
	got := parsePuckPediaGoalieName(html, "Montreal", false, 0)
	if got != "Jakub Dobes" {
		t.Errorf("Caps away (want home=MTL): got %q, want Jakub Dobes", got)
	}
	// Caps home vs MTL → we want away goalie = Jakub Dobes (MTL away).
	html2 := []byte(`
	<div>Montreal Canadiens at Washington Capitals 7:00PM</div>
	<span>#75 Jakub Dobes</span><span>CONFIRMED</span>
	<span>#79 Charlie Lindgren</span><span>CONFIRMED</span>
	`)
	got2 := parsePuckPediaGoalieName(html2, "Montreal", true, 0)
	if got2 != "Jakub Dobes" {
		t.Errorf("Caps home (want away=MTL): got %q, want Jakub Dobes", got2)
	}
}

func TestParsePuckPediaByGameID(t *testing.T) {
	// Escaped JSON as embedded in PuckPedia page: home (MTL) Dobes, away (WSH) Lindgren. Caps away → want home = Dobes.
	text := `x\"id\":\"2025020940\",\"startTimeUTC\":\"2026-03-01T00:00:00Z\"},\"home\":{\"team\":{\"short\":\"MTL\"},\"goalie\":{\"lastName\":\"Dobes\"}},\"away\":{\"team\":{\"short\":\"WAS\"},\"goalie\":{\"lastName\":\"Lindgren\"}}y`
	got := parsePuckPediaByGameID(text, 2025020940, false) // caps away → home goalie = Dobes
	if got != "Dobes" {
		t.Errorf("parsePuckPediaByGameID: got %q, want Dobes", got)
	}
	got2 := parsePuckPediaByGameID(text, 2025020940, true) // caps home → away goalie = Lindgren
	if got2 != "Lindgren" {
		t.Errorf("parsePuckPediaByGameID (caps home): got %q, want Lindgren", got2)
	}
}

func TestParsePuckPediaGoalieName_noMatch(t *testing.T) {
	html := []byte(`<div>Buffalo at Boston</div><span>#1 Ukko-Pekka Luukkonen</span><span>#37 Jeremy Swayman</span>`)
	got := parsePuckPediaGoalieName(html, "Philadelphia", true, 0)
	if got != "" {
		t.Errorf("wrong game: got %q, want empty", got)
	}
}
