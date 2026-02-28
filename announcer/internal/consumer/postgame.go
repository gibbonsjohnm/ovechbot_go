package consumer

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	PostGameStreamKey = "ovechkin:post_game"
)

// PostGamePayload is the message body for post-game evaluation (evaluator → announcer).
type PostGamePayload struct {
	Message string `json:"message"`
}

// PostGameConsumer reads from the post-game stream.
type PostGameConsumer struct {
	client *redis.Client
}

// NewPostGameConsumer returns a consumer for the post-game stream.
func NewPostGameConsumer(client *redis.Client) *PostGameConsumer {
	return &PostGameConsumer{client: client}
}

// EnsurePostGameGroup creates the consumer group for post-game if needed.
func (c *PostGameConsumer) EnsurePostGameGroup(ctx context.Context) error {
	return c.client.XGroupCreateMkStream(ctx, PostGameStreamKey, ConsumerGroup, "0").Err()
}

// ReadPostGames blocks and reads post-game messages; returns payloads and message IDs.
func (c *PostGameConsumer) ReadPostGames(ctx context.Context) ([]PostGamePayload, []string, error) {
	streams, err := c.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    ConsumerGroup,
		Consumer: ConsumerName,
		Streams:  []string{PostGameStreamKey, ">"},
		Count:    10,
		Block:    ReadBlockMillis * time.Millisecond,
	}).Result()
	if err != nil && err != redis.Nil {
		return nil, nil, err
	}
	if err == redis.Nil || len(streams) == 0 || len(streams[0].Messages) == 0 {
		return nil, nil, nil
	}
	var out []PostGamePayload
	var ids []string
	for _, msg := range streams[0].Messages {
		ids = append(ids, msg.ID)
		raw, ok := msg.Values["payload"].(string)
		if !ok {
			slog.Warn("post-game consumer: invalid payload type, skipping", "msg_id", msg.ID)
			continue
		}
		var p PostGamePayload
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			slog.Warn("post-game consumer: unmarshal failed, skipping", "msg_id", msg.ID, "error", err)
			continue
		}
		out = append(out, p)
	}
	return out, ids, nil
}

// AckPostGames acknowledges processed post-game message IDs.
func (c *PostGameConsumer) AckPostGames(ctx context.Context, ids ...string) error {
	if len(ids) == 0 {
		return nil
	}
	return c.client.XAck(ctx, PostGameStreamKey, ConsumerGroup, ids...).Err()
}
