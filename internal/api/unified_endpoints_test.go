package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetUnifiedTask(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		fmt.Fprint(w, ok(`{"taskId":2089609747366940674,"domain":"DRAW","status":"COMPLETED","media":{"resultMediaList":["https://cdn/x.png"]}}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, &fakeTokens{token: "qj_pat_x"})
	raw, status, err := c.GetUnifiedTask(context.Background(), "image", "2089609747366940674")
	require.NoError(t, err)
	assert.Equal(t, "/integration/tasks/image/2089609747366940674", gotPath)
	assert.Equal(t, TaskCompleted, status)
	assert.Contains(t, string(raw), "2089609747366940674", "int64 taskId 原样透传")
}

func TestGetUnifiedTaskBusinessError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		fmt.Fprint(w, `{"code":3002,"message":"资源不存在","data":null}`)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, &fakeTokens{token: "qj_pat_x"})
	_, _, err := c.GetUnifiedTask(context.Background(), "video", "9")
	require.Error(t, err)
}

func TestListUnifiedTasks(t *testing.T) {
	var gotURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		fmt.Fprint(w, ok(`{"pageNum":1,"pageSize":2,"total":6850,"records":[{"taskId":1,"domain":"DRAW","status":"COMPLETED"}]}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, &fakeTokens{token: "qj_pat_x"})
	raw, err := c.ListUnifiedTasks(context.Background(), "image", "COMPLETED", 1, 2)
	require.NoError(t, err)
	assert.Contains(t, gotURL, "/integration/tasks/image")
	assert.Contains(t, gotURL, "status=COMPLETED")
	assert.Contains(t, gotURL, "page=1")
	assert.Contains(t, gotURL, "size=2")
	assert.Contains(t, string(raw), "6850")
}

func TestListUnifiedTasksOmitsEmptyParams(t *testing.T) {
	var gotURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		fmt.Fprint(w, ok(`{"pageNum":1,"pageSize":20,"total":0,"records":[]}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, &fakeTokens{token: "qj_pat_x"})
	_, err := c.ListUnifiedTasks(context.Background(), "video", "", 0, 0)
	require.NoError(t, err)
	assert.Equal(t, "/integration/tasks/video", gotURL, "空参数不进 query string")
}
