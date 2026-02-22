package stream

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// StreamKey is the Redis stream key for Ovechkin goal events.
	StreamKey = "ovechkin:goals"
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
