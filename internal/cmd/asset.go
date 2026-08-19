package cmd

import (
	"context"
	"fmt"
	"mime"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/zriyox/qianjue-cli/internal/api"
	"github.com/zriyox/qianjue-cli/internal/clierr"
)

// uploadedAsset is one finished upload, reported per input file.
type uploadedAsset struct {
	File      string `json:"file"`
	ObjectKey string `json:"objectKey"`
	URL       string `json:"url"`
	Size      int64  `json:"size"`
}

func newAssetCommand(app *appContext) *cobra.Command {
	assetCmd := &cobra.Command{Use: "asset", Short: "素材直传（用于带输入图的任务）"}
	assetCmd.AddCommand(newAssetUploadCommand(app))
	return assetCmd
}

// newAssetUploadCommand uploads local files straight to object storage using
// server-issued presigned URLs, so the file bytes never pass through the
// qianjue API server.
func newAssetUploadCommand(app *appContext) *cobra.Command {
	var dir string
	var glob string
	var concurrency int

	c := &cobra.Command{
		Use:   "upload [文件...]",
		Short: "上传本地文件并返回可用作任务输入的公开 URL（支持批量）",
		RunE: func(cmd *cobra.Command, args []string) error {
			paths, err := collectUploadPaths(args, dir, glob)
			if err != nil {
				return err
			}
			if len(paths) == 0 {
				return clierr.Usage("没有要上传的文件：请给出文件路径，或用 --dir 指定目录")
			}
			if len(paths) > maxUploadBatch {
				return clierr.Usage("一次最多上传 %d 个文件，当前 %d 个", maxUploadBatch, len(paths))
			}

			items := make([]api.PresignItem, 0, len(paths))
			sizes := make(map[string]int64, len(paths))
			for _, p := range paths {
				info, err := os.Stat(p)
				if err != nil {
					return clierr.Usage("读取文件失败 %s: %v", p, err)
				}
				if info.IsDir() {
					return clierr.Usage("%s 是目录，请用 --dir", p)
				}
				sizes[p] = info.Size()
				items = append(items, api.PresignItem{
					FileName:    filepath.Base(p),
					ContentType: guessContentType(p),
					Size:        info.Size(),
				})
			}

			resolved, err := app.resolveConfig()
			if err != nil {
				return err
			}
			client, err := app.newAuthedClient(resolved)
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			// 一次请求换回整批签名，批量上传不必逐个往返
			presigns, err := client.PresignUploads(ctx, items)
			if err != nil {
				return err
			}
			if len(presigns) != len(paths) {
				return clierr.New(clierr.KindServer,
					fmt.Sprintf("预签名数量与文件数不一致：期望 %d，实际 %d", len(paths), len(presigns)))
			}

			uploaded, err := uploadAll(ctx, app, paths, presigns, sizes, concurrency)
			if err != nil {
				return err
			}

			rows := make([][2]string, 0, len(uploaded))
			for _, u := range uploaded {
				rows = append(rows, [2]string{filepath.Base(u.File), u.URL})
			}
			return app.printer.Success("asset.upload", uploaded, app.meta, rows)
		},
	}

	c.Flags().StringVar(&dir, "dir", "", "上传该目录下的文件（不递归）")
	c.Flags().StringVar(&glob, "glob", "", "配合 --dir 使用的文件名匹配，如 '*.png'")
	c.Flags().IntVar(&concurrency, "concurrency", defaultUploadConcurrency, "并发上传数")
	return c
}

const (
	// maxUploadBatch mirrors the server-side per-request presign limit.
	maxUploadBatch = 20
	// defaultUploadConcurrency keeps a directory upload brisk without
	// saturating a home uplink.
	defaultUploadConcurrency = 4
)

// collectUploadPaths resolves explicit arguments and/or a --dir listing into a
// stable, sorted file list.
func collectUploadPaths(args []string, dir, glob string) ([]string, error) {
	paths := append([]string{}, args...)
	if dir != "" {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, clierr.Usage("读取目录失败 %s: %v", dir, err)
		}
		for _, e := range entries {
			if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue // 跳过隐藏文件，避免 .DS_Store 之类混进批次
			}
			if glob != "" {
				ok, err := filepath.Match(glob, e.Name())
				if err != nil {
					return nil, clierr.Usage("无效的 --glob 表达式 %q: %v", glob, err)
				}
				if !ok {
					continue
				}
			}
			paths = append(paths, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(paths)
	return paths, nil
}

// uploadAll PUTs every file concurrently, preserving input order in the result.
func uploadAll(ctx context.Context, app *appContext, paths []string,
	presigns []api.PresignResult, sizes map[string]int64, concurrency int) ([]uploadedAsset, error) {

	if concurrency < 1 {
		concurrency = 1
	}
	results := make([]uploadedAsset, len(paths))
	errs := make([]error, len(paths))
	// 不设整体超时：大文件上传本来就可能很久，一刀切会误杀正常上传。
	// 但必须给连接握手和响应头设上限，否则对端假死时会永久挂起。
	httpClient := &http.Client{
		Transport: &http.Transport{
			DialContext:           (&net.Dialer{Timeout: 15 * time.Second}).DialContext,
			TLSHandshakeTimeout:   15 * time.Second,
			ResponseHeaderTimeout: 60 * time.Second,
			ExpectContinueTimeout: 5 * time.Second,
		},
	}

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for i := range paths {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			p := presigns[i]
			if err := api.PutPresigned(ctx, httpClient, p.UploadURL, p.ContentType, paths[i]); err != nil {
				errs[i] = fmt.Errorf("%s: %w", filepath.Base(paths[i]), err)
				return
			}
			app.printer.Progressf("已上传 %s", filepath.Base(paths[i]))
			results[i] = uploadedAsset{
				File:      paths[i],
				ObjectKey: p.ObjectKey,
				URL:       p.PublicURL,
				Size:      sizes[paths[i]],
			}
		}(i)
	}
	wg.Wait()

	for _, err := range errs {
		if err != nil {
			return nil, clierr.New(clierr.KindTransport, "上传失败："+err.Error())
		}
	}
	return results, nil
}

// guessContentType derives a MIME type from the extension. An empty result is
// fine: the server falls back to its own detection.
func guessContentType(path string) string {
	ct := mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	return ct
}
