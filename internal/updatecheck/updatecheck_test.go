package updatecheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// releaseServer answers the release-page redirect the way GitHub does.
func releaseServer(t *testing.T, hits *atomic.Int32, location string, status int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if location != "" {
			w.Header().Set("Location", location)
		}
		w.WriteHeader(status)
	}))
}

type fixedClock struct{ at time.Time }

func (c *fixedClock) now() time.Time { return c.at }

func newChecker(t *testing.T, url, cachePath string, clock *fixedClock) Checker {
	t.Helper()
	return Checker{
		CurrentVersion:   "v1.0.0",
		LatestReleaseURL: url,
		CachePath:        cachePath,
		TTL:              24 * time.Hour,
		FailureTTL:       time.Hour,
		Now:              clock.now,
	}
}

func TestRunReportsNewerRelease(t *testing.T) {
	var hits atomic.Int32
	srv := releaseServer(t, &hits, "https://github.com/zriyox/qianjue-cli/releases/tag/v1.2.0", http.StatusFound)
	defer srv.Close()

	clock := &fixedClock{at: time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC)}
	checker := newChecker(t, srv.URL, filepath.Join(t.TempDir(), "update-check.json"), clock)

	result, err := checker.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "v1.0.0", result.Current)
	assert.Equal(t, "v1.2.0", result.Latest)
	assert.True(t, result.Outdated)
	assert.Equal(t, int32(1), hits.Load())
}

func TestRunDoesNotSendCredentials(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, name := range []string{"Authorization", "Cookie", "X-Api-Key"} {
			if value := r.Header.Get(name); value != "" {
				seen = append(seen, name+"="+value)
			}
		}
		w.Header().Set("Location", "https://github.com/zriyox/qianjue-cli/releases/tag/v1.2.0")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()

	clock := &fixedClock{at: time.Now()}
	_, err := newChecker(t, srv.URL, "", clock).Run(context.Background())
	require.NoError(t, err)
	assert.Empty(t, seen, "更新检查是公开公告请求，不得携带凭证")
}

func TestRunServesFromCacheWithinTTL(t *testing.T) {
	var hits atomic.Int32
	srv := releaseServer(t, &hits, "https://github.com/zriyox/qianjue-cli/releases/tag/v1.2.0", http.StatusFound)
	defer srv.Close()

	cachePath := filepath.Join(t.TempDir(), "update-check.json")
	clock := &fixedClock{at: time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC)}
	checker := newChecker(t, srv.URL, cachePath, clock)

	first, err := checker.Run(context.Background())
	require.NoError(t, err)
	require.True(t, first.Outdated)
	assert.NotErrorIs(t, err, ErrUnavailable)

	clock.at = clock.at.Add(time.Hour)
	second, err := checker.Run(context.Background())
	require.NoError(t, err, "成功结果必须走缓存，不是 ErrUnavailable")
	assert.True(t, second.Outdated)
	assert.Equal(t, "v1.2.0", second.Latest)
	assert.Equal(t, int32(1), hits.Load(), "TTL 内必须走缓存，不再发请求")
}

func TestRunRefetchesAfterTTL(t *testing.T) {
	var hits atomic.Int32
	srv := releaseServer(t, &hits, "https://github.com/zriyox/qianjue-cli/releases/tag/v1.2.0", http.StatusFound)
	defer srv.Close()

	clock := &fixedClock{at: time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC)}
	checker := newChecker(t, srv.URL, filepath.Join(t.TempDir(), "update-check.json"), clock)

	_, err := checker.Run(context.Background())
	require.NoError(t, err)

	clock.at = clock.at.Add(25 * time.Hour)
	_, err = checker.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int32(2), hits.Load())
}

// A failed lookup must suppress retries for FailureTTL: an offline machine
// should pay the timeout once per hour, not once per command.
func TestRunCachesFailuresSoOfflineMachinesDoNotRetryEveryCommand(t *testing.T) {
	var hits atomic.Int32
	srv := releaseServer(t, &hits, "", http.StatusBadGateway)
	defer srv.Close()

	clock := &fixedClock{at: time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC)}
	checker := newChecker(t, srv.URL, filepath.Join(t.TempDir(), "update-check.json"), clock)

	_, err := checker.Run(context.Background())
	require.Error(t, err)

	// Within FailureTTL the failure is served from cache: no request, and the
	// caller is told the version is unconfirmed rather than "up to date".
	_, err = checker.Run(context.Background())
	require.ErrorIs(t, err, ErrUnavailable)
	assert.Equal(t, int32(1), hits.Load())

	clock.at = clock.at.Add(2 * time.Hour)
	_, err = checker.Run(context.Background())
	require.Error(t, err)
	assert.Equal(t, int32(2), hits.Load())
}

