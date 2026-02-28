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
	// Caps away @ MTL → we want home goalie = Jakub Dobes.
	got := parsePuckPediaGoalieName(html, "Montreal", false)
	if got != "Jakub Dobes" {
		t.Errorf("Caps away (want home=MTL): got %q, want Jakub Dobes", got)
	}
	// Caps home vs MTL → we want away goalie = Charlie Lindgren (MTL is away).
	html2 := []byte(`
	<div>Montreal Canadiens at Washington Capitals 7:00PM</div>
	<span>#75 Jakub Dobes</span><span>CONFIRMED</span>
	<span>#79 Charlie Lindgren</span><span>CONFIRMED</span>
	`)
	got2 := parsePuckPediaGoalieName(html2, "Montreal", true)
	if got2 != "Jakub Dobes" {
		t.Errorf("Caps home (want away=MTL): got %q, want Jakub Dobes", got2)
	}
}

func TestParsePuckPediaGoalieName_noMatch(t *testing.T) {
	html := []byte(`<div>Buffalo at Boston</div><span>#1 Ukko-Pekka Luukkonen</span><span>#37 Jeremy Swayman</span>`)
	got := parsePuckPediaGoalieName(html, "Philadelphia", true)
	if got != "" {
		t.Errorf("wrong game: got %q, want empty", got)
	}
}
