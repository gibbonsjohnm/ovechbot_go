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

func TestGoalAnnouncementDescription(t *testing.T) {
	got := GoalAnnouncementDescription(921)
	if got != GoalAnnouncementDescriptionWithEnrichment(921, "", "") {
		t.Error("GoalAnnouncementDescription should match no-enrichment case")
	}
	if !strings.Contains(got, "921") {
		t.Errorf("description should contain 921: %q", got)
	}
}

func TestGoalAnnouncementDescriptionWithEnrichment(t *testing.T) {
	got := GoalAnnouncementDescriptionWithEnrichment(921, "Igor Shesterkin", "Rangers")
	if !strings.Contains(got, "921") {
		t.Errorf("description should contain 921: %q", got)
	}
	if !strings.Contains(got, "Igor Shesterkin") {
		t.Errorf("description should contain goalie: %q", got)
	}
	if !strings.Contains(got, "Rangers") {
		t.Errorf("description should contain opponent: %q", got)
	}
	gotNoOpp := GoalAnnouncementDescriptionWithEnrichment(921, "Igor Shesterkin", "")
	if !strings.Contains(gotNoOpp, "Scored on **Igor Shesterkin**") {
		t.Errorf("without opponent should still show goalie: %q", gotNoOpp)
	}
}
