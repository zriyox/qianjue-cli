// Package backoff implements the polling schedule of cli-contract.md §19:
// 2s, 3s, 5s, 8s then capped at 10s, each with up to 20% jitter.
package backoff

import (
	"math/rand"
	"time"
)

var steps = []time.Duration{2 * time.Second, 3 * time.Second, 5 * time.Second, 8 * time.Second}

const max = 10 * time.Second

// Interval returns the wait before poll attempt n (0-based), jittered.
func Interval(attempt int) time.Duration {
	base := max
	if attempt < len(steps) {
		base = steps[attempt]
	}
	jitter := time.Duration(rand.Int63n(int64(base) / 5)) // ≤20%
	return base + jitter
}
