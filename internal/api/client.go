// Package api is the thin HTTP protocol layer for the Integration backend:
// R<T> envelope parsing, trace id propagation, bearer auth injection with a
// single 2002-triggered refresh retry, and the cli-contract.md §24 error
// mapping via clierr.FromAPI.
package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"time"

	"github.com/zriyox/qianjue-cli/internal/buildinfo"
	"github.com/zriyox/qianjue-cli/internal/clierr"
)

// TokenSource provides the bearer token for authenticated requests and the
// one-shot recovery hook for expired access tokens (cli-contract.md §11).
type TokenSource interface {
	// Token returns a currently usable token; Device Flow sources may refresh
	// proactively when the token is within 5 minutes of expiry.
	Token(ctx context.Context) (string, error)
	// HandleAuthError is invoked on HTTP 401 with business code 2002. It
	// returns true when a refresh succeeded and the request may be retried
	// once. Non-refreshable sources (PAT, env token) always return false.
	HandleAuthError(ctx context.Context, code int) (bool, error)
}

// NewTraceID returns a 32-hex-char trace id (uuid v4 without dashes),
// generated once per top-level command (cli-contract.md §27).
func NewTraceID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return hex.EncodeToString(b[:])
}

// UUID4 returns a canonical dashed uuid v4 (used for idempotency keys).
func UUID4() string {
	h := NewTraceID()
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// UserAgent is sent on every request.
func UserAgent() string {
	return fmt.Sprintf("qianjue-cli/%s (%s; %s)", buildinfo.Version, runtime.GOOS, runtime.GOARCH)
}

// Result is one parsed backend response.
type Result struct {
	HTTPStatus int
	Code       int
	Message    string
	Data       json.RawMessage
}

// Err converts a non-200 business code into the mapped CLIError.
func (r *Result) Err() *clierr.CLIError {
	if r.Code == 200 {
		return nil
	}
	return clierr.FromAPI(r.HTTPStatus, r.Code, r.Message, r.Data)
}

// envelope is the backend R<T> shape (R.java: code/message/data).
type envelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// Client is a per-command HTTP client bound to one trace id.
type Client struct {
	baseURL    string
	hc         *http.Client
	traceID    string
	tokens     TokenSource
	tracef     func(format string, args ...any) // nil unless --trace
	retryDelay time.Duration                    // GET retry backoff base; shrunk in tests
}

// NewClient builds a client. tokens may be nil for anonymous-only usage;
// tracef may be nil to disable trace logging.
func NewClient(baseURL string, httpTimeout time.Duration, tokens TokenSource, traceID string, tracef func(string, ...any)) *Client {
	return &Client{
		baseURL:    baseURL,
		hc:         &http.Client{Timeout: httpTimeout},
		traceID:    traceID,
		tokens:     tokens,
		tracef:     tracef,
		retryDelay: 500 * time.Millisecond,
	}
}

func (c *Client) TraceID() string { return c.traceID }

func (c *Client) trace(format string, args ...any) {
	if c.tracef != nil {
		c.tracef(format, args...)
	}
}

// do sends one request and parses the R envelope. Behavior:
//   - GET: transport errors and HTTP 5xx retried up to 2 times (contract §25).
//   - POST/PUT: never retried at this layer; unknown results bubble up as
//     TRANSPORT for the idempotency layer to recover with the original key.
//   - authed + HTTP 401 + code 2002: TokenSource.HandleAuthError may allow a
//     single retry with a fresh token.
func (c *Client) do(ctx context.Context, method, path string, headers map[string]string, body []byte, authed bool) (*Result, error) {
	authRetried := false
	transportRetries := 0
	for {
		res, err := c.doOnce(ctx, method, path, headers, body, authed)
		if err != nil {
			if method == http.MethodGet && transportRetries < 2 && ctx.Err() == nil {
				transportRetries++
				c.trace("transport error, retry %d/2: %v", transportRetries, err)
				select {
				case <-time.After(c.retryDelay * time.Duration(transportRetries)):
				case <-ctx.Done():
					return nil, clierr.Interrupted()
				}
				continue
			}
			return nil, err
		}
		if method == http.MethodGet && res.HTTPStatus >= 500 && transportRetries < 2 {
			transportRetries++
			c.trace("HTTP %d, retry %d/2", res.HTTPStatus, transportRetries)
			select {
			case <-time.After(c.retryDelay * time.Duration(transportRetries)):
			case <-ctx.Done():
				return nil, clierr.Interrupted()
			}
			continue
		}
		if authed && !authRetried && res.HTTPStatus == 401 && res.Code == 2002 && c.tokens != nil {
			retried, herr := c.tokens.HandleAuthError(ctx, res.Code)
			if herr != nil {
				return nil, herr
			}
			if retried {
				authRetried = true
				c.trace("access token 已刷新，重放请求")
				continue
			}
		}
		return res, nil
	}
}

func (c *Client) doOnce(ctx context.Context, method, path string, headers map[string]string, body []byte, authed bool) (*Result, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, clierr.Usage("构造请求失败: %v", err)
	}
	req.Header.Set("User-Agent", UserAgent())
	req.Header.Set("X-Trace-Id", c.traceID)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if authed {
		if c.tokens == nil {
			return nil, clierr.New(clierr.KindAuth, "缺少凭证：请先执行 qianjue auth login 或设置 QIANJUE_TOKEN")
		}
		token, terr := c.tokens.Token(ctx)
		if terr != nil {
			return nil, terr
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}

	c.trace("%s %s (trace=%s)", method, path, c.traceID)
	resp, err := c.hc.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, clierr.Interrupted()
		}
		return nil, clierr.Transport(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, clierr.Transport(fmt.Errorf("读取响应失败: %w", err))
	}
	c.trace("HTTP %d (%d bytes)", resp.StatusCode, len(raw))

	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil || env.Code == 0 {
		if resp.StatusCode >= 400 {
			return nil, clierr.FromAPI(resp.StatusCode, 0, fmt.Sprintf("非预期响应（HTTP %d）", resp.StatusCode), nil)
		}
		// 2xx 但响应体不可解析：对创建类请求属于未知结果，按 TRANSPORT 上抛。
		return nil, clierr.Transport(fmt.Errorf("响应解析失败（HTTP %d）", resp.StatusCode))
	}
	return &Result{HTTPStatus: resp.StatusCode, Code: env.Code, Message: env.Message, Data: env.Data}, nil
}

// decodeData unmarshals Result.Data into out after business-code validation.
func decodeData(res *Result, out any) error {
	if err := res.Err(); err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(res.Data, out); err != nil {
		return clierr.Transport(fmt.Errorf("响应 data 解析失败: %w", err))
	}
	return nil
}
