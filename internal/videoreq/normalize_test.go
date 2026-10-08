package videoreq

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// decodeAny mirrors the normalizer's number handling so assertions compare the
// exact literals that were sent.
func decodeAny(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var doc map[string]any
	require.NoError(t, decoder.Decode(&doc), "body: %s", raw)
	return doc
}

func firstItem(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	doc := decodeAny(t, raw)
	items, ok := doc["items"].([]any)
	require.True(t, ok, "items missing: %v", doc)
	require.NotEmpty(t, items)
	item, ok := items[0].(map[string]any)
	require.True(t, ok)
	return item
}

func referenceImages(t *testing.T, item map[string]any) []any {
	t.Helper()
	config, ok := item["seedanceConfig"].(map[string]any)
	require.True(t, ok, "seedanceConfig missing: %v", item)
	return config["referenceImages"].([]any)
}

func TestNormalizeFillsPrimaryImageAndReferenceImages(t *testing.T) {
	raw := []byte(`{"sourceType":"VIDEO_TASK","modelCode":"SEEDANCE_2_0_MINI","items":[{"prompt":"转场","inputImages":[{"url":"https://x/1.png"},{"url":"https://x/2.png"},{"url":"https://x/3.png"}]}]}`)

	out, report, err := NormalizeInputImages(raw)
	require.NoError(t, err)
	require.True(t, report.Changed())
	assert.Equal(t, 1, report.ItemsWithReferenceImages)
	assert.Equal(t, 2, report.ReferenceImages)
	assert.Equal(t, 1, report.PrimaryImagesFilled)

	item := firstItem(t, out)
	assert.Equal(t, "https://x/1.png", item["inputImageUrl"])
	assert.NotContains(t, item, "inputImageOosKey")
	assert.Equal(t, []any{
		map[string]any{"url": "https://x/2.png"},
		map[string]any{"url": "https://x/3.png"},
	}, referenceImages(t, item))
}

func TestNormalizeCarriesOosKeysAndMetadata(t *testing.T) {
	raw := []byte(`{"modelCode":"SEEDANCE_2_5","items":[{"inputImages":[{"oosKey":"head/key.png"},{"url":"https://x/2.png","metadata":{"width":100}},{"url":"https://x/3.png","oosKey":"three.png"}]}]}`)

	out, report, err := NormalizeInputImages(raw)
	require.NoError(t, err)
	assert.Equal(t, 2, report.ReferenceImages)

	item := firstItem(t, out)
	assert.Equal(t, "head/key.png", item["inputImageOosKey"])
	assert.NotContains(t, item, "inputImageUrl")
	assert.Equal(t, []any{
		map[string]any{"url": "https://x/2.png", "metadata": map[string]any{"width": json.Number("100")}},
		map[string]any{"url": "https://x/3.png", "oosKey": "three.png"},
	}, referenceImages(t, item))
}

func TestNormalizeKeepsExplicitPrimaryImage(t *testing.T) {
	raw := []byte(`{"modelCode":"SEEDANCE_2_0","items":[{"inputImageUrl":"https://x/head.png","inputImageOosKey":"head.png","inputImages":[{"url":"https://x/1.png"},{"url":"https://x/2.png"}]}]}`)

	out, report, err := NormalizeInputImages(raw)
	require.NoError(t, err)
	assert.Equal(t, 0, report.PrimaryImagesFilled)
	assert.Equal(t, 1, report.ReferenceImages)

	item := firstItem(t, out)
	assert.Equal(t, "https://x/head.png", item["inputImageUrl"])
	assert.Equal(t, "head.png", item["inputImageOosKey"])
	assert.Equal(t, []any{map[string]any{"url": "https://x/2.png"}}, referenceImages(t, item))
}

func TestNormalizeKeepsExistingReferenceImages(t *testing.T) {
	raw := []byte(`{"modelCode":"SEEDANCE_2_0_MINI","items":[{"inputImageUrl":"https://x/1.png","inputImages":[{"url":"https://x/1.png"},{"url":"https://x/2.png"}],"seedanceConfig":{"referenceImages":[{"url":"https://y/hand.png"}]}}]}`)

	out, report, err := NormalizeInputImages(raw)
	require.NoError(t, err)
	assert.False(t, report.Changed())
	assert.Equal(t, raw, out, "an explicitly supplied referenceImages list must pass through untouched")
}

