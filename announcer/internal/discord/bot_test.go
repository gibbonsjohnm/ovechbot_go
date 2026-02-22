package discord

import (
	"strings"
	"testing"
)

func TestNewBot_EmptyToken(t *testing.T) {
	_, err := NewBot(Config{Token: ""})
	if err == nil {
		t.Fatal("expected error for empty token")
	}
	if !strings.Contains(err.Error(), "token") {
		t.Errorf("err = %v", err)
	}
}

func TestStatusNameForGame_WhenPlaying(t *testing.T) {
	got := StatusNameForGame("WSH", "PHI")
	want := "WSH vs PHI"
	if got != want {
		t.Errorf("StatusNameForGame(WSH, PHI) = %q; want %q", got, want)
	}
}

func TestStatusNameForGame_WhenNotPlaying(t *testing.T) {
	got := StatusNameForGame("", "")
	want := "Nothing :("
	if got != want {
		t.Errorf("StatusNameForGame(_, _) = %q; want %q", got, want)
	}
}

func TestStatusNameForGame_Partial(t *testing.T) {
	got := StatusNameForGame("WSH", "")
	if got != "Nothing :(" {
		t.Errorf("one empty should yield Nothing :(: %q", got)
	}
}
