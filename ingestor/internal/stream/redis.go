package stream

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// StreamKey is the Redis stream key for Ovechkin goal events.
	StreamKey = "ovechkin:goals"
	// SeenGoalsKeyPrefix is the Redis SET key prefix for goals already emitted per game: "ovechkin:seen_goals:{gameID}".
	SeenGoalsKeyPrefix = "ovechkin:seen_goals:"
	seenGoalsTTL      = 7 * 24 * time.Hour
)

// GoalEvent is the payload emitted when the goal count increases.
type GoalEvent struct {
	PlayerID     int       `json:"player_id"`
	Goals        int       `json:"goals"`
	RecordedAt   time.Time `json:"recorded_at"`
	Opponent     string    `json:"opponent,omitempty"`      // e.g. "NSH"
	OpponentName string    `json:"opponent_name,omitempty"` // e.g. "Predators"
	GoalieName   string    `json:"goalie_name,omitempty"` // goalie scored on
}

// Producer writes goal events to a Redis stream.
type Producer struct {
	client *redis.Client
}

// NewProducer returns a Redis stream producer.
func NewProducer(client *redis.Client) *Producer {
	return &Producer{client: client}
}

// EmitGoalEvent adds a goal event to the stream.
func (p *Producer) EmitGoalEvent(ctx context.Context, e GoalEvent) (string, error) {
	e.RecordedAt = time.Now().UTC()
	body, err := json.Marshal(e)
	if err != nil {
		return "", fmt.Errorf("marshal event: %w", err)
	}

	id, err := p.client.XAdd(ctx, &redis.XAddArgs{
		Stream: StreamKey,
		Values: map[string]interface{}{
			"payload": string(body),
			"goals":   e.Goals,
		},
	}).Result()
	if err != nil {
		return "", fmt.Errorf("xadd: %w", err)
	}
	return id, nil
}

// MarkGoalSeen records that we have emitted an event for this goal (gameID + goalsToDate).
// It returns true if the goal was already seen (duplicate), false if this is the first time (should emit).
// Uses a Redis SET per game with TTL so restarts and multiple ingestors share state.
func (p *Producer) MarkGoalSeen(ctx context.Context, gameID, goalsToDate int) (alreadySeen bool, err error) {
	key := SeenGoalsKeyPrefix + strconv.Itoa(gameID)
	member := strconv.Itoa(goalsToDate)
	added, err := p.client.SAdd(ctx, key, member).Result()
	if err != nil {
		return false, fmt.Errorf("sadd seen goal: %w", err)
	}
	if added == 0 {
		return true, nil
	}
	if err := p.client.Expire(ctx, key, seenGoalsTTL).Err(); err != nil {
		// Non-fatal: key will persist; we still marked the goal
		return false, nil
	}
	return false, nil
}
