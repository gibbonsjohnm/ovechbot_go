package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"ovechbot_go/evaluator/internal/nhl"

	"github.com/redis/go-redis/v9"
)

const (
	gameLogKey               = "ovechkin:game_log"
	predictionSnapshotPrefix = "ovechkin:prediction_snapshot:"
	lastReportedKey          = "ovechkin:evaluator_last_reported_game"
	postGameStreamKey        = "ovechkin:post_game" // announcer consumes this and posts to Discord
	checkInterval            = 30 * time.Minute
	evaluatorRunTimeout      = 90 * time.Second
)

type predictionSnapshot struct {
	GameID         int64  `json:"game_id"`
	ProbabilityPct int    `json:"probability_pct"`
	OddsAmerican  string `json:"odds_american,omitempty"`
	GoalieName    string `json:"goalie_name,omitempty"`
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	redisAddr := getEnv("REDIS_ADDR", "redis:6379")
	rdb := redis.NewClient(&redis.Options{Addr: redisAddr})
	defer rdb.Close()

	for {
		run(rdb)
		select {
		case <-time.After(checkInterval):
			// loop again
		}
	}
}

// run checks for the most recent completed Caps game (state OFF), fetches boxscore
// and prediction data, and publishes exactly one post-game message per game to Redis.
// The announcer consumes from ovechkin:post_game and posts to Discord. last_reported
// is updated only after a successful publish so we never send repeatedly for the same game.
func run(rdb *redis.Client) {
	ctx, cancel := context.WithTimeout(context.Background(), evaluatorRunTimeout)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		slog.Warn("evaluator: redis ping failed", "error", err)
		return
	}

	// Only consider games that have ended (schedule shows OFF).
	game, err := nhl.LastCompletedGame(ctx)
	if err != nil {
		slog.Warn("evaluator: last completed game failed", "error", err)
		return
	}
	if game == nil {
		slog.Debug("evaluator: no completed game")
		return
	}

	lastReported, _ := rdb.Get(ctx, lastReportedKey).Int64()
	if lastReported >= game.GameID {
		slog.Debug("evaluator: already reported for game", "game_id", game.GameID)
		return
	}

	snapBytes, err := rdb.Get(ctx, predictionSnapshotPrefix+strconv.FormatInt(game.GameID, 10)).Bytes()
	var predPct int
	var odds, goalie string
	if err == nil {
		var snap predictionSnapshot
		_ = json.Unmarshal(snapBytes, &snap)
		predPct = snap.ProbabilityPct
		odds = snap.OddsAmerican
		goalie = snap.GoalieName
	}

	stats, err := nhl.OvechkinGameStats(ctx, game.GameID)
	if err != nil {
		slog.Warn("evaluator: boxscore failed", "game_id", game.GameID, "error", err)
		return
	}
	if stats == nil {
		slog.Warn("evaluator: Ovechkin not in boxscore", "game_id", game.GameID)
		return
	}

	// Hit = (we said >=50% and he scored) or (we said <50% and he didn't)
	scored := stats.Goals > 0
	hit := (predPct >= 50 && scored) || (predPct < 50 && !scored)
	result := "Miss"
	if hit {
		result = "Hit"
	}

	actualStr := "no goal"
	if scored {
		actualStr = "scored"
	}

	msg := fmt.Sprintf("📊 **Post-game evaluation** · %s vs **%s**\n", game.GameDate, game.OpponentAbbrev)
	msg += fmt.Sprintf("**Ovi:** %dG, %dA, %d PTS · TOI %s · %d shifts · %d SOG\n",
		stats.Goals, stats.Assists, stats.Points, stats.TOI, stats.Shifts, stats.SOG)
	if predPct > 0 {
		msg += fmt.Sprintf("**Prediction:** %d%% · Actual: %s · **%s**", predPct, actualStr, result)
		if odds != "" {
			msg += fmt.Sprintf(" · Odds had: %s", odds)
		}
		if goalie != "" {
			msg += fmt.Sprintf(" · Goalie: %s", goalie)
		}
		msg += "\n"
	} else {
		msg += "_(No prediction snapshot for this game)_\n"
	}

	slog.Info("evaluator: publishing post-game summary", "game_id", game.GameID, "result", result)

	payload, _ := json.Marshal(struct{ Message string }{Message: msg})
	if err := rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: postGameStreamKey,
		Values: map[string]any{"payload": string(payload)},
	}).Err(); err != nil {
		slog.Warn("evaluator: publish to post_game stream failed", "error", err)
		return
	}
	// Only mark as reported after a successful publish so we send exactly once per game.
	if err := rdb.Set(ctx, lastReportedKey, game.GameID, 30*24*time.Hour).Err(); err != nil {
		slog.Warn("evaluator: set last reported failed", "error", err)
	}
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
