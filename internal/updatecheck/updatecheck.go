// Package updatecheck tells the user that a newer CLI release is published.
//
// It deliberately avoids the GitHub REST API. The release page
// (…/releases/latest) answers with a 302 to …/releases/tag/vX.Y.Z, so one
// request with redirects disabled yields the published tag without spending
// the anonymous 60-requests-per-hour API quota. The lookup never carries
// credentials: it is a release announcement, not an API call.
package updatecheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/zriyox/qianjue-cli/internal/config"
)

const (
	// DefaultTimeout bounds the whole lookup. Measured from mainland China,
	// github.com answers the release redirect in ~1.5s, so a sub-second budget
	// would fail every single time; the caller keeps the cost rare by caching
	// the outcome instead of by shortening this.
	DefaultTimeout = 3 * time.Second
	// cacheSchemaVersion guards the on-disk cache shape.
	cacheSchemaVersion = "1"
	// maxBodyBytes caps what we are willing to read from a redirect body.
	maxBodyBytes = 4096
)

// ErrUnavailable reports that no fresh version information could be produced:
// the last lookup failed and its failure is still within FailureTTL. Callers
// treat it as "暂时无法确认", never as "已是最新".
var ErrUnavailable = errors.New("暂时无法确认最新版本")

// Result is one check outcome.
type Result struct {
	// Current is the running build's version.
	Current string
	// Latest is the published tag, empty when nothing could be read.
	Latest string
	// Outdated reports that Latest is strictly newer than Current.
	Outdated bool
	// ReleaseURL points at the published release.
	ReleaseURL string
}

// Checker performs the release lookup behind a local cache.
type Checker struct {
	// CurrentVersion is buildinfo.Version.
	CurrentVersion string
	// LatestReleaseURL answers with a redirect to the newest release tag.
	LatestReleaseURL string
	// CachePath remembers the last lookup. Empty disables caching.
	CachePath string
	// TTL bounds the staleness of a cached lookup.
	TTL time.Duration
	// FailureTTL bounds how long a failed lookup suppresses retries, so an
	// offline machine pays the timeout once per hour, not once per command.
	FailureTTL time.Duration
	// Client performs the request; nil uses a client with DefaultTimeout.
	Client *http.Client
	// Now is the clock, injectable for tests.
	Now func() time.Time
}

// Run returns the latest release, from cache when the cache is still fresh.
func (c Checker) Run(ctx context.Context) (Result, error) {
	cached, hasCache := c.readCache()
	if hasCache && c.fresh(cached) {
		if cached.Failed {
			return c.result(cached.Latest), ErrUnavailable
		}
		return c.result(cached.Latest), nil
	}
	latest, err := c.fetch(ctx)
	if err != nil {
		// Remember the failure so a broken network costs the timeout once per
		// FailureTTL instead of once per command. The last known tag is kept:
		// it is still the newest release we have seen.
		c.writeCache(cacheFile{
			SchemaVersion: cacheSchemaVersion,
			CheckedAt:     c.now(),
			Latest:        cached.Latest,
			Failed:        true,
		})
		return c.result(cached.Latest), err
	}
	c.writeCache(cacheFile{SchemaVersion: cacheSchemaVersion, CheckedAt: c.now(), Latest: latest})
	return c.result(latest), nil
}

func (c Checker) result(latest string) Result {
	return Result{
		Current:    c.CurrentVersion,
		Latest:     latest,
		Outdated:   latest != "" && newer(latest, c.CurrentVersion),
		ReleaseURL: c.LatestReleaseURL,
	}
}

func (c Checker) fresh(cached cacheFile) bool {
	ttl := c.TTL
	if cached.Failed {
		ttl = c.FailureTTL
	}
	if ttl <= 0 {
		return false
	}
	return c.now().Sub(cached.CheckedAt) < ttl
}

func (c Checker) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// client clones the injected client so setting CheckRedirect cannot leak back
// into a caller-owned client.
func (c Checker) client() *http.Client {
	base := c.Client
	if base == nil {
		base = &http.Client{}
	}
	clone := *base
	if clone.Timeout == 0 {
		clone.Timeout = DefaultTimeout
	}
	clone.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &clone
}

