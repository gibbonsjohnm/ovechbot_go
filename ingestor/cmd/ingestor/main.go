package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"ovechbot_go/ingestor/internal/nhl"
	"ovechbot_go/ingestor/internal/stream"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	redisAddr := getEnv("REDIS_ADDR", "redis:6379")
	pollInterval := getDurationEnv("POLL_INTERVAL", 20*time.Second)

	rdb := redis.NewClient(&redis.Options{Addr: redisAddr})
	defer rdb.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	nhlClient := nhl.NewClient()
	producer := stream.NewProducer(rdb)

	// career total we use for announcements: add 1 for each goal we detect; sync from API when not in a live game
	lastKnownCareerTotal := 0

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	if err := pingRedis(ctx, rdb); err != nil {
		slog.Error("redis ping failed", "error", err)
		os.Exit(1)
	}
	goals, err := nhlClient.CareerGoals(ctx)
	if err != nil {
		slog.Error("initial nhl fetch failed", "error", err)
		os.Exit(1)
	}
	lastKnownCareerTotal = goals
	slog.Info("ingestor started", "stream", stream.StreamKey, "current_goals", goals, "poll_interval", pollInterval)

	for {
		select {
		case <-ctx.Done():
			slog.Info("shutting down ingestor", "reason", ctx.Err())
			return
		case <-ticker.C:
			caps, err := nhlClient.CapsGameFromScoreNow(ctx)
			if err != nil {
				slog.Warn("score/now fetch failed", "error", err)
				continue
			}

			if caps == nil {
				if apiGoals, err := nhlClient.CareerGoals(ctx); err == nil && apiGoals > lastKnownCareerTotal {
					lastKnownCareerTotal = apiGoals
				}
				continue
			}

			if nhl.LiveGameStates[caps.GameState] {
				for _, g := range caps.Goals {
					if g.PlayerID != nhl.OvechkinPlayerID {
						continue
					}
					alreadySeen, err := producer.MarkGoalSeen(ctx, caps.GameID, g.GoalsToDate)
					if err != nil {
						slog.Warn("mark goal seen failed", "error", err, "game_id", caps.GameID, "goals_to_date", g.GoalsToDate)
						continue
					}
					if alreadySeen {
						continue
					}

					// Add this goal to career total for the announcement (don't rely on API which may lag)
					lastKnownCareerTotal++
					careerGoals := lastKnownCareerTotal
					evt := stream.GoalEvent{PlayerID: nhl.OvechkinPlayerID, Goals: careerGoals}
					info, _ := nhlClient.GoalGameInfo(ctx, caps.GameID)
					if info != nil {
						evt.Opponent = info.Opponent
						evt.OpponentName = info.OpponentName
					}
					// Use play-by-play for the goalie actually in net for this goal (not boxscore starter).
					// If play-by-play doesn't have the goal yet (API lag), retry once after a short delay
					// so we don't fall back to boxscore and show the wrong goalie after a mid-game change.
					goalieName := nhlClient.GoalieForGoal(ctx, caps.GameID, nhl.OvechkinPlayerID, g.GoalsToDate)
					if goalieName == "" {
						select {
						case <-ctx.Done():
						case <-time.After(8 * time.Second):
							goalieName = nhlClient.GoalieForGoal(ctx, caps.GameID, nhl.OvechkinPlayerID, g.GoalsToDate)
						}
					}
					if goalieName != "" {
						evt.GoalieName = goalieName
					} else if info != nil {
						// Fallback only if play-by-play never had this goal (e.g. API issue)
						evt.GoalieName = info.GoalieName
					}
					id, err := producer.EmitGoalEvent(ctx, evt)
					if err != nil {
						slog.Error("emit goal event failed", "error", err, "goals", careerGoals)
						continue
					}
					slog.Info("goal event emitted (live)", "stream_id", id, "goals", careerGoals, "game_id", caps.GameID, "goals_to_date", g.GoalsToDate)
				}
			} else {
				if apiGoals, err := nhlClient.CareerGoals(ctx); err == nil && apiGoals > lastKnownCareerTotal {
					lastKnownCareerTotal = apiGoals
				}
			}
		}
	}
}

func pingRedis(ctx context.Context, rdb *redis.Client) error {
	return rdb.Ping(ctx).Err()
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func getDurationEnv(key string, defaultVal time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return defaultVal
}
