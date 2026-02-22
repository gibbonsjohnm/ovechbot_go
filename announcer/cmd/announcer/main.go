package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/redis/go-redis/v9"
	"ovechbot_go/announcer/internal/consumer"
	"ovechbot_go/announcer/internal/discord"
	"ovechbot_go/announcer/internal/nhl"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	redisAddr := getEnv("REDIS_ADDR", "redis:6379")
	discordToken := os.Getenv("DISCORD_BOT_TOKEN")
	discordChannelID := os.Getenv("DISCORD_ANNOUNCE_CHANNEL_ID")
	discordGuildID := os.Getenv("DISCORD_GUILD_ID") // optional; empty = global commands
	ovechkinImageURL := os.Getenv("DISCORD_OVECHKIN_IMAGE_URL")

	rdb := redis.NewClient(&redis.Options{Addr: redisAddr})
	defer rdb.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := rdb.Ping(ctx).Err(); err != nil {
		slog.Error("redis ping failed", "error", err)
		os.Exit(1)
	}

	c := consumer.NewConsumer(rdb)
	if err := c.EnsureGroup(ctx); err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		slog.Warn("consumer group ensure", "group", consumer.ConsumerGroup, "error", err)
	}
	slog.Info("announcer started", "stream", consumer.StreamKey, "group", consumer.ConsumerGroup)

	var bot *discord.Bot
	if discordToken != "" {
		var err error
		bot, err = discord.NewBot(discord.Config{
			Token:               discordToken,
			AnnounceChannelID:   discordChannelID,
			OvechkinImageURL:    ovechkinImageURL,
		})
		if err != nil {
			slog.Error("discord bot create failed", "error", err)
			os.Exit(1)
		}
		nhlClient := nhl.NewClient()
		// Slash command handlers
		bot.AddInteractionHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			name := i.ApplicationCommandData().Name
			switch name {
			case "ping":
				respond(s, i, "🏒 **Pong!** Ovechbot is online.")
			case "goals":
				// Defer then followup so NHL API call can take >3s
				deferRespond(s, i, func() string {
					goals, err := nhlClient.CareerGoals(context.Background())
					if err != nil {
						return "❌ Could not fetch goal total: " + err.Error()
					}
					return fmt.Sprintf("🥅 **Alex Ovechkin** has **%d** career goals (regular season).", goals)
				})
			case "lastgoal":
				deferRespond(s, i, func() string {
					info, err := nhlClient.LastGoalGame(context.Background())
					if err != nil {
						return "❌ Could not fetch last goal: " + err.Error()
					}
					msg := fmt.Sprintf("📅 **Last goal:** %s vs **%s** (%s)", info.GameDate, info.OpponentName, info.Opponent)
					if info.GoalieName != "" {
						msg += fmt.Sprintf("\n🧤 Opposing goalie: **%s**", info.GoalieName)
					}
					return msg
				})
			}
		})
		// Log when Discord gateway is ready (bot shows online)
		bot.Session().AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
			slog.Info("discord connected", "user", r.User.Username, "id", r.User.ID)
		})
		slog.Info("connecting to Discord gateway...")
		if err := bot.Session().Open(); err != nil {
			slog.Error("discord open failed", "error", err)
			os.Exit(1)
		}
		defer bot.Session().Close()
		slog.Info("discord gateway open")
		registered, err := bot.RegisterSlashCommands(discordGuildID)
		if err != nil {
			slog.Warn("discord register commands failed", "error", err)
		} else {
			slog.Info("discord slash commands registered", "count", len(registered), "guild_id", discordGuildID)
		}
		// Status: "Watching HOME vs AWAY" when Capitals are in the schedule, else "Watching the NHL"
		go runStatusUpdates(ctx, bot, nhlClient)
	} else {
		slog.Info("DISCORD_BOT_TOKEN not set; Discord announcements and commands disabled")
	}

	// Consumer loop: on goal event, log and post to Discord
	for {
		select {
		case <-ctx.Done():
			slog.Info("shutting down announcer", "reason", ctx.Err())
			return
		default:
			events, ids, err := c.ReadMessages(ctx)
			if err != nil {
				slog.Warn("read messages failed", "error", err)
				continue
			}
			for _, e := range events {
				slog.Info("goal notification",
					"player_id", e.PlayerID,
					"goals", e.Goals,
					"recorded_at", e.RecordedAt,
					"message", fmt.Sprintf("Alex Ovechkin has scored! Career goals: %d", e.Goals),
				)
				if bot != nil && bot.Session() != nil {
					if err := bot.PostGoalAnnouncement(ctx, e.Goals, e.RecordedAt); err != nil {
						slog.Warn("discord post failed", "error", err)
					}
				}
			}
			if len(ids) > 0 {
				if err := c.Ack(ctx, ids...); err != nil {
					slog.Warn("ack failed", "error", err)
				}
			}
		}
	}
}

func respond(s *discordgo.Session, i *discordgo.InteractionCreate, content string) {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content:         content,
			AllowedMentions: &discordgo.MessageAllowedMentions{},
		},
	})
	if err != nil {
		slog.Warn("discord respond failed", "error", err)
	}
}

// deferRespond responds with "thinking" then sends a followup with the result (for slow NHL API).
func deferRespond(s *discordgo.Session, i *discordgo.InteractionCreate, fn func() string) {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{},
	})
	if err != nil {
		slog.Warn("discord defer respond failed", "error", err)
		return
	}
	content := fn()
	_, err = s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Content:         content,
		AllowedMentions:  &discordgo.MessageAllowedMentions{},
	})
	if err != nil {
		slog.Warn("discord followup failed", "error", err)
	}
}

// runStatusUpdates periodically sets the bot status to "Watching HOME vs AWAY" or "Watching the NHL".
func runStatusUpdates(ctx context.Context, bot *discord.Bot, nhlClient *nhl.Client) {
	ticker := time.NewTicker(3 * time.Minute)
	defer ticker.Stop()
	update := func() {
		game, err := nhlClient.CurrentCapitalsGame(ctx)
		if err != nil {
			slog.Warn("status update: fetch schedule failed", "error", err)
			return
		}
		home, away := "", ""
		if game != nil {
			home, away = game.HomeAbbrev, game.AwayAbbrev
		}
		if err := bot.SetWatchingStatus(home, away); err != nil {
			slog.Warn("status update failed", "error", err)
		}
	}
	update() // once immediately
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			update()
		}
	}
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