func TestNormalizeMapsEveryItem(t *testing.T) {
	raw := []byte(`{"modelCode":"SEEDANCE_2_0_FAST","items":[{"inputImages":[{"url":"https://x/a1.png"},{"url":"https://x/a2.png"}]},{"inputImages":[{"url":"https://x/b1.png"},{"url":"https://x/b2.png"},{"url":"https://x/b3.png"}]}]}`)

	out, report, err := NormalizeInputImages(raw)
	require.NoError(t, err)
	assert.Equal(t, 2, report.ItemsWithReferenceImages)
	assert.Equal(t, 3, report.ReferenceImages)
	assert.Equal(t, 2, report.PrimaryImagesFilled)

	doc := decodeAny(t, out)
	items := doc["items"].([]any)
	second := items[1].(map[string]any)
	assert.Equal(t, "https://x/b1.png", second["inputImageUrl"])
	assert.Len(t, referenceImages(t, second), 2)
}

func TestNormalizeLeavesSingleImageModelsAlone(t *testing.T) {
	// Web submits only inputImages[0] for these models, so the CLI must not
	// invent a reference field that the provider would ignore or reject.
	raw := []byte(`{"modelCode":"GROK_IMAGINE_1_5","items":[{"inputImages":[{"url":"https://x/1.png"},{"url":"https://x/2.png"}]}]}`)

	out, report, err := NormalizeInputImages(raw)
	require.NoError(t, err)
	assert.False(t, report.Changed())
	assert.Equal(t, raw, out)
	assert.Empty(t, report.ReferenceField)
}

func TestNormalizeLeavesSingleInputImageAlone(t *testing.T) {
	raw := []byte(`{"modelCode":"SEEDANCE_2_0_MINI","items":[{"inputImageUrl":"https://x/1.png","inputImages":[{"url":"https://x/1.png"}]}]}`)

	out, report, err := NormalizeInputImages(raw)
	require.NoError(t, err)
	assert.False(t, report.Changed())
	assert.Equal(t, raw, out)
}

func TestNormalizeDropsUnusableEntriesBeforeMapping(t *testing.T) {
	raw := []byte(`{"modelCode":"SEEDANCE_2_0_MINI","items":[{"inputImages":[{"url":""},{"url":"https://x/1.png"},{"url":"https://x/2.png"}]}]}`)

	out, _, err := NormalizeInputImages(raw)
	require.NoError(t, err)

	item := firstItem(t, out)
	assert.Equal(t, "https://x/1.png", item["inputImageUrl"])
	assert.Equal(t, []any{map[string]any{"url": "https://x/2.png"}}, referenceImages(t, item))
}

func TestNormalizeAcceptsSEEDANCEAliasCaseInsensitively(t *testing.T) {
	raw := []byte(`{"modelCode":" seedance ","items":[{"inputImages":[{"url":"https://x/1.png"},{"url":"https://x/2.png"}]}]}`)

	out, report, err := NormalizeInputImages(raw)
	require.NoError(t, err)
	assert.True(t, report.Changed())
	assert.Len(t, referenceImages(t, firstItem(t, out)), 1)
}

// A rewrite must not round int64 literals through float64 nor HTML-escape URLs.
func TestNormalizePreservesNumberLiteralsAndURLBytes(t *testing.T) {
	raw := []byte(`{"modelCode":"SEEDANCE_2_5","items":[{"durationSeconds":9007199254740993,"inputImages":[{"url":"https://x/1.png"},{"url":"https://x/a&b<c.png"}]}]}`)

	out, report, err := NormalizeInputImages(raw)
	require.NoError(t, err)
	require.True(t, report.Changed())

	assert.Contains(t, string(out), "9007199254740993")
	assert.Contains(t, string(out), "https://x/a&b<c.png")
	assert.NotContains(t, string(out), `\u003c`)
	assert.NotContains(t, string(out), "9007199254740992")
}

func TestNormalizeRejectsNonObjectSeedanceConfig(t *testing.T) {
	raw := []byte(`{"modelCode":"SEEDANCE_2_0_MINI","items":[{"inputImages":[{"url":"https://x/1.png"},{"url":"https://x/2.png"}],"seedanceConfig":"480p"}]}`)

	_, _, err := NormalizeInputImages(raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "seedanceConfig")
}

