package stream

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestEmitGoalEvent_Success(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	ctx := context.Background()
	producer := NewProducer(rdb)

	evt := GoalEvent{PlayerID: 8471214, Goals: 920}
	id, err := producer.EmitGoalEvent(ctx, evt)
	if err != nil {
		t.Fatalf("EmitGoalEvent: %v", err)
	}
	if id == "" {
		t.Error("expected non-empty stream ID")
	}

	// Verify stream has one entry with correct payload
	entries, err := rdb.XRange(ctx, StreamKey, "-", "+").Result()
	if err != nil {
		t.Fatalf("XRange: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d; want 1", len(entries))
	}
	payload, ok := entries[0].Values["payload"].(string)
	if !ok {
		t.Fatal("payload not string")
	}
	var got GoalEvent
	if err := json.Unmarshal([]byte(payload), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Goals != 920 || got.PlayerID != 8471214 {
		t.Errorf("got %+v", got)
	}
	if got.RecordedAt.IsZero() {
		t.Error("RecordedAt should be set")
	}
	_ = id
}

func TestEmitGoalEvent_Multiple(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	ctx := context.Background()
	producer := NewProducer(rdb)

	for i := 1; i <= 3; i++ {
		_, err := producer.EmitGoalEvent(ctx, GoalEvent{PlayerID: 8471214, Goals: 919 + i})
		if err != nil {
			t.Fatalf("EmitGoalEvent %d: %v", i, err)
		}
	}

	n, err := rdb.XLen(ctx, StreamKey).Result()
	if err != nil {
		t.Fatalf("XLen: %v", err)
	}
	if n != 3 {
		t.Errorf("XLen = %d; want 3", n)
	}
}

func TestNewProducer(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"}) // not connected
	p := NewProducer(rdb)
	if p == nil || p.client != rdb {
		t.Error("NewProducer failed")
	}
}
