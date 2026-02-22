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
	pollInterval := getDurationEnv("POLL_INTERVAL", 60*time.Second)

	rdb := redis.NewClient(&redis.Options{Addr: redisAddr})
	defer rdb.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	nhlClient := nhl.NewClient()
	producer := stream.NewProducer(rdb)

	var lastGoals int
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	// Initial poll to set baseline
	if err := pingRedis(ctx, rdb); err != nil {
		slog.Error("redis ping failed", "error", err)
		os.Exit(1)
	}
	goals, err := nhlClient.CareerGoals(ctx)
	if err != nil {
		slog.Error("initial nhl fetch failed", "error", err)
		os.Exit(1)
	}
	lastGoals = goals
	slog.Info("ingestor started", "stream", stream.StreamKey, "current_goals", goals)

	for {
		select {
		case <-ctx.Done():
			slog.Info("shutting down ingestor", "reason", ctx.Err())
			return
		case <-ticker.C:
			goals, err := nhlClient.CareerGoals(ctx)
			if err != nil {
				slog.Warn("nhl fetch failed", "error", err)
				continue
			}
			if goals > lastGoals {
				evt := stream.GoalEvent{PlayerID: nhl.OvechkinPlayerID, Goals: goals}
				if info, err := nhlClient.LastGoalGameInfo(ctx); err == nil && info != nil {
					evt.Opponent = info.Opponent
					evt.OpponentName = info.OpponentName
					evt.GoalieName = info.GoalieName
				}
				id, err := producer.EmitGoalEvent(ctx, evt)
				if err != nil {
					slog.Error("emit goal event failed", "error", err, "goals", goals)
					continue
				}
				slog.Info("goal event emitted", "stream_id", id, "goals", goals, "previous", lastGoals)
				lastGoals = goals
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
