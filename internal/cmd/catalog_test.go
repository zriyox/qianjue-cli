package cmd

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zriyox/qianjue-cli/internal/clierr"
)

func TestCatalogModelsPassesThroughServerFields(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		fmt.Fprint(w, `{"code":200,"message":"操作成功","data":{"models":[{"modelCode":"GPT_IMAGE_2","displayName":"千谲灵感 Max","allowedOutputResolutions":["1K","2K","4K"],"brandNewField":"x"}]}}`)
	}))
	defer srv.Close()

	app, stdout, _ := imageApp(t, srv.URL)
	exit := run(app, []string{"catalog", "models", "--output", "json"})
	require.Equal(t, 0, exit, "stdout: %s", stdout.String())

	assert.Equal(t, "/integration/catalog/image-models", gotPath)
	env := decodeEnvelope(t, stdout)
	assert.Equal(t, "catalog.models", env.Command)
	// 原样透传：平台新增能力字段不需要发 CLI 新版本
	assert.Contains(t, stdout.String(), "brandNewField")
	assert.Contains(t, stdout.String(), "千谲灵感 Max")
}

func TestCatalogModelsRequiresAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"code":1002,"message":"未认证","data":null}`)
	}))
	defer srv.Close()

	app, stdout, _ := imageApp(t, srv.URL)
	exit := run(app, []string{"catalog", "models", "--output", "json"})
	assert.Equal(t, clierr.ExitAuth, exit)
	assert.Equal(t, "AUTH", decodeEnvelope(t, stdout).Error.Kind)
}
