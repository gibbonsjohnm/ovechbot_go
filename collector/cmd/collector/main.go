package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"ovechbot_go/collector/internal/cache"
	"ovechbot_go/collector/internal/nhl"

	"github.com/redis/go-redis/v9"
)

// Seasons to fetch for Ovechkin game log (startYear+endYear format).
var gameLogSeasons = []string{"20232024", "20242025", "20252026"}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	redisAddr := getEnv("REDIS_ADDR", "redis:6379")
	interval := getEnv("COLLECTOR_INTERVAL", "6h")
	collectInterval, err := time.ParseDuration(interval)
	if err != nil {
		collectInterval = 6 * time.Hour
	}

	rdb := redis.NewClient(&redis.Options{Addr: redisAddr})
	defer rdb.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := rdb.Ping(ctx).Err(); err != nil {
		slog.Error("redis ping failed", "error", err)
		os.Exit(1)
	}

	nhlClient := nhl.NewClient()
	c := cache.New(rdb)

	run := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		var allLog []nhl.GameLogEntry
		for _, seasonID := range gameLogSeasons {
			entries, err := nhlClient.GameLog(ctx, seasonID)
			if err != nil {
				slog.Warn("game log fetch failed", "season", seasonID, "error", err)
				continue
			}
			allLog = append(allLog, entries...)
		}
		if len(allLog) > 0 {
			if err := c.WriteGameLog(ctx, allLog); err != nil {
				slog.Warn("write game log failed", "error", err)
			} else {
				slog.Info("game log updated", "entries", len(allLog))
			}
		}

		standings, err := nhlClient.Standings(ctx)
		if err != nil {
			slog.Warn("standings fetch failed", "error", err)
			return
		}
		if err := c.WriteStandings(ctx, standings); err != nil {
			slog.Warn("write standings failed", "error", err)
		} else {
			slog.Info("standings updated", "teams", len(standings))
		}
	}

	run()
	ticker := time.NewTicker(collectInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Info("collector shutting down", "reason", ctx.Err())
			return
		case <-ticker.C:
			run()
		}
	}
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