func TestNormalizeRejectsMalformedJSON(t *testing.T) {
	_, _, err := NormalizeInputImages([]byte(`{"modelCode":`))
	require.Error(t, err)
}

// --- kling-v3-omni -----------------------------------------------------------
// The provider payload for kling-v3-omni is inputImageUrl plus
// omniConfig.imageList (KlingOmniVideoModeAdapter.addImageList), so the extra
// materials must land in omniConfig.imageList instead of seedanceConfig.

func omniImageList(t *testing.T, item map[string]any) []any {
	t.Helper()
	config, ok := item["omniConfig"].(map[string]any)
	require.True(t, ok, "omniConfig missing: %v", item)
	return config["imageList"].([]any)
}

func TestNormalizeShapesKlingOmniInputImages(t *testing.T) {
	raw := []byte(`{"modelCode":"kling-v3-omni","items":[{"prompt":"镜头","inputImages":[{"oosKey":"head.png","url":"https://x/1.png"},{"url":"https://x/2.png"},{"url":"https://x/3.png"}]}]}`)

	out, report, err := NormalizeInputImages(raw)
	require.NoError(t, err)
	require.True(t, report.Changed())
	assert.Equal(t, "omniConfig.imageList", report.ReferenceField)
	assert.Equal(t, 1, report.ItemsWithReferenceImages)
	assert.Equal(t, 2, report.ReferenceImages)
	assert.Equal(t, 1, report.PrimaryImagesFilled)

	item := firstItem(t, out)
	assert.Equal(t, "https://x/1.png", item["inputImageUrl"])
	assert.Equal(t, "head.png", item["inputImageOosKey"])
	assert.NotContains(t, item, "seedanceConfig")
	assert.Equal(t, []any{
		map[string]any{"imageUrl": "https://x/2.png"},
		map[string]any{"imageUrl": "https://x/3.png"},
	}, omniImageList(t, item))
}

func TestNormalizeKlingOmniCarriesOosKeyOnlyEntries(t *testing.T) {
	// The backend resolves an imageOosKey-only entry to a URL before submitting
	// (KlingVideoProviderClient.resolveOmniConfig), and the web client sends
	// imageOosKey without a URL, so the key alone must survive.
	raw := []byte(`{"modelCode":"KLING-V3-OMNI","items":[{"inputImages":[{"url":"https://x/1.png"},{"imageOosKey":"ignored.png"},{"oosKey":"ref.png"}]}]}`)

	out, report, err := NormalizeInputImages(raw)
	require.NoError(t, err)
	require.True(t, report.Changed())
	assert.Equal(t, 1, report.ReferenceImages)

	assert.Equal(t, []any{map[string]any{"imageOosKey": "ref.png"}}, omniImageList(t, firstItem(t, out)))
}

func TestNormalizeKlingOmniKeepsExistingImageList(t *testing.T) {
	raw := []byte(`{"modelCode":"kling-v3-omni","items":[{"inputImageUrl":"https://x/1.png","inputImages":[{"url":"https://x/1.png"},{"url":"https://x/2.png"}],"omniConfig":{"mode":"pro","imageList":[{"imageUrl":"https://y/hand.png"}]}}]}`)

	out, report, err := NormalizeInputImages(raw)
	require.NoError(t, err)
	assert.False(t, report.Changed())
	assert.Equal(t, raw, out, "an explicitly supplied imageList must pass through untouched")
}

func TestNormalizeKlingOmniKeepsExplicitPrimaryImage(t *testing.T) {
	raw := []byte(`{"modelCode":"kling-v3-omni","items":[{"inputImageUrl":"https://x/head.png","inputImages":[{"url":"https://x/1.png"},{"url":"https://x/2.png"}]}]}`)

	out, report, err := NormalizeInputImages(raw)
	require.NoError(t, err)
	assert.Equal(t, 0, report.PrimaryImagesFilled)
	assert.Equal(t, 1, report.ReferenceImages)

	item := firstItem(t, out)
	assert.Equal(t, "https://x/head.png", item["inputImageUrl"])
	assert.Equal(t, []any{map[string]any{"imageUrl": "https://x/2.png"}}, omniImageList(t, item))
}

