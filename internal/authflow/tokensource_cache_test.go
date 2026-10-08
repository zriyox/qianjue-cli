package authflow

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zriyox/qianjue-cli/internal/api"
	"github.com/zriyox/qianjue-cli/internal/cred"
)

// countingStore counts credential-store reads so a test can assert that one
// process does not hit the OS store once per HTTP request. On macOS every read
// may pop a keychain authorization dialog, so the count is a user-visible
// property, not a micro-optimization.
type countingStore struct {
	cred.Store
	mu   sync.Mutex
	gets int
}

func (c *countingStore) Get(account string) (*cred.Record, error) {
	c.mu.Lock()
	c.gets++
	c.mu.Unlock()
	return c.Store.Get(account)
}

func (c *countingStore) GetCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gets
}

// deadClient points at a port that refuses connections: any refresh attempt
// fails loudly, so tests that must not refresh prove it by not erroring.
func deadClient() *api.Client {
	return api.NewClient("http://127.0.0.1:1", time.Second, nil, api.NewTraceID(), nil)
}

func freshRecord(at time.Time, expiresIn time.Duration) *cred.Record {
	return &cred.Record{
		CredentialType:       cred.TypeDeviceFlow,
		AccessToken:          "qj_at_cached",
		AccessTokenExpiresAt: at.Add(expiresIn),
		RefreshToken:         "qj_rt_old",
		SessionExpiresAt:     at.Add(29 * 24 * time.Hour),
		SessionID:            "qj_ds_1",
		Scopes:               []string{"task.create"},
	}
}

// 一条命令里 N 次请求（`task wait` 的轮询、`image batch` 的 N 张图）过去会读 N 次
// 系统凭证库；缓存之后整条命令只读一次。
func TestDeviceFlowTokenSourceReusesCachedTokenAcrossRequests(t *testing.T) {
	store := &countingStore{Store: cred.NewMemoryStore()}
	require.NoError(t, store.Set(cred.DeviceFlowAccount("local"), freshRecord(time.Now(), time.Hour)))
	ts := NewDeviceFlowTokenSource(store, lockEnv(t), "local", deadClient())

	for i := 0; i < 5; i++ {
		token, err := ts.Token(context.Background())
		require.NoError(t, err)
		assert.Equal(t, "qj_at_cached", token)
	}
	assert.Equal(t, 1, store.GetCount(), "同一进程内重复请求不得重复读凭证库")
}

// 缓存不能变成「永不回读」：进入 T-5min 刷新窗口（契约 §11 触发条件 1）必须回读并刷新。
func TestDeviceFlowTokenSourceReReadsWhenCachedTokenEntersRefreshWindow(t *testing.T) {
	var calls atomic.Int32
	srv := refreshServer(t, &calls)
	defer srv.Close()

	store := &countingStore{Store: cred.NewMemoryStore()}
	clock := time.Now()
	rec := freshRecord(clock, time.Hour)
	require.NoError(t, store.Set(cred.DeviceFlowAccount("local"), rec))

	ts := NewDeviceFlowTokenSource(store, lockEnv(t), "local", api.NewClient(srv.URL, 5*time.Second, nil, api.NewTraceID(), nil))
	ts.now = func() time.Time { return clock }

	for i := 0; i < 3; i++ {
		token, err := ts.Token(context.Background())
		require.NoError(t, err)
		assert.Equal(t, "qj_at_cached", token)
	}
	cachedReads := store.GetCount()
	assert.Equal(t, 1, cachedReads, "新鲜凭证走缓存")
	assert.Equal(t, int32(0), calls.Load(), "新鲜凭证不该触发刷新")

	clock = rec.AccessTokenExpiresAt.Add(-4 * time.Minute) // 距过期不足 5 分钟
	token, err := ts.Token(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "qj_at_new1", token)
	assert.Equal(t, int32(1), calls.Load(), "进入刷新窗口必须刷新")
	// 刷新路径本身要读三次（拿锁前后各读一次记录 + 暂存回读校验），
	// 这里只要求「确实回读了」，不把次数写死。
	refreshedReads := store.GetCount()
	assert.Greater(t, refreshedReads, cachedReads, "进入刷新窗口必须回读凭证库")

	for i := 0; i < 3; i++ {
		token, err = ts.Token(context.Background())
		require.NoError(t, err)
		assert.Equal(t, "qj_at_new1", token)
	}
	assert.Equal(t, refreshedReads, store.GetCount(), "刷新后的凭证继续走缓存")
}

// 401/2002（契约 §11 触发条件 2）走 force 刷新，轮换结果必须进缓存：
// 重放请求和后续请求都不该再读凭证库。
func TestDeviceFlowTokenSourceHandleAuthErrorCachesRotatedToken(t *testing.T) {
	var calls atomic.Int32
	srv := refreshServer(t, &calls)
	defer srv.Close()

	store := &countingStore{Store: cred.NewMemoryStore()}
	rec := freshRecord(time.Now(), time.Hour) // 本地看还新鲜，服务端已判 2002
	require.NoError(t, store.Set(cred.DeviceFlowAccount("local"), rec))

	ts := NewDeviceFlowTokenSource(store, lockEnv(t), "local", api.NewClient(srv.URL, 5*time.Second, nil, api.NewTraceID(), nil))

	retried, err := ts.HandleAuthError(context.Background(), 2002)
	require.NoError(t, err)
	assert.True(t, retried)
	assert.Equal(t, int32(1), calls.Load())
	afterForce := store.GetCount()

	token, err := ts.Token(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "qj_at_new1", token)
	assert.Equal(t, afterForce, store.GetCount(), "重放与后续请求必须命中缓存")
}

// 并发调用不能各自去读一次凭证库（batch 场景同一 client 会被并发使用）。
func TestDeviceFlowTokenSourceConcurrentRequestsReadStoreOnce(t *testing.T) {
	store := &countingStore{Store: cred.NewMemoryStore()}
	require.NoError(t, store.Set(cred.DeviceFlowAccount("local"), freshRecord(time.Now(), time.Hour)))
	ts := NewDeviceFlowTokenSource(store, lockEnv(t), "local", deadClient())

	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		tokens  []string
		failure error
	)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			token, err := ts.Token(context.Background())
			mu.Lock()
			defer mu.Unlock()
			if err != nil && failure == nil {
				failure = err
			}
			tokens = append(tokens, token)
		}()
	}
	wg.Wait()

	require.NoError(t, failure)
	require.Len(t, tokens, 16)
	for _, token := range tokens {
		assert.Equal(t, "qj_at_cached", token)
	}
	assert.Equal(t, 1, store.GetCount(), "并发请求也只能读一次凭证库")
}

// 没有可用缓存时（首次请求）仍然必须回读，行为与改动前一致。
func TestDeviceFlowTokenSourceFirstRequestReadsStore(t *testing.T) {
	store := &countingStore{Store: cred.NewMemoryStore()}
	require.NoError(t, store.Set(cred.DeviceFlowAccount("local"), freshRecord(time.Now(), time.Hour)))
	ts := NewDeviceFlowTokenSource(store, lockEnv(t), "local", deadClient())

	token, err := ts.Token(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "qj_at_cached", token)
	assert.Equal(t, 1, store.GetCount())
}
