package consumer

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestEnsureReminderGroup(t *testing.T) {
	rdb, cleanup := newMiniRedisClient(t)
	defer cleanup()

	ctx := context.Background()
	c := NewReminderConsumer(rdb)

	if err := c.EnsureReminderGroup(ctx); err != nil {
		t.Fatalf("EnsureReminderGroup: %v", err)
	}

	// Second call should return BUSYGROUP
	err := c.EnsureReminderGroup(ctx)
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		t.Errorf("second EnsureReminderGroup: %v", err)
	}
}

func TestReadReminders_Valid(t *testing.T) {
	rdb, cleanup := newMiniRedisClient(t)
	defer cleanup()

	ctx := context.Background()
	c := NewReminderConsumer(rdb)
	if err := c.EnsureReminderGroup(ctx); err != nil {
		t.Fatalf("EnsureReminderGroup: %v", err)
	}

	p := ReminderPayload{
		GameID:         20250042,
		Opponent:       "PHI",
		HomeAway:       "home",
		ProbabilityPct: 45,
		StartTimeUTC:   "2026-03-01T00:00:00Z",
		OddsAmerican:   "+120",
		GoalieName:     "S. Ersson",
	}
	raw, _ := json.Marshal(p)
	if _, err := rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: RemindersStreamKey,
		Values: map[string]interface{}{"payload": string(raw)},
	}).Result(); err != nil {
		t.Fatalf("XAdd: %v", err)
	}

	type result struct {
		payloads []ReminderPayload
		ids      []string
		err      error
	}
	done := make(chan result, 1)
	readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	go func() {
		payloads, ids, err := c.ReadReminders(readCtx)
		done <- result{payloads, ids, err}
	}()

	select {
	case res := <-done:
		if res.err != nil {
			t.Fatalf("ReadReminders: %v", res.err)
		}
		if len(res.payloads) != 1 {
			t.Fatalf("len(payloads) = %d; want 1", len(res.payloads))
		}
		got := res.payloads[0]
		if got.Opponent != p.Opponent {
			t.Errorf("opponent = %q; want %q", got.Opponent, p.Opponent)
		}
		if got.ProbabilityPct != p.ProbabilityPct {
			t.Errorf("probability_pct = %d; want %d", got.ProbabilityPct, p.ProbabilityPct)
		}
		if got.GoalieName != p.GoalieName {
			t.Errorf("goalie_name = %q; want %q", got.GoalieName, p.GoalieName)
		}
		if len(res.ids) != 1 {
			t.Fatalf("len(ids) = %d; want 1", len(res.ids))
		}
		if err := c.AckReminders(ctx, res.ids...); err != nil {
			t.Fatalf("AckReminders: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("ReadReminders timed out")
	}
}

func TestReadReminders_MissingPayloadKey(t *testing.T) {
	rdb, cleanup := newMiniRedisClient(t)
	defer cleanup()

	ctx := context.Background()
	c := NewReminderConsumer(rdb)
	if err := c.EnsureReminderGroup(ctx); err != nil {
		t.Fatalf("EnsureReminderGroup: %v", err)
	}

	if _, err := rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: RemindersStreamKey,
		Values: map[string]interface{}{"wrong_key": "some value"},
	}).Result(); err != nil {
		t.Fatalf("XAdd: %v", err)
	}

	type result struct {
		payloads []ReminderPayload
		ids      []string
		err      error
	}
	done := make(chan result, 1)
	readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	go func() {
		payloads, ids, err := c.ReadReminders(readCtx)
		done <- result{payloads, ids, err}
	}()

	select {
	case res := <-done:
		if res.err != nil {
			t.Fatalf("ReadReminders: %v", res.err)
		}
		if len(res.ids) != 1 {
			t.Fatalf("len(ids) = %d; want 1", len(res.ids))
		}
		if len(res.payloads) != 0 {
			t.Errorf("len(payloads) = %d; want 0", len(res.payloads))
		}
	case <-time.After(3 * time.Second):
		t.Fatal("ReadReminders timed out")
	}
}

func TestReadReminders_InvalidJSON(t *testing.T) {
	rdb, cleanup := newMiniRedisClient(t)
	defer cleanup()

	ctx := context.Background()
	c := NewReminderConsumer(rdb)
	if err := c.EnsureReminderGroup(ctx); err != nil {
		t.Fatalf("EnsureReminderGroup: %v", err)
	}

	if _, err := rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: RemindersStreamKey,
		Values: map[string]interface{}{"payload": "{bad json"},
	}).Result(); err != nil {
		t.Fatalf("XAdd: %v", err)
	}

	type result struct {
		payloads []ReminderPayload
		ids      []string
		err      error
	}
	done := make(chan result, 1)
	readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	go func() {
		payloads, ids, err := c.ReadReminders(readCtx)
		done <- result{payloads, ids, err}
	}()

	select {
	case res := <-done:
		if res.err != nil {
			t.Fatalf("ReadReminders: %v", res.err)
		}
		if len(res.ids) != 1 {
			t.Fatalf("len(ids) = %d; want 1", len(res.ids))
		}
		if len(res.payloads) != 0 {
			t.Errorf("len(payloads) = %d; want 0", len(res.payloads))
		}
	case <-time.After(3 * time.Second):
		t.Fatal("ReadReminders timed out")
	}
}

func TestReadReminders_Empty(t *testing.T) {
	rdb, cleanup := newMiniRedisClient(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	c := NewReminderConsumer(rdb)
	if err := c.EnsureReminderGroup(ctx); err != nil {
		t.Fatalf("EnsureReminderGroup: %v", err)
	}

	payloads, ids, err := c.ReadReminders(ctx)
	if err != nil && err != context.DeadlineExceeded {
		t.Fatalf("ReadReminders: %v", err)
	}
	if len(payloads) != 0 || len(ids) != 0 {
		t.Errorf("payloads=%d ids=%d; want both 0", len(payloads), len(ids))
	}
}

func TestAckReminders_Empty(t *testing.T) {
	rdb, cleanup := newMiniRedisClient(t)
	defer cleanup()

	c := NewReminderConsumer(rdb)
	if err := c.AckReminders(context.Background()); err != nil {
		t.Errorf("AckReminders() with no ids should be no-op: %v", err)
	}
}
