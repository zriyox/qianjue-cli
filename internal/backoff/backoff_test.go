package backoff

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestIntervalScheduleAndJitterBounds(t *testing.T) {
	bases := []time.Duration{2 * time.Second, 3 * time.Second, 5 * time.Second, 8 * time.Second,
		10 * time.Second, 10 * time.Second}
	for attempt, base := range bases {
		for range 50 {
			d := Interval(attempt)
			assert.GreaterOrEqual(t, d, base, "attempt %d", attempt)
			assert.Less(t, d, base+base/5+time.Millisecond, "jitter ≤20%%（attempt %d）", attempt)
		}
	}
}
