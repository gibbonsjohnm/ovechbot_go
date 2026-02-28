package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	_ "time/tzdata" // embed IANA timezone data so LoadLocation("America/New_York") works without system tzdata

	"github.com/bwmarrin/discordgo"
	"github.com/redis/go-redis/v9"
	"ovechbot_go/announcer/internal/consumer"
	"ovechbot_go/announcer/internal/discord"
	"ovechbot_go/announcer/internal/nhl"
)

const nextPredictionKey = "ovechkin:next_prediction"

// lastAnnouncedGoal is the most recent goal event we posted to Discord (used by /lastgoal to avoid NHL API when current).
var lastAnnouncedMu sync.Mutex
var lastAnnouncedGoal *consumer.GoalEvent

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
	remConsumer := consumer.NewReminderConsumer(rdb)
	if err := remConsumer.EnsureReminderGroup(ctx); err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		slog.Warn("reminder group ensure", "stream", consumer.RemindersStreamKey, "error", err)
	}
	postGameConsumer := consumer.NewPostGameConsumer(rdb)
	if err := postGameConsumer.EnsurePostGameGroup(ctx); err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		slog.Warn("post-game group ensure", "stream", consumer.PostGameStreamKey, "error", err)
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
					careerGoals, err := nhlClient.CareerGoals(context.Background())
					if err != nil {
						return "❌ Could not fetch goal total: " + err.Error()
					}
					lastAnnouncedMu.Lock()
					cached := lastAnnouncedGoal
					lastAnnouncedMu.Unlock()
					if cached != nil && cached.Goals == careerGoals {
						oppName := cached.OpponentName
						if oppName == "" {
							oppName = cached.Opponent
						}
						msg := fmt.Sprintf("📅 **Last goal:** #%d · %s vs **%s** (%s)", cached.Goals, cached.RecordedAt.Format("Jan 2, 2006"), oppName, cached.Opponent)
						if cached.GoalieName != "" {
							msg += fmt.Sprintf("\n:goal: Opposing goalie: **%s**", cached.GoalieName)
						}
						return msg + "\n_(from stream)_"
					}
					info, err := nhlClient.LastGoalGame(context.Background())
					if err != nil {
						return "❌ Could not fetch last goal: " + err.Error()
					}
					msg := fmt.Sprintf("📅 **Last goal:** %s vs **%s** (%s)", info.GameDate, info.OpponentName, info.Opponent)
					if info.GoalieName != "" {
						msg += fmt.Sprintf("\n:goal: Opposing goalie: **%s**", info.GoalieName)
					}
					return msg
				})
			case "nextgame":
				deferRespond(s, i, func() string {
					game, err := nhlClient.NextCapitalsGame(context.Background())
					if err != nil {
						return "❌ Could not fetch schedule: " + err.Error()
					}
					if game == nil {
						return "📅 No upcoming Capitals game in the schedule (season may be over or not started)."
					}
					et, err := time.LoadLocation("America/New_York")
					if err != nil {
						et = time.FixedZone("ET", -5*3600)
					}
					startET := game.StartTimeUTC.In(et)
					when := startET.Format("Mon Jan 2, 3:04 PM ET")
					var msg string
					if nhl.InProgressGameStates[game.GameState] {
						msg = fmt.Sprintf("🏒 **Capitals are playing now:** %s @ **%s**\n📍 %s · %s", game.AwayAbbrev, game.HomeAbbrev, game.Venue, when)
					} else {
						msg = fmt.Sprintf("📅 **Next game:** %s @ **%s**\n📍 %s · %s", game.AwayAbbrev, game.HomeAbbrev, game.Venue, when)
					}
					// Append Ovi scoring prediction (and optional odds) if predictor has written one for this game
					if b, err := rdb.Get(context.Background(), nextPredictionKey).Bytes(); err == nil {
						var pred struct {
							GameID         int64  `json:"game_id"`
							ProbabilityPct int    `json:"probability_pct"`
							OddsAmerican   string `json:"odds_american,omitempty"`
							GoalieName     string `json:"goalie_name,omitempty"`
						}
						if json.Unmarshal(b, &pred) == nil && pred.GameID == game.GameID && pred.ProbabilityPct > 0 {
							msg += "\n📊 Ovi scoring chance: **" + strconv.Itoa(pred.ProbabilityPct) + "%**"
							if pred.OddsAmerican != "" {
								msg += " · Anytime goal: **" + pred.OddsAmerican + "**"
							}
							if pred.GoalieName != "" {
								msg += "\n:goal: Probable goalie: **" + pred.GoalieName + "**"
							}
						}
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
		// Reminder consumer: pre-game messages with Ovi scoring probability (from predictor)
		go runReminderConsumer(ctx, remConsumer, bot)
		// Post-game consumer: evaluation summary (evaluator → Redis → announcer)
		go runPostGameConsumer(ctx, postGameConsumer, bot)
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
					if err := bot.PostGoalAnnouncement(ctx, e.Goals, e.RecordedAt, e.GoalieName, e.OpponentName); err != nil {
						slog.Warn("discord post failed", "error", err)
					}
				}
				// Cache for /lastgoal so we can answer from stream data when still current
				dup := e
				lastAnnouncedMu.Lock()
				lastAnnouncedGoal = &dup
				lastAnnouncedMu.Unlock()
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

// runPostGameConsumer reads from ovechkin:post_game and posts evaluation summary to Discord.
func runPostGameConsumer(ctx context.Context, c *consumer.PostGameConsumer, bot *discord.Bot) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			payloads, ids, err := c.ReadPostGames(ctx)
			if err != nil {
				slog.Warn("read post-game failed", "error", err)
				continue
			}
			if bot != nil && bot.Session() != nil {
				for _, p := range payloads {
					if err := bot.PostMessage(ctx, p.Message); err != nil {
						slog.Warn("post-game send failed", "error", err)
					}
				}
			}
			if len(ids) > 0 {
				if err := c.AckPostGames(ctx, ids...); err != nil {
					slog.Warn("post-game ack failed", "error", err)
				}
			}
		}
	}
}

// runReminderConsumer reads from ovechkin:reminders and posts to Discord.
func runReminderConsumer(ctx context.Context, rem *consumer.ReminderConsumer, bot *discord.Bot) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			payloads, ids, err := rem.ReadReminders(ctx)
			if err != nil {
				slog.Warn("read reminders failed", "error", err)
				continue
			}
			if bot != nil && bot.Session() != nil {
				for _, p := range payloads {
					if err := bot.PostGameReminder(ctx, p.Opponent, p.HomeAway, p.ProbabilityPct, p.StartTimeUTC, p.OddsAmerican, p.GoalieName); err != nil {
						slog.Warn("post reminder failed", "error", err)
					}
				}
			}
			if len(ids) > 0 {
				if err := rem.AckReminders(ctx, ids...); err != nil {
					slog.Warn("reminder ack failed", "error", err)
				}
			}
		}
	}
}

// runStatusUpdates periodically sets the bot status to "Watching HOME vs AWAY" or "Watching the NHL".
func runStatusUpdates(ctx context.Context, bot *discord.Bot, nhlClient *nhl.Client) {
	ticker := time.NewTicker(3 * time.Minute)
	defer ticker.Stop()
	update := func() {
		game, err := nhlClient.CurrentLiveCapitalsGame(ctx)
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