func TestNormalizeKlingOmniLeavesSingleImageAlone(t *testing.T) {
	raw := []byte(`{"modelCode":"kling-v3-omni","items":[{"inputImageUrl":"https://x/1.png","inputImages":[{"url":"https://x/1.png"}]}]}`)

	out, report, err := NormalizeInputImages(raw)
	require.NoError(t, err)
	assert.False(t, report.Changed())
	assert.Equal(t, raw, out)
}

func TestNormalizeKlingOmniDropsUnusableEntries(t *testing.T) {
	raw := []byte(`{"modelCode":"kling-v3-omni","items":[{"inputImages":[{"url":""},{"oosKey":"  "},{"url":"https://x/1.png"},{"url":"https://x/2.png"}]}]}`)

	out, report, err := NormalizeInputImages(raw)
	require.NoError(t, err)
	require.True(t, report.Changed())

	item := firstItem(t, out)
	assert.Equal(t, "https://x/1.png", item["inputImageUrl"])
	assert.Equal(t, []any{map[string]any{"imageUrl": "https://x/2.png"}}, omniImageList(t, item))
}

func TestNormalizeKlingOmniMapsEveryItem(t *testing.T) {
	raw := []byte(`{"modelCode":"kling-v3-omni","items":[{"inputImages":[{"url":"https://x/a1.png"},{"url":"https://x/a2.png"}]},{"inputImages":[{"url":"https://x/b1.png"},{"url":"https://x/b2.png"},{"url":"https://x/b3.png"}]}]}`)

	out, report, err := NormalizeInputImages(raw)
	require.NoError(t, err)
	assert.Equal(t, 2, report.ItemsWithReferenceImages)
	assert.Equal(t, 3, report.ReferenceImages)
	assert.Equal(t, 2, report.PrimaryImagesFilled)

	items := decodeAny(t, out)["items"].([]any)
	second := items[1].(map[string]any)
	assert.Equal(t, "https://x/b1.png", second["inputImageUrl"])
	assert.Len(t, omniImageList(t, second), 2)
}

func TestNormalizeKlingOmniOmitsMetadataFromImageList(t *testing.T) {
	// The web client sends only imageOosKey/imageUrl in omniConfig.imageList
	// (inputImages carries the metadata), so the shape must match exactly.
	raw := []byte(`{"modelCode":"kling-v3-omni","items":[{"inputImages":[{"url":"https://x/1.png"},{"url":"https://x/2.png","metadata":{"width":100}}]}]}`)

	out, report, err := NormalizeInputImages(raw)
	require.NoError(t, err)
	require.True(t, report.Changed())
	assert.Equal(t, []any{map[string]any{"imageUrl": "https://x/2.png"}}, omniImageList(t, firstItem(t, out)))
}

func TestNormalizeKlingOmniPreservesNumberLiteralsAndURLBytes(t *testing.T) {
	raw := []byte(`{"modelCode":"kling-v3-omni","items":[{"durationSeconds":9007199254740993,"inputImages":[{"url":"https://x/1.png"},{"url":"https://x/a&b<c.png"}]}]}`)

	out, report, err := NormalizeInputImages(raw)
	require.NoError(t, err)
	require.True(t, report.Changed())

	assert.Contains(t, string(out), "9007199254740993")
	assert.Contains(t, string(out), "https://x/a&b<c.png")
	assert.NotContains(t, string(out), `\u003c`)
}

func TestNormalizeKlingOmniRejectsNonObjectOmniConfig(t *testing.T) {
	raw := []byte(`{"modelCode":"kling-v3-omni","items":[{"inputImages":[{"url":"https://x/1.png"},{"url":"https://x/2.png"}],"omniConfig":"pro"}]}`)

	_, _, err := NormalizeInputImages(raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "omniConfig")
}

func TestNormalizeReportsSeedanceReferenceField(t *testing.T) {
	raw := []byte(`{"modelCode":"SEEDANCE_2_0_MINI","items":[{"inputImages":[{"url":"https://x/1.png"},{"url":"https://x/2.png"}]}]}`)

	_, report, err := NormalizeInputImages(raw)
	require.NoError(t, err)
	assert.Equal(t, "seedanceConfig.referenceImages", report.ReferenceField)
}
