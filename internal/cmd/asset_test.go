package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zriyox/qianjue-cli/internal/clierr"
)

// storageStub stands in for object storage: it records what the CLI actually
// PUT so the tests can assert the wire contract.
type storageStub struct {
	mu       sync.Mutex
	bodies   map[string]string
	types    map[string]string
	authSeen []string
	status   int
}

func newStorageStub() (*storageStub, *httptest.Server) {
	s := &storageStub{bodies: map[string]string{}, types: map[string]string{}, status: 200}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		defer s.mu.Unlock()
		s.bodies[r.URL.Path] = string(body)
		s.types[r.URL.Path] = r.Header.Get("Content-Type")
		s.authSeen = append(s.authSeen, r.Header.Get("Authorization"))
		if s.status != 200 {
			w.WriteHeader(s.status)
			_, _ = w.Write([]byte("<Error><Code>SignatureDoesNotMatch</Code></Error>"))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	return s, srv
}

// presignServer answers the presign call, pointing upload URLs at the storage stub.
func presignServer(t *testing.T, storageURL string, gotItems *[]map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/integration/assets/presign-uploads", r.URL.Path)
		require.Equal(t, http.MethodPost, r.Method)

		var body struct {
			Items []map[string]any `json:"items"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		if gotItems != nil {
			*gotItems = body.Items
		}

		results := make([]map[string]any, 0, len(body.Items))
		for _, it := range body.Items {
			name := it["fileName"].(string)
			ct, _ := it["contentType"].(string)
			results = append(results, map[string]any{
				"fileName":    name,
				"objectKey":   "third-party/images/" + name,
				"uploadUrl":   storageURL + "/put/" + name + "?X-Amz-Signature=sig",
				"publicUrl":   "https://cdn.example.com/third-party/images/" + name,
				"contentType": ct,
				"expiresAt":   "2026-08-19T00:15:00Z",
			})
		}
		out, _ := json.Marshal(map[string]any{"code": 200, "message": "操作成功", "data": results})
		_, _ = w.Write(out)
	}))
}

func writeAssetFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
	return p
}

func TestAssetUploadSendsBytesToStorageNotToAPI(t *testing.T) {
	stub, storage := newStorageStub()
	defer storage.Close()
	srv := presignServer(t, storage.URL, nil)
	defer srv.Close()

	dir := t.TempDir()
	png := writeAssetFile(t, dir, "a.png", "PNGDATA")

	app, stdout, _ := imageApp(t, srv.URL)
	exit := run(app, []string{"asset", "upload", png, "--output", "json"})
	require.Equal(t, 0, exit, "stdout: %s", stdout.String())

	// 文件内容必须直达对象存储
	assert.Equal(t, "PNGDATA", stub.bodies["/put/a.png"])
	// Content-Type 参与签名，必须与预签名返回的一致
	assert.Equal(t, "image/png", stub.types["/put/a.png"])
	// 红线：绝不能把 qianjue 的 Bearer 凭证发给第三方对象存储
	for _, got := range stub.authSeen {
		assert.Empty(t, got, "预签名 PUT 不得携带 Authorization 头")
	}

	env := decodeEnvelope(t, stdout)
	assert.Equal(t, "asset.upload", env.Command)
	data := env.Data.([]any)
	require.Len(t, data, 1)
	first := data[0].(map[string]any)
	assert.Equal(t, "https://cdn.example.com/third-party/images/a.png", first["url"])
}

func TestAssetUploadBatchesInOnePresignCall(t *testing.T) {
	stub, storage := newStorageStub()
	defer storage.Close()

	var calls int
	var items []map[string]any
	inner := presignServer(t, storage.URL, &items)
	defer inner.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		inner.Config.Handler.ServeHTTP(w, r)
	}))
	defer srv.Close()

	dir := t.TempDir()
	a := writeAssetFile(t, dir, "a.png", "A")
	b := writeAssetFile(t, dir, "b.jpg", "B")

	app, stdout, _ := imageApp(t, srv.URL)
	exit := run(app, []string{"asset", "upload", a, b, "--output", "json"})
	require.Equal(t, 0, exit, "stdout: %s", stdout.String())

	// 批量必须一次拿完签名，而不是每个文件一次往返
	assert.Equal(t, 1, calls)
	assert.Len(t, items, 2)
	assert.Equal(t, "A", stub.bodies["/put/a.png"])
	assert.Equal(t, "B", stub.bodies["/put/b.jpg"])
}

func TestAssetUploadDirSkipsHiddenFilesAndHonorsGlob(t *testing.T) {
	stub, storage := newStorageStub()
	defer storage.Close()
	var items []map[string]any
	srv := presignServer(t, storage.URL, &items)
	defer srv.Close()

	dir := t.TempDir()
	writeAssetFile(t, dir, "keep.png", "K")
	writeAssetFile(t, dir, "skip.txt", "S")
	writeAssetFile(t, dir, ".DS_Store", "X") // 目录扫描最常见的脏文件

	app, stdout, _ := imageApp(t, srv.URL)
	exit := run(app, []string{"asset", "upload", "--dir", dir, "--glob", "*.png", "--output", "json"})
	require.Equal(t, 0, exit, "stdout: %s", stdout.String())

	require.Len(t, items, 1)
	assert.Equal(t, "keep.png", items[0]["fileName"])
	assert.Equal(t, "K", stub.bodies["/put/keep.png"])
	_, uploadedDS := stub.bodies["/put/.DS_Store"]
	assert.False(t, uploadedDS)
}

func TestAssetUploadStorageRejectionIsReportedAsTransport(t *testing.T) {
	stub, storage := newStorageStub()
	stub.status = 403
	defer storage.Close()
	srv := presignServer(t, storage.URL, nil)
	defer srv.Close()

	dir := t.TempDir()
	png := writeAssetFile(t, dir, "a.png", "PNGDATA")

	app, stdout, _ := imageApp(t, srv.URL)
	exit := run(app, []string{"asset", "upload", png, "--output", "json"})
	assert.Equal(t, clierr.ExitTransport, exit)
	assert.Equal(t, "TRANSPORT", decodeEnvelope(t, stdout).Error.Kind)
}

func TestAssetUploadRejectsMissingFile(t *testing.T) {
	app, _, _ := imageApp(t, "http://localhost:1")
	exit := run(app, []string{"asset", "upload", filepath.Join(t.TempDir(), "nope.png"), "--output", "json"})
	assert.Equal(t, clierr.ExitUsage, exit)
}

func TestAssetUploadRejectsEmptySelection(t *testing.T) {
	app, _, _ := imageApp(t, "http://localhost:1")
	exit := run(app, []string{"asset", "upload", "--dir", t.TempDir(), "--output", "json"})
	assert.Equal(t, clierr.ExitUsage, exit)
}

func TestAssetUploadRejectsOverBatchLimit(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < maxUploadBatch+1; i++ {
		writeAssetFile(t, dir, fmt.Sprintf("f%02d.png", i), "x")
	}
	app, _, _ := imageApp(t, "http://localhost:1")
	exit := run(app, []string{"asset", "upload", "--dir", dir, "--output", "json"})
	assert.Equal(t, clierr.ExitUsage, exit)
}
