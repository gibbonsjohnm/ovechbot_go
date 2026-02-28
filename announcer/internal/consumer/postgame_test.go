package consumer

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newMiniRedisClient(t *testing.T) (*redis.Client, func()) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return rdb, func() {
		rdb.Close()
		mr.Close()
	}
}

func TestEnsurePostGameGroup(t *testing.T) {
	rdb, cleanup := newMiniRedisClient(t)
	defer cleanup()

	ctx := context.Background()
	c := NewPostGameConsumer(rdb)

	if err := c.EnsurePostGameGroup(ctx); err != nil {
		t.Fatalf("EnsurePostGameGroup: %v", err)
	}

	// Second call should return BUSYGROUP
	err := c.EnsurePostGameGroup(ctx)
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		t.Errorf("second EnsurePostGameGroup: %v", err)
	}
}

func TestReadPostGames_Valid(t *testing.T) {
	rdb, cleanup := newMiniRedisClient(t)
	defer cleanup()

	ctx := context.Background()
	c := NewPostGameConsumer(rdb)
	if err := c.EnsurePostGameGroup(ctx); err != nil {
		t.Fatalf("EnsurePostGameGroup: %v", err)
	}

	p := PostGamePayload{Message: "Game summary: Ovi scored!"}
	raw, _ := json.Marshal(p)
	if _, err := rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: PostGameStreamKey,
		Values: map[string]interface{}{"payload": string(raw)},
	}).Result(); err != nil {
		t.Fatalf("XAdd: %v", err)
	}

	type result struct {
		payloads []PostGamePayload
		ids      []string
		err      error
	}
	done := make(chan result, 1)
	readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	go func() {
		payloads, ids, err := c.ReadPostGames(readCtx)
		done <- result{payloads, ids, err}
	}()

	select {
	case res := <-done:
		if res.err != nil {
			t.Fatalf("ReadPostGames: %v", res.err)
		}
		if len(res.payloads) != 1 {
			t.Fatalf("len(payloads) = %d; want 1", len(res.payloads))
		}
		if res.payloads[0].Message != p.Message {
			t.Errorf("message = %q; want %q", res.payloads[0].Message, p.Message)
		}
		if len(res.ids) != 1 {
			t.Fatalf("len(ids) = %d; want 1", len(res.ids))
		}
		if err := c.AckPostGames(ctx, res.ids...); err != nil {
			t.Fatalf("AckPostGames: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("ReadPostGames timed out")
	}
}

func TestReadPostGames_MissingPayloadKey(t *testing.T) {
	rdb, cleanup := newMiniRedisClient(t)
	defer cleanup()

	ctx := context.Background()
	c := NewPostGameConsumer(rdb)
	if err := c.EnsurePostGameGroup(ctx); err != nil {
		t.Fatalf("EnsurePostGameGroup: %v", err)
	}

	// Message with wrong key name — type assertion will fail, message skipped
	if _, err := rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: PostGameStreamKey,
		Values: map[string]interface{}{"wrong_key": "some value"},
	}).Result(); err != nil {
		t.Fatalf("XAdd: %v", err)
	}

	type result struct {
		payloads []PostGamePayload
		ids      []string
		err      error
	}
	done := make(chan result, 1)
	readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	go func() {
		payloads, ids, err := c.ReadPostGames(readCtx)
		done <- result{payloads, ids, err}
	}()

	select {
	case res := <-done:
		if res.err != nil {
			t.Fatalf("ReadPostGames: %v", res.err)
		}
		// ID should still be returned even if payload was skipped
		if len(res.ids) != 1 {
			t.Fatalf("len(ids) = %d; want 1", len(res.ids))
		}
		// No payloads decoded
		if len(res.payloads) != 0 {
			t.Errorf("len(payloads) = %d; want 0", len(res.payloads))
		}
	case <-time.After(3 * time.Second):
		t.Fatal("ReadPostGames timed out")
	}
}

func TestReadPostGames_InvalidJSON(t *testing.T) {
	rdb, cleanup := newMiniRedisClient(t)
	defer cleanup()

	ctx := context.Background()
	c := NewPostGameConsumer(rdb)
	if err := c.EnsurePostGameGroup(ctx); err != nil {
		t.Fatalf("EnsurePostGameGroup: %v", err)
	}

	if _, err := rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: PostGameStreamKey,
		Values: map[string]interface{}{"payload": "{bad json"},
	}).Result(); err != nil {
		t.Fatalf("XAdd: %v", err)
	}

	type result struct {
		payloads []PostGamePayload
		ids      []string
		err      error
	}
	done := make(chan result, 1)
	readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	go func() {
		payloads, ids, err := c.ReadPostGames(readCtx)
		done <- result{payloads, ids, err}
	}()

	select {
	case res := <-done:
		if res.err != nil {
			t.Fatalf("ReadPostGames: %v", res.err)
		}
		if len(res.ids) != 1 {
			t.Fatalf("len(ids) = %d; want 1", len(res.ids))
		}
		if len(res.payloads) != 0 {
			t.Errorf("len(payloads) = %d; want 0", len(res.payloads))
		}
	case <-time.After(3 * time.Second):
		t.Fatal("ReadPostGames timed out")
	}
}

func TestReadPostGames_Empty(t *testing.T) {
	rdb, cleanup := newMiniRedisClient(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	c := NewPostGameConsumer(rdb)
	if err := c.EnsurePostGameGroup(ctx); err != nil {
		t.Fatalf("EnsurePostGameGroup: %v", err)
	}

	payloads, ids, err := c.ReadPostGames(ctx)
	if err != nil && err != context.DeadlineExceeded {
		t.Fatalf("ReadPostGames: %v", err)
	}
	if len(payloads) != 0 || len(ids) != 0 {
		t.Errorf("payloads=%d ids=%d; want both 0", len(payloads), len(ids))
	}
}

func TestAckPostGames_Empty(t *testing.T) {
	rdb, cleanup := newMiniRedisClient(t)
	defer cleanup()

	c := NewPostGameConsumer(rdb)
	if err := c.AckPostGames(context.Background()); err != nil {
		t.Errorf("AckPostGames() with no ids should be no-op: %v", err)
	}
}
