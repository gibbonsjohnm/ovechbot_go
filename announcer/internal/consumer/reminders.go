package consumer

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	RemindersStreamKey = "ovechkin:reminders"
)

// ReminderPayload matches the predictor's reminder payload.
type ReminderPayload struct {
	GameID         int64  `json:"game_id"`
	Opponent       string `json:"opponent"`
	HomeAway       string `json:"home_away"`
	ProbabilityPct int    `json:"probability_pct"`
	StartTimeUTC   string `json:"start_time_utc"`
	GameDate       string `json:"game_date"`
	OddsAmerican   string `json:"odds_american,omitempty"`
	GoalieName     string `json:"goalie_name,omitempty"`
}

// ReminderConsumer reads from the reminders stream.
type ReminderConsumer struct {
	client *redis.Client
}

// NewReminderConsumer returns a consumer for the reminders stream.
func NewReminderConsumer(client *redis.Client) *ReminderConsumer {
	return &ReminderConsumer{client: client}
}

// EnsureReminderGroup creates the consumer group for reminders if needed.
func (c *ReminderConsumer) EnsureReminderGroup(ctx context.Context) error {
	return c.client.XGroupCreateMkStream(ctx, RemindersStreamKey, ConsumerGroup, "0").Err()
}

// ReadReminders blocks and reads reminder messages; returns payloads and message IDs.
func (c *ReminderConsumer) ReadReminders(ctx context.Context) ([]ReminderPayload, []string, error) {
	streams, err := c.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    ConsumerGroup,
		Consumer: ConsumerName,
		Streams:  []string{RemindersStreamKey, ">"},
		Count:    10,
		Block:    ReadBlockMillis * time.Millisecond,
	}).Result()
	if err != nil && err != redis.Nil {
		return nil, nil, err
	}
	if err == redis.Nil || len(streams) == 0 || len(streams[0].Messages) == 0 {
		return nil, nil, nil
	}
	var out []ReminderPayload
	var ids []string
	for _, msg := range streams[0].Messages {
		ids = append(ids, msg.ID)
		raw, _ := msg.Values["payload"].(string)
		var p ReminderPayload
		_ = json.Unmarshal([]byte(raw), &p)
		out = append(out, p)
	}
	return out, ids, nil
}

// AckReminders acknowledges processed reminder message IDs.
func (c *ReminderConsumer) AckReminders(ctx context.Context, ids ...string) error {
	if len(ids) == 0 {
		return nil
	}
	return c.client.XAck(ctx, RemindersStreamKey, ConsumerGroup, ids...).Err()
}