func (c Checker) fetch(ctx context.Context) (string, error) {
	if c.LatestReleaseURL == "" {
		return "", errors.New("未配置发布地址")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.LatestReleaseURL, nil)
	if err != nil {
		return "", err
	}
	response, err := c.client().Do(request)
	if err != nil {
		return "", err
	}
	defer func() { _ = response.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxBodyBytes))

	location := response.Header.Get("Location")
	if response.StatusCode != http.StatusFound || location == "" {
		return "", fmt.Errorf("发布地址返回 %d 且缺少 Location", response.StatusCode)
	}
	tag := tagFromLocation(location)
	if tag == "" {
		return "", fmt.Errorf("无法从 Location %q 解析版本号", location)
	}
	return tag, nil
}

// tagFromLocation pulls the last path segment out of a redirect target, e.g.
// https://github.com/o/r/releases/tag/v1.2.0?x=1 -> v1.2.0.
func tagFromLocation(location string) string {
	trimmed := strings.TrimSpace(location)
	if index := strings.IndexAny(trimmed, "?#"); index >= 0 {
		trimmed = trimmed[:index]
	}
	trimmed = strings.TrimRight(trimmed, "/")
	if index := strings.LastIndex(trimmed, "/"); index >= 0 {
		trimmed = trimmed[index+1:]
	}
	if _, ok := parseVersion(trimmed); !ok {
		return ""
	}
	return trimmed
}

// newer reports whether latest is strictly newer than current. Both must parse;
// an unrecognized version (a local "dev" build, a branch tag) never nags.
func newer(latest, current string) bool {
	latestParts, ok := parseVersion(latest)
	if !ok {
		return false
	}
	currentParts, ok := parseVersion(current)
	if !ok {
		return false
	}
	for index := 0; index < len(latestParts) || index < len(currentParts); index++ {
		var latestPart, currentPart int
		if index < len(latestParts) {
			latestPart = latestParts[index]
		}
		if index < len(currentParts) {
			currentPart = currentParts[index]
		}
		if latestPart != currentPart {
			return latestPart > currentPart
		}
	}
	return false
}

// parseVersion turns v1.2.3 / 1.2.3-rc1 into [1 2 3]. Pre-release and build
// suffixes are dropped: this only answers "is a newer release published", not
// full semver precedence.
func parseVersion(value string) ([]int, bool) {
	trimmed := strings.TrimPrefix(strings.TrimSpace(value), "v")
	if index := strings.IndexAny(trimmed, "-+"); index >= 0 {
		trimmed = trimmed[:index]
	}
	if trimmed == "" {
		return nil, false
	}
	segments := strings.Split(trimmed, ".")
	parts := make([]int, 0, len(segments))
	for _, segment := range segments {
		number, err := strconv.Atoi(segment)
		if err != nil {
			return nil, false
		}
		parts = append(parts, number)
	}
	return parts, true
}

type cacheFile struct {
	SchemaVersion string    `json:"schemaVersion"`
	CheckedAt     time.Time `json:"checkedAt"`
	Latest        string    `json:"latest,omitempty"`
	// Failed marks a lookup that errored; it shortens the retry window and
	// stops the stale tag from being reported as freshly confirmed.
	Failed bool `json:"failed,omitempty"`
}

// readCache degrades to "no cache" on any error: a broken cache file must never
// break a command.
func (c Checker) readCache() (cacheFile, bool) {
	if c.CachePath == "" {
		return cacheFile{}, false
	}
	raw, err := os.ReadFile(c.CachePath)
	if err != nil {
		return cacheFile{}, false
	}
	var cached cacheFile
	if err := json.Unmarshal(raw, &cached); err != nil {
		return cacheFile{}, false
	}
	if cached.SchemaVersion != cacheSchemaVersion || cached.CheckedAt.IsZero() {
		return cacheFile{}, false
	}
	return cached, true
}

// writeCache is best effort: an unwritable state dir must not fail the command.
// The state directory may not exist yet, so creation is part of the write.
func (c Checker) writeCache(cached cacheFile) {
	if c.CachePath == "" {
		return
	}
	raw, err := json.Marshal(cached)
	if err != nil {
		return
	}
	_ = config.AtomicWrite(c.CachePath, raw)
}
