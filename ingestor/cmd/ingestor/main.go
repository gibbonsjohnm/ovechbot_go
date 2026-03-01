package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"sync"
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

	// seenGoals: keys "gameID:goalsToDate" for Ovechkin goals we already emitted (real-time path)
	var seenMu sync.Mutex
	seenGoals := make(map[string]struct{})
	lastLiveGameID := 0
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
				// No Capitals game in score window; clear seen set and sync career total from API
				seenMu.Lock()
				if lastLiveGameID != 0 {
					lastLiveGameID = 0
					seenGoals = make(map[string]struct{})
				}
				seenMu.Unlock()
				if apiGoals, err := nhlClient.CareerGoals(ctx); err == nil && apiGoals > lastKnownCareerTotal {
					lastKnownCareerTotal = apiGoals
				}
				continue
			}

			if nhl.LiveGameStates[caps.GameState] {
				lastLiveGameID = caps.GameID
				for _, g := range caps.Goals {
					if g.PlayerID != nhl.OvechkinPlayerID {
						continue
					}
					key := fmt.Sprintf("%d:%d", caps.GameID, g.GoalsToDate)
					seenMu.Lock()
					if _, ok := seenGoals[key]; ok {
						seenMu.Unlock()
						continue
					}
					seenGoals[key] = struct{}{}
					seenMu.Unlock()

					// Add this goal to career total for the announcement (don't rely on API which may lag)
					lastKnownCareerTotal++
					careerGoals := lastKnownCareerTotal
					evt := stream.GoalEvent{PlayerID: nhl.OvechkinPlayerID, Goals: careerGoals}
					info, _ := nhlClient.GoalGameInfo(ctx, caps.GameID)
					if info != nil {
						evt.Opponent = info.Opponent
						evt.OpponentName = info.OpponentName
					}
					// Use play-by-play for the goalie actually in net for this goal (not boxscore starter)
					if name := nhlClient.GoalieForGoal(ctx, caps.GameID, nhl.OvechkinPlayerID, g.GoalsToDate); name != "" {
						evt.GoalieName = name
					} else if info != nil {
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
				// Game no longer live; clear seen set and sync career total from API for next game
				seenMu.Lock()
				if lastLiveGameID != 0 && lastLiveGameID == caps.GameID {
					lastLiveGameID = 0
					seenGoals = make(map[string]struct{})
				}
				seenMu.Unlock()
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
