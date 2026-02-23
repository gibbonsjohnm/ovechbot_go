package reminder

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"ovechbot_go/predictor/internal/schedule"

	"github.com/redis/go-redis/v9"
)

const (
	StreamKey                   = "ovechkin:reminders"
	SentKeyPrefix               = "reminder_sent:"
	SentKeyTTL                  = 25 * time.Hour
	NextPredictionKey           = "ovechkin:next_prediction"
	NextPredictionTTL           = 1 * time.Hour
	PredictionSnapshotKeyPrefix = "ovechkin:prediction_snapshot:"
	PredictionSnapshotTTL       = 7 * 24 * time.Hour
)

// Payload is the reminder message for the announcer.
type Payload struct {
	GameID         int64  `json:"game_id"`
	Opponent       string `json:"opponent"`
	HomeAway       string `json:"home_away"`
	ProbabilityPct int    `json:"probability_pct"`
	StartTimeUTC   string `json:"start_time_utc"`
	GameDate       string `json:"game_date"`
	// OddsAmerican is Ovechkin anytime goal scorer (e.g. "+140"). Optional.
	OddsAmerican string `json:"odds_american,omitempty"`
	// GoalieName is the opposing starter (e.g. "S. Ersson"). Optional; may be empty until lineup is published.
	GoalieName string `json:"goalie_name,omitempty"`
}

// Producer writes reminders to Redis stream and marks games sent.
type Producer struct {
	client *redis.Client
}

// NewProducer returns a reminder producer.
func NewProducer(client *redis.Client) *Producer {
	return &Producer{client: client}
}

// AlreadySent returns true if we already sent a reminder for this game.
func (p *Producer) AlreadySent(ctx context.Context, gameID int64) (bool, error) {
	key := SentKeyPrefix + strconv.FormatInt(gameID, 10)
	_, err := p.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return false, nil
	}
	return err == nil, err
}

// Publish writes a reminder to the stream and marks the game as sent.
func (p *Producer) Publish(ctx context.Context, g *schedule.Game, probabilityPct int, oddsAmerican, goalieName string) error {
	homeAway := "AWAY"
	if g.IsHome() {
		homeAway = "HOME"
	}
	payload := Payload{
		GameID:         g.GameID,
		Opponent:       g.Opponent(),
		HomeAway:       homeAway,
		ProbabilityPct: probabilityPct,
		StartTimeUTC:   g.StartTimeUTC.Format(time.RFC3339),
		GameDate:       g.GameDate,
		OddsAmerican:   oddsAmerican,
		GoalieName:     goalieName,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal reminder: %w", err)
	}
	_, err = p.client.XAdd(ctx, &redis.XAddArgs{
		Stream: StreamKey,
		Values: map[string]interface{}{"payload": string(body), "game_id": g.GameID},
	}).Result()
	if err != nil {
		return err
	}
	return p.client.Set(ctx, SentKeyPrefix+strconv.FormatInt(g.GameID, 10), "1", SentKeyTTL).Err()
}

// WriteNextPrediction stores the current next-game prediction so /nextgame can display it.
func (p *Producer) WriteNextPrediction(ctx context.Context, g *schedule.Game, probabilityPct int, oddsAmerican, goalieName string) error {
	payload := Payload{
		GameID:         g.GameID,
		Opponent:       g.Opponent(),
		HomeAway:       "AWAY",
		ProbabilityPct: probabilityPct,
		StartTimeUTC:   g.StartTimeUTC.Format(time.RFC3339),
		GameDate:       g.GameDate,
		OddsAmerican:   oddsAmerican,
		GoalieName:     goalieName,
	}
	if g.IsHome() {
		payload.HomeAway = "HOME"
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if err := p.client.Set(ctx, NextPredictionKey, string(body), NextPredictionTTL).Err(); err != nil {
		return err
	}
	// Snapshot for evaluator backtesting (same payload, longer TTL).
	snapshotKey := PredictionSnapshotKeyPrefix + strconv.FormatInt(g.GameID, 10)
	return p.client.Set(ctx, snapshotKey, string(body), PredictionSnapshotTTL).Err()
}
