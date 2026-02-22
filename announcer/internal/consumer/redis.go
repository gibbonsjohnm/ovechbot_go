package consumer

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// StreamKey must match the Ingestor stream key.
	StreamKey       = "ovechkin:goals"
	ConsumerGroup   = "announcers"
	ConsumerName    = "announcer-1"
	ReadBlockMillis = 5000
)

// GoalEvent matches the payload emitted by the Ingestor.
type GoalEvent struct {
	PlayerID     int       `json:"player_id"`
	Goals        int       `json:"goals"`
	RecordedAt   time.Time `json:"recorded_at"`
	Opponent     string    `json:"opponent,omitempty"`
	OpponentName string    `json:"opponent_name,omitempty"`
	GoalieName   string    `json:"goalie_name,omitempty"`
}

// Consumer reads from the Redis stream via consumer group.
type Consumer struct {
	client *redis.Client
}

// NewConsumer returns a Redis stream consumer.
func NewConsumer(client *redis.Client) *Consumer {
	return &Consumer{client: client}
}

// EnsureGroup creates the consumer group if it does not exist (MKSTREAM so empty stream is created).
func (c *Consumer) EnsureGroup(ctx context.Context) error {
	return c.client.XGroupCreateMkStream(ctx, StreamKey, ConsumerGroup, "0").Err()
}

// ReadMessages blocks and reads new messages for this consumer; returns payloads and acks.
func (c *Consumer) ReadMessages(ctx context.Context) ([]GoalEvent, []string, error) {
	streams, err := c.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    ConsumerGroup,
		Consumer: ConsumerName,
		Streams:  []string{StreamKey, ">"},
		Count:    10,
		Block:    ReadBlockMillis * time.Millisecond,
	}).Result()
	if err != nil && err != redis.Nil {
		return nil, nil, err
	}
	if err == redis.Nil || len(streams) == 0 || len(streams[0].Messages) == 0 {
		return nil, nil, nil
	}

	var events []GoalEvent
	var ids []string
	for _, msg := range streams[0].Messages {
		ids = append(ids, msg.ID)
		raw, ok := msg.Values["payload"].(string)
		if !ok {
			continue
		}
		var e GoalEvent
		if err := json.Unmarshal([]byte(raw), &e); err != nil {
			continue
		}
		events = append(events, e)
	}
	return events, ids, nil
}

// Ack acknowledges processed message IDs.
func (c *Consumer) Ack(ctx context.Context, ids ...string) error {
	if len(ids) == 0 {
		return nil
	}
	return c.client.XAck(ctx, StreamKey, ConsumerGroup, ids...).Err()
}
