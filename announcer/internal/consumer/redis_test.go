package consumer

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestEnsureGroup(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	ctx := context.Background()
	c := NewConsumer(rdb)

	err = c.EnsureGroup(ctx)
	if err != nil {
		t.Fatalf("EnsureGroup: %v", err)
	}

	// Second call should error with BUSYGROUP (group already exists)
	err = c.EnsureGroup(ctx)
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		t.Errorf("second EnsureGroup: %v", err)
	}
}

func TestReadMessages_And_Ack(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	ctx := context.Background()
	c := NewConsumer(rdb)

	if err := c.EnsureGroup(ctx); err != nil {
		t.Fatalf("EnsureGroup: %v", err)
	}

	// Add a message to the stream (simulating Ingestor)
	evt := GoalEvent{PlayerID: 8471214, Goals: 921, RecordedAt: time.Now().UTC()}
	payload, _ := json.Marshal(evt)
	_, err = rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: StreamKey,
		Values: map[string]interface{}{"payload": string(payload), "goals": evt.Goals},
	}).Result()
	if err != nil {
		t.Fatalf("XAdd: %v", err)
	}

	// Read with short block so test doesn't hang (miniredis may not block like real Redis)
	// Use a goroutine and timeout to call ReadMessages
	type result struct {
		events []GoalEvent
		ids    []string
		err    error
	}
	done := make(chan result, 1)
	readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	go func() {
		events, ids, err := c.ReadMessages(readCtx)
		done <- result{events, ids, err}
	}()

	select {
	case res := <-done:
		if res.err != nil {
			t.Fatalf("ReadMessages: %v", res.err)
		}
		if len(res.events) != 1 {
			t.Fatalf("len(events) = %d; want 1", len(res.events))
		}
		if res.events[0].Goals != 921 || res.events[0].PlayerID != 8471214 {
			t.Errorf("event = %+v", res.events[0])
		}
		if len(res.ids) != 1 {
			t.Fatalf("len(ids) = %d; want 1", len(res.ids))
		}
		if err := c.Ack(ctx, res.ids...); err != nil {
			t.Fatalf("Ack: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("ReadMessages timed out")
	}
}

func TestReadMessages_Empty(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	c := NewConsumer(rdb)
	if err := c.EnsureGroup(ctx); err != nil {
		t.Fatalf("EnsureGroup: %v", err)
	}

	// No messages in stream - ReadMessages may block then return on context cancel, or return (nil,nil,nil)
	events, ids, err := c.ReadMessages(ctx)
	if err != nil && err != context.DeadlineExceeded {
		t.Fatalf("ReadMessages: %v", err)
	}
	if len(events) != 0 || len(ids) != 0 {
		t.Errorf("events=%d ids=%d; want both 0", len(events), len(ids))
	}
}

func TestAck_Empty(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	ctx := context.Background()
	c := NewConsumer(rdb)

	err = c.Ack(ctx)
	if err != nil {
		t.Errorf("Ack() with no ids should be no-op: %v", err)
	}
}

func TestNewConsumer(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	c := NewConsumer(rdb)
	if c == nil || c.client != rdb {
		t.Error("NewConsumer failed")
	}
}
