package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// PresignItem describes one file the caller intends to upload.
type PresignItem struct {
	FileName    string `json:"fileName"`
	ContentType string `json:"contentType,omitempty"`
	Size        int64  `json:"size"`
}

// PresignResult is one signed upload slot. ContentType is echoed back because
// it is part of the signature: the PUT must send exactly this value or object
// storage rejects the request.
type PresignResult struct {
	FileName    string `json:"fileName"`
	ObjectKey   string `json:"objectKey"`
	UploadURL   string `json:"uploadUrl"`
	PublicURL   string `json:"publicUrl"`
	ContentType string `json:"contentType"`
	ExpiresAt   string `json:"expiresAt"`
}

// PresignUploads calls POST /integration/assets/presign-uploads for a whole
// batch in one round trip. Results come back in request order.
func (c *Client) PresignUploads(ctx context.Context, items []PresignItem) ([]PresignResult, error) {
	body, err := json.Marshal(map[string]any{"items": items})
	if err != nil {
		return nil, err
	}
	res, err := c.do(ctx, http.MethodPost, "/integration/assets/presign-uploads", nil, body, true)
	if err != nil {
		return nil, err
	}
	var out []PresignResult
	if err := decodeData(res, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// PutPresigned uploads a local file to a presigned URL.
//
// This deliberately does NOT go through Client.do: the presigned URL carries its
// own signature in the query string and points at object storage, not at the
// qianjue API. Attaching our bearer token (or any extra signed header) would
// both leak the credential to a third-party host and risk breaking signature
// validation. Content-Type must match what was signed.
func PutPresigned(ctx context.Context, httpClient *http.Client, uploadURL, contentType, filePath string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL, f)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)
	// ContentLength must be explicit: a chunked PUT would not match the signature.
	req.ContentLength = info.Size()

	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Minute}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("对象存储拒绝上传（HTTP %d）：%s",
			resp.StatusCode, bytes.TrimSpace(snippet))
	}
	return nil
}
