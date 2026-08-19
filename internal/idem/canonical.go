// Package idem implements the CLI-local idempotency machinery: canonical
// JSON digests, the pre-flight request log, and unknown-result recovery
// (cli-contract.md §15/§16/§18).
package idem

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"unicode"

	"github.com/zriyox/qianjue-cli/internal/clierr"
)

// MaxRequestBytes is the local request-file size cap (plan assumption A3).
const MaxRequestBytes = 1 << 20 // 1 MiB

// CanonicalJSON re-serializes raw as compact JSON with object keys sorted
// recursively (arrays keep their order) and numbers preserved verbatim via
// json.Number. This is the digest input of cli-contract.md §15 — a CLI-local
// consistency check, NOT the server-side typed-request fingerprint.
func CanonicalJSON(raw []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, clierr.Usage("请求不是合法 JSON: %v", err)
	}
	if dec.More() {
		return nil, clierr.Usage("请求文件包含多个 JSON Document")
	}
	var buf bytes.Buffer
	if err := writeCanonical(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeCanonical(buf *bytes.Buffer, v any) error {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeScalar(buf, k); err != nil {
				return err
			}
			buf.WriteByte(':')
			if err := writeCanonical(buf, x[k]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	case []any:
		buf.WriteByte('[')
		for i, item := range x {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeCanonical(buf, item); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
	case json.Number:
		buf.WriteString(string(x))
	default:
		return writeScalar(buf, x)
	}
	return nil
}

// writeScalar marshals strings/bools/null without HTML escaping so UTF-8
// text stays byte-identical.
func writeScalar(buf *bytes.Buffer, v any) error {
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return clierr.Usage("序列化 JSON 失败: %v", err)
	}
	// Encoder 会追加换行，去掉
	buf.Truncate(buf.Len() - 1)
	return nil
}

// Digest returns the SHA-256 hex of the canonical JSON.
func Digest(raw []byte) (string, error) {
	canonical, err := CanonicalJSON(raw)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

// forbiddenFieldNames mirrors the server-side snapshot guard exactly
// (IntegrationRequestFingerprintService.java:28-41).
var forbiddenFieldNames = map[string]bool{
	"authorization": true, "cookie": true, "password": true, "secret": true,
	"token": true, "apikey": true, "accesstoken": true, "refreshtoken": true,
	"devicecode": true, "clientsecret": true, "credential": true, "credentials": true,
}

func normalizeFieldName(name string) string {
	var b strings.Builder
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

// CheckForbiddenFields rejects requests whose (normalized) field names match
// the credential blocklist, before anything is persisted or sent.
func CheckForbiddenFields(raw []byte) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return clierr.Usage("请求不是合法 JSON: %v", err)
	}
	return walkForbidden(v)
}

func walkForbidden(v any) error {
	switch x := v.(type) {
	case map[string]any:
		for k, child := range x {
			if forbiddenFieldNames[normalizeFieldName(k)] {
				return clierr.Usage("请求包含敏感字段名 %q，拒绝发送（凭证不得进入请求快照）", k)
			}
			if err := walkForbidden(child); err != nil {
				return err
			}
		}
	case []any:
		for _, item := range x {
			if err := walkForbidden(item); err != nil {
				return err
			}
		}
	}
	return nil
}

// CheckRequestObject performs the type-agnostic pre-flight checks: size cap and
// JSON object shape. Used by domains whose typed request has no polymorphic
// "type" discriminator (e.g. video, whose kind is chosen by endpoint).
func CheckRequestObject(raw []byte) error {
	if len(raw) > MaxRequestBytes {
		return clierr.Usage("请求文件超过本地上限 %d 字节", MaxRequestBytes)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return clierr.Usage("请求不是合法 JSON: %v", err)
	}
	if dec.More() {
		return clierr.Usage("请求文件包含多个 JSON Document")
	}
	if _, ok := v.(map[string]any); !ok {
		return clierr.Usage("请求必须是 JSON Object")
	}
	return nil
}

// CheckRequestShape performs the local pre-flight checks of cli-contract.md
// §13: size cap, JSON object shape, and a non-empty type field.
func CheckRequestShape(raw []byte) error {
	if len(raw) > MaxRequestBytes {
		return clierr.Usage("请求文件超过本地上限 %d 字节", MaxRequestBytes)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return clierr.Usage("请求不是合法 JSON: %v", err)
	}
	if dec.More() {
		return clierr.Usage("请求文件包含多个 JSON Document")
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return clierr.Usage("请求必须是 JSON Object")
	}
	typeVal, ok := obj["type"].(string)
	if !ok || strings.TrimSpace(typeVal) == "" {
		return clierr.Usage("请求缺少非空的 type 字段")
	}
	return nil
}
