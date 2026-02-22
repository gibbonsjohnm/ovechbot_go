package discord

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Capitals red (approx)
const embedColor = 0xC41E3A

// Default Ovechkin headshot from NHL assets (current season).
const defaultOvechkinImage = "https://assets.nhle.com/mugs/nhl/20252026/WSH/8471214.png"

// Bot wraps a Discord session and channel for goal announcements and commands.
type Bot struct {
	session *discordgo.Session
	// channelID is where goal announcements are posted
	channelID string
	// imageURL for Ovechkin (embed thumbnail)
	imageURL string
	mu       sync.Mutex
}

// Config for the Discord bot.
type Config struct {
	Token          string
	AnnounceChannelID string
	OvechkinImageURL  string // optional; default used if empty
}

// NewBot creates a Discord bot. Token must be non-empty.
func NewBot(cfg Config) (*Bot, error) {
	if cfg.Token == "" {
		return nil, fmt.Errorf("discord token required")
	}
	s, err := discordgo.New("Bot " + cfg.Token)
	if err != nil {
		return nil, err
	}
	// Required for gateway to stay connected and for the bot to show as online.
	s.Identify.Intents = discordgo.IntentsGuilds
	img := cfg.OvechkinImageURL
	if img == "" {
		img = defaultOvechkinImage
	}
	return &Bot{
		session:   s,
		channelID: cfg.AnnounceChannelID,
		imageURL:  img,
	}, nil
}

// GoalAnnouncementDescription returns the embed description text for a goal announcement (testable).
func GoalAnnouncementDescription(goals int) string {
	return fmt.Sprintf("**Alex Ovechkin** has scored!\n\n🥅 **Career goals (regular season): %d**", goals)
}

// StatusNameForGame returns the "Watching" activity name: "HOME vs AWAY" or "Nothing :(" when no live Capitals game (testable).
func StatusNameForGame(homeAbbrev, awayAbbrev string) string {
	if homeAbbrev != "" && awayAbbrev != "" {
		return homeAbbrev + " vs " + awayAbbrev
	}
	return "Nothing :("
}

// PostGoalAnnouncement sends a rich embed to the announce channel when Ovechkin scores.
func (b *Bot) PostGoalAnnouncement(ctx context.Context, goals int, recordedAt time.Time) error {
	if b.channelID == "" {
		return nil
	}
	b.mu.Lock()
	s := b.session
	b.mu.Unlock()
	if s == nil {
		return nil
	}
	embed := &discordgo.MessageEmbed{
		Title:       "🚨 GOAL! 🚨",
		Description: GoalAnnouncementDescription(goals),
		Color:       embedColor,
		Thumbnail:   &discordgo.MessageEmbedThumbnail{URL: b.imageURL},
		Timestamp:   recordedAt.Format(time.RFC3339),
		Footer:      &discordgo.MessageEmbedFooter{Text: "Washington Capitals • NHL"},
	}
	_, err := s.ChannelMessageSendEmbed(b.channelID, embed)
	if err != nil {
		return fmt.Errorf("send embed: %w", err)
	}
	slog.Info("discord goal announcement sent", "channel", b.channelID, "goals", goals)
	return nil
}

// Session returns the discordgo session (for registering handlers and opening).
func (b *Bot) Session() *discordgo.Session {
	return b.session
}

// RegisterSlashCommands registers /goals, /lastgoal, /ping. Call after Open() so State is ready.
func (b *Bot) RegisterSlashCommands(guildID string) ([]*discordgo.ApplicationCommand, error) {
	appID := b.session.State.User.ID
	commands := []*discordgo.ApplicationCommand{
		{
			Name:        "goals",
			Description: "Check Alex Ovechkin's career goal total (regular season)",
		},
		{
			Name:        "lastgoal",
			Description: "When and vs whom was Ovi's most recent goal?",
		},
		{
			Name:        "ping",
			Description: "Ping the bot to check if it's online",
		},
	}
	var registered []*discordgo.ApplicationCommand
	for _, cmd := range commands {
		created, err := b.session.ApplicationCommandCreate(appID, guildID, cmd)
		if err != nil {
			return registered, fmt.Errorf("create command %s: %w", cmd.Name, err)
		}
		registered = append(registered, created)
	}
	return registered, nil
}

// AddInteractionHandler registers the handler for slash commands. Pass NHL client for /goals and /lastgoal.
func (b *Bot) AddInteractionHandler(handler func(s *discordgo.Session, i *discordgo.InteractionCreate)) {
	b.session.AddHandler(handler)
}

// SetWatchingStatus sets the bot's activity to "Watching HOME vs AWAY" when a live Capitals game is on, or "Nothing :(" when not.
// Pass empty strings for both when no live Capitals game.
func (b *Bot) SetWatchingStatus(homeAbbrev, awayAbbrev string) error {
	b.mu.Lock()
	s := b.session
	b.mu.Unlock()
	if s == nil {
		return nil
	}
	name := StatusNameForGame(homeAbbrev, awayAbbrev)
	return s.UpdateStatusComplex(discordgo.UpdateStatusData{
		Status: "online",
		Activities: []*discordgo.Activity{
			{
				Type: discordgo.ActivityTypeWatching,
				Name: name,
			},
		},
	})
}
