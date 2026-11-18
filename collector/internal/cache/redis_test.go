package cache

import (
	"testing"
	"time"
)

func TestTTLFor_OutlivesInterval(t *testing.T) {
	// Keys must survive until the next refresh, with room for one failed run.
	for _, interval := range []time.Duration{time.Minute, 30 * time.Minute, 6 * time.Hour, 24 * time.Hour} {
		if ttl := TTLFor(interval); ttl < 2*interval {
			t.Errorf("TTLFor(%s) = %s; want >= %s", interval, ttl, 2*interval)
		}
	}
}

func TestTTLFor_Minimum(t *testing.T) {
	if got := TTLFor(time.Minute); got != time.Hour {
		t.Errorf("TTLFor(1m) = %s; want 1h", got)
	}
}