func TestRunUnreachableHostIsNotFatal(t *testing.T) {
	clock := &fixedClock{at: time.Now()}
	// 127.0.0.1:1 refuses instantly, so this stays fast and offline.
	checker := newChecker(t, "http://127.0.0.1:1/releases/latest", filepath.Join(t.TempDir(), "c.json"), clock)
	result, err := checker.Run(context.Background())
	require.Error(t, err)
	assert.False(t, result.Outdated)
	assert.Empty(t, result.Latest)
}

func TestRunIgnoresUnrecognizedTag(t *testing.T) {
	var hits atomic.Int32
	srv := releaseServer(t, &hits, "https://github.com/zriyox/qianjue-cli/releases/tag/nightly", http.StatusFound)
	defer srv.Close()

	clock := &fixedClock{at: time.Now()}
	_, err := newChecker(t, srv.URL, "", clock).Run(context.Background())
	require.Error(t, err, "认不出的 tag 不能当成版本号上报")
}

func TestRunReportsUpToDateWithoutNagging(t *testing.T) {
	var hits atomic.Int32
	srv := releaseServer(t, &hits, "https://github.com/zriyox/qianjue-cli/releases/tag/v1.0.0", http.StatusFound)
	defer srv.Close()

	clock := &fixedClock{at: time.Now()}
	result, err := newChecker(t, srv.URL, "", clock).Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "v1.0.0", result.Latest)
	assert.False(t, result.Outdated)
}

func TestRunNeverNagsAnUnparseableCurrentVersion(t *testing.T) {
	var hits atomic.Int32
	srv := releaseServer(t, &hits, "https://github.com/zriyox/qianjue-cli/releases/tag/v9.9.9", http.StatusFound)
	defer srv.Close()

	for _, current := range []string{"dev", "", "unknown", "HEAD-abc1234"} {
		checker := newChecker(t, srv.URL, "", &fixedClock{at: time.Now()})
		checker.CurrentVersion = current
		result, err := checker.Run(context.Background())
		require.NoError(t, err)
		assert.False(t, result.Outdated, "current=%q 不该被判成有新版", current)
	}
}

func TestRunCorruptCacheIsIgnored(t *testing.T) {
	var hits atomic.Int32
	srv := releaseServer(t, &hits, "https://github.com/zriyox/qianjue-cli/releases/tag/v1.2.0", http.StatusFound)
	defer srv.Close()

	cachePath := filepath.Join(t.TempDir(), "update-check.json")
	require.NoError(t, writeFile(cachePath, "{not json"))
	clock := &fixedClock{at: time.Now()}

	result, err := newChecker(t, srv.URL, cachePath, clock).Run(context.Background())
	require.NoError(t, err)
	assert.True(t, result.Outdated)
	assert.Equal(t, int32(1), hits.Load())
}

func TestRunHonoursContextCancellation(t *testing.T) {
	var hits atomic.Int32
	srv := releaseServer(t, &hits, "https://github.com/zriyox/qianjue-cli/releases/tag/v1.2.0", http.StatusFound)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := newChecker(t, srv.URL, "", &fixedClock{at: time.Now()}).Run(ctx)
	require.Error(t, err)
}

func TestTagFromLocation(t *testing.T) {
	cases := map[string]string{
		"https://github.com/zriyox/qianjue-cli/releases/tag/v1.2.0":  "v1.2.0",
		"https://github.com/zriyox/qianjue-cli/releases/tag/v1.2.0/": "v1.2.0",
		"/zriyox/qianjue-cli/releases/tag/1.2.0":                     "1.2.0",
		"https://example.com/releases/tag/v1.2.0?utm_source=test":    "v1.2.0",
		"https://example.com/releases/tag/v1.2.0#notes":              "v1.2.0",
		"https://github.com/zriyox/qianjue-cli/releases":             "",
		"https://github.com/zriyox/qianjue-cli/releases/tag/nightly": "",
		"https://github.com/zriyox/qianjue-cli/releases/tag/":        "",
	}
	for location, want := range cases {
		assert.Equal(t, want, tagFromLocation(location), "location=%s", location)
	}
}

func TestNewer(t *testing.T) {
	cases := []struct {
		latest  string
		current string
		want    bool
	}{
		{"v1.2.0", "v1.0.0", true},
		{"v1.0.1", "v1.0.0", true},
		{"v2.0.0", "v1.9.9", true},
		{"v1.0.0", "v1.0.0", false},
		{"v1.0.0", "v1.2.0", false},
		{"1.2.0", "1.0.0", true},
		{"v1.2.0", "1.0.0-dev", true},
		{"v1.2.0", "dev", false},
		{"v1.2", "v1.2.0", false},
		{"v1.2.0.1", "v1.2.0", true},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, newer(tc.latest, tc.current), "latest=%s current=%s", tc.latest, tc.current)
	}
}
