package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"ovechbot_go/predictor/internal/cache"
	"ovechbot_go/predictor/internal/goalie"
	"ovechbot_go/predictor/internal/model"
	"ovechbot_go/predictor/internal/odds"
	"ovechbot_go/predictor/internal/reminder"
	"ovechbot_go/predictor/internal/schedule"

	"github.com/redis/go-redis/v9"
)

const (
	checkInterval       = 10 * time.Minute
	reminderWindow      = 55 * time.Minute // send reminder when game is in 55-65 min
	reminderWindowEnd   = 65 * time.Minute
	oddsFetchWindow     = 36 * time.Hour   // only call Odds API when game is within 36h (saves credits)
	oddsCacheTTL        = 12 * time.Hour   // cache odds per game_id so we don't refetch every tick
	oddsCacheKeyPrefix  = "ovechkin:odds:"
	calibrationLogKey   = "ovechkin:calibration:log"
	calibrationMinGames = 10
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	redisAddr := getEnv("REDIS_ADDR", "redis:6379")
	rdb := redis.NewClient(&redis.Options{Addr: redisAddr})
	defer rdb.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := rdb.Ping(ctx).Err(); err != nil {
		slog.Error("redis ping failed", "error", err)
		os.Exit(1)
	}

	reader := cache.NewReader(rdb)
	producer := reminder.NewProducer(rdb)
	oddsClient := odds.NewClient(getEnv("ODDS_API_KEY", ""))
	goalieClient := goalie.NewClient()

	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	run := func() {
		// 2m so we have time for a 1m retry wait when game log is empty at startup
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		slog.Info("predictor tick", "action", "fetch_next_game")
		g, err := schedule.NextGame(ctx)
		if err != nil {
			slog.Warn("next game fetch failed", "error", err)
			return
		}
		if g == nil {
			slog.Info("no upcoming game", "message", "schedule empty or season not active")
			return
		}
		until := time.Until(g.StartTimeUTC)
		slog.Info("next game", "game_id", g.GameID, "opponent", g.Opponent(), "home", g.IsHome(), "start_utc", g.StartTimeUTC.Format(time.RFC3339), "until_kickoff", until.Round(time.Minute).String())

		gameLog, err := reader.ReadGameLog(ctx)
		if err != nil {
			slog.Warn("game log read failed", "error", err)
			return
		}
		if len(gameLog) == 0 {
			slog.Info("game log empty, retrying once in 1m in case collector is still filling at startup")
			select {
			case <-ctx.Done():
				return
			case <-time.After(1 * time.Minute):
			}
			retryCtx, retryCancel := context.WithTimeout(context.Background(), 15*time.Second)
			gameLog, err = reader.ReadGameLog(retryCtx)
			retryCancel()
			if err != nil || len(gameLog) == 0 {
				slog.Info("game log still empty after retry, skipping prediction until next tick")
				return
			}
		}
		standings, errStand := reader.ReadStandings(ctx)
		standingsOk := errStand == nil && len(standings) > 0
		slog.Info("data loaded", "game_log_entries", len(gameLog), "standings_loaded", standingsOk)

		goalieSavePct := 0.0
		goalieName := ""
		slog.Info("goalie: fetching opposing starter", "game_id", g.GameID)
		if gi, err := goalieClient.OpposingStarter(ctx, g); err != nil {
			slog.Warn("goalie: fetch failed", "game_id", g.GameID, "error", err)
		} else if gi == nil {
			slog.Info("goalie: none found", "game_id", g.GameID, "hint", "boxscore not yet published or no goalies in lineup")
		} else {
			goalieName = gi.Name
			goalieSavePct = gi.SavePct
			if goalieSavePct > 0 {
				slog.Info("goalie: found, applying strength factor", "game_id", g.GameID, "name", gi.Name, "save_pct", gi.SavePct)
			} else {
				slog.Info("goalie: found (no season SV%), using name only", "game_id", g.GameID, "name", gi.Name)
			}
		}

		pct := model.Predict(g, gameLog, standings, goalieSavePct)
		slog.Info("prediction", "probability_pct", pct, "game_id", g.GameID)

		// Odds: use cache when possible; only call API when game is within 36h (500 credits/month limit).
		oddsAmerican := ""
		oddsKey := oddsCacheKeyPrefix + strconv.FormatInt(g.GameID, 10)
		if cached, _ := rdb.Get(ctx, oddsKey).Result(); cached != "" {
			oddsAmerican = cached
		} else if until <= oddsFetchWindow && getEnv("ODDS_API_KEY", "") != "" {
			if o, err := oddsClient.OvechkinAnytimeGoal(ctx, g); err != nil {
				slog.Warn("odds fetch failed", "error", err)
			} else if o != nil {
				oddsAmerican = o.American
				_ = rdb.Set(ctx, oddsKey, o.American, oddsCacheTTL).Err()
				slog.Info("odds", "anytime_goal_american", o.American, "game_id", g.GameID)
			} else {
				slog.Info("odds not found for this game", "game_id", g.GameID, "hint", "no matching event or Ovechkin line in player_goal_scorer_anytime")
			}
		}

		// Blend with market implied probability when odds available (85% model, 15% market).
		if oddsAmerican != "" {
			if implied, ok := odds.ImpliedPctFromAmerican(oddsAmerican); ok && implied > 0 {
				blended := int(0.85*float64(pct) + 0.15*float64(implied) + 0.5)
				if blended < 15 {
					blended = 15
				}
				if blended > 75 {
					blended = 75
				}
				slog.Info("prediction blended with market", "model_pct", pct, "implied_pct", implied, "final_pct", blended)
				pct = blended
			}
		}

		// Apply calibration scale from evaluator history (hit rate vs mean predicted prob).
		if scale := calibrationScale(ctx, rdb); scale != 1.0 {
			calibrated := int(float64(pct)*scale + 0.5)
			if calibrated < 15 {
				calibrated = 15
			}
			if calibrated > 75 {
				calibrated = 75
			}
			slog.Info("prediction calibrated", "before", pct, "scale", scale, "after", calibrated)
			pct = calibrated
		}

		if err := producer.WriteNextPrediction(ctx, g, pct, oddsAmerican, goalieName); err != nil {
			slog.Warn("write next prediction failed", "error", err)
		} else {
			slog.Info("next_prediction written", "game_id", g.GameID, "probability_pct", pct, "odds_american", oddsAmerican)
		}

		// Send reminder only when game is in 55–65 min window and not already sent
		if until < reminderWindow || until > reminderWindowEnd {
			slog.Info("reminder skip", "reason", "outside_window", "until_kickoff", until.Round(time.Minute).String(), "window", "55m-65m")
			return
		}
		sent, err := producer.AlreadySent(ctx, g.GameID)
		if err != nil {
			slog.Warn("reminder already-sent check failed", "error", err)
			return
		}
		if sent {
			slog.Info("reminder skip", "reason", "already_sent", "game_id", g.GameID)
			return
		}
		if err := producer.Publish(ctx, g, pct, oddsAmerican, goalieName); err != nil {
			slog.Warn("publish reminder failed", "error", err)
			return
		}
		slog.Info("reminder published", "game_id", g.GameID, "opponent", g.Opponent(), "probability_pct", pct)
	}

	for {
		run()
		select {
		case <-ctx.Done():
			slog.Info("predictor shutting down", "reason", ctx.Err())
			return
		case <-ticker.C:
			// loop
		}
	}
}

// calibrationScale reads evaluator history from Redis and returns scale = hit_rate / mean_predicted_prob (capped 0.8–1.2). Returns 1.0 if not enough data.
func calibrationScale(ctx context.Context, rdb *redis.Client) float64 {
	entries, err := rdb.LRange(ctx, calibrationLogKey, 0, 99).Result()
	if err != nil || len(entries) < calibrationMinGames {
		return 1.0
	}
	var sumScored int
	var sumPredProb float64
	for _, s := range entries {
		var e struct {
			PredPct int `json:"pred_pct"`
			Scored  int `json:"scored"`
		}
		if json.Unmarshal([]byte(s), &e) != nil {
			continue
		}
		sumScored += e.Scored
		sumPredProb += float64(e.PredPct) / 100
	}
	if sumPredProb <= 0 {
		return 1.0
	}
	hitRate := float64(sumScored) / float64(len(entries))
	meanPred := sumPredProb / float64(len(entries))
	scale := hitRate / meanPred
	if scale < 0.8 {
		scale = 0.8
	}
	if scale > 1.2 {
		scale = 1.2
	}
	return scale
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
